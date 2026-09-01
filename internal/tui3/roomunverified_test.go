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
//
// THE FACTS LIVE ON THE TOP BAR NOW (topbar.go): the room's mark is the bar's
// lead glyph, the trail is the crumb, and the state word rides the trimmings —
// but the law is the one this test has always held: the room never calls work
// that ran to the end "stopped".
func TestTheRoomSaysTheNodeNeedsALook(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskUnverified,
		unverifiedNotice("finished, but needs your look — nothing came back either way"))})
	node := a.tasks[7]

	if got := a.roomStateWord(node); got != taskUnverifiedWord {
		t.Fatalf("the room calls a node that needs a look %q, want %q", got, taskUnverifiedWord)
	}
	// THE MARK IS THE RAIL'S OWN THIRD ONE, not the queued glyph a settled node
	// was wearing on its own page.
	if got := a.roomMark(node); got != glyphUnverified {
		t.Fatalf("the room draws it with %q, want %q", got, glyphUnverified)
	}

	// And the line a person actually reads, in one piece: the top bar's crumb
	// and trimmings where the pinned header used to hold them.
	a.room = &taskRoom{id: 7, title: "Port the parser", unfolded: map[int]bool{}, live: -1, think: -1}
	bar := plain(a.topBarWord(120))
	want := "Port the parser #7"
	if !strings.Contains(bar, want) {
		t.Fatalf("the top bar's crumb is %q, want it to contain %q", bar, want)
	}
	if !strings.Contains(bar, taskUnverifiedWord) {
		t.Fatalf("the top bar's trimmings are %q, want the %q word", bar, taskUnverifiedWord)
	}
	for _, never := range []string{taskStoppedWord, mergeWordAborted} {
		if strings.Contains(bar, never) {
			t.Fatalf("the top bar says %q about work that ran to the end: %q", never, bar)
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
