package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/go-chi/chi/v5/middleware"
)

// RecoverMiddleware turns a handler panic into a logged JSON:API 500 response instead of a
// dropped connection. http.ErrAbortHandler is re-raised because net/http uses it on purpose.
func RecoverMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(recovered)
				}
				logger.ErrorContext(r.Context(), "http handler panicked",
					"request_id", middleware.GetReqID(r.Context()),
					"method", r.Method,
					"path", r.URL.Path,
					"panic", fmt.Sprint(recovered),
					"stack", string(debug.Stack()),
				)
				writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
			}()
			next.ServeHTTP(w, r)
		})
	}
}
