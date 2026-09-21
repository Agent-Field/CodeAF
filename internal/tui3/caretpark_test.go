package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/connect"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/charmbracelet/x/ansi"
)

// ── WHERE THE TERMINAL'S OWN CURSOR SITS ────────────────────────────────────
//
// The frame hands view.go one row and one column, view.go hands them to the
// terminal as a tea.Cursor, and the renderer parks the hardware cursor there
// after the frame flushes (view.go's [app.View], bubbletea's cursed renderer).
// Every test in this file pins that park against a frame it can see: the caret
// lands on the line of the box the person is typing into, immediately after
// the words they have typed. A caret anywhere else — one row above the box, in
// the divider's legend, in a resting sentence at the foot — is a cursor
// blinking in a box that is not the one the words are landing in.

// caretRowOf finds the one row the box is drawn on: the typed words are on
// it and the box's own prompt leads it. The words alone do not name the row —
// a settings description can quote a word of the query ("no provider will
// take the request"), and the foot's key line can quote the box's whole
// hint — so the row is told by its prompt, which no other row on the frame
// leads with.
func caretRowOf(t *testing.T, lines []string, prompt, typed string) (int, []string) {
	t.Helper()
	plainLines := make([]string, len(lines))
	for i, line := range lines {
		plainLines[i] = plain(line)
	}
	for i, line := range plainLines {
		if strings.Contains(line, typed) && strings.HasPrefix(strings.TrimSpace(line), prompt) {
			return i, plainLines
		}
	}
	t.Fatalf("no row on the frame carries %q behind the %q prompt:\n%s",
		typed, prompt, strings.Join(plainLines, "\n"))
	return -1, nil
}

// THE RECORD CARD, WHICH IS A READING AND NOT A BOX. Nothing on it is typed
// into — the card's own header says so — and a blinking bar parked at the
// frame's origin over the title is a cursor pointing at a key that does not
// exist. The caret is hidden there, by the same law the job page and home at
// rest follow.
func TestTheRecordCardHidesTheCaret(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 120, 24
	a.raisePlace(pageTasks)
	a.taskSheet = tasksPlace{detailOn: true, detail: session.TaskIndexEntry{
		ID: "1", Title: "Port the picker onto the new list",
		Status: string(session.TaskDone), DurationMS: 360000,
	}}
	a.frame()
	if a.caret {
		t.Fatal("a card with no box on it left the caret blinking over the title")
	}
}

// THE SEARCH BOX, LIVE, WITH THE NOTE RIDING THE RULE ABOVE IT. The owner's
// screenshot put the terminal's cursor mid-text on the rule's legend while the
// query sat typed in the box under it — the right column, one row high. The
// park is computed from the box's own row here, so the legend rendering above
// it cannot move the caret without moving the box too.
func TestTheSearchBoxParksItsCaretAfterTheQuery(t *testing.T) {
	a, _ := sheetApp(t)
	a.openSettings()
	a.raiseSettings()
	a.sheet.query.insert("provide")
	a.sheet.build()
	lines, _, caretX, caretY, up := a.placeFrameNow(90, 30)
	if !up {
		t.Fatal("the settings sheet did not take the frame")
	}
	boxRow, plainLines := caretRowOf(t, lines, "›", "provide")
	if caretY != boxRow {
		t.Fatalf("the caret is on row %d, want the box's row %d:\n%s",
			caretY, boxRow, strings.Join(plainLines, "\n"))
	}
	// AND IN THE COLUMN JUST PAST THE TYPED WORDS, not one short of them and
	// not one past: the next keystroke lands on the cell the caret stands on.
	before := ansi.Truncate(plainLines[caretY], caretX, "")
	if !strings.HasSuffix(before, "provide") {
		t.Fatalf("the caret stands at %d, after %q, want immediately after %q",
			caretX, before, "provide")
	}
	// AND THE NOTE RIDES THE RULE ONE ROW ABOVE THE BOX, which is the line the
	// screenshot's cursor was resting on: the foot line renders there, and the
	// park ignores it.
	if rule := plainLines[caretY-1]; !strings.Contains(rule, "saved to") {
		t.Fatalf("the rule above the box lost its note: %q", rule)
	}
}

// AND AT REST THE CARET STANDS ON THE SAME ROW, on the first cell the first
// typed character will land on — the park may not move between rest and typed,
// because the box does not (the block is the same height typed in or not).
func TestTheSearchBoxRestsItsCaretOnTheSameRow(t *testing.T) {
	a, _ := sheetApp(t)
	a.openSettings()
	a.raiseSettings()
	a.sheet.build()
	lines, _, caretX, caretY, _ := a.placeFrameNow(90, 30)
	boxRow, plainLines := caretRowOf(t, lines, "›", "say what you want done")
	if caretY != boxRow {
		t.Fatalf("the resting caret is on row %d, want the box's row %d:\n%s",
			caretY, boxRow, strings.Join(plainLines, "\n"))
	}
	// The rest park is the prompt's own first cell — the cell the first typed
	// character will land on, behind the prompt's one space.
	before := ansi.Truncate(plainLines[caretY], caretX, "")
	if !strings.HasSuffix(strings.TrimRight(before, " "), "›") {
		t.Fatalf("the resting caret stands at %d, after %q, want the prompt's first cell",
			caretX, before)
	}
}

// THE CONNECTIONS KEY ENTRY, WHICH IS A BOX INSIDE THE ROW IT WAS OPENED FROM.
// Every key goes to it while it is open ([placeSettings.owns]) — and the park
// used to stay in the search box at rest at the foot of the sheet, a blinking
// cursor in a box twelve rows away from the one the words were landing in.
func TestTheConnectionsEntryParksItsCaretInTheBox(t *testing.T) {
	rows := append([]connect.Status(nil), catalog...)
	for i := range rows {
		if !rows[i].Connected {
			rows[i].Auth = connect.AuthKey
		}
	}
	a, _ := capsApp(t, rows)
	a.sheet.conn.expanded = ""
	a.sheet.build()
	opened := false
	for _, item := range a.sheet.items {
		if item.conn == nil || item.conn.kind != connService || item.conn.connected {
			continue
		}
		a.sheet.cursor = serviceRowAt(a, item.conn.service)
		a.sheet.build()
		drive(t, a, key("enter"))
		if a.sheet.conn.entry != nil {
			opened = true
			break
		}
	}
	if !opened {
		t.Fatal("no keyed catalog row opened a box")
	}
	a.sheet.conn.entry.box.insert("sk-live-abc123")
	lines, _, caretX, caretY, _ := a.placeFrameNow(a.width, a.height)
	boxRow, plainLines := caretRowOf(t, lines, "›", "•••")
	if caretY != boxRow {
		t.Fatalf("the caret is on row %d, want the key box's row %d:\n%s",
			caretY, boxRow, strings.Join(plainLines, "\n"))
	}
	// The mask gives way to nothing on the row: the caret stands after the last
	// bullet, before the dim count, which is the only reading of "immediately
	// after the typed value" a masked box can draw.
	before := ansi.Truncate(plainLines[caretY], caretX, "")
	if !strings.HasSuffix(before, "••••") {
		t.Fatalf("the caret stands at %d, after %q, want immediately after the mask",
			caretX, before)
	}
	// AND NOT IN THE RESTING SEARCH BOX AT THE FOOT, which is where it stood
	// before the sheet learned to answer for its own row's box.
	for i, line := range plainLines {
		if strings.Contains(line, "say what you want done") && i == caretY {
			t.Fatalf("the caret is parked in the resting search box at row %d", i)
		}
	}
}

// A CHOICE HAS NO BOX: the entry showing its variable list owns the keyboard
// and has no line to type into, and the caret is hidden rather than parked on
// the first choice — the law the /connect panel already follows (input.go).
func TestAChoiceEntryHidesTheCaret(t *testing.T) {
	rows := append([]connect.Status(nil), catalog...)
	for i := range rows {
		if !rows[i].Connected {
			rows[i].Auth = connect.AuthKey
		}
	}
	a, _ := capsApp(t, rows)
	a.sheet.conn.expanded = ""
	a.sheet.build()
	for _, item := range a.sheet.items {
		if item.conn == nil || item.conn.kind != connService || item.conn.connected {
			continue
		}
		a.sheet.cursor = serviceRowAt(a, item.conn.service)
		a.sheet.build()
		drive(t, a, key("enter"))
		break
	}
	if a.sheet.conn.entry == nil {
		t.Fatal("no keyed catalog row opened a box")
	}
	a.sheet.conn.entry.choices = []entryChoice{{ID: "STRIPE_KEY", Name: "the stripe key"}}
	a.placeFrameNow(a.width, a.height)
	if a.caret {
		t.Fatal("a choice list has no box, and the caret was left blinking anyway")
	}
}

// THE TASKS FILTER, WHICH MOVED INTO THE LIST'S CONTROL ROW. The caret stayed
// behind in the foot's resting sentence when the box moved up — a cursor at
// the bottom of the frame while the letters landed rows above it. The park
// follows the box: on the control row, immediately after the typed words.
func TestTheTasksFilterParksItsCaretInTheControlRow(t *testing.T) {
	a := placeApp(t)
	runCmd(a.showPage(pageTasks))
	if a.taskSheet.reading.held == 0 {
		t.Fatal("this fixture's tasks page holds nothing")
	}
	a.taskSheet.query.insert("fix the parser")
	lines, _, caretX, caretY, _ := a.placeFrameNow(a.width, 24)
	plainLines := make([]string, len(lines))
	for i, line := range lines {
		plainLines[i] = plain(line)
	}
	// The control row is the one row carrying the typed words: no prose on
	// this place quotes a filter, and the foot draws the resting sentence only.
	boxRow := -1
	for i, line := range plainLines {
		if strings.Contains(line, "fix the parser") {
			boxRow = i
		}
	}
	if boxRow < 0 {
		t.Fatalf("the control row is not on the frame:\n%s", strings.Join(plainLines, "\n"))
	}
	if caretY != boxRow {
		t.Fatalf("the caret is on row %d, want the control row %d:\n%s",
			caretY, boxRow, strings.Join(plainLines, "\n"))
	}
	before := ansi.Truncate(plainLines[caretY], caretX, "")
	if !strings.HasSuffix(before, "fix the parser") {
		t.Fatalf("the caret stands at %d, after %q, want immediately after the typed words",
			caretX, before)
	}
	// AND NOT IN THE FOOT'S RESTING SENTENCE, which is an invitation and not
	// the box any more.
	for i, line := range plainLines {
		if strings.Contains(line, "type to filter this list") && i == caretY {
			t.Fatalf("the caret is parked in the foot's resting sentence at row %d", i)
		}
	}
}

// AND AT REST THE CARET STANDS ON THE CONTROL ROW'S FIRST CELL — the cell the
// first typed character will land on — not in the foot's invitation, which is
// an invitation and not the box any more. The control row is found by the line
// its own painter draws: the resting hint and the foot's sentence share their
// first words, so the row cannot be told apart by the hint alone.
func TestTheTasksFilterRestsItsCaretOnTheControlRow(t *testing.T) {
	a := placeApp(t)
	runCmd(a.showPage(pageTasks))
	if a.taskSheet.reading.held == 0 {
		t.Fatal("this fixture's tasks page holds nothing")
	}
	lines, _, caretX, caretY, _ := a.placeFrameNow(a.width, 24)
	plainLines := make([]string, len(lines))
	for i, line := range lines {
		plainLines[i] = plain(line)
	}
	// AND AT REST THE CONTROL ROW IS STILL THE BOX, found by the line its own
	// painter draws — the resting hint and the foot's sentence share their
	// first words, so the row cannot be told apart by the hint alone.
	control, _ := tasksControlRow("", a.taskSheet.order, taskPaneList(a.width), a.pal)
	control = plain(control)
	rest := -1
	for i, line := range plainLines {
		if strings.HasPrefix(line, control) {
			rest = i
		}
	}
	if rest < 0 {
		t.Fatalf("the control row is not on the frame:\n%s", strings.Join(plainLines, "\n"))
	}
	if caretY != rest {
		t.Fatalf("the resting caret is on row %d, want the control row %d:\n%s",
			caretY, rest, strings.Join(plainLines, "\n"))
	}
	// The cell before the caret is the box's own — the space after the filter
	// mark, which is the first cell a typed character lands on.
	before := ansi.Truncate(plainLines[caretY], caretX, "")
	if !strings.HasSuffix(before, " ") {
		t.Fatalf("the resting caret stands at %d, after %q, want the box's first cell",
			caretX, before)
	}
}
