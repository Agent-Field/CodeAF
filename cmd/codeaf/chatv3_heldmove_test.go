package main

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// THE MOVE THAT COULD NOT WORK (2026-09-23). A window on today's build pressed
// enter on a conversation an older in-process window was holding. Its engine
// refused the journal with the sentence below, the surface's open door handed
// that sentence back as a bare string, and home — which takes the asking road
// only on [session.ErrSessionLocked] — printed "open codeaf here and press
// enter on it to move it here" instead of asking. The request was never
// written; nothing ever moved. The refusal has to arrive as the lock it is.
func TestAnEngineRefusingAHeldJournalOpensAsTheLockItIs(t *testing.T) {
	workspace := "/home/somebody/api"
	said := errors.New("engine: " + sessionHeldElsewhereSentence(workspace))
	fleet := &engineFleet{
		workspace: workspace,
		dial: func(engineAsk) (*engineConn, error) {
			return nil, said
		},
	}
	_, err := fleet.open(workspace, workspace+"/conversation/transcript.jsonl")
	if err == nil {
		t.Fatal("a refused journal opened")
	}
	if !errors.Is(err, session.ErrSessionLocked) {
		t.Fatalf("the engine's held-journal refusal reached the surface as %q, not as the lock home asks the holder about", err)
	}
	// AND IT STILL READS AS WRITTEN, for every door that prints it.
	if err.Error() != said.Error() {
		t.Fatalf("the refusal was reworded: %q", err)
	}

	// Every other refusal is left exactly as it came.
	other := errors.New("engine: the workspace could not be opened")
	fleet.dial = func(engineAsk) (*engineConn, error) { return nil, other }
	if _, err := fleet.open(workspace, workspace+"/c/transcript.jsonl"); errors.Is(err, session.ErrSessionLocked) {
		t.Fatalf("an unrelated refusal was read as a held journal: %v", err)
	}
}
