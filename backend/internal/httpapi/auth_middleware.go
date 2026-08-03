package httpapi

import (
	"context"
	"net/http"
	"strings"

	"dynamic-pdb/backend/internal/auth"
	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
)

type contextKey string

const userContextKey contextKey = "auth.user"

const bearerAuthScopesKey = "bearerAuth.Scopes"

func WithUser(ctx context.Context, user *models.User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

func UserFromContext(ctx context.Context) (*models.User, bool) {
	user, ok := ctx.Value(userContextKey).(*models.User)
	return user, ok
}

// AuthMiddleware enforces bearer auth on routes whose OpenAPI definition
// declares `security: [bearerAuth: []]`. On public routes it is best-effort:
// a valid token identifies the caller, while a missing or invalid token is
// treated as an anonymous request.
func AuthMiddleware(jwt *auth.JWT, database *db.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			required := r.Context().Value(bearerAuthScopesKey) != nil

			header := r.Header.Get("Authorization")
			const prefix = "Bearer "
			if !strings.HasPrefix(header, prefix) {
				if required {
					writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing bearer token")
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			userID, err := jwt.Parse(strings.TrimPrefix(header, prefix))
			if err != nil {
				if required {
					writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid bearer token")
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			user, err := database.Users.Get(r.Context(), userID)
			if err != nil {
				if required {
					writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user not found")
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
		})
	}
}
