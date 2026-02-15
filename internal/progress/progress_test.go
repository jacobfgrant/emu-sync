package progress

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestReporterEmitsJSON(t *testing.T) {
	var buf bytes.Buffer
	r := &Reporter{w: &buf, enabled: true}

	r.Start("roms/snes/Game.sfc", 1024)
	r.Complete("roms/snes/Game.sfc")
	r.FileError("roms/bad.rom", fmt.Errorf("connection reset"))
	r.Delete("roms/old.rom")
	r.Done(1, 1, 1, 0)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5", len(lines))
	}

	// Verify each line is valid JSON
	for i, line := range lines {
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i, err)
		}
	}

	// Check first event
	var first Event
	json.Unmarshal([]byte(lines[0]), &first)
	if first.Type != EventStart {
		t.Errorf("first event type = %q, want %q", first.Type, EventStart)
	}
	if first.File != "roms/snes/Game.sfc" {
		t.Errorf("first event file = %q", first.File)
	}
	if first.Size != 1024 {
		t.Errorf("first event size = %d, want 1024", first.Size)
	}

	// Check done event
	var last Event
	json.Unmarshal([]byte(lines[4]), &last)
	if last.Type != EventDone {
		t.Errorf("last event type = %q, want %q", last.Type, EventDone)
	}
	if last.Downloaded != 1 || last.Deleted != 1 || last.Errors != 1 {
		t.Errorf("done event counts: downloaded=%d deleted=%d errors=%d",
			last.Downloaded, last.Deleted, last.Errors)
	}
}

func TestNewReporterWriter(t *testing.T) {
	var buf bytes.Buffer
	r := NewReporterWriter(&buf)

	r.Start("test/file.rom", 512)
	r.Complete("test/file.rom")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}

	var e Event
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil {
		t.Fatalf("line 0 not valid JSON: %v", err)
	}
	if e.Type != EventStart || e.File != "test/file.rom" || e.Size != 512 {
		t.Errorf("unexpected start event: %+v", e)
	}
}

func TestReporterDisabled(t *testing.T) {
	var buf bytes.Buffer
	r := &Reporter{w: &buf, enabled: false}

	r.Start("file", 100)
	r.Complete("file")

	if buf.Len() != 0 {
		t.Errorf("disabled reporter should produce no output, got %q", buf.String())
	}
}
