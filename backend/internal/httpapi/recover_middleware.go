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
// When the handler already sent a status or body, a 500 can no longer replace it, so the
// panic is logged and the connection is dropped with http.ErrAbortHandler instead; otherwise
// the client would receive a 200 with the error JSON appended to a partial body.
func RecoverMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recorder := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(recovered)
				}
				// Status is set by the first WriteHeader or Write, so a non-zero status means
				// the response has started.
				responseStarted := recorder.Status() != 0
				logger.ErrorContext(r.Context(), "http handler panicked",
					"request_id", middleware.GetReqID(r.Context()),
					"method", r.Method,
					"path", r.URL.Path,
					"response_started", responseStarted,
					"panic", fmt.Sprint(recovered),
					"stack", string(debug.Stack()),
				)
				if responseStarted {
					panic(http.ErrAbortHandler)
				}
				writeError(recorder, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
			}()
			next.ServeHTTP(recorder, r)
		})
	}
}
