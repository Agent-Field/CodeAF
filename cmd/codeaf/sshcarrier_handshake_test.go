package main

import (
	"bytes"
	"github.com/Agent-Field/codeaf/internal/remote"
	"io"
	"testing"
)

// The first writer outlives the handshake, so it must change where it sends
// diagnostics when the surface takes ownership of the terminal.
func TestOriginalSSHCarrierStopsPaintingAfterItsHandshake(t *testing.T) {
	link := &engineLink{}
	terminal, tail := new(bytes.Buffer), &tailWriter{}
	carrier := link.stderrWriter(tail, terminal)
	_, _ = io.WriteString(carrier, "host-key prompt\n")
	// dialEngine stores its successful client at this boundary.
	link.client = new(remote.Client)
	_, _ = io.WriteString(carrier, "connection lost\n")
	if terminal.String() != "host-key prompt\n" {
		t.Fatalf("established carrier painted over frame: %q", terminal.String())
	}
	if tail.String() != "host-key prompt\nconnection lost\n" {
		t.Fatalf("diagnostic tail=%q", tail.String())
	}
}
