package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE TWO-LINE LAW, at [tierPhone]: every list on this surface — the model
// picker, the session picker, the command list, the settings rows, the file and
// task completion — puts its dim tail on a line of its own rather than cutting
// the row in half twice (palette.go's [overlayLines]).
//
// Every test here is a 44-column frame, which is a phone terminal, and the ones
// that assert the law's OTHER half are 60, 80 and 120: those tiers draw exactly
// what they always drew, byte for byte.

// phoneWidth is the frame these tests are about — under [layoutTier]'s 60, and
// about what a phone keyboard leaves of a phone screen.
const phoneWidth = 44

// phoneCatalog carries a note on every row: a window, a price and an arena
// score is forty cells of tail against a thirty-cell id, which is the row the
// old law could not draw.
var phoneCatalog = []Model{
	{ID: "anthropic/claude-sonnet-4.5", ContextLength: 200_000, PromptPrice: 3e-6, CompletionPrice: 15e-6, ArenaElo: 1300, Reasoning: true},
	{ID: "openai/gpt-4.1-mini", ContextLength: 1_000_000, PromptPrice: 0.4e-6, CompletionPrice: 1.6e-6, ArenaElo: 1250},
	{ID: "moonshotai/kimi-k3", ContextLength: 128_000},
	{ID: "gpt-5-classic", ContextLength: 400_000, ArenaElo: 1400},
}

// phonePicker is the model overlay open on a phone-sized frame.
func phonePicker(t *testing.T, width int) *app {
	t.Helper()
	a := pickerApp(t, &fakeAgent{model: "openai/gpt-4.1-mini"}, phoneCatalog)
	a.width, a.height = width, 30
	typeLine(t, a, "/model")
	return a
}

// overlayBlock is the open list as the frame draws it, in the rows the frame
// reserved for it — the two numbers this whole slice has to keep in step.
func overlayBlock(a *app) []string {
	return a.overlayRows(a.width, a.overlayHeight())
}

// banded reports whether a line carries the selection background, and hovered
// the pointer's. Both are asserted by their escape sequence rather than by the
// row's text: the point of a band is that it spans the row, and a two-line row
// that banded only its first line would still read correctly as text.
func banded(pal palette, line string) bool { return strings.HasPrefix(line, bandLead(pal)) }

func bandLead(pal palette) string {
	lead, _, _ := strings.Cut(pal.band(" ", 1), " ")
	return lead
}

func hoverLead(pal palette) string {
	lead, _, _ := strings.Cut(pal.hover(" ", 1), " ")
	return lead
}

// ── the row ─────────────────────────────────────────────────────────────────

// THE ID GETS THE LINE AND THE FACTS GET THE ONE UNDER IT. Nothing truncates to
// meaninglessness: the whole id is there, and so is the whole tail.
func TestAPhonePickerRowWrapsItsNoteOntoASecondLine(t *testing.T) {
	a := phonePicker(t, phoneWidth)
	lines := overlayBlock(a)

	at := -1
	for i, line := range lines {
		if strings.Contains(plain(line), "anthropic/claude-sonnet-4.5") {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("the model's own row is not on the list:\n%s", strings.Join(plainAll(lines), "\n"))
	}
	head, tail := plain(lines[at]), plain(lines[at+1])
	if strings.TrimSpace(head) != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("the first line is not the id alone: %q", head)
	}
	if strings.Contains(head, "200k") {
		t.Fatalf("the tail is still sharing the id's line: %q", head)
	}
	// The tail hangs two cells in from the label, which sits two cells in from
	// the lead — so a wrapped row reads as one thing rather than as two rows.
	if !strings.HasPrefix(tail, strings.Repeat(" ", overlayIndent)) {
		t.Fatalf("the tail is not indented under the label: %q", tail)
	}
	for _, want := range []string{"200k", "$3/$15 per M", "elo 1300"} {
		if !strings.Contains(tail, want) {
			t.Fatalf("the tail lost %q: %q", want, tail)
		}
	}
	if strings.Contains(tail, glyphMore) {
		t.Fatalf("the tail is still being truncated: %q", tail)
	}
}

// A ROW WITH NO TAIL STAYS ONE LINE. A blank second line under every path would
// spend half a phone screen saying nothing.
func TestAPhoneRowWithNoTailStaysOneLine(t *testing.T) {
	lines := overlayLines("internal/tui3/palette.go", "", false, false, false, phoneWidth, newTestPalette())
	if len(lines) != 1 {
		t.Fatalf("a row with no note drew %d lines: %q", len(lines), lines)
	}
}

// THE SELECTED ROW IS ONE BAND OVER BOTH LINES. A cursor that painted the name
// and left the facts under it unbanded would read as a cursor on half a row.
func TestThePhoneSelectionBandSpansBothLinesOfARow(t *testing.T) {
	a := phonePicker(t, phoneWidth)
	lines := overlayBlock(a)

	at := -1
	for i, line := range lines {
		if banded(a.pal, line) {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("no row is banded at all:\n%s", strings.Join(plainAll(lines), "\n"))
	}
	if at+1 >= len(lines) || !banded(a.pal, lines[at+1]) {
		t.Fatalf("the band stops at the first line of the row: %q", plain(lines[at]))
	}
	if at+2 < len(lines) && banded(a.pal, lines[at+2]) {
		t.Fatal("the band ran past the row it belongs to")
	}
}

// THE POINTER OVER EITHER LINE IS OVER THE ROW. The frame records the pointer
// as a screen line (view.go's chromeOverlay); a row that is two of them has to
// answer to both, or half of every row is dead to the mouse.
func TestThePointerOverEitherLineLightsTheWholeRow(t *testing.T) {
	a := phonePicker(t, phoneWidth)
	// The FIRST row of the block, whichever of its two lines the pointer is on.
	// It is not the cursor's row — the picker opens on the model in use, which
	// is the second — because a band and a hover on one row would make either
	// answer look right.
	for _, on := range []int{0, 1} {
		a.hot = hoverAt{kind: hoverOverlay, index: on}
		lines := overlayBlock(a)
		lead := hoverLead(a.pal)
		if !strings.HasPrefix(lines[0], lead) || !strings.HasPrefix(lines[1], lead) {
			t.Fatalf("the pointer on line %d lit only part of the row:\n%q\n%q", on, lines[0], lines[1])
		}
		if strings.HasPrefix(lines[2], lead) {
			t.Fatalf("the pointer on line %d lit the row under it too", on)
		}
	}
}

// ── the block ───────────────────────────────────────────────────────────────

// THE LIST IS EXACTLY AS TALL AS IT SAID IT WOULD BE. The frame subtracts that
// number from the conversation before the list is drawn ([app.overlayHeight]),
// so a block that came back short would leave the frame short of the terminal.
func TestThePhoneOverlayFillsTheRowsItReserved(t *testing.T) {
	for _, width := range []int{phoneWidth, 52, 60, 80, 120} {
		a := phonePicker(t, width)
		if want, got := a.overlayHeight(), len(overlayBlock(a)); want != got {
			t.Fatalf("at %d columns the list reserved %d rows and drew %d", width, want, got)
		}
		// And the same once the cursor has walked to the bottom of the list.
		for range phoneCatalog {
			drive(t, a, key("down"))
		}
		if want, got := a.overlayHeight(), len(overlayBlock(a)); want != got {
			t.Fatalf("at %d columns, cursor at the end: reserved %d, drew %d", width, want, got)
		}
	}
}

// A PAIR IS NEVER SPLIT BY THE BOTTOM EDGE, and the cursor's row is always one
// of the whole ones. The window is handed an ODD number of rows here, which is
// the case that can only be answered by dropping a row rather than by drawing
// half of it.
func TestThePhoneWindowKeepsThePairWhole(t *testing.T) {
	a := phonePicker(t, phoneWidth)
	for _, n := range []int{1, 3, 5, 7} {
		lines := a.pick.rows(a.width, n, a.pal, -1, a.reasoningFor)
		if len(lines) != n {
			t.Fatalf("a %d-row window drew %d rows", n, len(lines))
		}
		// A window with one row is the one case that cannot hold a pair, and it
		// draws the wider frames' single line rather than nothing at all.
		if n == 1 && strings.TrimSpace(plain(lines[0])) == "" {
			t.Fatal("a one-row window opened onto a blank")
		}
		for i, line := range lines {
			text := plain(line)
			if strings.TrimSpace(text) == "" {
				continue // the blank under the last whole row
			}
			// A tail with no label above it is the split this rules out.
			if strings.HasPrefix(text, strings.Repeat(" ", overlayIndent)) && i == 0 {
				t.Fatalf("a %d-row window opened on an orphaned tail: %q", n, text)
			}
		}
	}
}

// THE CURSOR'S ROW IS INSIDE THE WINDOW WHOLE, at the edge it was scrolled to.
// The band is the assertion: both of its lines are on screen, so the row a
// person is about to press with enter is one they can read.
func TestThePhoneCursorRowFitsWholeAtTheWindowEdge(t *testing.T) {
	a := phonePicker(t, phoneWidth)
	// Six rows holds three wrapped models; the catalog has four, so the cursor
	// walking to the last one has to scroll.
	const n = 6
	for range phoneCatalog {
		drive(t, a, key("down"))
	}
	lines := a.pick.rows(a.width, n, a.pal, -1, a.reasoningFor)
	at := -1
	for i, line := range lines {
		if banded(a.pal, line) {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("the cursor's row scrolled out of the window:\n%s", strings.Join(plainAll(lines), "\n"))
	}
	if at+1 >= len(lines) || !banded(a.pal, lines[at+1]) {
		t.Fatalf("the cursor's row is cut by the window's edge: %q", plain(lines[at]))
	}
	if got := plain(lines[at]); !strings.Contains(got, "gpt-5-classic") {
		t.Fatalf("the cursor is on %q, want the last model", got)
	}
}

// ── the other tiers ─────────────────────────────────────────────────────────

// EVERY OTHER TIER DRAWS EXACTLY WHAT IT ALWAYS DREW. The wide, standard and
// narrow frames are asserted against [overlayRow] itself — the one-line law,
// unchanged — byte for byte, escape sequences included.
func TestTheWiderTiersAreByteIdenticalToTheOneLineLaw(t *testing.T) {
	for _, width := range []int{120, 80, 60} {
		a := phonePicker(t, width)
		lines := overlayBlock(a)
		if len(lines) != len(phoneCatalog) {
			t.Fatalf("at %d columns the list is %d rows for %d models", width, len(lines), len(phoneCatalog))
		}
		for i, line := range lines {
			model := a.pick.all[a.pick.hits[i]]
			label, note := a.pick.rowText(model, a.reasoningFor(model.ID), width)
			want := overlayRow(label, note, i == a.pick.cursor, model.ID == a.pick.current, false, width, a.pal)
			if line != want {
				t.Fatalf("at %d columns row %d changed:\n got %q\nwant %q", width, i, line, want)
			}
		}
	}
}

// ── the same law, everywhere a list appears ─────────────────────────────────

// THE COMMAND LIST. "/settings   open the settings panel · ctrl+," is one line
// on a laptop and two on a phone.
func TestThePhoneCommandListWrapsItsDescription(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "openai/gpt-4.1-mini"})
	a.width, a.height = phoneWidth, 30
	drive(t, a, key("/"), key("s"), key("e"))
	if !a.menu.open {
		t.Fatal("a typed / has to open the command list")
	}
	lines := plainAll(overlayBlock(a))
	at := -1
	for i, line := range lines {
		if strings.Contains(line, "/settings") {
			at = i
			break
		}
	}
	if at < 0 || at+1 >= len(lines) {
		t.Fatalf("the /settings row is not on the list:\n%s", strings.Join(lines, "\n"))
	}
	if strings.Contains(lines[at], "open the settings") {
		t.Fatalf("the description is still on the command's line: %q", lines[at])
	}
	if !strings.Contains(lines[at+1], "open the settings panel · ctrl+,") {
		t.Fatalf("the description was not wrapped whole: %q", lines[at+1])
	}
}

// THE SESSION PICKER. The name leads and what was last happening in it — the
// half of the row that used to be cut to nothing by a long name — goes under.
func TestThePhoneResumeRowWrapsTheSentenceAndTheAge(t *testing.T) {
	a, _ := rosterApp(t, &fakeAgent{model: "openai/gpt-4.1-mini"}, rosterSessions())
	a.width, a.height = phoneWidth, 30
	typeLine(t, a, "/resume")
	if !a.roster.open {
		t.Fatal("/resume has to open the session picker")
	}
	lines := plainAll(overlayBlock(a))
	at := -1
	for i, line := range lines {
		if strings.Contains(line, "Port the Resume Picker to Tui3") {
			at = i
			break
		}
	}
	if at < 0 || at+1 >= len(lines) {
		t.Fatalf("the named session is not on the list:\n%s", strings.Join(lines, "\n"))
	}
	tail := lines[at+1]
	if !strings.HasPrefix(tail, strings.Repeat(" ", overlayIndent)) {
		t.Fatalf("the tail is not indented under the name: %q", tail)
	}
	if !strings.Contains(tail, "run the migration") || !strings.Contains(tail, "2h ago") {
		t.Fatalf("the tail lost the sentence or the age: %q", tail)
	}
}

// THE FILE AND TASK COMPLETION. A picture's tag is a tail and wraps; a plain
// path has none and stays one line.
func TestThePhoneCompletionWrapsOnlyTheRowsWithATail(t *testing.T) {
	a, _, _ := attachLab(t, map[string]int{"shot.png": 12, "notes.md": 12})
	a.width, a.height = phoneWidth, 30
	// Two letters is what opens the list ([completeMin]), and "ot" is in both
	// names — one picture and one file that is not one.
	drive(t, a, key("@"), key("o"), key("t"))
	drive(t, a, filesLoadedMsg{paths: []string{"shot.png", "notes.md"}})

	lines := plainAll(overlayBlock(a))
	picture, plainPath := false, false
	for i, line := range lines {
		switch {
		case strings.Contains(line, "shot.png"):
			picture = true
			if strings.Contains(line, imageTag) {
				t.Fatalf("the tag is still on the path's line: %q", line)
			}
			if i+1 >= len(lines) || !strings.Contains(lines[i+1], imageTag) {
				t.Fatalf("the picture's tag did not wrap under it: %q", line)
			}
		case strings.Contains(line, "notes.md"):
			plainPath = true
			if i+1 < len(lines) && strings.HasPrefix(lines[i+1], strings.Repeat(" ", overlayIndent)) &&
				strings.TrimSpace(lines[i+1]) != "" {
				t.Fatalf("a path with no tail drew a second line: %q", lines[i+1])
			}
		}
	}
	if !picture || !plainPath {
		t.Fatalf("the list is missing the rows this is about:\n%s", strings.Join(lines, "\n"))
	}
}

// THE SETTINGS ROWS. The value — which is the whole of what the row is about,
// and the first thing a narrow row used to cut — moves under the name.
func TestThePhoneSettingsRowPutsTheValueUnderTheName(t *testing.T) {
	a, dir := sheetApp(t)
	a.width, a.height = phoneWidth, 30
	a.openSettings()
	cursorTo(t, a, config.KeyToolApprovalMode)

	lines, hits, _, _ := a.sheetFrame(a.width, a.height)
	at := -1
	for i := range lines {
		if hits[i].kind == sheetHitRow && hits[i].index == a.sheet.cursor {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatal("the cursor's row is not on the panel")
	}
	if at+1 >= len(lines) || hits[at+1].kind != sheetHitRow || hits[at+1].index != a.sheet.cursor {
		t.Fatalf("the value's line answers to something else than its own row: %+v", hits[at+1])
	}
	item, _ := a.sheet.current()
	value := item.row.Value()
	if value == "" {
		value = "—"
	}
	head, tail := plain(lines[at]), plain(lines[at+1])
	if strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(head), "›")) != item.meta.label {
		t.Fatalf("the first line is not the name alone: %q", head)
	}
	if !strings.Contains(tail, value) {
		t.Fatalf("the value %q did not wrap under the name: %q", value, tail)
	}
	// AND A PRESS ON EITHER LINE IS A PRESS ON THE ROW: the row is already
	// selected, so the second press on either of its lines answers it.
	before := config.ToolApprovalModeAt(dir)
	a.sheetPress(2, at+1)
	if got := config.ToolApprovalModeAt(dir); got == before {
		t.Fatalf("a press on the value's line did nothing: still %q", got)
	}
}

// THE SETTINGS PANEL'S OWN MODEL PICKER — the /model list drawn inside a slot
// row (settings.go's [sheet.selectLines]) — resolves a click to the model that
// was pressed, and not to the one a line further down.
func TestThePhoneSettingsPickerResolvesTheRowThatWasPressed(t *testing.T) {
	a, _ := sheetApp(t)
	a.width, a.height = phoneWidth, 30
	a.models = func() []Model { return phoneCatalog }
	a.openSettings()
	cursorToModelSlot(t, a)
	a.activate()
	if a.sheet.sel == nil {
		t.Fatal("the model row did not open its picker")
	}

	_, hits, _, _ := a.sheetFrame(a.width, a.height)
	for y, hit := range hits {
		if hit.kind != sheetHitOption || hit.index != 1 {
			continue
		}
		a.sheet.sel.pick.cursor = 0
		a.sheetPress(2, y)
		if a.sheet.sel.pick.cursor != 1 {
			t.Fatalf("a press on line %d selected hit %d, want 1", y, a.sheet.sel.pick.cursor)
		}
		return
	}
	t.Fatal("the second model has no line to press")
}

// ── the frame around them ───────────────────────────────────────────────────

// A WRAPPED LIST STILL LEAVES THE FRAME WHOLE: the status line, the box and a
// row of conversation all survive a phone-sized terminal with a list open.
func TestThePhoneFrameSurvivesAWrappedList(t *testing.T) {
	a := phonePicker(t, phoneWidth)
	a.height = 14
	lines := strings.Split(frame(a), "\n")
	if len(lines) != a.height {
		t.Fatalf("the frame is %d rows on a %d-row terminal", len(lines), a.height)
	}
	if !strings.Contains(plain(lines[len(lines)-1]), "gpt-4.1-mini") {
		t.Fatalf("the status line was pushed off the frame: %q", plain(lines[len(lines)-1]))
	}
}

// cursorToModelSlot walks the panel onto the first row that opens the model
// picker, on whichever tab holds one.
func cursorToModelSlot(t *testing.T, a *app) {
	t.Helper()
	for tab := range settingTabs {
		a.sheet.tab = tab
		a.sheet.cursor, a.sheet.top = 0, 0
		a.sheet.build()
		for i, item := range a.sheet.items {
			if !item.heading() && item.row.Kind == config.SettingModel {
				a.sheet.cursor = i
				return
			}
		}
	}
	t.Fatal("no model slot row on any tab")
}

// plainAll strips a block of lines.
func plainAll(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, plain(line))
	}
	return out
}

// newTestPalette is the palette every test here paints with.
func newTestPalette() palette { return newPalette(tokens.ANSI256, false) }
