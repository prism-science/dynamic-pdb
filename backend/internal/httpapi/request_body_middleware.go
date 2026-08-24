package httpapi

import (
	"bytes"
	"errors"
	"io"
	"net/http"
)

const MaxRequestBodyBytes int64 = 1 << 20

func RequestBodyLimitMiddleware(maxBytes int64) func(http.Handler) http.Handler {
	if maxBytes <= 0 {
		panic("request body limit must be positive")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body == nil || r.Body == http.NoBody {
				next.ServeHTTP(w, r)
				return
			}
			if r.ContentLength > maxBytes {
				writeRequestBodyTooLarge(w)
				return
			}

			limitedBody := http.MaxBytesReader(w, r.Body, maxBytes)
			body, readErr := io.ReadAll(limitedBody)
			closeErr := limitedBody.Close()

			var maxBytesError *http.MaxBytesError
			switch {
			case errors.As(readErr, &maxBytesError):
				writeRequestBodyTooLarge(w)
				return
			case readErr != nil:
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
				return
			case closeErr != nil:
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
				return
			}

			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
			next.ServeHTTP(w, r)
		})
	}
}

func writeRequestBodyTooLarge(w http.ResponseWriter) {
	writeError(w, http.StatusRequestEntityTooLarge, "REQUEST_BODY_TOO_LARGE", "request body exceeds 1 MiB limit")
}
