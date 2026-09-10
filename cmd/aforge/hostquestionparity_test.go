package main

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/tui3"
)

// A REMOTE SURFACE MUST NOT BE A LESSER SURFACE, and the questions seam is where
// that law was broken next: [tui3.DrawsQuestions] is one type assertion over
// three methods, so a wire missing any one of them draws no question at all —
// and on the road a plain `aforge` takes, which attaches this machine's session
// host over a socket, that is every question the engine raises. It was measured:
// three minutes on an `ask`, no block, no chip, the turn still running. This is
// the assertion the compiler cannot make for us — internal/remote cannot import
// internal/tui3 — so it is made here, where the door already holds both halves.
func TestARemoteAgentCarriesTheWholeQuestionsSeam(t *testing.T) {
	if !tui3.DrawsQuestions((*remote.Agent)(nil)) {
		t.Fatal("a hosted surface cannot draw questions: internal/remote is missing part of the questions seam")
	}
}
