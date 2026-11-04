package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// RequestLifecycleMiddlewares returns the middleware that names, bounds, logs and recovers
// every request. Register it first, in the order returned; each position matters:
//   - middleware.RequestID first, because the access log and later log lines quote the id.
//   - ReadRequestTimeoutMiddleware outside AccessLogMiddleware, so the log line can report the
//     deadline's context_error.
//   - RecoverMiddleware inside AccessLogMiddleware, so a recovered panic is logged as its 500.
//   - ReadTimeoutStatusMiddleware inside RecoverMiddleware, so a panic stays a 500, and inside
//     AccessLogMiddleware, so the log line shows the 504 the client received.
func RequestLifecycleMiddlewares(logger *slog.Logger, readTimeout time.Duration) []func(http.Handler) http.Handler {
	return []func(http.Handler) http.Handler{
		middleware.RequestID,
		ReadRequestTimeoutMiddleware(readTimeout),
		AccessLogMiddleware(logger),
		RecoverMiddleware(logger),
		ReadTimeoutStatusMiddleware(),
	}
}
