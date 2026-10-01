package tui3

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/manual"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// THE MODEL MENU'S OWN ROW GRAMMAR. The current model is the chosen row of the
// /model overlay, and it used to wear the ladder's selected ground for it — a
// full-width band that read as a highlighted row wherever the cursor happened
// to be. The band is the CURSOR's language on an overlay ("where ↑/↓ has got
// to"), and a mark that outranked it made the chosen row look like a row the
// hand was on when it was not. So the menu's front mark keeps its accent and
// its weight and takes NO ground. The cursor and pointer lift the rows they
// reach. These tests assert the rows a person actually sees.

// modelMenuLines is the drawn menu, through the app's own door.
func modelMenuLines(a *app) []string { return a.overlayRows(a.width, a.overlayHeight()) }

// modelMenuRow returns the drawn line that carries one model's row.
func modelMenuRow(t *testing.T, a *app, id string) string {
	t.Helper()
	for _, line := range modelMenuLines(a) {
		if strings.Contains(line, id) {
			return line
		}
	}
	t.Fatalf("%s is not on the drawn model menu:\n%s", id, plain(strings.Join(modelMenuLines(a), "\n")))
	return ""
}

// groundsAreDrawn guards the assertions below against a palette that paints no
// background at all: on the NoColor tier there is no ground to take off, so
// "no ground" would be true of every row and prove nothing.
func groundsAreDrawn(a *app) bool { return a.pal.profile != tokens.NoColor }

func TestTheCurrentModelKeepsBoldAccentButNoGround(t *testing.T) {
	a := pickerApp(t, &fakeAgent{model: "openai/gpt-4.1-mini"}, pickerCatalog)
	typeLine(t, a, "/model")
	// Move away from the held model, so its persistent mark can be read
	// independently of the keyboard cursor's band.
	drive(t, a, key("up"), key("up"), key("up"))
	marked := modelMenuRow(t, a, "openai/gpt-4.1-mini")
	// BOLD BLUE, RETAINED AND NOW EXPLICIT: the label keeps the accent and the
	// weight it read at under the band.
	want := a.pal.bold(a.pal.accent("openai/gpt-4.1-mini"))
	if !strings.Contains(marked, want) {
		t.Fatalf("the current model's row lost its bold accent; want %q in:\n%s", want, plain(marked))
	}
	// AND NO GROUND. A background run on the row would be the highlighted row
	// the menu must not wear.
	if groundsAreDrawn(a) && strings.Contains(marked, "\x1b[48;") {
		t.Fatalf("the current model's row wears a ground:\n%q", marked)
	}

	// AND THE CURSOR'S HIGHLIGHT IS ON THE SCREEN AFTER THE WALK: on the
	// list's first row, and on no other.
	if a.pick.cursor != 0 {
		t.Fatalf("the walk left the cursor on row %d, want the first", a.pick.cursor)
	}
	var grounded []string
	for _, line := range modelMenuLines(a) {
		if strings.Contains(line, "\x1b[48;") {
			grounded = append(grounded, line)
		}
	}
	if groundsAreDrawn(a) {
		if len(grounded) != 1 || !strings.Contains(grounded[0], "anthropic/claude-gpt-echo") {
			t.Fatalf("the frame carries the cursor's ground on %d rows, want the first row alone:\n%s",
				len(grounded), plain(strings.Join(modelMenuLines(a), "\n")))
		}
	}

	// AND THE CURSOR ON THE MARKED ROW STILL LIFTS IT — the mark must not have
	// made the row unhighlightable, only unhighlighted on its own. The walk
	// down from the list's first row reaches the model in use on its own row.
	drive(t, a, key("down"), key("down"), key("down"))
	both := modelMenuRow(t, a, "openai/gpt-4.1-mini")
	if groundsAreDrawn(a) && !strings.Contains(both, "\x1b[48;") {
		t.Fatalf("the cursor on the current model's row lifts nothing:\n%q", both)
	}
	if !strings.Contains(both, want) {
		t.Fatalf("the marked row under the cursor lost its bold accent:\n%q", plain(both))
	}
}

func TestTheModelPickerOwnsTheWheelAndClampsAtBothEnds(t *testing.T) {
	// A TRANSCRIPT THAT CAN SCROLL, so "the transcript did not move" is a real
	// assertion and not the accident of a short conversation.
	a := benchApp(20)
	a.models = func() []Model { return pickerCatalog }
	a.model = "openai/gpt-4.1-mini"
	a.frame()
	// Park the transcript away from the live edge first, with the picker shut:
	// a notch up on the bare frame is the gesture that used to leak past the
	// open menu.
	drive(t, a, tea.MouseWheelMsg{X: 4, Y: a.bodyTop() + 1, Button: tea.MouseWheelUp})
	if a.offset == 0 {
		t.Fatal("the fixture transcript is too short to scroll")
	}
	a.openPicker()
	offset := a.offset

	last := len(a.pick.list) - 1
	// Far more notches than the list has rows: the walk must stop at the end
	// and never hand the wheel to the conversation behind the menu.
	for i := 0; i < 20; i++ {
		drive(t, a, tea.MouseWheelMsg{X: 4, Y: a.bodyTop() + 1, Button: tea.MouseWheelDown})
	}
	if a.pick.cursor != last {
		t.Fatalf("the cursor rests on %d of %d rows, want the last", a.pick.cursor, last)
	}
	if a.offset != offset {
		t.Fatalf("the wheel moved the transcript behind the menu: %d → %d", offset, a.offset)
	}
	if a.pick.top > last {
		t.Fatalf("the list window ran past the end: top %d of %d rows", a.pick.top, last)
	}
	// The list really did move for the wheel — a clamp on a list the wheel
	// cannot move would prove nothing.
	if a.pick.cursor == 0 && a.pick.top == 0 {
		t.Fatal("the wheel never moved the list, so the clamp proves nothing")
	}

	for i := 0; i < 20; i++ {
		drive(t, a, tea.MouseWheelMsg{X: 4, Y: a.bodyTop() + 1, Button: tea.MouseWheelUp})
	}
	if a.pick.cursor != 0 {
		t.Fatalf("the cursor rests on %d, want the first row", a.pick.cursor)
	}
	if a.pick.top != 0 {
		t.Fatalf("the list window rests at top %d, want 0", a.pick.top)
	}
	if a.offset != offset {
		t.Fatalf("the wheel moved the transcript behind the menu going up: %d → %d", offset, a.offset)
	}
}

func TestThePickerCursorStopsAtTheEndsOfTheList(t *testing.T) {
	a := pickerApp(t, &fakeAgent{model: "openai/gpt-4.1-mini"}, pickerCatalog)
	typeLine(t, a, "/model")
	last := len(a.pick.list) - 1

	for i := 0; i < 30; i++ {
		drive(t, a, key("down"))
	}
	if a.pick.cursor != last {
		t.Fatalf("thirty downs left the cursor on %d of %d rows, want the last", a.pick.cursor, last)
	}
	// AND THE WINDOW STAYS INSIDE THE LIST: a page key is a bigger step, and a
	// window that followed it past the last row would draw nothing at all.
	drive(t, a, key("pgdown"))
	if a.pick.cursor != last {
		t.Fatalf("a page down past the end moved the cursor to %d, want the last", a.pick.cursor)
	}
	if a.pick.top < 0 || a.pick.top > last {
		t.Fatalf("the list window left the list: top %d of %d rows", a.pick.top, last)
	}

	for i := 0; i < 30; i++ {
		drive(t, a, key("up"))
	}
	if a.pick.cursor != 0 {
		t.Fatalf("thirty ups left the cursor on %d, want the first row", a.pick.cursor)
	}
	drive(t, a, key("pgup"))
	if a.pick.cursor != 0 {
		t.Fatalf("a page up past the top moved the cursor to %d, want the first", a.pick.cursor)
	}
	// The walk is honest both ways: one down from the top reaches the second
	// row, so the clamps above are not a list that lost its motion.
	drive(t, a, key("down"))
	if a.pick.cursor != 1 {
		t.Fatalf("one down from the top rests on %d, want the second row", a.pick.cursor)
	}
}

// A model list owns the whole wheel gesture while it is up. The page's cursor,
// window and transcript must stay where they were until the list closes.
func checkModelDoorWheel(t *testing.T, a *app, p *picker, pageState func() [3]int, closeList func()) {
	t.Helper()
	p.move(-len(p.list))
	before := pageState()
	wheel := func(button tea.MouseButton) {
		drive(t, a, tea.MouseWheelMsg{X: 4, Y: a.bodyTop() + 3, Button: button})
	}
	wheel(tea.MouseWheelDown)
	if p.cursor != 3 {
		t.Fatalf("wheel left model cursor at %d, want 3", p.cursor)
	}
	if got := pageState(); got != before {
		t.Fatalf("wheel moved page beneath list: %v -> %v", before, got)
	}
	for i := 0; i < len(p.list); i++ {
		wheel(tea.MouseWheelDown)
	}
	if p.cursor != len(p.list)-1 {
		t.Fatalf("wheel did not clamp at the last row: %d", p.cursor)
	}
	for i := 0; i < len(p.list); i++ {
		wheel(tea.MouseWheelUp)
	}
	if p.cursor != 0 {
		t.Fatalf("wheel did not clamp at the first row: %d", p.cursor)
	}
	if got := pageState(); got != before {
		t.Fatalf("clamped wheel moved page beneath list: %v -> %v", before, got)
	}
	closeList()
	wheel(tea.MouseWheelDown)
	afterDown := pageState()
	wheel(tea.MouseWheelUp)
	if afterDown == before && pageState() == before {
		t.Fatal("closed list still prevented the page from moving")
	}
}

func TestTheSettingsModelListOwnsTheWheel(t *testing.T) {
	a, _ := sheetApp(t)
	a.models = func() []Model { return groupedCatalog }
	a.openSettings()
	for i := 0; i < 4; i++ {
		drive(t, a, key("right"))
	}
	cursorTo(t, a, config.ModelSettingKey("talk"))
	drive(t, a, key("enter"))
	if a.sheet.sel == nil {
		t.Fatal("settings row opened no list")
	}
	checkModelDoorWheel(t, a, &a.sheet.sel.pick,
		func() [3]int { return [3]int{a.sheet.cursor, a.sheet.top, a.offset} },
		func() { drive(t, a, key("esc")) })
}

func TestTheHomeDraftModelListOwnsTheWheel(t *testing.T) {
	a := placeApp(t)
	a.models = func() []Model { return groupedCatalog }
	a.showPage(pageHome)
	a.openTargetPicker()
	checkModelDoorWheel(t, a, &a.target.pick,
		func() [3]int { return [3]int{a.home.cursor, a.home.top, a.offset} },
		func() { drive(t, a, key("esc")) })
}

func TestTheComposerModelListOwnsTheWheel(t *testing.T) {
	a := layerApp(t, pageHome, 120)
	a.models = func() []Model { return groupedCatalog }
	a.placeKeyPress(key("alt+o"))
	if !a.composer.pick.open {
		t.Fatal("alt+o opened no list")
	}
	checkModelDoorWheel(t, a, &a.composer.pick,
		func() [3]int { return [3]int{a.home.cursor, a.home.top, a.offset} },
		func() { drive(t, a, key("esc"), key("esc")) })
}

// An opened block is placed by what a person can read, including its heading
// and reason line. A fitting block must preserve the rows already above it.
func foldScrollFixture() picker {
	var p picker
	p.start(groupedCatalog, "alpha/model-4")
	p.unfold, p.machines, p.width = "alpha/model-4", true, 100
	p.relist()
	p.cursor = 4 // The auto row is the first confirmation inside this fold.
	p.top = 0
	return p
}

func TestOpeningAVisibleFoldKeepsTheRowsAboveIt(t *testing.T) {
	p := foldScrollFixture()
	p.revealFold(12)
	if p.top != 0 {
		t.Fatalf("already visible fold scrolled top 0 -> %d", p.top)
	}
	rows, owners := p.rowsOwned(100, 12, newPalette(tokens.ANSI256, false), -1, nil)
	if !slices.Contains(owners, p.cursor) || !strings.Contains(plain(strings.Join(rows, "\n")), "alpha/model-1") {
		t.Fatalf("opening fold lost the cursor or preceding context: %s", plain(strings.Join(rows, "\n")))
	}
}

func TestOpeningAFoldBelowTheWindowPutsItsEndAtTheBottom(t *testing.T) {
	p := foldScrollFixture()
	p.revealFold(7)
	rows, owners := p.rowsOwned(100, 7, newPalette(tokens.ANSI256, false), -1, nil)
	shown := plain(strings.Join(rows, "\n"))
	if len(rows) != 7 || !strings.Contains(shown, "alpha/model-3") || !strings.Contains(plain(rows[len(rows)-1]), "default") {
		t.Fatalf("fold below the window did not end at its bottom with preceding context:\n%s", shown)
	}
	if !slices.Contains(owners, p.cursor) {
		t.Fatal("fold placement hid the cursor")
	}
}

func TestOpeningATallFoldStartsAtItsModelAndShowsTheCursor(t *testing.T) {
	p := foldScrollFixture()
	p.revealFold(4)
	rows, owners := p.rowsOwned(100, 4, newPalette(tokens.ANSI256, false), -1, nil)
	if p.top != 3 || !strings.Contains(plain(strings.Join(rows, "\n")), "alpha/model-4") || !slices.Contains(owners, p.cursor) {
		t.Fatalf("tall fold lost its model or cursor: top=%d owners=%v", p.top, owners)
	}
}

// A person reading the manual before pressing Enter must get the same safe
// opening rule as the menu, including the lists reached through other doors.
func TestTheModelMenuManualExplainsHeldModelsAndWheelOwnership(t *testing.T) {
	for _, page := range []string{"commands", "models-and-cost"} {
		text, ok := manual.Chat().Page(page)
		if !ok {
			t.Fatalf("missing manual page %s", page)
		}
		text = strings.Join(strings.Fields(text), " ")
		if !strings.Contains(text, "Every model list opens on the model it holds") {
			t.Errorf("%s does not explain that every model list opens on the model it holds", page)
		}
	}
	text, _ := manual.Chat().Page("keys")
	text = strings.Join(strings.Fields(text), " ")
	if !strings.Contains(text, "home's draft list and the task composer's list") {
		t.Error("model-list wheel help omits home's draft list and the task composer's list")
	}
}
