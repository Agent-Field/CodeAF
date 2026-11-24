package tui3

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/session"
)

// R1: a live view mirrors the host's own words, and when another computer
// takes the chat the host says that it did. If this window is the computer that
// took it, the sentence arrives in the moment between the move landing and the
// moved chat taking the screen, and it is about the chat as it was, not about
// the chat now. It must not stop the words the person types next.
func TestR1ASentenceTheHostSaysAboutTheMoveDoesNotStopTheWordsTypedNext(t *testing.T) {
	a, _ := liveWithRecord(t, session.DisplayEntry{Role: "user", Text: "host question"})
	cell := filepath.Base(session.DirOf(a.file))
	// What a finished move leaves behind: the new hold, at the fence the move
	// took, still waiting for its writer to say it holds.
	a.machineRead.sendHolds = map[string]sendHold{cell: {fence: 2, awaitingWriter: true}}

	a.note(chatlist.Superseded("laptop"))

	if held := a.machineRead.sendHolds[cell]; held.blocked {
		t.Fatalf("a sentence a live view mirrored blocked the hold the move just made: %+v", held)
	}
}

// R5: outside a live view, a displacement sentence still stops the window's own
// sends, which is what it is for.
func TestR5ASentenceAboutADisplacementStillStopsAWindowThatIsNotALiveView(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.dismissWelcome()
	a.file = filepath.Join(t.TempDir(), retryRow.Cell, "session.jsonl")
	cell := filepath.Base(session.DirOf(a.file))
	a.machineRead.sendHolds = map[string]sendHold{cell: {fence: 2}}

	a.note(chatlist.Superseded("laptop"))

	if held := a.machineRead.sendHolds[cell]; !held.blocked {
		t.Fatalf("a displaced window's own hold was not blocked by the sentence: %+v", held)
	}
}
