package providers

import (
	"context"
	"fmt"
	"time"
)

// Retrier retries an operation on transient errors with exponential backoff.
type Retrier struct {
	// Retries is the number of additional attempts after the first.
	Retries int
	// Base is the delay before the first retry; it doubles each time.
	Base time.Duration
}

// NewRetrier returns the default policy: 2 retries starting at 500ms.
func NewRetrier() Retrier {
	return Retrier{Retries: 2, Base: 500 * time.Millisecond}
}

// Do runs fn, retrying while it returns a transient error, up to r.Retries times.
// It returns early if ctx is cancelled.
func (r Retrier) Do(ctx context.Context, fn func() error) error {
	delay := r.Base
	var err error
	for attempt := 0; ; attempt++ {
		if err = fn(); err == nil || !IsTransient(err) || attempt >= r.Retries {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("retry aborted: %w", ctx.Err())
		case <-time.After(delay):
		}
		delay *= 2
	}
}
