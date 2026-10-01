package tui3

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/remote"
)

// A late write may already have saved the person's words. Calling it a failure
// invites a second write, so the receipt must leave the question open instead.
func TestALateRememberDoesNotClaimFailureOrWriteAgain(t *testing.T) {
	for _, err := range []error{remote.ErrLate, fmt.Errorf("the engine: %w", remote.ErrLate)} {
		agent := &lateMemoryWriter{rememberingAgent: &rememberingAgent{}, err: err}
		a := newTestApp(agent)
		settleMemorySlash(t, a, "/remember keep this once")
		got := plain(lastNote(t, a))
		if !strings.Contains(got, "saving that has not answered yet") || !strings.Contains(got, "/memory before trying again") {
			t.Fatalf("late memory receipt = %q", got)
		}
		if strings.Contains(got, "could not remember") || agent.calls != 1 {
			t.Fatalf("late memory claimed failure or wrote again: %q, calls=%d", got, agent.calls)
		}
	}
}

type lateMemoryWriter struct {
	*rememberingAgent
	err   error
	calls int
}

func (a *lateMemoryWriter) Remember(string) (string, error) {
	a.calls++
	return "", a.err
}
