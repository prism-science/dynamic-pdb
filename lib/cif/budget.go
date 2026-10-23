package cif

import (
	"context"
	"time"
)

// TimeBudget is how long one extraction may run before it stops reading and
// reports what it has, with a "parser limit: time budget exceeded" warning.
// Reading is linear in the input, so a 4 MiB head takes well under a second;
// the budget is a backstop for a slow machine or an input nobody has thought of.
const TimeBudget = 5 * time.Second

// checkInterval is how many tokenizer steps (or scanned lines) pass between two
// looks at the context.
const checkInterval = 1024

const timeBudgetNote = "time budget exceeded: the rest of the file was not read"

// timeCheck polls a context every checkInterval ticks. The first tick looks, so
// a context that is already done stops the work before it starts.
type timeCheck struct {
	ctx   context.Context
	ticks int
	err   error
}

func (c *timeCheck) expired() bool {
	if c.err != nil {
		return true
	}
	if c.ticks%checkInterval == 0 {
		c.err = c.ctx.Err()
	}
	c.ticks++
	return c.err != nil
}

func withTimeBudget(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, TimeBudget)
}
