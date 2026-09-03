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
