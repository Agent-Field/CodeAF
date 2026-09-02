package main

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/tui3"
)

// A REMOTE SURFACE MUST NOT BE A LESSER SURFACE, and the task seam is where that
// law was quietly broken: [tui3.DrawsTasks] is one type assertion over five
// methods, so a wire missing any one of them leaves the rail unsubscribed with
// nothing on any screen saying why. This is the assertion the compiler cannot
// make for us — internal/remote cannot import internal/tui3 — so it is made
// here, where the door already holds both halves.
func TestARemoteAgentCarriesTheWholeTaskSeam(t *testing.T) {
	if !tui3.DrawsTasks((*remote.Agent)(nil)) {
		t.Fatal("a hosted surface cannot draw tasks: internal/remote is missing part of the task seam")
	}
}
