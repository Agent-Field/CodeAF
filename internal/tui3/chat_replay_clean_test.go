package tui3

import (
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
	"strings"
	"testing"
)

func TestReplayedConfirmedUpdateStaysVisibleBeforeLaterWork(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.workMode = config.WorkFold
	es, _ := a.replayBlocks([]session.DisplayEntry{
		{Role: "user", Text: "Check both items"},
		{Role: "assistant", Text: "**First result is ready.**", Answer: true},
		{Role: "assistant", Text: "Checking the other item."},
		{Role: "tool", Tool: "read", CallID: "r", Answered: true, Args: `{"path":"other.txt"}`, Output: "second"},
		{Role: "assistant", Text: "**Both results are ready.**", Answer: true},
	}, chatReplay(0))
	a.entries = es
	a.touch()
	page := strings.Join(plainRows(a), "\n")
	for _, want := range []string{"First result is ready.", "Both results are ready."} {
		if !strings.Contains(page, want) {
			t.Fatalf("lost confirmed update %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "**") || strings.Contains(page, "Checking the other item") {
		t.Fatalf("unclean replay:\n%s", page)
	}
}

func TestNestedTranscriptDisclosurePersistsAndStaysLocal(t *testing.T) {
	a, _ := orchApp(t, orchRun4())
	a.workMode = config.WorkFold
	run := a.orchOf()
	run.transcript, run.journal = "plan", foldFixture()
	first := a.orchTranscriptDeck()
	if len(a.deckFolds(first)) == 0 {
		t.Fatal("nested transcript has no fold")
	}
	a.toggleWorkfold(1)
	if !a.orchTranscriptDeck().workOpen[1] {
		t.Fatal("disclosure lost at next frame")
	}
	a.toggleWorkfold(1)
	if a.orchTranscriptDeck().workOpen[1] {
		t.Fatal("disclosure cannot close")
	}
	a.setCapOpen(a.bodyDeck(), 3, true)
	if !a.orchTranscriptDeck().capOpen[3] {
		t.Fatal("caption expansion lost at next frame")
	}
	if a.room.workOpen[1] || a.room.capOpen[3] || a.workOpen[1] {
		t.Fatal("nested state leaked to parent")
	}
}

func TestDismissCommandRunsWithOneEnter(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(1, "Finished", session.TaskDone, session.TaskNotice{})})
	typeLine(t, a, "/dismiss")
	if !a.input.empty() || !a.doneCardAt(a.doneEntryFor(1)).dismissed {
		t.Fatalf("dismiss did not execute with one Enter; draft=%q", a.input.String())
	}
	typeLine(t, a, "/dismiss undo")
	if a.doneCardAt(a.doneEntryFor(1)).dismissed {
		t.Fatal("undo did not execute")
	}
}

func TestRequestedCommandOutputDoesNotBecomeInternalWork(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.workMode = config.WorkFold
	a.slash("/help")
	if len(a.entries) == 0 || !a.entries[len(a.entries)-1].told {
		t.Fatal("requested help was not marked as addressed to person")
	}
	a.touch()
	if page := strings.Join(plainRows(a), "\n"); !strings.Contains(page, "/dismiss") {
		t.Fatalf("requested help hidden:\n%s", page)
	}
}
