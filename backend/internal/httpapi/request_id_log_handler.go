package httpapi

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/go-chi/chi/v5/middleware"
)

const requestIDLogKey = "request_id"

// RequestIDLogHandler adds the request id that middleware.RequestID stored in the context to
// every record, so a handler's error line can be matched to the access log line of the same
// request. Only the *Context logging methods pass the request context through.
type RequestIDLogHandler struct {
	next slog.Handler
}

func NewRequestIDLogHandler(next slog.Handler) *RequestIDLogHandler {
	return &RequestIDLogHandler{next: next}
}

func (h *RequestIDLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *RequestIDLogHandler) Handle(ctx context.Context, record slog.Record) error {
	requestID := middleware.GetReqID(ctx)
	if requestID != "" && !recordHasAttr(record, requestIDLogKey) {
		record.AddAttrs(slog.String(requestIDLogKey, requestID))
	}
	if err := h.next.Handle(ctx, record); err != nil {
		return fmt.Errorf("handle log record: %w", err)
	}
	return nil
}

func (h *RequestIDLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return NewRequestIDLogHandler(h.next.WithAttrs(attrs))
}

func (h *RequestIDLogHandler) WithGroup(name string) slog.Handler {
	return NewRequestIDLogHandler(h.next.WithGroup(name))
}

func recordHasAttr(record slog.Record, key string) bool {
	found := false
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == key {
			found = true
			return false
		}
		return true
	})
	return found
}
