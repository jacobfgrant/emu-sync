package ratelimit

import (
	"io"
	"sync"
	"time"
)

// Limiter controls throughput across all readers sharing it.
// Safe for concurrent use.
type Limiter struct {
	mu        sync.Mutex
	rate      int64 // bytes per second
	available int64
	last      time.Time
}

// NewLimiter creates a limiter that allows bytesPerSec throughput.
func NewLimiter(bytesPerSec int64) *Limiter {
	return &Limiter{
		rate:      bytesPerSec,
		available: bytesPerSec, // start with a full bucket
		last:      time.Now(),
	}
}

// wait blocks until n bytes of capacity are available, then consumes them.
func (l *Limiter) wait(n int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Refill tokens based on elapsed time
	now := time.Now()
	elapsed := now.Sub(l.last)
	l.last = now
	l.available += int64(elapsed.Seconds() * float64(l.rate))
	if l.available > l.rate {
		l.available = l.rate
	}

	l.available -= int64(n)
	if l.available >= 0 {
		return
	}

	// Need to wait for tokens to refill
	deficit := -l.available
	sleepTime := time.Duration(float64(deficit) / float64(l.rate) * float64(time.Second))
	l.mu.Unlock()
	time.Sleep(sleepTime)
	l.mu.Lock()
	l.last = time.Now()
	l.available = 0
}

// Reader wraps an io.Reader with rate limiting.
type Reader struct {
	r       io.Reader
	limiter *Limiter
}

// NewReader wraps r with rate limiting from the shared limiter.
func NewReader(r io.Reader, limiter *Limiter) *Reader {
	return &Reader{r: r, limiter: limiter}
}

func (r *Reader) Read(p []byte) (int, error) {
	// Cap read size to avoid holding the limiter for too long
	const maxChunk = 64 * 1024 // 64KB
	if len(p) > maxChunk {
		p = p[:maxChunk]
	}

	n, err := r.r.Read(p)
	if n > 0 {
		r.limiter.wait(n)
	}
	return n, err
}
