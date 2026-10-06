//go:build !windows

package session

import (
	"os"
	"syscall"
	"testing"
	"time"
)

// A consumer path replaced by a FIFO between its boundary read and the post-turn
// recheck must not stall the observation. A stat-before-open is not enough \u2014 the
// FIFO is what open() would block on \u2014 so the reader opens non-blocking and then
// refuses the non-regular descriptor.
func TestContextualForwardingFifoConsumerDoesNotBlock(t *testing.T) {
	root, _, _, consumer := contextualForwardingFixture(t)
	contextualForwardBoundaryRead(t, root, consumer, "root-read")
	if err := os.Remove(consumer); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(consumer, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		root.observeContextualDependencies(root.memoryTurnSource("inspect the report utility"))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		// Release the blocked open so the test binary can exit, then fail.
		if w, err := os.OpenFile(consumer, os.O_WRONLY, 0); err == nil {
			w.Close()
		}
		<-done
		t.Fatal("observeContextualDependencies blocked on a FIFO consumer recheck")
	}
}
