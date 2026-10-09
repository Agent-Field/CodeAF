package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE ITEM PAGE ON A WIDE TERMINAL (owner's screenshots, 2026-10-09) ──────
//
// The issue's details stand beside its body when the pane is wide enough; the
// panes wrap what a person reads instead of cutting it; the peek draws no bare
// `thinking  —`; and the floor narrowed to one repo has a road back.

// factoryIssueLab is item 6 with a body of its own words, its page open on
// the issue at width.
func factoryIssueLab(t *testing.T, width int) *app {
	t.Helper()
	a := factoryPlaceLab(t)
	a.width, a.height = width, 50
	it := factoryPaneItem(t, a, 6)
	it.Body = "the meter crashes when the window is resized during a long run and the spend row stops counting"
	factoryOn(t, a, 6)
	drive(t, a, key("enter"))
	if !a.fp.open || a.fp.stage != 0 {
		t.Fatalf("item 6's page is not open on the issue (open %v, row %d)", a.fp.open, a.fp.stage)
	}
	return a
}

// WIDE: the read stands on the body's own rows, to the right of the body's
// measure; NARROW: it stands under the body, never beside it. At both widths
// no row runs past the width and nothing is cut with an ellipsis.
func TestFactoryIssueDetailsBesideTheBody(t *testing.T) {
	const read = "underspecified; two questions for the author first"
	for _, c := range []struct {
		width  int
		beside bool
	}{{200, true}, {160, true}, {110, false}, {80, false}} {
		a := factoryIssueLab(t, c.width)
		rows := factoryBodyPlain(a, c.width, 40)
		bodyRow, readRow := -1, -1
		for i, r := range rows {
			if bodyRow < 0 && strings.Contains(r, "the meter crashes") {
				bodyRow = i
			}
			if readRow < 0 && strings.Contains(r, read[:20]) {
				readRow = i
			}
			if w := ansi.StringWidth(r); w > c.width {
				t.Errorf("at %d row %d is %d cells: %q", c.width, i, w, r)
			}
		}
		if bodyRow < 0 || readRow < 0 {
			t.Fatalf("at %d the issue has no body or no read:\n%s", c.width, strings.Join(rows, "\n"))
		}
		page := strings.Join(rows, "\n")
		if strings.Contains(page, a.icon(tokens.GEllipsis)) {
			t.Errorf("at %d the issue cuts something with an ellipsis:\n%s", c.width, page)
		}
		if c.beside {
			bodyX := strings.Index(rows[bodyRow], "the meter crashes")
			readX := strings.Index(rows[readRow], read[:20])
			if readRow > bodyRow+2 || readX < bodyX+factoryPageProseW {
				t.Errorf("at %d the read is not beside the body (body row %d, read row %d at %d):\n%s", c.width, bodyRow, readRow, readX, page)
			}
			continue
		}
		if readRow <= bodyRow || strings.Contains(rows[readRow], "the meter crashes") {
			t.Errorf("at %d the read is not under the body:\n%s", c.width, page)
		}
	}
}

// THE SPLIT ASKS FOR A READABLE BODY AND A USABLE COLUMN, and an issue with
// no words of its own stacks its details from the margin.
func TestFactoryIssueSplitWidths(t *testing.T) {
	if _, _, ok := factoryIssueSplit(factoryPageProseW + factoryIssueSideGap + factoryIssueSideMin - 1); ok {
		t.Fatal("a pane too narrow for the column split")
	}
	body, side, ok := factoryIssueSplit(400)
	if !ok || body != factoryPageProseW || side != factoryIssueSideMax {
		t.Fatalf("at 400 the split is %d + %d (%v)", body, side, ok)
	}
	rows := factoryBeside([]string{"left", "", "more"}, []string{"right"}, 10, 2)
	if rows[0] != "left        right" || rows[1] != "" || rows[2] != "more" {
		t.Fatalf("beside = %q", rows)
	}
}

// A STEP'S ASK IS WRAPPED, NEVER CUT: every word of a long ask is in the
// step's pane at a narrow measure, and no line ends in an ellipsis.
func TestFactoryStepAskWraps(t *testing.T) {
	a := factoryPlaceLab(t)
	it := factoryPaneItem(t, a, 1)
	views := a.factoryItemStages(*it)
	if len(views) == 0 {
		t.Fatal("item 1 has no steps")
	}
	ask := "read the plan against the issue and the recipe, name every claim it makes that no check covers, and say which step should cover it before anything is written"
	views[0].stage.Ask = ask
	const measure = 48
	pane := a.factoryStagePane(*it, views, 0, measure, 30)
	text := ansi.Strip(strings.Join(pane, " "))
	for _, word := range strings.Fields(ask) {
		if !strings.Contains(text, word) {
			t.Fatalf("the step's pane lost %q:\n%s", word, strings.Join(pane, "\n"))
		}
	}
	for _, line := range pane {
		if w := ansi.StringWidth(line); w > measure {
			t.Errorf("a pane line is %d cells, past %d: %q", w, measure, ansi.Strip(line))
		}
		if strings.HasSuffix(strings.TrimSpace(ansi.Strip(line)), a.icon(tokens.GEllipsis)) {
			t.Errorf("a pane line is cut: %q", ansi.Strip(line))
		}
	}
}

// THE LOG WRAPS TOO, newest at the foot: a long line is read whole under its
// time and mark, and the pane never runs past its room.
func TestFactoryLogRowsWrap(t *testing.T) {
	a := factoryPlaceLab(t)
	it := factoryPaneItem(t, a, 2)
	if it.Stream == nil || len(it.Stream.Log) == 0 {
		t.Skip("item 2 has no log in the fixture")
	}
	long := it.Stream.Log[len(it.Stream.Log)-1]
	long.Text = strings.Repeat("the relay answered late again ", 6)
	rows := a.factoryLogRows(long, 50)
	if len(rows) < 2 {
		t.Fatalf("a long log line is %d row", len(rows))
	}
	if got := ansi.Strip(strings.Join(rows, " ")); strings.Count(got, "relay") != 6 {
		t.Fatalf("the wrapped log line lost words: %q", got)
	}
	if tail := a.factoryLogTail([]factory.LogLine{long, long}, 50, 3); len(tail) != 3 {
		t.Fatalf("the tail is %d rows in a room of 3", len(tail))
	}
}

// THE PEEK DRAWS NO BARE `thinking  —`: an item with no budget and no
// thinking word draws no chip row, and one with a thinking word draws it from
// the margin, where the title starts.
func TestFactoryPeekThinkingChip(t *testing.T) {
	a := factoryPlaceLab(t)
	it := factoryPaneItem(t, a, 8)
	it.Cap = 0
	text := strings.Join(factoryPaneOn(t, a, 8, factoryPaneW(150), 30), "\n")
	if strings.Contains(text, wordThinking) || strings.Contains(text, "—") {
		t.Fatalf("an item with no thinking word draws one:\n%s", text)
	}
	stages := factoryStages(a.fp.snap, *it)
	at := factoryEffortStage(a.fp.snap, *it)
	if at < 0 {
		t.Fatal("item 8 has no stage `e` turns")
	}
	it.Stages = append(stages[:0:0], stages...)
	it.Stages[at].Effort = "strong"
	rows := factoryPaneOn(t, a, 8, factoryPaneW(150), 30)
	titleX := len(rows[0]) - len(strings.TrimLeft(rows[0], " "))
	found := false
	for _, r := range rows {
		if strings.TrimSpace(r) == wordThinking+"  strong" {
			found = true
			if x := len(r) - len(strings.TrimLeft(r, " ")); x != titleX {
				t.Errorf("the thinking chip starts at %d, the title at %d: %q", x, titleX, r)
			}
		}
	}
	if !found {
		t.Fatalf("the peek draws no `thinking  strong`:\n%s", strings.Join(rows, "\n"))
	}
}

// THE SETTINGS PANE SAYS `auto` where thinking has no word of its own, never
// a dash.
func TestFactorySettingsThinkingAuto(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbsLab(t, f, 120)
	factoryVerbsOpen(t, a, 4)
	lines := strings.Join(factorySettingsLines(t, a, 120), "\n")
	if !strings.Contains(lines, wordThinking) || strings.Contains(lines, "—") {
		t.Fatalf("the settings pane:\n%s", lines)
	}
}

// THE ROUND TRIP: the repo crumb on the item page narrows the floor, whose
// first row is the trail `Factory › codeaf · N` and whose hint names `esc all
// repos`; the pointer on `Factory` wears the pointer's ground, and a press on
// it puts every repo back with the cursor on its item. Narrowed again, `esc`
// puts every repo back first and leaves the factory only on its second press.
func TestFactoryNarrowedFloorCrumbRoundTrip(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbsLab(t, f, 150)
	a.pal = newPalette(tokens.TrueColor, false)
	factoryVerbsOpen(t, a, 4)
	it, _ := a.factoryCursorItem()
	narrow := func() {
		t.Helper()
		if !a.fp.open {
			factoryVerbsOpen(t, a, 4)
		}
		x := factoryCrumbX(t, a, factoryCrumbRepo)
		drive(t, a, clickAt(x, placeHeadRows), releaseAt(x, placeHeadRows))
		if a.fp.open || a.factoryViewNow().repo != it.Repo {
			t.Fatalf("the repo crumb did not narrow the floor to %q", it.Repo)
		}
	}
	narrow()
	lines := strings.Split(frame(a), "\n")
	trail := wordFloorCrumb + " › " + factoryRepoShort(it.Repo) + " · "
	y := -1
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(ansi.Strip(l)), trail) {
			y = i
			break
		}
	}
	if y < 0 {
		t.Fatalf("the narrowed floor has no trail %q:\n%s", trail, plain(strings.Join(lines, "\n")))
	}
	if hint := plain(strings.Join(lines, "\n")); !strings.Contains(hint, factoryHintClause(keyBack, wordAllRepos)) {
		t.Fatalf("the narrowed floor does not name esc all repos:\n%s", hint)
	}
	x := strings.Index(ansi.Strip(lines[y]), wordFloorCrumb)
	drive(t, a, tea.MouseMotionMsg{X: x, Y: y})
	if !a.fp.stripHot {
		t.Fatal("the pointer on Factory did not take the crumb")
	}
	if row := strings.Split(frame(a), "\n")[y]; !strings.Contains(row, factoryGround(a)+wordFloorCrumb) {
		t.Fatalf("the Factory crumb does not wear the pointer's ground: %q", row)
	}
	drive(t, a, tea.MouseMotionMsg{X: x + 40, Y: y})
	if a.fp.stripHot {
		t.Fatal("the pointer off Factory kept the crumb")
	}
	drive(t, a, clickAt(x, y), releaseAt(x, y))
	if a.fp.repo != 0 || !a.at(pageFactory) {
		t.Fatalf("a press on Factory left the floor narrowed (repo %d)", a.fp.repo)
	}
	if cur, ok := a.factoryCursorItem(); !ok || cur.ID != it.ID {
		t.Fatal("every repo back lost the item the cursor was on")
	}
	if row := plain(strings.Split(frame(a), "\n")[y]); !strings.Contains(row, wordAllRepos) {
		t.Fatalf("every repo back does not say all repos: %q", row)
	}

	narrow()
	drive(t, a, key("esc"))
	if a.fp.repo != 0 || !a.at(pageFactory) {
		t.Fatalf("esc on the narrowed floor: repo %d, on the factory %v", a.fp.repo, a.at(pageFactory))
	}
	drive(t, a, key("esc"))
	if a.at(pageFactory) {
		t.Fatal("esc on the whole floor did not leave the factory")
	}
}
