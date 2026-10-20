package httpapi

import (
	"context"
	"net/http"
	"time"
)

// ReadRequestTimeoutMiddleware gives GET and HEAD requests a deadline, so slow reads stop and
// release their database connections. Writes are exempt because completing a large multipart
// upload can legitimately take minutes; the http.Server write timeout still bounds them.
func ReadRequestTimeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	if timeout <= 0 {
		panic("read request timeout must be positive")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				next.ServeHTTP(w, r)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
