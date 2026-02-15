package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSucceedsImmediately(t *testing.T) {
	calls := 0
	err := WithBackoff(context.Background(), 3, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestSucceedsAfterRetries(t *testing.T) {
	calls := 0
	err := WithBackoff(context.Background(), 3, func() error {
		calls++
		if calls < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestExhaustsRetries(t *testing.T) {
	sentinel := errors.New("persistent error")
	calls := 0
	err := WithBackoff(context.Background(), 2, func() error {
		calls++
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
	if calls != 3 { // initial + 2 retries
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestZeroRetriesCallsOnce(t *testing.T) {
	calls := 0
	err := WithBackoff(context.Background(), 0, func() error {
		calls++
		return errors.New("fail")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	calls := 0
	go func() {
		// Cancel after a short delay — before the first backoff completes
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err := WithBackoff(ctx, 5, func() error {
		calls++
		return errors.New("fail")
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls > 2 {
		t.Errorf("calls = %d, expected at most 2 before cancellation", calls)
	}
}
