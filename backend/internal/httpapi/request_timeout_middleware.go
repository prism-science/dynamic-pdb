package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// CheckReadRequestTimeout returns an error unless readTimeout is shorter than the database
// statement timeout. The request deadline must fire first: then a single slow query on a read
// is cancelled by the deadline and answered with a 504 by ReadTimeoutStatusMiddleware. If the
// statement timeout fired first, the deadline would not have passed yet and the client would
// get a 500. A zero statementTimeout means PostgreSQL has none, so any readTimeout is fine.
func CheckReadRequestTimeout(readTimeout, statementTimeout time.Duration) error {
	if statementTimeout > 0 && readTimeout >= statementTimeout {
		return fmt.Errorf("read request timeout %s must be shorter than the database statement_timeout %s",
			readTimeout, statementTimeout)
	}
	return nil
}

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
