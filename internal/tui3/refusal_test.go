package tui3

// A tool call the engine refused on its own arguments is a repair the model
// makes, not a sentence the person reads. The schema's refusal names fields and
// quotes the offending argument — mail addressed to the model — and before
// issue #890 it was appended to the row and the row was opened, so the person
// read a repair instruction about a field they had never heard of, expanded,
// beside a card that already said the call was refused. These tests hold the
// line: the row keeps the person's words and stays shut, and a failure out in
// the world still opens itself.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// TestAToolArgumentRefusalIsNotDrawnToThePerson is issue #890's own shape: a
// propose_task whose declared check is a composed shell command, refused by the
// belt, settled through the reducer the chat uses.
func TestAToolArgumentRefusalIsNotDrawnToThePerson(t *testing.T) {
	base := time.Now()
	a, agent := formingTurn(t)
	a.clock = func() time.Time { return base }
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming("c1", taskTool,
		taskTool+" Fix the nil-map crash", `{"title":"Fix the nil-map","checks":["cd 1-check && ./run.sh"]`)})

	card := a.formingCard()
	if card == nil {
		t.Fatal("the forming fragment raised no card")
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventToolBegin, Tool: taskTool, CallID: "c1",
		Hint: taskTool + " Fix the nil-map crash",
	}})
	refusal := `Invalid arguments: checks must each be ONE command with no shell composition — "cd 1-check && ./run.sh" is not`
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventToolFailed, Tool: taskTool, CallID: "c1",
		Hint: refusal, Output: refusal,
	}})

	if a.formingCard() != nil {
		t.Fatal("a refused propose_task left its card forming")
	}
	if settled := plainStringRowsText(a.taskCardRows(card, 60, false)); !strings.Contains(settled, taskFormingRefused) {
		t.Fatalf("the refused proposal does not say what happened: %q", settled)
	}
	if screen := plainStringRowsText(plainRows(a)); strings.Contains(screen, "Invalid arguments:") {
		t.Fatalf("the schema's repair sentence is drawn to the person:\n%s", screen)
	}
	rows := 0
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind != entryTool || e.tool != taskTool {
			continue
		}
		rows++
		if e.open {
			t.Fatalf("a refused call opened itself: %q", e.text)
		}
		if !strings.Contains(e.text, refusedCallWord) {
			t.Fatalf("the refused call's row does not say so: %q", e.text)
		}
		if !strings.Contains(e.detail.Output, "checks must each be ONE command") {
			t.Fatalf("the schema's sentence left the detail it still owes ctrl+o: %q", e.detail.Output)
		}
	}
	if rows == 0 {
		t.Fatal("the refused call left no row at all")
	}

	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
}

// TestAGenuinelyFailedCallStillOpensItself is the control for the law above: a
// bash that failed out in the world is the one row that opens itself, carrying
// its own output — A FAILURE OPENS ITSELF keeps the case it was written for.
func TestAGenuinelyFailedCallStillOpensItself(t *testing.T) {
	a, agent := formingTurn(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventToolBegin, Tool: "bash", CallID: "b1", Hint: "bash make test",
	}})
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventToolFailed, Tool: "bash", CallID: "b1",
		Hint: "make: *** no rule to make target 'test'", Output: "make: *** no rule to make target 'test'",
	}})
	found := false
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind != entryTool || e.tool != "bash" {
			continue
		}
		found = true
		if !e.open {
			t.Fatalf("a failed bash stayed shut: %q", e.text)
		}
		if !strings.Contains(e.text, "no rule to make target") {
			t.Fatalf("the failed bash lost its own reason: %q", e.text)
		}
	}
	if !found {
		t.Fatal("the failed bash left no row")
	}
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
}
