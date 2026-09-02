package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/auth"
	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/integrations/github"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/services/cdn"
	"dynamic-pdb/backend/internal/types"
)

type Server struct {
	githubClient github.Client
	fileCDN      cdn.Service
	authConfig   auth.Config
	jwt          *auth.JWT
	authorizer   auth.Authorizer
	database     *db.DB
}

func GlobalRateLimitMiddleware(env string) func(http.Handler) http.Handler {
	resolveClientIP := middleware.ClientIPFromRemoteAddr
	if env == "production" {
		resolveClientIP = middleware.ClientIPFromXFFTrustedProxies(2)
	}

	limitRequests := httprate.LimitBy(100, 10*time.Second, func(r *http.Request) (string, error) {
		return httprate.CanonicalizeIP(middleware.GetClientIP(r.Context())), nil
	})
	return func(next http.Handler) http.Handler {
		return resolveClientIP(limitRequests(next))
	}
}

func NewServer(
	githubClient github.Client,
	fileCDN cdn.Service,
	authConfig auth.Config,
	jwt *auth.JWT,
	database *db.DB,
) *Server {
	return &Server{
		githubClient: githubClient,
		fileCDN:      fileCDN,
		authConfig:   authConfig,
		jwt:          jwt,
		authorizer:   auth.NewAuthorizer(database),
		database:     database,
	}
}

func (s *Server) ExchangeGithubToken(w http.ResponseWriter, r *http.Request) {
	var req GitHubExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}

	s.exchangeGithubAccessToken(w, r, req.AccessToken)
}

func (s *Server) ExchangeGithubCode(w http.ResponseWriter, r *http.Request) {
	var req GitHubCodeExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}

	accessToken, err := s.githubClient.ExchangeCode(r.Context(), req.Code, req.RedirectUri)
	switch {
	case errors.Is(err, github.ErrInvalidCode):
		writeError(w, http.StatusUnauthorized, "INVALID_GITHUB_CODE", "github authorization code is invalid or expired")
		return
	case errors.Is(err, github.ErrAccessDenied):
		writeError(w, http.StatusUnauthorized, "GITHUB_ACCESS_DENIED", "github authorization was denied")
		return
	case errors.Is(err, github.ErrRedirectURIMismatch):
		writeError(w, http.StatusBadRequest, "INVALID_REDIRECT_URI", "github redirect uri does not match the configured callback")
		return
	case errors.Is(err, github.ErrOAuthNotConfigured):
		slog.Error("github oauth is not configured")
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "github oauth is not configured")
		return
	case errors.Is(err, github.ErrIncorrectClientCredentials):
		slog.Error("github oauth client credentials are invalid")
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "github oauth client credentials are invalid")
		return
	case err != nil:
		slog.Error("github code exchange failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to exchange github authorization code")
		return
	}

	s.exchangeGithubAccessToken(w, r, accessToken)
}

func (s *Server) exchangeGithubAccessToken(w http.ResponseWriter, r *http.Request, accessToken string) {
	githubUser, err := s.githubClient.GetUser(r.Context(), accessToken)
	if errors.Is(err, github.ErrInvalidToken) {
		writeError(w, http.StatusUnauthorized, "INVALID_GITHUB_TOKEN", "github access token is invalid")
		return
	}
	if err != nil {
		slog.Error("github get user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to verify github user")
		return
	}

	orgs, err := s.githubClient.ListOrgs(r.Context(), accessToken)
	if err != nil {
		slog.Error("github list orgs failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list github orgs")
		return
	}
	if !s.isInAllowedOrg(orgs) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "user is not a member of any allowed organization")
		return
	}

	displayName := githubDisplayName(githubUser)
	now := time.Now().UTC()
	persisted, err := s.database.Users.Create(r.Context(), models.User{
		ID: uuid.New(),
		ExternalRef: types.ExternalRef{
			Source: "github",
			Value:  strconv.FormatInt(githubUser.ID, 10),
		},
		Email:       githubUser.Email,
		DisplayName: displayName,
		AvatarURL:   githubUser.AvatarURL,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		slog.Error("persist user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to persist user")
		return
	}

	effectivePermissions, err := s.database.Permissions.List(r.Context(), db.PermissionFilters{
		UserID: &persisted.ID,
	})
	if err != nil {
		slog.Error("load user permissions failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load user permissions")
		return
	}
	permissionKeys := make([]string, 0, len(effectivePermissions))
	for _, permission := range effectivePermissions {
		permissionKeys = append(permissionKeys, string(permission.Key))
	}

	token, expiresAt, err := s.jwt.Issue(persisted.ID)
	if err != nil {
		slog.Error("issue jwt failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to issue token")
		return
	}

	writeJSON(w, http.StatusOK, TokenDocument{
		Data: TokenData{
			Type: jsonAPITypeAuthTokens,
			Attributes: TokenAttributes{
				TokenType:   Bearer,
				AccessToken: token,
				ExpiresAt:   expiresAt,
				Name:        displayName,
				Email:       githubUser.Email,
				Login:       githubUser.Login,
				Permissions: permissionKeys,
			},
		},
	})
}

func githubDisplayName(githubUser github.User) string {
	name := strings.TrimSpace(githubUser.Name)
	if name != "" {
		return name
	}
	return githubUser.Login
}

func (s *Server) isInAllowedOrg(orgs []github.Organization) bool {
	for _, o := range orgs {
		for _, allowed := range s.authConfig.AllowedOrgs {
			if o.Login == allowed {
				return true
			}
		}
	}
	return false
}

func (s *Server) Livez(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, ProbeDocument{Meta: ProbeMeta{Status: "ok"}})
}

func (s *Server) Readyz(w http.ResponseWriter, r *http.Request) {
	if err := s.database.Ping(r.Context()); err != nil {
		slog.Warn("readyz: db ping failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, ProbeDocument{Meta: ProbeMeta{Status: "not ready"}})
		return
	}
	writeJSON(w, http.StatusOK, ProbeDocument{Meta: ProbeMeta{Status: "ok"}})
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, Error{
		Errors: []ErrorObject{
			{
				Status: strconv.Itoa(status),
				Code:   &code,
				Detail: &message,
			},
		},
	})
}

// RouteErrorHandler writes parameter binding errors from the generated router.
func RouteErrorHandler(w http.ResponseWriter, _ *http.Request, err error) {
	writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", JSONAPIMediaType)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("write json response failed", "err", err)
	}
}
