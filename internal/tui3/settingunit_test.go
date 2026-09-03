package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// sheetRowsDrawn is every setting row of every tab as a reader sees it — the
// label, the gutter and the value, with the escape codes taken off.
func sheetRowsDrawn(t *testing.T, a *app, width int) []string {
	t.Helper()
	out := make([]string, 0, 64)
	was := a.sheet.tab
	defer func() {
		a.sheet.tab = was
		a.sheet.cursor, a.sheet.top = 0, 0
		a.sheet.build()
	}()
	for tab := range settingTabs {
		a.sheet.tab = tab
		a.sheet.cursor, a.sheet.top = 0, 0
		a.sheet.build()
		for _, item := range a.sheet.items {
			if item.row.Key == "" {
				continue
			}
			for _, line := range a.sheet.rowLines(item, false, false, width, a.pal) {
				out = append(out, strings.TrimSpace(plain(line)))
			}
		}
	}
	return out
}

// A SETTINGS ROW DRAWS ITS UNIT, so a whole tab of numbers can be decided
// without moving the cursor onto each one.
//
// Eleven rows across five tabs used to be a name and a unitless number —
// `ssh reuse 300`, `answer room 65536`, `memory floor 1536` — with the unit
// living only in the one sentence under whichever row the cursor happened to be
// on. The unit is the registry's now ([config.Setting.Unit]), so it is written
// once beside the default and every surface that draws the number gets it.
func TestEverySettingRowDrawsWhatItsNumberMeans(t *testing.T) {
	a, _ := sheetApp(t)
	a.openSettings()
	drawn := sheetRowsDrawn(t, a, 120)

	for _, want := range []string{
		"ssh reuse", "300s",
		"ssh heartbeat", "3s",
		"approval countdown", "10s",
		"background after", "30s",
		"task countdown", "15s",
		"compact at", "60%",
		"answer room", "65536 tok",
		"working set", "160000 tok",
		"context reuse", "250%",
		"memory floor", "1536 MB",
		"busy machine", "1.5 per core",
		"tenure after", "3 clean firings",
	} {
		found := false
		for _, row := range drawn {
			found = found || strings.Contains(row, want)
		}
		if !found {
			t.Fatalf("no settings row draws %q\nthe rows drawn were:\n  %s",
				want, strings.Join(drawn, "\n  "))
		}
	}

	// AND NO ROW IS LEFT AS A BARE FIGURE. The registry's own test holds the
	// law; this one holds the panel to it, because a row could still lose its
	// unit on the way to the screen.
	for _, row := range drawn {
		fields := strings.Fields(row)
		if len(fields) < 2 {
			continue
		}
		tail := fields[len(fields)-1]
		if !bareFigure(tail) {
			continue
		}
		key := strings.Join(fields[:len(fields)-1], " ")
		if strings.Contains(key, "rounds") || strings.Contains(key, "tasks") ||
			strings.Contains(key, "heartbeats") || strings.HasPrefix(row, "$") {
			// The label names what is counted, which is the other way a row may
			// answer ([config.UnitInLabel]).
			continue
		}
		t.Fatalf("the row %q ends in a bare figure — %s what?\n"+
			"  drawn: %s\n"+
			"  want:  %s <unit>   — declare a Unit on its registry row", row, tail, row, row)
	}
}

func bareFigure(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
}

// THE SSH ROWS ARE ON THE TAB A PERSON WOULD OPEN TO FIND THEM.
//
// They sat under `Session`, which is the conversation in front of the reader —
// while every one of them lands NEXT LAUNCH and belongs to the machine. A
// developer whose `--host` link keeps dropping opens the tab about what this
// machine reaches on your behalf, and that is Workspace, where the Google and
// Slack sign-in rows already answer the same question about a service. Nothing
// stored moves: the keys are untouched and only the tab a row is drawn under
// changed.
func TestTheSshRowsAreOnTheTabAboutReachingAnotherMachine(t *testing.T) {
	for _, key := range []string{
		config.KeySSHControlPersist, config.KeySSHServerAlive,
		config.KeySSHServerMisses, config.KeySSHIPQoS,
	} {
		meta, ok := settingUI[key]
		if !ok {
			t.Fatalf("row %q has no place on the panel at all", key)
		}
		if meta.tab != tabWorkspace {
			t.Fatalf("row %q is drawn under %q\n  drawn: %s tab · %s\n  want:  %s tab · %s",
				key, meta.tab, meta.tab, meta.label, tabWorkspace, meta.label)
		}
	}
}

// THE TAB STRIP FOLLOWS THE CURSOR, AT EVERY WIDTH THIS SURFACE IS DRAWN AT.
//
// It was built from the first chip and cut on the right, so at eighty columns
// standing on Providers or Connections the accent was on a chip that had been
// cut off the end and NO TAB WAS INKED ANYWHERE — the screen stopped telling a
// person where they were standing, and the two tabs holding every third-party
// account were never seen by anyone on a laptop split pane.
func TestTheSettingsTabStripAlwaysInksTheTabYouAreStandingOn(t *testing.T) {
	pal := newPalette(tokens.ANSI256, false)
	band := "\x1b[48;5;" + itoa(int(hueSelected.idx)) + "m"
	for _, width := range []int{160, 120, 80, 60, 40} {
		for active, title := range settingTabs {
			bar := sheetTabBar(width, active, pal)
			flat := plain(bar)
			if got := ansi.StringWidth(flat); got > width {
				t.Fatalf("at %d columns the strip is %d cells wide:\n  %q", width, got, flat)
			}
			if !strings.Contains(flat, " "+title+" ") {
				t.Fatalf("at %d columns, standing on %q, the strip does not draw it\n"+
					"  drawn: %q\n  want:  a strip carrying the chip ` %s `", width, title, flat, title)
			}
			if !strings.Contains(bar, band) {
				t.Fatalf("at %d columns, standing on %q, no chip is inked\n  drawn: %q",
					width, title, flat)
			}
			// And the chip the pointer would resolve to is that same chip.
			spans := tabSpans(width, active)
			span := spans[active]
			if span.to <= span.from {
				t.Fatalf("at %d columns the open tab %q has no span", width, title)
			}
			for _, x := range []int{span.from, span.to - 1} {
				if at, ok := tabAtColumn(x, width, active); !ok || at != active {
					t.Fatalf("at %d columns, column %d of %q resolved to %d (ok=%v)",
						width, x, title, at, ok)
				}
			}
			// Sliced by CELLS and not by bytes: the cut mark is one cell and
			// three bytes, so a byte index would land inside it.
			cells := []rune(flat)
			if got := strings.TrimSpace(string(cells[span.from:span.to])); got != title {
				t.Fatalf("at %d columns the span for %q covers %q", width, title, got)
			}
		}
	}
}

// AND A STRIP THAT COULD NOT SHOW EVERY TAB SAYS SO AT THE END IT CUT. A bar
// that simply stopped would be a bar lying about how many tabs there are.
func TestTheSettingsTabStripMarksTheTabsItCouldNotShow(t *testing.T) {
	pal := newPalette(tokens.ANSI256, false)
	// Standing on the first tab at eighty columns: the strip cannot reach the
	// last chip, so it wears a mark on the right and none on the left.
	flat := plain(sheetTabBar(80, 0, pal))
	if strings.HasPrefix(strings.TrimSpace(flat), glyphMore) {
		t.Fatalf("standing on the first tab, the strip claims something is cut off its left:\n  %q", flat)
	}
	if !strings.HasSuffix(strings.TrimSpace(flat), glyphMore) {
		t.Fatalf("the strip drops tabs and does not say so:\n  drawn: %q\n  want:  a trailing %q", flat, glyphMore)
	}
	// Standing on the last tab, the cut is on the other side.
	flat = plain(sheetTabBar(80, len(settingTabs)-1, pal))
	if !strings.HasPrefix(strings.TrimSpace(flat), glyphMore) {
		t.Fatalf("standing on the last tab, the strip does not say what is behind it:\n  %q", flat)
	}
	if strings.HasSuffix(strings.TrimSpace(flat), glyphMore) {
		t.Fatalf("standing on the last tab, the strip claims there is more after it:\n  %q", flat)
	}
	// A frame wide enough for all nine wears no mark at either end.
	flat = plain(sheetTabBar(160, 4, pal))
	if strings.Contains(flat, glyphMore) {
		t.Fatalf("a strip that fits still marks a cut:\n  %q", flat)
	}
}

// A DESCRIPTION THAT IS CUT SAYS SO. It stopped mid-clause at two lines with no
// mark — `…new work waits for midnight or` — which reads as a rendering fault
// rather than as an omission, and sent people to the source for the rest.
func TestASettingsDescriptionThatIsCutSaysSo(t *testing.T) {
	about := strings.TrimSpace(strings.Repeat("a sentence that keeps going and going ", 12))
	lines := settingAboutLines(about, 60)
	if len(lines) != settingAboutRows {
		t.Fatalf("a description too long for the panel took %d lines, want %d", len(lines), settingAboutRows)
	}
	if last := lines[len(lines)-1]; !strings.HasSuffix(last, glyphMore) {
		t.Fatalf("the last line of a cut description does not say it was cut\n"+
			"  drawn: %q\n  want:  a line ending in %q", last, glyphMore)
	}
	// A description that fits is left whole, mark and all.
	short := settingAboutLines("seconds an ssh connection stays reusable.", 120)
	if len(short) != 1 || strings.Contains(short[0], glyphMore) {
		t.Fatalf("a description that fits was marked as cut: %q", short)
	}
}

// THE BOX A VALUE IS TYPED INTO SAYS WHAT THE ROW TAKES.
//
// Opening an empty money limit leaves the composer's own resting sentence —
// `say what you want done` — where a dollar amount goes, which invites prose
// into a field that refuses it. The sentence the panel answers with is
// [config.Setting.Accepts], the row's own writer read forwards, so the
// invitation and the refusal cannot drift apart.
func TestOpeningAValueSaysWhatThatRowTakes(t *testing.T) {
	a, dir := sheetApp(t)
	a.openSettings()
	registry := config.NewSettings(config.SettingsOptions{ProfileDir: dir})
	row, ok := registry.Row(config.KeySpendRail)
	if !ok {
		t.Fatal("the per-conversation row is not in the registry")
	}
	cursorTo(t, a, config.KeySpendRail)
	drive(t, a, key("enter"))
	if a.sheet.edit == nil {
		t.Fatal("enter on a money row opened no box")
	}
	want := row.Accepts()
	if !strings.Contains(a.sheet.edit.label, want) {
		t.Fatalf("the box says nothing about what it takes\n"+
			"  drawn: %q\n  want:  a line carrying %q", a.sheet.edit.label, want)
	}
	// A plain text row has nothing to add: "text" is not a fact about a row.
	plainRow := config.Setting{Kind: config.SettingText, Label: "google sign-in id"}
	if got := sheetEditNote("google sign-in id", plainRow); got != "google sign-in id" {
		t.Fatalf("a plain text row grew a sentence about itself: %q", got)
	}
}

// sheetRowDrawn is ONE settings row as a reader sees it, with its escape codes
// off and its trailing blanks kept — the gutter and the column this test is
// about are made of exactly those blanks, so [sheetRowsDrawn]'s TrimSpace would
// take the evidence away with the noise.
func sheetRowDrawn(t *testing.T, a *app, label string, width int) (string, string) {
	t.Helper()
	was := a.sheet.tab
	defer func() {
		a.sheet.tab = was
		a.sheet.cursor, a.sheet.top = 0, 0
		a.sheet.build()
	}()
	for tab := range settingTabs {
		a.sheet.tab = tab
		a.sheet.cursor, a.sheet.top = 0, 0
		a.sheet.build()
		for _, item := range a.sheet.items {
			// A READING IS A ROW TOO — `per task` is one, and it is the row this
			// test's eighty-cell half is named after (settingspend.go).
			name := item.meta.label
			if item.read != nil {
				name = item.read.name
			}
			if name != label {
				continue
			}
			lines := a.sheet.rowLines(item, false, false, width, a.pal)
			if len(lines) == 0 {
				t.Fatalf("the %q row drew nothing at %d cells", label, width)
			}
			return strings.TrimRight(plain(lines[0]), " "), name
		}
	}
	t.Fatalf("no settings row is labelled %q", label)
	return "", ""
}

// A ROW'S VALUE SITS IN A COLUMN, AND NEVER BUTTS THE NAME IN FRONT OF IT.
//
// Both of these were one expression judged at two widths — the gap between a
// label and the tail right-aligned against it, taken from the whole frame with a
// single cell of floor under it ([overlayPairRoom] now answers both):
//
//   - at eighty, the Spending tab drew `per task no limit of its own · it spends
//     against the day and this conversation` — a label and a tail that exactly
//     filled the frame with one word space between them, so two facts read as one
//     sentence and `per task` stopped being findable as a row;
//   - at a hundred and sixty, `ssh reuse` put a hundred and fifty blank cells in
//     front of `300s`, and the eye crossing that gap landed on the wrong row.
func TestASettingsValueSitsInAColumnAndNeverButtsItsLabel(t *testing.T) {
	a, _ := sheetApp(t)
	a.openSettings()

	// EIGHTY: the row that exactly filled the frame. What must give way is the
	// tail's last FACT, not the gutter — rowfit drops whole clauses.
	row, label := sheetRowDrawn(t, a, "per task", 80)
	at := strings.Index(row, label)
	if at < 0 {
		t.Fatalf("the 80-cell row does not carry its own label %q: %q", label, row)
	}
	tail := row[at+len(label):]
	if tail == "" {
		t.Fatalf("the %q row lost its value whole at 80 cells: %q", label, row)
	}
	// TWO IS WRITTEN OUT HERE ON PURPOSE. Asserting against [rowGutter] would be
	// a test that moves with the constant it is meant to hold — the first draft of
	// this one passed with the gutter back at one, because one cell is always at
	// least one cell. The law is the number.
	const wantGutter = 2
	if gutter := len(tail) - len(strings.TrimLeft(tail, " ")); gutter < wantGutter {
		t.Fatalf("at 80 cells the row reads\n  %q\n— %d cell(s) between %q and %q, want at least %d, "+
			"so the two facts read as one sentence", row, gutter, label, strings.TrimSpace(tail), wantGutter)
	}
	// AND THE VALUE IS STILL A WHOLE CLAUSE. A gutter bought by cutting the tail
	// in half would be this row's other law broken to keep this one.
	if strings.HasSuffix(row, glyphMore) {
		t.Fatalf("at 80 cells the row bought its gutter with an ellipsis: %q", row)
	}
	if rowGutter < wantGutter {
		t.Fatalf("rowGutter is %d — a single cell between a label and its tail is a word space, not a gutter", rowGutter)
	}

	// A HUNDRED AND SIXTY: the same function, the other end. The tail stops at
	// the measure and the rest of the frame is left empty.
	wide, _ := sheetRowDrawn(t, a, "ssh reuse", 160)
	if got := ansi.StringWidth(wide); got > 102 {
		t.Fatalf("at 160 cells the row is %d cells wide and reads\n  %q\n— the value is right-aligned to the frame "+
			"rather than to the %d-cell measure", got, wide, overlayMeasure)
	}
	if !strings.Contains(wide, "ssh reuse") || !strings.Contains(wide, "300s") {
		t.Fatalf("the 160-cell row is no longer the pair this test is about: %q", wide)
	}

	// AND THE MEASURE NEVER CUTS AN IDENTITY (rowfit.go's law 1): a pair too wide
	// for the measure keeps the frame instead of losing its name to a margin.
	long := strings.Repeat("x", 130)
	drawn := strings.TrimRight(plain(overlayRow(long, "300s", false, false, false, 160, a.pal)), " ")
	if !strings.Contains(drawn, long) {
		t.Fatalf("a 130-cell label with a 4-cell tail was cut to fit the measure at 160 cells: %q", drawn)
	}
}
