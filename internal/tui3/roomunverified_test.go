package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE ROOM KNOWS THE THIRD STATE TOO.
//
// A node nobody could judge wears session's "aborted" merge exactly as a stopped
// one does (task_run.go's abortedMerge), so every surface that reads the merge
// word instead of the STATE calls work that ran to the end "stopped". The rail
// and the landed card were taught the difference; the node's own page was not,
// and the same node read three ways depending on which surface you were on.
func TestTheRoomHeaderSaysTheNodeNeedsALook(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskUnverified,
		unverifiedNotice("finished, but needs your look — nothing came back either way"))})
	node := a.tasks[7]

	if got := a.roomStateWord(node); got != tierYourCallWord {
		t.Fatalf("the header calls a node that needs a look %q, want %q", got, tierYourCallWord)
	}
	// THE MARK IS THE RAIL'S OWN THIRD ONE, not the queued glyph a settled node
	// was wearing on its own page.
	if got := a.roomMark(node); got != glyphAsk {
		t.Fatalf("the room draws it with %q, want %q", got, glyphAsk)
	}

	// And the line a person actually reads, in one piece.
	a.room = a.newRoom(7, "Port the parser")
	// THE TRAIL IS THE PATH AND THE FACTS ARE UNDER IT (room.go): the state glyph
	// leads the state word on the facts row rather than the breadcrumb it had
	// nothing to do with.
	head := plain(roomHeadAll(a, 120))
	trail := a.chatCrumbWord() + roomCrumbSep + "Port the parser"
	facts := glyphAsk + " " + tierYourCallWord
	for _, want := range []string{trail, facts} {
		if !strings.Contains(head, want) {
			t.Fatalf("the room header is %q, want it to contain %q", head, want)
		}
	}
	for _, never := range []string{taskStoppedWord, mergeWordAborted} {
		if strings.Contains(head, never) {
			t.Fatalf("the room header says %q about work that ran to the end: %q", never, head)
		}
	}
}

// The other two settled states are untouched: the third case is an addition to
// the switch, not a rewrite of it.
func TestTheRoomStillSaysStoppedAboutAStoppedNode(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskDone,
		session.TaskNotice{Merge: mergeWordAborted, Branch: "task/parser"})})

	if got := a.roomStateWord(a.tasks[7]); got != taskStoppedWord {
		t.Fatalf("a stopped node says %q, want %q", got, taskStoppedWord)
	}
}
