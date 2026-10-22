// Package validation checks write requests against Dynamic PDB Schema v0.2.0
// (docs/schema/dynamic-pdb-schema-v0.2.0.md) before anything is written to the database.
package validation

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

type Code string

const (
	CodeRequired         Code = "REQUIRED"
	CodeNotInSchema      Code = "NOT_IN_SCHEMA"
	CodeOutOfRange       Code = "OUT_OF_RANGE"
	CodeInvalidFormat    Code = "INVALID_FORMAT"
	CodeDuplicate        Code = "DUPLICATE"
	CodeReservedKey      Code = "RESERVED_KEY"
	CodeUnknownReference Code = "UNKNOWN_REFERENCE"
)

// FieldError is one schema violation. Pointer is a JSON Pointer (RFC 6901) into the request body.
type FieldError struct {
	Pointer string
	Code    Code
	Detail  string
}

// Error carries every schema violation found in one request.
type Error struct {
	FieldErrors []FieldError
}

func (e *Error) Error() string {
	details := make([]string, 0, len(e.FieldErrors))
	for _, fieldError := range e.FieldErrors {
		details = append(details, fieldError.Pointer+": "+fieldError.Detail)
	}
	return "schema validation failed: " + strings.Join(details, "; ")
}

// Mode selects what happens to a request that violates the schema.
type Mode string

const (
	// ModeEnforce rejects the write.
	ModeEnforce Mode = "enforce"
	// ModeLogOnly logs every violation and lets the write through, for rolling the schema out.
	ModeLogOnly Mode = "log_only"
)

func (mode Mode) Validate() error {
	switch mode {
	case ModeEnforce, ModeLogOnly:
		return nil
	default:
		return fmt.Errorf("unknown validation mode: %s", mode)
	}
}

// Apply returns an *Error carrying fieldErrors in enforce mode. In log-only mode it logs each
// violation with logAttrs and returns nil.
func (mode Mode) Apply(ctx context.Context, logger *slog.Logger, fieldErrors []FieldError, logAttrs ...any) error {
	if len(fieldErrors) == 0 {
		return nil
	}
	switch mode {
	case ModeEnforce:
		return &Error{FieldErrors: fieldErrors}
	case ModeLogOnly:
		for _, fieldError := range fieldErrors {
			attrs := append([]any{
				"pointer", fieldError.Pointer,
				"code", string(fieldError.Code),
				"detail", fieldError.Detail,
			}, logAttrs...)
			logger.WarnContext(ctx, "schema violation allowed by log_only validation mode", attrs...)
		}
		return nil
	default:
		panic(fmt.Sprintf("unknown validation mode: %s", mode))
	}
}

// Field is an optional request field that distinguishes an absent field from an explicit null.
type Field[T any] struct {
	Present bool
	Value   *T
}

func Present[T any](value *T) Field[T] {
	return Field[T]{Present: true, Value: value}
}

var pointerTokenEscaper = strings.NewReplacer("~", "~0", "/", "~1")

func pointerJoin(base string, tokens ...any) string {
	var pointer strings.Builder
	pointer.WriteString(base)
	for _, token := range tokens {
		pointer.WriteByte('/')
		pointer.WriteString(pointerTokenEscaper.Replace(fmt.Sprint(token)))
	}
	return pointer.String()
}

func fieldError(pointer string, code Code, format string, args ...any) FieldError {
	return FieldError{Pointer: pointer, Code: code, Detail: fmt.Sprintf(format, args...)}
}
