//go:build linux || darwin

package session

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestMediaReferenceRejectsSourceSwappedToFIFOAfterValidation(t *testing.T) {
	row, root, _, _ := referenceFixture(t)
	live, err := referenceLive(row, []string{root}, row.Version())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(live.Path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(live.Path, 0o600); err != nil {
		t.Fatal(err)
	}
	// Exercise the actual open/stat boundary after the source was authorized as
	// a regular image. There is deliberately no FIFO writer to unblock an open.
	done := make(chan error, 1)
	go func() {
		file, _, _, err := openLiveMediaReference(context.Background(), live)
		if file != nil {
			_ = file.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrMediaReference) {
			t.Fatalf("wanted changed-source refusal, got %v", err)
		}
	case <-time.After(time.Second):
		// Unblock a regressed blocking open before failing, so the test owns no
		// surviving goroutine or descriptor even on the broken implementation.
		unblock, err := os.OpenFile(live.Path, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if err == nil {
			defer unblock.Close()
		}
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		t.Fatal("source replaced by FIFO blocked the reference worker")
	}
}
