//go:build unix

package main

import (
	"context"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// WP-12 (g): replacing an added file with a named pipe cannot make a source
// read wait for a writer or read bytes from a non-regular descriptor.
func TestWorkstreamAgentSourceRefusesAReplacementFIFO(t *testing.T) {
	b, _, w, door := workstreamAgentFixture(t)
	path := workstreamSourcesFile(t)
	item, err := b.WorkstreamSourceAdd(appwire.WorkstreamSourceAddParams{WorkstreamID: w.ID, Kind: "file", Path: &path})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() {
		raw, err := door.Read(context.Background(), session.WorkstreamReadAsk{What: "source", SourceID: item.Source.ID})
		if err != nil {
			done <- err.Error()
			return
		}
		done <- string(raw)
	}()
	select {
	case answer := <-done:
		if !strings.Contains(answer, "not a regular text file") {
			t.Fatalf("special file answer: %s", answer)
		}
	case <-time.After(5 * time.Second):
		if writer, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			writer.Close()
		}
		t.Fatal("source read blocked on a named pipe")
	}
}
