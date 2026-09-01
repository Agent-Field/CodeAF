package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// endedRailText is the rail's rows as one line, because the rail wraps a row's
// sentence across its narrow column and a test reads the sentence, not the wrap.
func endedRailText(a *app) string {
	rows := plain(strings.Join(a.railRows(12), "\n"))
	return strings.Join(strings.Fields(strings.ReplaceAll(rows, "│", " ")), " ")
}

func endedNotice(ending session.TaskEnding, report string) session.TaskNotice {
	return session.TaskNotice{
		Elapsed: 400 * time.Second,
		Report:  report,
		Ending:  ending,
		Merge:   mergeWordAborted,
		Branch:  "task/parser",
		Changed: []string{"internal/parse/keys.go"},
	}
}

// A NODE THE CONNECTION DROPPED OUT FROM UNDER IS NOT A FAILURE AND WAS NOT
// STOPPED. Six rows read "stopped — branch kept" one evening, and three of them
// were this: the row now says what happened, wears the mark that asks for a
// steer rather than the cross that reports a fault, and keeps the half of the
// old sentence that is still true.
func TestAHaltedNodeSaysWhyAndWearsTheSteerMarkNotTheCross(t *testing.T) {
	for _, tc := range []struct {
		ending session.TaskEnding
		word   string
	}{
		{session.TaskEndingWire, endingWordWire},
		{session.TaskEndingCircling, endingWordCircling},
		{session.TaskEndingBlocked, endingWordBlocked},
		{session.TaskEndingSteps, endingWordSteps},
		{session.TaskEndingNotes, endingWordNotes},
	} {
		a, _, _ := taskApp(t)
		drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskFailed,
			endedNotice(tc.ending, "lost the connection to the model: read: connection reset by peer"))})

		text := taskText(a)
		for _, want := range []string{tc.word + " — " + taskBranchKept + " · task/parser"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s: the card is missing %q:\n%s", tc.ending, want, text)
			}
		}
		if strings.Contains(text, taskStoppedKept) {
			t.Fatalf("%s: the card says the node was stopped:\n%s", tc.ending, text)
		}
		rail := endedRailText(a)
		if want := glyphHalted + " " + plain(a.taskMark(identFor(7))) + " Port the parser"; !strings.Contains(rail, want) {
			t.Fatalf("%s: the rail is missing %q:\n%s", tc.ending, want, rail)
		}
		if !strings.Contains(rail, tc.word+" — "+taskBranchKept) {
			t.Fatalf("%s: the rail row does not say why:\n%s", tc.ending, rail)
		}
		for _, never := range []string{glyphBad + " " + plain(a.taskMark(identFor(7))), taskStoppedKept} {
			if strings.Contains(rail, never) {
				t.Fatalf("%s: the rail says %q:\n%s", tc.ending, never, rail)
			}
		}
	}
}

// A CHECK THAT DID NOT ACCEPT THE WORK, OR A RUN THAT BROKE, IS THE CROSS — and
// its row says which. A failed node an older engine sent with no reason keeps
// every word it always had.
func TestARefusedOrBrokenNodeKeepsTheCrossAndAnUnexplainedOneKeepsItsOldWords(t *testing.T) {
	for _, tc := range []struct {
		ending session.TaskEnding
		row    string
	}{
		{session.TaskEndingRefused, endingWordRefused + " — " + taskBranchKept},
		{session.TaskEndingError, endingWordError + " — " + taskBranchKept},
		{"", taskStoppedKept},
	} {
		a, _, _ := taskApp(t)
		drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskFailed,
			endedNotice(tc.ending, "incomplete — the parser still drops the last key"))})
		rail := endedRailText(a)
		if want := glyphBad + " " + plain(a.taskMark(identFor(7))) + " Port the parser"; !strings.Contains(rail, want) {
			t.Fatalf("%q: the rail is missing %q:\n%s", tc.ending, want, rail)
		}
		if !strings.Contains(rail, tc.row+" · task/parser") {
			t.Fatalf("%q: the rail row is missing %q:\n%s", tc.ending, tc.row, rail)
		}
		if strings.Contains(rail, glyphHalted+" ") {
			t.Fatalf("%q: the rail wears the steer mark:\n%s", tc.ending, rail)
		}
	}
}

// A PERSON'S STOP OUTRANKS EVERY REASON, in the mark and in the words.
func TestAStoppedNodeStillWearsTheStopMarkOverItsEnding(t *testing.T) {
	a, _, _ := taskApp(t)
	notice := endedNotice(session.TaskEndingStopped, "stopped")
	notice.Stopped = true
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskFailed, notice)})
	rail := endedRailText(a)
	if !strings.Contains(rail, glyphStopped+" "+plain(a.taskMark(identFor(7)))+" Port the parser") {
		t.Fatalf("the rail lost the stop mark:\n%s", rail)
	}
	if !strings.Contains(rail, taskStoppedKept+" · task/parser") {
		t.Fatalf("the rail lost the stopped sentence:\n%s", rail)
	}
}

// A WORKER STOPPED BY THE WRITE-YOUR-NOTES RULE HAS ITS OWN ROW, and the row is
// pinned in the words themselves rather than through the constant that spells
// them — a reworded constant is exactly the change this is here to catch. It
// said nothing at all before: the node settled as an ordinary failure and the
// rail read "stopped — branch kept", which told somebody nothing was wrong when
// the turn had been ended for refusing to write anything down.
func TestAWorkerThatWouldNotWriteItsNotesSaysSoOnTheRailAndInTheRoom(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskFailed,
		endedNotice(session.TaskEndingNotes, "stopped here · would not write its notes down"))})

	row := "would not write its notes down — branch kept"
	// The branch is not asserted beside it: this row is the longest of the
	// endings and the rail drops the branch to keep the name whole, which is the
	// narrow-row law working rather than a missing word.
	if rail := endedRailText(a); !strings.Contains(rail, row) {
		t.Fatalf("the rail row does not read %q:\n%s", row, rail)
	}
	if room := taskText(a); !strings.Contains(room, row) {
		t.Fatalf("the room does not read %q:\n%s", row, room)
	}
	// AND IT IS THE STEER MARK, NOT THE CROSS. Nobody found anything wrong with
	// the work; it is on the branch and the next move is a person's.
	if rail := endedRailText(a); !strings.Contains(rail, glyphHalted+" "+plain(a.taskMark(identFor(7)))+" Port the parser") {
		t.Fatalf("the rail does not wear the steer mark:\n%s", rail)
	}
	// AND NO MACHINERY VOCABULARY REACHES IT (CLAUDE.md's vocabulary law).
	for _, banned := range []string{"rule", "held", "process", "loop", "enforce"} {
		if strings.Contains(strings.ToLower(row), banned) {
			t.Fatalf("the row says %q to a person: %q", banned, row)
		}
	}
}
