package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// AccessLogMiddleware writes one structured log line per request and echoes the request id
// in the response so clients can quote it when reporting a failure. Server errors are logged
// at error level. It must run after middleware.RequestID; when it runs after
// ReadRequestTimeoutMiddleware, timed-out and abandoned requests carry a context_error.
func AccessLogMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := middleware.GetReqID(r.Context())
			if requestID != "" {
				w.Header().Set(middleware.RequestIDHeader, requestID)
			}
			started := time.Now()
			recorder := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(recorder, r)

			status := recorder.Status()
			if status == 0 {
				// net/http sends 200 when a handler returns without writing anything.
				status = http.StatusOK
			}
			level := slog.LevelInfo
			if status >= http.StatusInternalServerError {
				level = slog.LevelError
			}
			attrs := []slog.Attr{
				slog.String("request_id", requestID),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("query", r.URL.RawQuery),
				slog.Int("status", status),
				slog.Int("bytes", recorder.BytesWritten()),
				slog.Float64("duration_ms", float64(time.Since(started).Microseconds())/1000),
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("user_agent", r.UserAgent()),
			}
			if err := r.Context().Err(); err != nil {
				attrs = append(attrs, slog.String("context_error", err.Error()))
			}
			logger.LogAttrs(r.Context(), level, "http request", attrs...)
		})
	}
}
