package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A re-read of an unchanged file with the same offset/limit returns a
// one-line notice instead of the full content.
func TestReadHistoryUnchangedFileReturnsNotice(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, "data.txt")
	if err := os.WriteFile(path, []byte("line one\nline two\n"), 0644); err != nil {
		t.Fatal(err)
	}
	h := &readHistoryState{reads: map[string]readFingerprint{}}

	// First read: records the fingerprint, returns no notice.
	notice, repeated := h.checkRepeatRead(path, 1, 2000)
	if repeated {
		t.Fatalf("first read returned a notice: %q", notice)
	}

	// Second read of the same file, same offset/limit, file unchanged.
	notice, repeated = h.checkRepeatRead(path, 1, 2000)
	if !repeated {
		t.Fatal("second read of unchanged file was not flagged as a repeat")
	}
	if !strings.Contains(notice, "unchanged since you last read it") {
		t.Fatalf("notice = %q, want it to say 'unchanged since you last read it'", notice)
	}
}

// A read of a file that has changed since the last read is always allowed.
func TestReadHistoryChangedFileIsAllowed(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, "data.txt")
	if err := os.WriteFile(path, []byte("original content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	h := &readHistoryState{reads: map[string]readFingerprint{}}

	// First read.
	h.checkRepeatRead(path, 1, 2000)

	// Modify the file.
	if err := os.WriteFile(path, []byte("modified content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Second read: file changed, so it should be allowed (no notice).
	notice, repeated := h.checkRepeatRead(path, 1, 2000)
	if repeated {
		t.Fatalf("read of changed file returned a notice: %q", notice)
	}
}

// A read with a different offset is allowed even if the file is unchanged.
func TestReadHistoryDifferentOffsetIsAllowed(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, "data.txt")
	var content strings.Builder
	for i := range 100 {
		fmt.Fprintf(&content, "line %d\n", i)
	}
	if err := os.WriteFile(path, []byte(content.String()), 0644); err != nil {
		t.Fatal(err)
	}
	h := &readHistoryState{reads: map[string]readFingerprint{}}

	// First read with offset=1.
	h.checkRepeatRead(path, 1, 2000)

	// Second read with offset=50 — different range, should be allowed.
	notice, repeated := h.checkRepeatRead(path, 50, 2000)
	if repeated {
		t.Fatalf("read with different offset returned a notice: %q", notice)
	}
}
