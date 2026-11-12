//go:build !windows

package session

import (
	"golang.org/x/sys/unix"
	"os"
	"testing"
	"time"
)

func TestShareReceiptNeverWaitsForAFIFOWriter(t *testing.T) {
	root := t.TempDir()
	c, err := AdmitShareOperation(root, "s", "op", []string{"/w/a.png"}, ShareProvenance{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	c.Release()
	_, path := ShareOpPaths(root, "s", "op")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := LoadShareReceipt(root, "s", "op"); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("receipt read waited for FIFO writer")
	}
}
