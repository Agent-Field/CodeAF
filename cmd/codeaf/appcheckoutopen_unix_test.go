//go:build unix

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// THE SAFE OPEN'S SEAMS: a leaf swapped for a link or a pipe between its stat
// and its open is refused without a read or a hang, and a file that grows past
// the limit while being read is never returned.
func TestAppReadContainedSeams(t *testing.T) {
	repo := diffRepo(t)
	secret := filepath.Join(t.TempDir(), "secret.txt")
	write(t, secret, "TOP-SECRET\n")
	file := filepath.Join(repo, "f.txt")
	defer func() { appOpenSeam, appReadSeam = func() {}, func(*os.File) {} }()

	write(t, file, "plain\n")
	appOpenSeam = func() {
		_ = os.Remove(file)
		if err := os.Symlink(secret, file); err != nil {
			t.Skip("no symlinks")
		}
	}
	if data, reason := appReadContained(repo, "f.txt", 1<<10); data != nil || reason != "unreadable" {
		t.Fatalf("a link swapped in was read: %q %q", data, reason)
	}

	write(t, filepath.Join(repo, "g.txt"), "plain\n")
	appOpenSeam = func() {
		_ = os.Remove(filepath.Join(repo, "g.txt"))
		if err := mkfifo(filepath.Join(repo, "g.txt"), 0o644); err != nil {
			t.Skip("no fifos")
		}
	}
	done := make(chan string, 1)
	go func() { _, reason := appReadContained(repo, "g.txt", 1<<10); done <- reason }()
	select {
	case reason := <-done:
		if reason != "unreadable" {
			t.Fatalf("a pipe swapped in answered %q", reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a pipe swapped in held the read")
	}

	appOpenSeam = func() {}
	write(t, filepath.Join(repo, "h.txt"), "12345678\n")
	appReadSeam = func(f *os.File) {
		w, err := os.OpenFile(filepath.Join(repo, "h.txt"), os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(bytes.Repeat([]byte("more\n"), 100))
		_ = w.Close()
	}
	if data, reason := appReadContained(repo, "h.txt", 64); data != nil || reason != "too_large" {
		t.Fatalf("a file grown past the limit was returned: %d bytes, %q", len(data), reason)
	}
}
