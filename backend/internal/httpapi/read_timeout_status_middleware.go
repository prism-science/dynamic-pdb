package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
)

const (
	// readTimeoutErrorCode is the JSON:API error code of a read that ran past its deadline.
	readTimeoutErrorCode = "TIMEOUT"
	// readTimeoutRetryAfterSeconds is the Retry-After of a read that ran past its deadline.
	readTimeoutRetryAfterSeconds = 10
)

// ReadTimeoutStatusMiddleware answers 504 instead of 500 when a GET or HEAD request fails
// after its deadline has passed. Handlers report every database error as a 500, and a query
// cut short by ReadRequestTimeoutMiddleware is such an error, so without this a client could
// not tell a slow read from a server bug.
//
// Only a 500 written after the deadline is replaced; every other status, and any response
// written before the deadline, passes through unchanged. Writes are never changed: they
// have no deadline. See RequestLifecycleMiddlewares for where it must sit in the chain.
func ReadTimeoutStatusMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				next.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(&readTimeoutStatusWriter{ResponseWriter: w, ctx: r.Context()}, r)
		})
	}
}

// readTimeoutStatusWriter swaps a handler's 500 for a 504 when ctx's deadline has passed.
type readTimeoutStatusWriter struct {
	http.ResponseWriter
	ctx context.Context
	// wroteHeader is set by the first WriteHeader or Write call.
	wroteHeader bool
	// replacedWithTimeout means the 504 is already sent, so the handler's 500 body is dropped.
	replacedWithTimeout bool
}

func (w *readTimeoutStatusWriter) WriteHeader(status int) {
	if w.wroteHeader {
		// A second call is a handler bug; pass it on so net/http logs it as usual.
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.wroteHeader = true
	if status == http.StatusInternalServerError && errors.Is(w.ctx.Err(), context.DeadlineExceeded) {
		w.replacedWithTimeout = true
		w.ResponseWriter.Header().Set("Retry-After", strconv.Itoa(readTimeoutRetryAfterSeconds))
		writeError(w.ResponseWriter, http.StatusGatewayTimeout, readTimeoutErrorCode,
			"the request did not finish within the server's time limit; try again later")
		return
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *readTimeoutStatusWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.replacedWithTimeout {
		// Report the handler's bytes as written so it does not log a failed write.
		return len(body), nil
	}
	return w.ResponseWriter.Write(body)
}

// Unwrap lets http.ResponseController reach the underlying writer's optional interfaces.
func (w *readTimeoutStatusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
