package retry

import (
	"context"
	"math/rand"
	"time"
)

// WithBackoff retries fn up to maxRetries times with exponential backoff
// and jitter. Returns nil on the first successful attempt, or the last
// error after all retries are exhausted. Respects context cancellation
// between attempts. If maxRetries is 0, fn is called exactly once.
func WithBackoff(ctx context.Context, maxRetries int, fn func() error) error {
	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err = fn(); err == nil {
			return nil
		}

		if attempt == maxRetries {
			break
		}

		// Exponential backoff: 1s, 2s, 4s, ... plus random 0-1s jitter
		backoff := time.Duration(1<<uint(attempt)) * time.Second
		jitter := time.Duration(rand.Int63n(int64(time.Second)))
		delay := backoff + jitter

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return err
}
