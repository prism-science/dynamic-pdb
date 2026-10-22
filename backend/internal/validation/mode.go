package validation

import (
	"context"
	"fmt"
	"log/slog"
)

// Mode decides what happens to a write that breaks schema rules. Log-only lets the
// write through and logs each violation, so the rules can be rolled out before
// clients are rejected.
type Mode string

const (
	ModeLogOnly Mode = "log_only"
	ModeEnforce Mode = "enforce"
)

type Config struct {
	Mode Mode `mapstructure:"mode"`
}

func (mode Mode) Validate() error {
	switch mode {
	case ModeLogOnly, ModeEnforce:
		return nil
	default:
		return fmt.Errorf("unknown validation mode: %s", mode)
	}
}

// Check applies the mode to the field errors found for one write. subject names the
// write in logs, for example "POST /v1/entries".
func (mode Mode) Check(ctx context.Context, logger *slog.Logger, subject string, fieldErrors []FieldError) error {
	switch mode {
	case ModeEnforce:
		if len(fieldErrors) == 0 {
			return nil
		}
		return &Error{Fields: fieldErrors}
	case ModeLogOnly:
		if len(fieldErrors) == 0 {
			return nil
		}
		logger.WarnContext(ctx, "schema validation failed",
			"mode", string(mode),
			"subject", subject,
			"errors", describe(fieldErrors),
		)
		return nil
	default:
		return fmt.Errorf("unknown validation mode: %s", mode)
	}
}
