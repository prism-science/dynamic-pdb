package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/auth"
	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/integrations/github"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/services/analytics"
	"dynamic-pdb/backend/internal/services/cdn"
	"dynamic-pdb/backend/internal/types"
	"dynamic-pdb/backend/internal/validation"
	"dynamic-pdb/lib/cif"
	"dynamic-pdb/lib/rcsb"
)

type Server struct {
	githubClient github.Client
	fileCDN      cdn.Service
	authConfig   auth.Config
	jwt          *auth.JWT
	authorizer   auth.Authorizer
	database     *db.DB
	downloads    analytics.Tracker
	// publicBaseURL is the API's external base (for example https://dynamicpdb.com/api),
	// used to build absolute links.next URLs.
	publicBaseURL string
	// validationMode decides whether create requests that break schema rules are
	// rejected or only logged.
	validationMode validation.Mode
	// archive serves GET /v1/archive/pdb/{pdb_id}; see WithArchiveLookup.
	archive ArchiveLookup
	// extractionLimits bounds POST /v1/artifacts/{id}/extract.
	extractionLimits *extractionLimits
	// extractionTimeout is the longest one extraction may spend parsing; see
	// WithExtractionTimeout.
	extractionTimeout time.Duration
	// rcsbFiles names RCSB's coordinate file of an archive model, which decides whether an
	// activated model gets a sync job. Its base URL comes from DYNAMIC_PDB_RCSB_FILES_URL, as for
	// the data sync.
	rcsbFiles RCSBFiles
}

func NewServer(
	githubClient github.Client,
	fileCDN cdn.Service,
	authConfig auth.Config,
	jwt *auth.JWT,
	database *db.DB,
	downloads analytics.Tracker,
	publicBaseURL string,
	validationMode validation.Mode,
) *Server {
	return &Server{
		githubClient:   githubClient,
		fileCDN:        fileCDN,
		authConfig:     authConfig,
		jwt:            jwt,
		authorizer:     auth.NewAuthorizer(database),
		database:       database,
		downloads:      downloads,
		publicBaseURL:  publicBaseURL,
		validationMode: validationMode,
		extractionLimits: newExtractionLimits(
			defaultExtractionMaxInFlight, defaultExtractionUserRequests, defaultExtractionUserWindow,
		),
		extractionTimeout: cif.TimeBudget,
		rcsbFiles:         rcsb.NewClient(rcsb.WithCacheEntries(0)),
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
		slog.ErrorContext(r.Context(), "github oauth is not configured")
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "github oauth is not configured")
		return
	case errors.Is(err, github.ErrIncorrectClientCredentials):
		slog.ErrorContext(r.Context(), "github oauth client credentials are invalid")
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "github oauth client credentials are invalid")
		return
	case err != nil:
		slog.ErrorContext(r.Context(), "github code exchange failed", "err", err)
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
		slog.ErrorContext(r.Context(), "github get user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to verify github user")
		return
	}

	orgs, err := s.githubClient.ListOrgs(r.Context(), accessToken)
	if err != nil {
		slog.ErrorContext(r.Context(), "github list orgs failed", "err", err)
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
		slog.ErrorContext(r.Context(), "persist user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to persist user")
		return
	}

	effectivePermissions, err := s.database.Permissions.List(r.Context(), db.PermissionFilters{
		UserID: &persisted.ID,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "load user permissions failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load user permissions")
		return
	}
	permissionKeys := make([]string, 0, len(effectivePermissions))
	for _, permission := range effectivePermissions {
		permissionKeys = append(permissionKeys, string(permission.Key))
	}

	token, expiresAt, err := s.jwt.Issue(persisted.ID)
	if err != nil {
		slog.ErrorContext(r.Context(), "issue jwt failed", "err", err)
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

// readinessPingTimeout stays below the readiness probe's timeoutSeconds in the Helm chart,
// so a slow database is reported by this handler instead of by a kubelet timeout.
const readinessPingTimeout = time.Second

func (s *Server) Readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readinessPingTimeout)
	defer cancel()
	if err := s.database.Ping(ctx); err != nil {
		slog.WarnContext(r.Context(), "readyz: db ping failed", "err", err)
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

// writeFieldErrors writes every schema violation of a request as one 422 response.
func writeFieldErrors(w http.ResponseWriter, fieldErrors []validation.FieldError) {
	status := strconv.Itoa(http.StatusUnprocessableEntity)
	errorObjects := make([]ErrorObject, 0, len(fieldErrors))
	for _, fieldError := range fieldErrors {
		errorObjects = append(errorObjects, ErrorObject{
			Status: status,
			Code:   ptr(string(fieldError.Code)),
			Detail: ptr(fieldError.Detail),
			Source: &ErrorSource{Pointer: string(fieldError.Pointer)},
		})
	}
	writeJSON(w, http.StatusUnprocessableEntity, Error{Errors: errorObjects})
}

// RouteErrorHandler writes parameter binding errors from the generated router.
func RouteErrorHandler(w http.ResponseWriter, _ *http.Request, err error) {
	writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", JSONAPIMediaType)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		//nolint:sloglint // No request here; the access log line for this request carries its id and context_error.
		slog.Error("write json response failed", "err", err)
	}
}
