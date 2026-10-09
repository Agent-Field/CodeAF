package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE ITEM PAGE IS THE ISSUE'S MAP ────────────────────────────────────────
//
// The item page's left column is one row per facet of the item (owner
// decision, 2026-10-08): issue, manager, run with its stages and its log
// nested under it, result, settings.

// factoryFacetsOf is the left column for the item under the cursor as words,
// a nested row led by two spaces.
func factoryFacetsOf(a *app) []string {
	it, _ := a.factoryCursorItem()
	rows := a.factoryItemRows(it)
	cells := a.factoryPageCells(it, rows)
	out := make([]string, 0, len(rows))
	for i, r := range rows {
		word := factoryPageRowWord(r)
		if cells[i].indent == factoryNestW {
			word = "  " + word
		}
		out = append(out, word)
	}
	return out
}

// THE ROWS FOR A NEW, A RUNNING AND A LANDED ITEM, in order, with the stages
// and the log nested under `run`: the issue, the manager where the Talk door
// is, the run and its stages, the log once the stream said anything, the
// result once something came out, and the settings last.
func TestFactoryFacetRowsByState(t *testing.T) {
	a := factoryVerbsLab(t, &factoryFake{}, 150)
	for _, c := range []struct {
		id         int
		log, works bool
	}{{4, false, false}, {2, true, false}, {9, true, true}} {
		factoryVerbsOpen(t, a, c.id)
		it, _ := a.factoryCursorItem()
		want := []string{wordFacetIssue, wordFacetManager, wordFacetRun}
		for _, st := range factoryStages(a.fp.snap, it) {
			want = append(want, "  "+st.Name)
		}
		if c.log {
			want = append(want, "  "+wordFacetLog)
		}
		if c.works {
			want = append(want, wordFacetResult)
		}
		want = append(want, wordFacetSettings)
		if got := factoryFacetsOf(a); strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("item %d (%s): the rows are\n%q\nwant\n%q", c.id, it.State, got, want)
		}
		drive(t, a, key("esc"))
	}
}

// THE MANAGER IS ABSENT WITHOUT THE TALK DOOR, and the result is absent on a
// new item: a row with nothing behind it is not in the column.
func TestFactoryFacetRowsAbsentWithNothingBehind(t *testing.T) {
	a := factoryVerbLab(t, &factoryFake{})
	factoryOn(t, a, 4)
	drive(t, a, key("enter"))
	for _, row := range factoryFacetsOf(a) {
		if row == wordFacetManager {
			t.Fatalf("the manager row stands with no Talk door: %q", factoryFacetsOf(a))
		}
		if row == wordFacetResult || strings.TrimSpace(row) == wordFacetLog {
			t.Fatalf("a new item draws %q: %q", row, factoryFacetsOf(a))
		}
	}
}

// THE RUN ROW CARRIES THE RUN'S FACTS, `running 4m · $0.31`, dropped from the
// right whole when the column is too narrow, and a new item's run row says
// nothing after its word.
func TestFactoryFacetRunFacts(t *testing.T) {
	a := factoryVerbsLab(t, &factoryFake{}, 150)
	factoryVerbsOpen(t, a, 2)
	it, _ := a.factoryCursorItem()
	if got := a.factoryRunFacts(it, 400); !strings.HasPrefix(got, string(factory.StateRunning)) {
		t.Fatalf("the running item's run facts are %q", got)
	}
	if got := a.factoryRunFacts(it, len(string(factory.StateRunning))); got != string(factory.StateRunning) {
		t.Fatalf("a narrow column keeps %q, want the state alone", got)
	}
	drive(t, a, key("esc"))
	factoryVerbsOpen(t, a, 4)
	it, _ = a.factoryCursorItem()
	if got := a.factoryRunFacts(it, 400); got != "" {
		t.Fatalf("a new item's run row says %q", got)
	}
}

// factorySettingsLabelW is the settings' label column for stages, as the
// pane widens it for the longest stage name.
func factorySettingsLabelW(stages []factory.Stage) int {
	w := factorySetLabelW
	for _, st := range stages {
		w = max(w, factorySetNumW+ansi.StringWidth(st.Name)+factoryLabelGap)
	}
	return w
}

// factorySettingsLines is the settings pane of the item under the cursor at
// width, plain, its trailing air cut and blank lines kept.
func factorySettingsLines(t *testing.T, a *app, width int) []string {
	t.Helper()
	factoryRowNamed(t, a, wordFacetSettings)
	var out []string
	for _, row := range factoryBodyPlain(a, width, a.height-placeHeadRows-1)[a.fp.railTop:] {
		_, right, div := factorySplitAt(row)
		if div < 0 {
			continue
		}
		out = append(out, strings.TrimRight(right, " "))
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return out
}

// THE SETTINGS PANE is the three knobs with their values and keys, then one
// line per stage with its number and on or off, then the set group's other
// keys; and `t`, `e`, `c` and the digits act from it.
func TestFactorySettingsPane(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbsLab(t, f, 120)
	factoryVerbsOpen(t, a, 4)
	it, _ := a.factoryCursorItem()
	lines := factorySettingsLines(t, a, 120)
	lead := factorySpaces(factoryMargin)
	stages := factoryStages(a.fp.snap, it)
	labelW := factorySettingsLabelW(stages)
	values := map[string]string{}
	for _, c := range a.factoryChipList(it) {
		values[c.label] = c.value
	}
	want := []string{
		lead + factoryPad(wordThinking, labelW) + factoryPad(values[wordThinking], factorySetValueW) + keyThinking,
		lead + factoryPad(wordBudget, labelW) + factoryPad(values[wordBudget], factorySetValueW) + keyBudget,
		"",
	}
	for i, st := range stages {
		on := wordOn
		if !st.On {
			on = wordOff
		}
		want = append(want, lead+factoryPad(factoryPad(itoa(i+1), factorySetNumW)+st.Name, labelW)+on)
	}
	for i := range want {
		if i >= len(lines) || lines[i] != want[i] {
			t.Fatalf("the settings pane at 120 is\n%s\nwant it to start\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
		}
	}
	if foot := lines[len(lines)-1]; !strings.Contains(foot, factoryHintClause(keyStages, wordStages)) || !strings.Contains(foot, factoryHintClause(keyAddStage, wordAddStage)) {
		t.Fatalf("the settings pane's last line names no stage keys: %q", foot)
	}
	t.Logf("the settings pane at 120:\n%s", strings.Join(lines, "\n"))
	f.said()
	for _, c := range []struct{ key, want string }{
		{"c", "SetCap(4,"},
		{"e", "SetEffort(4,"},
		{"1", "SetStage(4,0,"},
	} {
		drive(t, a, key(c.key))
		if got := strings.Join(f.said(), " "); !strings.HasPrefix(got, c.want) {
			t.Errorf("%s on the settings row asked %q, want %s…", c.key, got, c.want)
		}
		if !a.fp.open || factoryPageRowName(a, it) != wordFacetSettings {
			t.Fatalf("%s on the settings row left the row", c.key)
		}
	}
	// `enter` on the settings row does nothing.
	drive(t, a, key("enter"))
	if got := f.said(); len(got) != 0 || !a.fp.open {
		t.Fatalf("enter on the settings row asked %v or left the page", got)
	}
}

// A RUNNING ITEM'S SETTINGS name only the keys that act on it: its gate and
// budget are read here and turned nowhere, and its stages carry no number.
func TestFactorySettingsPaneRunning(t *testing.T) {
	a := factoryVerbsLab(t, &factoryFake{}, 120)
	factoryVerbsOpen(t, a, 2)
	for _, line := range factorySettingsLines(t, a, 120) {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		last := f[len(f)-1]
		if last == keyBudget {
			t.Fatalf("a running item's settings offer %q: %q", last, line)
		}
		if f[0] == "1" {
			t.Fatalf("a running item's stages carry numbers: %q", line)
		}
	}
}

// `enter` ON THE MANAGER PUTS THE KEYS IN ITS BOX, and asks no door; `T`
// still opens the conversation; on the result `enter` does nothing.
func TestFactoryFacetEnter(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbsLab(t, f, 150)
	factoryVerbsOpen(t, a, 9)
	factoryRowNamed(t, a, wordFacetResult)
	drive(t, a, key("enter"))
	if got := f.said(); len(got) != 0 || a.fp.act.ask != nil {
		t.Fatalf("enter on the result asked %v or opened a row", got)
	}
	factoryRowNamed(t, a, wordFacetManager)
	drive(t, a, key("enter"))
	if got := f.said(); len(got) != 0 || !a.factoryBoxFocused() {
		t.Fatalf("enter on the manager asked %v (box focused %v)", got, a.factoryBoxFocused())
	}
	drive(t, a, key("esc"))
	drive(t, a, key("T"))
	if got := strings.Join(f.said(), " "); !strings.Contains(got, "Talk") {
		t.Fatalf("T on the manager asked %q, not the Talk door", got)
	}
}

// THE RESULT PANE is the sheet, then the diff and the checks under it.
func TestFactoryResultPane(t *testing.T) {
	a := factoryVerbsLab(t, &factoryFake{}, 150)
	factoryVerbsOpen(t, a, 9)
	it, _ := a.factoryCursorItem()
	pane := factoryPlanPane(a)
	if it.Diff != "" && !strings.Contains(pane, it.Diff) {
		t.Fatalf("the result pane has no diff %q:\n%s", it.Diff, pane)
	}
	if len(it.Proof) > 0 && !strings.Contains(pane, it.Proof[0].Text) {
		t.Fatalf("the result pane has no sheet:\n%s", pane)
	}
}

// factoryCrumbX is the screen column the trail drew crumb at.
func factoryCrumbX(t *testing.T, a *app, c factoryCrumb) int {
	t.Helper()
	for _, h := range a.fp.crumbHits {
		if h.crumb == c {
			return h.x0
		}
	}
	t.Fatalf("the trail drew no crumb %d: %+v", c, a.fp.crumbHits)
	return -1
}

// THE CRUMBS ARE BUTTONS: the pointer resting on one paints it, and it alone,
// with the pointer's ground; a press on `Factory` puts the floor back, a press
// on the repo puts it back narrowed to that repo, and a press on the number
// opens the item on github through `g`'s door.
func TestFactoryCrumbsAreButtons(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbsLab(t, f, 150)
	a.pal = newPalette(tokens.TrueColor, false)
	factoryVerbsOpen(t, a, 4)
	it, _ := a.factoryCursorItem()
	x := factoryCrumbX(t, a, factoryCrumbRepo)
	drive(t, a, tea.MouseMotionMsg{X: x, Y: placeHeadRows})
	if a.fp.crumbHover != factoryCrumbRepo {
		t.Fatalf("the pointer on the repo left the hover at %d", a.fp.crumbHover)
	}
	head := strings.Split(frame(a), "\n")[placeHeadRows]
	ground := factoryGround(a)
	if strings.Count(head, ground) != 1 || !strings.Contains(head, ground+factoryRepoShort(it.Repo)) {
		t.Fatalf("the trail does not paint the repo crumb alone: %q", head)
	}
	drive(t, a, tea.MouseMotionMsg{X: a.width - factoryMargins, Y: placeHeadRows})
	if a.fp.crumbHover != factoryCrumbNone {
		t.Fatalf("the pointer off the trail kept the hover at %d", a.fp.crumbHover)
	}

	// The number opens the item on github through `g`'s door.
	f.said()
	x = factoryCrumbX(t, a, factoryCrumbRef)
	drive(t, a, clickAt(x, placeHeadRows), releaseAt(x, placeHeadRows))
	if got := strings.Join(f.said(), " "); got != "Open(4)" || !a.fp.open {
		t.Fatalf("a press on the number asked %q (page open %v)", got, a.fp.open)
	}

	// The repo puts the floor back narrowed to the repo.
	frame(a)
	x = factoryCrumbX(t, a, factoryCrumbRepo)
	drive(t, a, clickAt(x, placeHeadRows), releaseAt(x, placeHeadRows))
	if a.fp.open || a.factoryViewNow().repo != it.Repo {
		t.Fatalf("a press on the repo: page open %v, the floor narrowed to %q, want %q", a.fp.open, a.factoryViewNow().repo, it.Repo)
	}
	if cur, ok := a.factoryCursorItem(); !ok || cur.ID != it.ID {
		t.Fatal("the floor narrowed to the repo lost the item the page was on")
	}

	// `Factory` puts the floor back as `esc` does.
	drive(t, a, key("esc"))
	factoryVerbsOpen(t, a, 4)
	x = factoryCrumbX(t, a, factoryCrumbFactory)
	drive(t, a, clickAt(x, placeHeadRows), releaseAt(x, placeHeadRows))
	if a.fp.open || !a.at(pageFactory) || a.fp.repo != 0 {
		t.Fatalf("a press on Factory: page open %v, repo %d", a.fp.open, a.fp.repo)
	}
}

// AN ITEM WITH NO PAGE ON GITHUB HAS NO NUMBER BUTTON: the ref is drawn and
// a press on it does nothing.
func TestFactoryCrumbRefNeedsTheDoor(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	factoryOn(t, a, 4)
	drive(t, a, key("enter"))
	frame(a)
	for _, h := range a.fp.crumbHits {
		if h.crumb == factoryCrumbRef {
			t.Fatalf("the ref is a button with no Open door: %+v", a.fp.crumbHits)
		}
	}
}

// THE `?` SHEET names the settings under `set` on the item page, and the
// walk down the rows under `move`.
func TestFactorySheetNamesTheSettings(t *testing.T) {
	a := factoryVerbsLab(t, &factoryFake{}, 150)
	factoryVerbsOpen(t, a, 2)
	var set, move []string
	for _, g := range a.factorySheet() {
		for _, r := range g.rows {
			switch g.name {
			case wordGroupSet:
				set = append(set, factoryHintClause(r.key, r.word))
			case wordGroupMove:
				move = append(move, factoryHintClause(r.key, r.word))
			}
		}
	}
	if !strings.Contains(strings.Join(set, "|"), factoryHintClause(keyWalk, wordFacetSettings)) {
		t.Fatalf("the sheet's set group does not name the settings: %q", set)
	}
	if !strings.Contains(strings.Join(move, "|"), factoryHintClause(keyWalk, wordRows)) {
		t.Fatalf("the sheet's move group does not walk the rows: %q", move)
	}
}

// THE ALIGNMENT AUDIT OF THE ITEM PAGE'S LEFT COLUMN AND ITS SETTINGS at 100,
// 120 and 150: every facet word starts at the margin and every nested row
// [factoryNestW] past it; the settings' values start at one cell and their
// keys at one cell; nothing passes the page's right margin.
func TestFactoryAlignFacets(t *testing.T) {
	for _, width := range []int{100, 120, 150} {
		a := factoryVerbsLab(t, &factoryFake{}, width)
		factoryVerbsOpen(t, a, 2)
		it, _ := a.factoryCursorItem()
		rows := a.factoryItemRows(it)
		body := factoryBodyPlain(a, width, a.height-placeHeadRows-1)
		for i, r := range rows {
			if i >= a.fp.railShown {
				break
			}
			left, _, div := factorySplitAt(body[a.fp.railTop+i])
			if div != factoryRailW {
				t.Errorf("at %d row %d's rule stands at %d, not %d", width, i, div, factoryRailW)
			}
			want := factoryMargin
			if r.kind == factoryPageStage || r.kind == factoryPageLog {
				want += factoryNestW
			}
			if x := factoryFirstInk(left); x != want {
				t.Errorf("at %d the row %q starts at %d, not %d", width, strings.TrimSpace(left), x, want)
			}
		}
		for i, row := range body {
			if end := len([]rune(strings.TrimRight(row, " "))); end > width-factoryMargin {
				t.Errorf("at %d body row %d ends at %d, past %d: %q", width, i, end, width-factoryMargin, row)
			}
		}
		// The settings: the values start at one cell, the keys at one cell.
		drive(t, a, key("esc"))
		factoryVerbsOpen(t, a, 4)
		cur, _ := a.factoryCursorItem()
		lines := factorySettingsLines(t, a, width)
		valueX := factoryMargin + factorySettingsLabelW(factoryStages(a.fp.snap, cur))
		keyX := valueX + factorySetValueW
		values, keys := 0, 0
		for _, line := range lines {
			plain := []rune(line)
			if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), keyStages) {
				continue
			}
			if len(plain) <= valueX || plain[valueX-1] != ' ' || plain[valueX] == ' ' {
				t.Errorf("at %d the value of %q does not start at %d", width, line, valueX)
			}
			values++
			if len(plain) > keyX {
				if plain[keyX-1] != ' ' || plain[keyX] == ' ' {
					t.Errorf("at %d the key of %q does not start at %d", width, line, keyX)
				}
				keys++
			}
		}
		if values == 0 || keys != 2 {
			t.Errorf("at %d the settings drew %d values and %d keys:\n%s", width, values, keys, strings.Join(lines, "\n"))
		}
		for _, line := range lines {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("at %d a settings line is %d cells: %q", width, w, line)
			}
		}
	}
}
