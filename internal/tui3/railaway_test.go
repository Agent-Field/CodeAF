package tui3

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
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

// altT is the roster's hand-off (task.go's [railHoldChord]). It used to be
// ctrl+t and moved under the other modifier when ctrl+t became the new-tab
// chord (chatstart.go's [app.newChatKey]); it is spelled from the constant so
// the tests below cannot go on pressing a key the surface has stopped binding.
func altT() tea.KeyPressMsg { return key(railHoldChord) }

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
	if a.bodyWidth() != full-sideColsFor(full) {
		t.Fatalf("the open column is not charged against the conversation: body=%d", a.bodyWidth())
	}

	drive(t, a, ctrlG())
	if a.railShowing() || a.railStanding() {
		t.Fatal("ctrl+g left the column on the frame")
	}
	// NOT ONE ROW OF THE ROSTER IS LEFT. What the frame keeps is the closed
	// column's own edge — [railGripCols] cells carrying a handle and at most one
	// glyph of state, and no task's name anywhere (task.go's [app.railGripRows])
	// — which is a different thing from the column and is tested as one below.
	edge := plain(strings.Join(a.railRows(a.viewHeight()), "\n"))
	for _, gone := range []string{"Ship the port", "Read the law", sideTasksWord} {
		if strings.Contains(edge, gone) {
			t.Fatalf("a closed column still drew %q:\n%q", gone, edge)
		}
	}
	// THE CONVERSATION TAKES THE COLUMNS BACK, all but the two the edge costs:
	// the width is what the transcript wraps at, what the wheel resolves through
	// and what a click is hit-tested against, so a body that stayed narrow would
	// be thirty columns of blank beside a paragraph — and a body that took the
	// edge's two as well would run a sentence out under the handle.
	if a.bodyWidth() != full-railGripCols {
		t.Fatalf("the closed column charges %d columns, want %d", full-a.bodyWidth(), railGripCols)
	}

	// WORK THAT LANDS WHILE THE COLUMN IS AWAY IS IN IT WHEN IT COMES BACK. The
	// roster is derived from the nodes on every frame, so there is no snapshot to
	// go stale — this is the test that keeps it that way.
	a.taskUpdate(update(9, "Late arrival", session.TaskRunning, session.TaskNotice{}))

	drive(t, a, ctrlG())
	if !a.railShowing() {
		t.Fatal("ctrl+g did not bring the column back")
	}
	if a.bodyWidth() != full-sideColsFor(full) {
		t.Fatalf("the reopened column is not charged against the conversation: body=%d", a.bodyWidth())
	}
	rail := strings.Join(railText(a, a.viewHeight()), "\n")
	for _, want := range []string{"Ship the port", "Late arrival"} {
		if !strings.Contains(rail, want) {
			t.Fatalf("the reopened column is a stale snapshot — %q is missing:\n%s", want, rail)
		}
	}
}

// THE HEADER NAMES THE KEY, AND THE KEY IS A BUTTON. A door that only the
// keyboard can open is a door half this surface cannot find.
func TestTheColumnDrawsItsOwnDoorAndThePressClosesIt(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	railRun(a)

	rows := railText(a, a.viewHeight())
	if !strings.HasSuffix(strings.TrimRight(rows[0], " "), sideHideKey) {
		t.Fatalf("the column's header names no way out of it: %q", rows[0])
	}
	// The paint is asked of the RAW rows: railText strips ANSI for reading, and
	// an assertion about a palette run on stripped text passes only where the
	// palette paints nothing.
	painted := strings.Join(a.railRows(a.viewHeight()), "\n")
	if !strings.Contains(painted, a.pal.dim(sideHideKey)) {
		t.Fatalf("the key is not in the quiet ink of a hint:\n%q", painted)
	}
	// The header stays at the top while the task action follows the list.
	lines, _ := a.railView(a.viewHeight())
	task := -1
	for i, line := range lines {
		if line.door == marginTaskType {
			task = i
		}
	}
	if lines[0].side == nil || lines[0].side.key != sideHeadKey || task <= 1 {
		t.Fatalf("the header is not pinned above the list and task action %d", task)
	}

	x, y := sideDoorOf(t, a, sideActHide)
	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	if !a.railAway || a.railShowing() {
		t.Fatal("a press on the key did not close the column")
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
	if a.hintWord() == a.sideBackHint() {
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
	if got := a.hintWord(); got != a.sideBackHint() {
		t.Fatalf("the legend's hint reads %q, want %q", got, a.sideBackHint())
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
	// Under railSlimFloor there is no column to lend (sidecol.go's [sideColsFor]).
	a.width = railSlimFloor - 20
	railRun(a)

	if a.railStanding() {
		t.Fatal("a frame this narrow drew a roster")
	}
	drive(t, a, ctrlG())
	if a.railAway {
		t.Fatal("ctrl+g took away a column the frame was not lending")
	}

	drive(t, a, altT())
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

// ── THE EDGE A CLOSED COLUMN LEAVES BEHIND ──────────────────────────────────
//
// ctrl+g used to make the column vanish without a trace, and a thing with no
// trace is a thing a person cannot get back: the chord is knowledge, and the
// person who pressed it by accident does not have it. So a closed column leaves
// an edge — two columns down the right of the frame with a handle in them — and
// the whole strip is a door.

// THE EDGE IS ON THE FRAME, IT COSTS WHAT IT SHOWS, AND PRESSING IT BRINGS THE
// COLUMN BACK.
func TestTheClosedColumnLeavesAnEdgeYouCanClick(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	railRun(a)
	full := a.width

	drive(t, a, ctrlG())
	if !a.railStowed() {
		t.Fatal("a closed column on a wide frame drew no edge")
	}
	rows := a.railRows(a.viewHeight())
	if len(rows) != a.viewHeight() {
		t.Fatalf("the edge drew %d rows, want %d", len(rows), a.viewHeight())
	}
	handles := 0
	for _, row := range rows {
		if ansi.StringWidth(plain(row)) != railGripCols {
			t.Fatalf("an edge row is %d cells wide, want %d: %q",
				ansi.StringWidth(plain(row)), railGripCols, plain(row))
		}
		if strings.Contains(row, railGripGlyph) {
			handles++
		}
	}
	if handles != 1 {
		t.Fatalf("the edge drew %d handles, want exactly one", handles)
	}
	if a.bodyWidth() != full-railGripCols {
		t.Fatalf("the edge costs %d columns, want %d", full-a.bodyWidth(), railGripCols)
	}

	// THE POINTER LIGHTS IT, because it answers to a click.
	middle := a.bodyTop() + a.viewHeight()/2
	a.setHover(a.bodyWidth(), middle)
	if !a.hoveringRailGrip() {
		t.Fatalf("the pointer over the edge lit nothing: %+v", a.hot)
	}

	// AND A PRESS ANYWHERE ON IT IS ctrl+g. Not the handle — anywhere: a strip
	// two cells wide is not a thing to aim at.
	if _, took := a.railPress(a.bodyWidth(), a.bodyTop()); !took {
		t.Fatal("a press on the edge fell through to the conversation")
	}
	if a.railAway || !a.railShowing() {
		t.Fatal("pressing the edge did not bring the column back")
	}
	if a.bodyWidth() != full-sideColsFor(full) {
		t.Fatalf("the reopened column is not charged against the conversation: body=%d", a.bodyWidth())
	}
}

// ── ONE CONTROL, TWO STATES, A FULL CYCLE BY MOUSE ──────────────────────────
//
// The report was two sentences: "the arrow does not even seem it is there", and
// "click should rotate between expanding and closing as well for full cycle".
// Both are about the same control. It was a dim `‹` — the lightest arrow in the
// font at the weight this surface paints telemetry — and it only ever went one
// way, so a person who found it could open the column with the pointer and then
// had to be told a chord to close it again.

// THE HANDLE IS INK AND NOT DIM. It is a control and not a report, and the whole
// of what a person has to find when the column is gone.
func TestTheClosedEdgesHandleIsInkAndNotTelemetry(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	railRun(a)
	drive(t, a, ctrlG())

	rows := a.railRows(a.viewHeight())
	handle := ""
	for _, row := range rows {
		if strings.Contains(plain(row), railGripGlyph) {
			handle = row
		}
	}
	if handle == "" {
		t.Fatalf("the edge drew no handle at all:\n%q", plain(strings.Join(rows, "\n")))
	}
	if !strings.Contains(handle, a.pal.ink(railGripGlyph)) {
		t.Fatalf("the handle is not painted in ink: %q", handle)
	}
	if strings.Contains(handle, a.pal.dim(railGripGlyph)) {
		t.Fatalf("the handle is still painted at telemetry weight: %q", handle)
	}

	// AND THE POINTER TAKES IT FURTHER, across both cells rather than the one:
	// the whole strip answers a click, so the whole strip lights.
	a.setHover(a.bodyWidth(), a.bodyTop()+a.viewHeight()/2)
	if !a.hoveringRailGrip() {
		t.Fatalf("the pointer over the edge lit nothing: %+v", a.hot)
	}
	lit := ""
	for _, row := range a.railRows(a.viewHeight()) {
		if strings.Contains(plain(row), railGripGlyph) {
			lit = row
		}
	}
	if lit == handle {
		t.Fatalf("the handle did not change under the pointer: %q", lit)
	}
	if ansi.StringWidth(plain(lit)) != railGripCols {
		t.Fatalf("the lit handle is %d cells wide, want %d: %q",
			ansi.StringWidth(plain(lit)), railGripCols, plain(lit))
	}
}

// AND THE POINTER GOES ROUND THE WHOLE CYCLE. `❯` on the standing column closes
// it, `❮` on the edge opens it again, and neither leg needs the chord.
func TestTheChevronClosesAndOpensTheColumnByPointerAlone(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	railRun(a)
	if !a.railShowing() {
		t.Fatal("a session with five nodes drew no column")
	}

	// THE OPEN LEG: the header's key lights under the pointer and closes the
	// column on a press.
	x, y := sideDoorOf(t, a, sideActHide)
	a.setHover(x, y)
	if a.hot.kind != hoverSide || a.hot.key != sideHeadKey || a.hot.index < 0 {
		t.Fatalf("the pointer over the column's key lit nothing: %+v", a.hot)
	}
	if lit := strings.Join(a.railRows(a.viewHeight()), "\n"); !strings.Contains(lit, a.pal.cursor(a.pal.ink(sideHideKey), 0)) {
		t.Fatalf("the key did not take its hover ground:\n%q", lit)
	}
	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	if !a.railAway || a.railShowing() {
		t.Fatal("pressing the key did not close the column")
	}

	// THE CLOSED LEG: the same control, the other way.
	if !a.railStowed() {
		t.Fatal("the closed column left no edge to press")
	}
	edge := plain(strings.Join(a.railRows(a.viewHeight()), "\n"))
	if !strings.Contains(edge, railGripGlyph) {
		t.Fatalf("the edge carries no chevron:\n%q", edge)
	}
	drive(t, a, tea.MouseClickMsg{X: a.bodyWidth(), Y: a.bodyTop(), Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: a.bodyWidth(), Y: a.bodyTop(), Button: tea.MouseLeft})
	if a.railAway || !a.railShowing() {
		t.Fatal("pressing the edge did not bring the column back")
	}
	// AND THE RIGHT EDGE NEVER CARRIES BOTH. One control in one of two states.
	back := plain(strings.Join(railText(a, a.viewHeight()), "\n"))
	if strings.Contains(back, railGripGlyph) {
		t.Fatalf("the standing column drew the closed edge's chevron:\n%q", back)
	}
}

// THE EDGE WHISPERS WHAT THE WORK IS DOING, and only while there is anything to
// whisper. A column full of finished work says nothing; one with something
// running says so in a cell, and one with something waiting on a person outranks
// it.
func TestTheClosedEdgeCarriesTheStateOfTheWork(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	a.taskUpdate(update(1, "Ship the port", session.TaskDone, session.TaskNotice{}))
	drive(t, a, ctrlG())

	edge := strings.Join(a.railRows(a.viewHeight()), "\n")
	for _, never := range []string{homeLiveGlyph, homeAskGlyph} {
		if strings.Contains(plain(edge), never) {
			t.Fatalf("the edge whispered %q over work that has landed:\n%q", never, plain(edge))
		}
	}

	// Something RUNNING earns the accent dot.
	a.taskUpdate(update(2, "Port the parser", session.TaskRunning, session.TaskNotice{}))
	if edge := plain(strings.Join(a.railRows(a.viewHeight()), "\n")); !strings.Contains(edge, homeLiveGlyph) {
		t.Fatalf("the edge said nothing about running work:\n%q", edge)
	}

	// And something WAITING ON A PERSON outranks it: it is the one of the two
	// that is asking for a hand.
	a.taskUpdate(update(3, "Mix the audio", session.TaskUnverified, session.TaskNotice{}))
	edge = plain(strings.Join(a.railRows(a.viewHeight()), "\n"))
	if !strings.Contains(edge, homeAskGlyph) {
		t.Fatalf("the edge said nothing about work that needs a person:\n%q", edge)
	}
	if strings.Contains(edge, homeLiveGlyph) {
		t.Fatalf("the edge said both things at once:\n%q", edge)
	}
}

// AND UNDER THE SLIM FLOOR THERE IS NO EDGE, because at that width there is no
// column to bring back — a door onto a room that does not exist.
func TestThereIsNoEdgeWhereThereCouldBeNoColumn(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	a.width = railSlimFloor - 20
	railRun(a)
	a.railAway = true

	if a.railStowed() {
		t.Fatal("a frame too narrow for a column drew its edge anyway")
	}
	if rows := a.railRows(a.viewHeight()); len(rows) != 0 {
		t.Fatalf("the narrow frame drew %d edge rows", len(rows))
	}
	if a.bodyWidth() != a.width {
		t.Fatalf("the narrow frame charged %d columns for an edge it has none of", a.width-a.bodyWidth())
	}
	if a.railGripAt(a.width-1, a.bodyTop()) {
		t.Fatal("the narrow frame answers a press on an edge it does not draw")
	}
}

// AND THERE IS NO EDGE WHILE THE COLUMN IS STANDING, which is the other half of
// the same law: the right-hand strip of the frame belongs to the roster in one
// shape or the other, never to both.
func TestThereIsNoEdgeWhileTheColumnStands(t *testing.T) {
	a, _, _ := taskApp(t)
	a.profileDir = t.TempDir()
	railRun(a)

	if !a.railShowing() {
		t.Fatal("a session with five nodes drew no column")
	}
	if a.railStowed() {
		t.Fatal("an open column drew its own closed edge")
	}
	if a.railGripAt(a.width-1, a.bodyTop()) {
		t.Fatal("the open column answers a press as though it were closed")
	}
}
