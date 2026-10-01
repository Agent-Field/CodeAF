//go:build unix

package session

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A program can leave a named pipe where its message should be. Finishing
// cannot wait for a writer before it saves the work and releases the copy.
func TestACommitMessageFIFOWithoutAWriterDoesNotBlockFinishing(t *testing.T) {
	repo := newTestRepo(t)
	folder := prepareIn(t, notedProgram(), repo, "Add the fix")
	writeFile(t, filepath.Join(folder.Dir, "fix.go"), "package fix\n")
	path := filepath.Join(folder.Dir, folder.Notes, programCommitMessageFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan ProgramFolderEnd, 1)
	go func() { done <- folder.Finish("the ending") }()
	select {
	case end := <-done:
		if !end.Committed || end.Refused != "" {
			t.Fatalf("finishing: %+v", end)
		}
	case <-time.After(10 * time.Second):
		// Release the old blocking implementation before the test's cleanup.
		writer, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			_ = writer.Close()
		}
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("finishing remained blocked after the FIFO was released")
		}
		t.Fatal("finishing blocked on a commit-message FIFO without a writer")
	}
	got := gitOut(t, repo, "log", "-1", "--format=%B", folder.Branch)
	if !strings.HasPrefix(got, "Add the fix\n\nthe ending\n") {
		t.Fatalf("FIFO did not use the fallback: %q", got)
	}
}
