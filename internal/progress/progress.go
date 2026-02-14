package progress

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	gosync "sync"
)

// Event types emitted as JSON lines.
const (
	EventStart    = "start"
	EventComplete = "complete"
	EventError    = "error"
	EventDelete   = "delete"
	EventSkip     = "skip"
	EventDone     = "done"
)

// Event is a single progress event emitted as a JSON line.
type Event struct {
	Type       string `json:"event"`
	File       string `json:"file,omitempty"`
	Size       int64  `json:"size,omitempty"`
	Error      string `json:"error,omitempty"`
	Downloaded int    `json:"downloaded,omitempty"`
	Deleted    int    `json:"deleted,omitempty"`
	Errors     int    `json:"errors,omitempty"`
	Skipped    int    `json:"skipped,omitempty"`
}

// Reporter emits progress events. Safe for concurrent use.
type Reporter struct {
	mu      gosync.Mutex
	w       io.Writer
	enabled bool
}

// NewReporter creates a reporter that writes JSON lines to stdout.
// If enabled is false, all methods are no-ops.
func NewReporter(enabled bool) *Reporter {
	return &Reporter{w: os.Stdout, enabled: enabled}
}

// Emit writes a single JSON event line.
func (r *Reporter) Emit(e Event) {
	if !r.enabled {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	fmt.Fprintln(r.w, string(data))
}

// Start emits a file download/upload start event.
func (r *Reporter) Start(file string, size int64) {
	r.Emit(Event{Type: EventStart, File: file, Size: size})
}

// Complete emits a file download/upload completion event.
func (r *Reporter) Complete(file string) {
	r.Emit(Event{Type: EventComplete, File: file})
}

// FileError emits a file error event.
func (r *Reporter) FileError(file string, err error) {
	r.Emit(Event{Type: EventError, File: file, Error: err.Error()})
}

// Delete emits a file deletion event.
func (r *Reporter) Delete(file string) {
	r.Emit(Event{Type: EventDelete, File: file})
}

// Skip emits a file skip event.
func (r *Reporter) Skip(file string) {
	r.Emit(Event{Type: EventSkip, File: file})
}

// Done emits a summary event.
func (r *Reporter) Done(downloaded, deleted, errors, skipped int) {
	r.Emit(Event{
		Type:       EventDone,
		Downloaded: downloaded,
		Deleted:    deleted,
		Errors:     errors,
		Skipped:    skipped,
	})
}
