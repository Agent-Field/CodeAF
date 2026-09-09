package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// endedRailText expands the finished report and reads its rows as one line, because the rail wraps a row's
// sentence across its narrow column and a test reads the sentence, not the wrap.
func endedRailText(a *app) string {
	a.railSetOpen(a.tasks[7], true)
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
// were this: the row now says WHAT HAPPENED, in the reading's own sentence, with
// the branch hung off it as a fact rather than welded into the state.
func TestAHaltedNodeSaysWhyAndIsNotCalledStopped(t *testing.T) {
	for _, tc := range []struct {
		ending session.TaskEnding
		word   string
	}{
		{session.TaskEndingWire, endingWordWire},
		{session.TaskEndingUpstream, endingWordUpstream},
		{session.TaskEndingCircling, endingWordCircling},
		{session.TaskEndingBlocked, endingWordBlocked},
		{session.TaskEndingSteps, endingWordSteps},
		{session.TaskEndingNotes, endingWordNotes},
	} {
		a, _, _ := taskApp(t)
		drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskFailed,
			endedNotice(tc.ending, "lost the connection to the model: read: connection reset by peer"))})

		// THE CARD SAYS THE STATE ONCE AND THE REASON ONCE, on two rows. It used to
		// fuse them into `lost the connection — branch kept`, which is a state, a
		// reason and a source-control fact in one phrase; the head carries the word
		// and the branch and the row under it carries why
		// (docs/design/task-states/DESIGN.md).
		text := taskText(a)
		for _, want := range []string{
			" · " + taskIncompleteState + " · ",
			" · " + taskBranchKept + " · task/parser",
			tc.word,
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s: the card is missing %q:\n%s", tc.ending, want, text)
			}
		}
		for _, never := range []string{"stopped — branch kept", tc.word + " — "} {
			if strings.Contains(text, never) {
				t.Fatalf("%s: the card says %q:\n%s", tc.ending, never, text)
			}
		}
		rail := endedRailText(a)
		// THE READING'S SENTENCE, AND NOT THIS SURFACE'S: `incomplete` plus the
		// reason internal/session spells for that ending, and the branch, which is
		// the one handle back to the work. The column is two rows deep, so on the
		// sentences too long for it the reason gives way and the handle stays
		// (task.go's [app.railUnder]) — which is why the row is asserted in the
		// parts it always carries.
		for _, want := range []string{taskRecordStoppedWord, "task/parser", glyphBad + " Port the parser"} {
			if !strings.Contains(rail, want) {
				t.Fatalf("%s: the rail row is missing %q:\n%s", tc.ending, want, rail)
			}
		}
		// AND THE REASON IS THERE WHEREVER THE COLUMN HAS THE CELLS.
		wide := plain(strings.Join(a.railUnder(a.tasks[7], 90), " "))
		if want := taskRecordStoppedWord + " · " + session.TaskReasonOf(tc.ending, "") + " · task/parser"; wide != want {
			t.Fatalf("%s: a wide column says %q, want %q", tc.ending, wide, want)
		}
		for _, never := range []string{glyphHalted + " Port the parser", taskStoppedWord + " —"} {
			if strings.Contains(rail, never) {
				t.Fatalf("%s: the rail says %q:\n%s", tc.ending, never, rail)
			}
		}
	}
}

// A CHECK THAT DID NOT ACCEPT THE WORK IS INCOMPLETE, not broken: it wears the
// steer mark and says what the check still needs. A run that actually broke,
// and an old failed row with no ending, keep the cross and the word failed.
func TestARefusedNodeIsIncompleteAndBrokenOrOldNodesStillFail(t *testing.T) {
	for _, tc := range []struct {
		ending session.TaskEnding
		mark   string
		home   string
		// state is the one word the rail, the room header, the record and the
		// landing card all say about this ending.
		state string
	}{
		{session.TaskEndingRefused, glyphBad, glyphBad, taskRecordStoppedWord},
		{session.TaskEndingError, glyphBad, glyphBad, taskRecordStoppedWord},
		{"", glyphBad, glyphBad, taskRecordStoppedWord},
	} {
		a, _, _ := taskApp(t)
		drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", session.TaskFailed,
			endedNotice(tc.ending, "incomplete — the parser still drops the last key"))})
		rail := endedRailText(a)
		if want := tc.mark + " Port the parser"; !strings.Contains(rail, want) {
			t.Fatalf("%q: the rail is missing %q:\n%s", tc.ending, want, rail)
		}
		row := tc.state
		if reason := session.TaskReasonOf(tc.ending, "incomplete — the parser still drops the last key"); reason != "" {
			row += " · " + reason
		}
		if wide := plain(strings.Join(a.railUnder(a.tasks[7], 90), " ")); wide != row+" · task/parser" {
			t.Fatalf("%q: the rail row is %q, want %q", tc.ending, wide, row+" · task/parser")
		}
		node := a.tasks[7]
		if got := a.roomStateWord(node); got != tc.state {
			t.Fatalf("%q: room state = %q, want %q", tc.ending, got, tc.state)
		}
		entry := session.TaskIndexEntry{Status: string(session.TaskFailed), Ending: tc.ending}
		if got := taskStateWord(entry, false); got != tc.state {
			t.Fatalf("%q: record state = %q, want %q", tc.ending, got, tc.state)
		}
		if got := plain(a.homeTaskGlyph(entry, session.SessionRow{})); got != tc.home {
			t.Fatalf("%q: home glyph = %q, want %q", tc.ending, got, tc.home)
		}
		// AND THE LANDING CARD SAYS `incomplete` FOR ALL THREE. `failed` is deleted
		// as a landing's word: a fault is `incomplete` plus `a fault: <line>` on the
		// row under it, and the record page above is the last reader that still
		// tells the two apart in one word (docs/design/task-states/DESIGN.md).
		tail := a.doneTail(&taskDone{status: doneStatus(session.TaskFacts{
			State: session.TaskFailed, Ending: tc.ending})})
		if !strings.Contains(tail, " · "+taskIncompleteState) {
			t.Fatalf("%q: landed-card tail = %q, want %q", tc.ending, tail, taskIncompleteState)
		}
		if strings.Contains(tail, "failed") {
			t.Fatalf("%q: a landing still says %q: %q", tc.ending, "failed", tail)
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
	if !strings.Contains(rail, glyphStopped+" Port the parser") {
		t.Fatalf("the rail lost the stop mark:\n%s", rail)
	}
	if !strings.Contains(rail, taskStoppedWord+" · task/parser") {
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

	// THE ROW IS THE READING'S SENTENCE, spelled here as a literal rather than
	// read out of the constant it comes from, because a reworded constant is
	// exactly the change this test is here to catch.
	row := "incomplete · would not write its notes down"
	// The column is two rows deep and this is the longest of the endings, so on a
	// narrow rail the reason gives way and the branch — the one handle back to the
	// work — stays (task.go's [app.railUnder]). The sentence is asserted where the
	// column has the cells for it.
	if wide := plain(strings.Join(a.railUnder(a.tasks[7], 90), " ")); !strings.Contains(wide, row) {
		t.Fatalf("the rail row does not read %q:\n%s", row, wide)
	}
	if rail := endedRailText(a); !strings.Contains(rail, "task/parser") {
		t.Fatalf("the rail lost the handle back to the work:\n%s", rail)
	}
	// THE CARD SAYS THE SAME THING ON TWO ROWS, which is its own law: the word and
	// the branch on the head, the reason under it, and nothing fused
	// (docs/design/task-states/DESIGN.md).
	card := taskText(a)
	for _, want := range []string{
		" · " + taskIncompleteState + " · ", "would not write its notes down", taskBranchKept,
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("the card is missing %q:\n%s", want, card)
		}
	}
	// The landing card keeps the card lane's own sentence for the same ending
	// until that lane lands.
	if room := taskText(a); !strings.Contains(room, "would not write its notes down") {
		t.Fatalf("the room does not say why the worker stopped:\n%s", room)
	}
	// AND IT IS THE OVER-TIER CELL, DIM. Nobody found anything wrong with the
	// work; it is on the branch and the next move is a person's, and the hue is
	// what says so rather than a fourth glyph (tasktier.go).
	if rail := endedRailText(a); !strings.Contains(rail, glyphBad+" Port the parser") {
		t.Fatalf("the rail does not wear the over-tier cell:\n%s", rail)
	}
	// AND NO MACHINERY VOCABULARY REACHES IT (CLAUDE.md's vocabulary law).
	for _, banned := range []string{"rule", "held", "process", "loop", "enforce"} {
		if strings.Contains(strings.ToLower(row), banned) {
			t.Fatalf("the row says %q to a person: %q", banned, row)
		}
	}
}
