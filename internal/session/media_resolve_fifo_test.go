//go:build linux || darwin

package session

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestMediaReferenceIndexFIFORefusesWithoutWaitingForWriter(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "index")
	if err := syscall.Mkfifo(index, 0600); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := FindMediaByPath(context.Background(), index, "owner", filepath.Join(dir, "file.png"), time.Now().Add(time.Second)); err == nil {
		t.Fatal("FIFO accepted")
	}
	if time.Since(start) > time.Second {
		t.Fatal("blocked opening FIFO")
	}
}
