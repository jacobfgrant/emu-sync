package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()

	// Create source file
	src := filepath.Join(dir, "src.txt")
	content := []byte("hello world")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("success", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "dst.txt")
		if err := copyFile(src, dst); err != nil {
			t.Fatalf("copyFile returned error: %v", err)
		}
		got, err := os.ReadFile(dst)
		if err != nil {
			t.Fatalf("reading dst: %v", err)
		}
		if string(got) != string(content) {
			t.Errorf("got %q, want %q", got, content)
		}
	})

	t.Run("source not found", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "dst.txt")
		err := copyFile(filepath.Join(dir, "nonexistent"), dst)
		if err == nil {
			t.Fatal("expected error for missing source")
		}
		if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
			t.Error("dst should not exist after failed copy")
		}
	})

	t.Run("destination not writable", func(t *testing.T) {
		err := copyFile(src, filepath.Join(dir, "no", "such", "dir", "dst.txt"))
		if err == nil {
			t.Fatal("expected error for bad destination path")
		}
	})
}

func TestRemoveFile(t *testing.T) {
	t.Run("removes existing file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "file.txt")
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		removeFile(path)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("file should have been removed")
		}
	})

	t.Run("nonexistent file is silent", func(t *testing.T) {
		// Should not panic or print errors
		removeFile(filepath.Join(t.TempDir(), "nonexistent"))
	})
}
