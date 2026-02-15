package ratelimit

import (
	"bytes"
	"io"
	"testing"
	"time"
)

func TestReaderLimitsThroughput(t *testing.T) {
	data := make([]byte, 100*1024) // 100KB
	for i := range data {
		data[i] = byte(i % 256)
	}

	// Limit to 50KB/s — reading 100KB should take ~2s
	limiter := NewLimiter(50 * 1024)
	r := NewReader(bytes.NewReader(data), limiter)

	start := time.Now()
	buf, err := io.ReadAll(r)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(buf) != len(data) {
		t.Fatalf("read %d bytes, want %d", len(buf), len(data))
	}
	if !bytes.Equal(buf, data) {
		t.Fatal("data mismatch")
	}

	// Should take at least 1s (100KB at 50KB/s, minus initial bucket)
	if elapsed < 1*time.Second {
		t.Errorf("elapsed %v, expected at least 1s for rate-limited read", elapsed)
	}
}

func TestReaderPreservesData(t *testing.T) {
	data := []byte("hello, world!")
	limiter := NewLimiter(1024 * 1024) // 1MB/s — fast enough to not slow test
	r := NewReader(bytes.NewReader(data), limiter)

	buf, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(buf) != string(data) {
		t.Errorf("got %q, want %q", string(buf), string(data))
	}
}

func TestReaderReportsEOF(t *testing.T) {
	data := []byte("short")
	limiter := NewLimiter(1024 * 1024)
	r := NewReader(bytes.NewReader(data), limiter)

	buf := make([]byte, 1024)
	n, err := r.Read(buf)
	if n != len(data) {
		t.Errorf("first read n = %d, want %d", n, len(data))
	}

	n, err = r.Read(buf)
	if err != io.EOF {
		t.Errorf("expected EOF, got n=%d err=%v", n, err)
	}
}

func TestSharedLimiterAcrossReaders(t *testing.T) {
	// Two readers sharing a 50KB/s limiter, each reading 50KB
	// Total = 100KB at 50KB/s, should take ~1s
	limiter := NewLimiter(50 * 1024)

	data1 := make([]byte, 50*1024)
	data2 := make([]byte, 50*1024)

	r1 := NewReader(bytes.NewReader(data1), limiter)
	r2 := NewReader(bytes.NewReader(data2), limiter)

	start := time.Now()

	// Read sequentially to measure combined throughput
	io.ReadAll(r1)
	io.ReadAll(r2)

	elapsed := time.Since(start)

	if elapsed < 1*time.Second {
		t.Errorf("elapsed %v, expected at least 1s for shared limiter", elapsed)
	}
}
