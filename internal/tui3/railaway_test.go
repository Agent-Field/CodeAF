package tui3

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE COLUMN IS THE PERSON'S TO CLOSE ─────────────────────────────────────
//
// The roster's column is thirty columns taken off a paragraph somebody is
// reading, and until ctrl+g there was no way to say "not now": one node raised
// it and only /new put it away. These are the whole of that claim — the key, the
// line at the bottom of the column that names it, what the conversation gets
// back, what still says work is happening once the column is gone, and the fact
// that the answer outlives the session.

// ctrlG is the column's own door (task.go's [railStowKey]).
func ctrlG() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl} }

// THE KEY CLOSES THE COLUMN, GIVES THE WIDTH BACK, AND OPENS IT AGAIN. Nothing
// about the roster is lost in between: it is the same list, drawn again.
func TestTheTaskColumnClosesAndReopensOnItsKey(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	railRun(a)

	if !a.railShowing() {
		t.Fatal("a session with five nodes drew no column to close")
	}
	full := a.width
	if a.bodyWidth() != full-railCols {
		t.Fatalf("the open column is not charged against the conversation: body=%d", a.bodyWidth())
	}

	drive(t, a, ctrlG())
	if a.railShowing() || a.railStanding() {
		t.Fatal("ctrl+g left the column on the frame")
	}
	if rows := a.railRows(a.viewHeight()); len(rows) != 0 {
		t.Fatalf("a closed column still drew %d rows", len(rows))
	}
	// THE CONVERSATION TAKES THE COLUMNS BACK, and it takes every one of them:
	// the width is what the transcript wraps at, what the wheel resolves through
	// and what a click is hit-tested against, so a body that stayed narrow would
	// be thirty columns of blank beside a paragraph.
	if a.bodyWidth() != full {
		t.Fatalf("the closed column still charges %d columns", full-a.bodyWidth())
	}

	// WORK THAT LANDS WHILE THE COLUMN IS AWAY IS IN IT WHEN IT COMES BACK. The
	// roster is derived from the nodes on every frame, so there is no snapshot to
	// go stale — this is the test that keeps it that way.
	a.taskUpdate(update(9, "Late arrival", session.TaskRunning, session.TaskNotice{}))

	drive(t, a, ctrlG())
	if !a.railShowing() {
		t.Fatal("ctrl+g did not bring the column back")
	}
	if a.bodyWidth() != full-railCols {
		t.Fatalf("the reopened column is not charged against the conversation: body=%d", a.bodyWidth())
	}
	rail := strings.Join(railText(a, a.viewHeight()), "\n")
	for _, want := range []string{"Ship the port", "Late arrival"} {
		if !strings.Contains(rail, want) {
			t.Fatalf("the reopened column is a stale snapshot — %q is missing:\n%s", want, rail)
		}
	}
}

// THE BOTTOM OF THE COLUMN NAMES THE KEY, AND THE LINE IS A BUTTON. A door that
// only the keyboard can open is a door half this surface cannot find.
func TestTheColumnDrawsItsOwnDoorAndThePressClosesIt(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	railRun(a)

	rows := railText(a, a.viewHeight())
	if !strings.Contains(strings.Join(rows, "\n"), railStowHint) {
		t.Fatalf("the column drew no way out of itself:\n%s", strings.Join(rows, "\n"))
	}
	// It is the LAST line of the column: the way out of anything is at the bottom
	// of it, under the aggregate and under the width offer alike.
	last := ""
	for _, row := range rows {
		if strings.TrimSpace(row) != "" {
			last = strings.TrimSpace(row)
		}
	}
	// The seam runs through it like every other line in the column.
	if !strings.HasSuffix(last, railStowHint) {
		t.Fatalf("the door is not the column's last line: %q", last)
	}

	pressed := false
	for y := a.bodyTop(); y < a.bodyTop()+a.viewHeight(); y++ {
		if line, ok := a.railLineAt(y); ok && line.stow {
			drive(t, a, tea.MouseClickMsg{X: a.bodyWidth() + 4, Y: y, Button: tea.MouseLeft})
			pressed = true
			break
		}
	}
	if !pressed {
		t.Fatal("the column's door was drawn on no pressable line")
	}
	if !a.railAway || a.railShowing() {
		t.Fatal("a press on the door did not close the column")
	}
}

// A CLOSED COLUMN NEVER HIDES RUNNING WORK. The strip stands itself up the
// moment the roster stands down, and the legend says which key brings the whole
// list back — both of them, and neither of them for a session that has run
// nothing.
func TestAClosedColumnStillSaysWhereTheWorkIs(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()

	// THE COLUMN STANDS EMPTY NOW, so the key works from the first frame — but
	// the legend stays quiet: a standing hint about a roster of nothing is the
	// emptiness law broken in the slot a person reads most (render.go's
	// [app.hintWord]).
	drive(t, a, ctrlG())
	if !a.railAway {
		t.Fatal("ctrl+g did not close the empty column")
	}
	if a.hintWord() == railBackHint {
		t.Fatal("a session with no work at all offered the way back to a roster of nothing")
	}
	drive(t, a, ctrlG())
	if a.railAway {
		t.Fatal("ctrl+g did not bring the empty column back")
	}

	railRun(a)
	drive(t, a, ctrlG())
	if !a.stripShowing() {
		t.Fatal("the closed column left running work with no door on the frame")
	}
	strip := plain(a.stripRow(a.width))
	if !strings.Contains(strip, "Ship the port") {
		t.Fatalf("the strip named no running node:\n%s", strip)
	}
	if got := a.hintWord(); got != railBackHint {
		t.Fatalf("the legend's hint reads %q, want %q", got, railBackHint)
	}

	// AND THE STRIP'S OWN DOOR BRINGS THE COLUMN BACK. Asking for the whole
	// roster is asking for it to be there (task.go's [app.railTake]).
	a.railTake(true)
	if a.railAway || !a.railShowing() {
		t.Fatal("a request for the roster left the column away")
	}
}

// A FRAME WITH NO COLUMN ON IT HAS NOTHING TO CLOSE. The key falls through there
// rather than writing a preference into the next session out of a keystroke that
// changed nothing on this one — but once the roster has been raised over the
// body, it is a roster, and the same key puts it away.
func TestTheKeyFallsThroughWhereNoRosterIsOnTheFrame(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	// Under railSlimFloor there is no column to lend (task.go's [railColsFor]).
	a.width = railSlimFloor - 20
	railRun(a)

	if a.railStanding() {
		t.Fatal("a frame this narrow drew a roster")
	}
	drive(t, a, ctrlG())
	if a.railAway {
		t.Fatal("ctrl+g took away a column the frame was not lending")
	}

	drive(t, a, ctrlT())
	if !a.railFull() {
		t.Fatal("ctrl+t did not raise the roster over the body")
	}
	drive(t, a, ctrlG())
	if !a.railAway || a.railFull() {
		t.Fatal("ctrl+g did not put the raised roster away")
	}
}

// THE ANSWER OUTLIVES THE SESSION. It is written the moment the column moves and
// read once at boot — which is what makes it a preference rather than a mode.
func TestTheColumnsPostureIsRememberedAcrossSessions(t *testing.T) {
	dir := t.TempDir()
	a, _, _ := taskApp(t)
	a.profileDir = dir
	railRun(a)

	if !config.TaskColumnAt(dir) {
		t.Fatal("a profile nobody has touched does not stand the column up")
	}
	drive(t, a, ctrlG())
	if config.TaskColumnAt(dir) {
		t.Fatal("closing the column was not written to the profile")
	}

	// A SESSION THAT OPENS NEXT OPENS WITHOUT IT.
	fresh := newApp(context.Background(), Options{
		Agent:      &fakeAgent{model: "deepseek/deepseek-v4-flash"},
		Workspace:  "/tmp/lab",
		ProfileDir: dir,
	})
	if !fresh.railAway {
		t.Fatal("a new session stood the column back up for somebody who had closed it")
	}

	// And the key is the same key on the way back.
	drive(t, a, ctrlG())
	if !config.TaskColumnAt(dir) {
		t.Fatal("reopening the column was not written to the profile")
	}
}
