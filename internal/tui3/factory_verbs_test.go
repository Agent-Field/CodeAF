package tui3

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE VERBS ON THE RIGHT OF THE ITEM PAGE (factory_verbs.go) ──────────────

// factoryVerbsLab is the fake seam with the doors the fixture's fake leaves
// out (the item's conversation, its forge page and a fresh read), so every
// group of the column has its rows, at width, in the plain palette.
func factoryVerbsLab(t *testing.T, f *factoryFake, width int) *app {
	t.Helper()
	a := factoryVerbLab(t, f)
	s := f.seam()
	s.Talk = func(context.Context, int) (string, error) { return "", f.rec("Talk") }
	s.Open = func(id int) (string, error) { return "", f.rec("Open", id) }
	s.Refresh = func(_ context.Context, id int) error { return f.rec("Refresh", id) }
	a.factory = s
	a.pal = newPalette(tokens.NoColor, false)
	a.width = width
	return a
}

// factoryVerbsOpen opens item id's page and draws one frame, so the column's
// rows and their places are the frame's.
func factoryVerbsOpen(t *testing.T, a *app, id int) {
	t.Helper()
	factoryOn(t, a, id)
	drive(t, a, key("enter"))
	if !a.fp.open {
		t.Fatalf("enter on item %d opened no page", id)
	}
	frame(a)
}

// factoryVerbsText is the column as the body drew it: each row past the
// second rule, its runs of air folded to one space, blank rows left out.
func factoryVerbsText(a *app) []string {
	var out []string
	for _, row := range factoryBodyPlain(a, a.width, a.height-placeHeadRows-1) {
		verbs, ok := factoryVerbsAt(row)
		if !ok {
			continue
		}
		if f := strings.Join(strings.Fields(verbs), " "); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// factoryVerbsHit is the screen cell of the column's row named word.
func factoryVerbsHit(t *testing.T, a *app, word string) (int, int) {
	t.Helper()
	for _, h := range a.fp.verbHits {
		if h.verb.word == word {
			return a.fp.verbX + factoryVerbLeadW + factoryVerbIndentW, placeHeadRows + h.row
		}
	}
	t.Fatalf("the column drew no %q row: %v", word, factoryVerbsText(a))
	return -1, -1
}

// AT 120 A NEW ITEM DRAWS THE THREE GROUPS, each row its word, a `set` row's
// value and the key at the right, and the head's second row carries no chips.
func TestFactoryVerbsOnTheRightOfANewItem(t *testing.T) {
	a := factoryVerbsLab(t, &factoryFake{}, 120)
	factoryVerbsOpen(t, a, 4)
	it, _ := a.factoryCursorItem()
	got := strings.Join(factoryVerbsText(a), "\n")
	want := strings.Join([]string{
		"do",
		"run r",
		"chat T",
		"select space",
		"set",
		"ask me at " + factoryGateWord(it.Gate) + " t",
		"thinking — e",
		"budget " + factoryMoney(it.Cap) + " c",
		"stages 3 of 3",
		"also",
		"open on github g",
		"refresh u",
		"dismiss d",
	}, "\n")
	if got != want {
		t.Fatalf("the new item's column at 120 is\n%s\nwant\n%s", got, want)
	}
	body := factoryBodyPlain(a, a.width, a.height-placeHeadRows-1)
	if strings.Contains(body[1], wordAskAt) || strings.Contains(body[1], wordBudget) {
		t.Fatalf("the head's second row keeps the chips beside the column: %q", body[1])
	}
	for _, row := range body {
		if _, pane, div := factorySplitAt(row); div >= 0 && strings.Contains(pane, factoryHintClause(keyRun, wordRun)) {
			t.Fatalf("the pane names the verbs the column already says: %q", pane)
		}
	}
}

// EACH STATE DRAWS ITS OWN VERBS, and A GROUP WITH NOTHING IN IT IS NOT DRAWN:
// a parked item and a landed one have no setting to turn, so no `set`.
func TestFactoryVerbsByState(t *testing.T) {
	for _, c := range []struct {
		id   int
		want []string
	}{
		{2, []string{"do", "stop x", "chat T", "pause space", "steer S", "set", "thinking — e", "also"}},
		{1, []string{"do", "yes y", "no n", "in words a", "chat T", "also"}},
		{9, []string{"do", "approve with changes e", "request changes B", "re-run checks v", "chat T", "also"}},
	} {
		a := factoryVerbsLab(t, &factoryFake{}, 120)
		factoryVerbsOpen(t, a, c.id)
		got := factoryVerbsText(a)
		at := 0
		for _, row := range got {
			if at < len(c.want) && row == c.want[at] {
				at++
			}
		}
		if at != len(c.want) {
			t.Errorf("item %d's column is\n%s\nwant in order\n%s", c.id, strings.Join(got, "\n"), strings.Join(c.want, "\n"))
		}
		for _, row := range got {
			if (c.id == 1 || c.id == 9) && row == wordGroupSet {
				t.Errorf("item %d draws a `set` group with nothing to set: %v", c.id, got)
			}
		}
	}
}

// UNDER THE COLUMN'S WIDTH THE PAGE IS AS IT WAS: no second rule, the chips
// on the head's second row, the action line on the pane.
func TestFactoryVerbsNotDrawnWhenNarrow(t *testing.T) {
	a := factoryVerbsLab(t, &factoryFake{}, 90)
	factoryVerbsOpen(t, a, 4)
	if a.factoryVerbsDrawn() || len(factoryVerbsText(a)) > 0 {
		t.Fatalf("at 90 the page drew the column: %v", factoryVerbsText(a))
	}
	body := factoryBodyPlain(a, a.width, a.height-placeHeadRows-1)
	if !strings.HasPrefix(strings.TrimSpace(body[1]), wordAskAt) {
		t.Fatalf("at 90 the chips are not on the head's second row: %q", body[1])
	}
	if !strings.Contains(strings.Join(body, "\n"), factoryHintClause(keyRun, wordRun)) {
		t.Fatalf("at 90 the pane has no action line:\n%s", strings.Join(body, "\n"))
	}
}

// A PRESS ON A ROW IS ITS KEY: `run` asks the launch door, `ask me at` moves
// the gate as `t` does, `pause` pauses as `space` does.
func TestFactoryVerbsPressRunsTheKey(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbsLab(t, f, 120)
	factoryVerbsOpen(t, a, 4)
	x, y := factoryVerbsHit(t, a, wordRun)
	drive(t, a, clickAt(x, y), releaseAt(x, y))
	if got := strings.Join(f.said(), " "); got != "Launch(4)" {
		t.Fatalf("a press on run asked %q", got)
	}

	f = &factoryFake{}
	a = factoryVerbsLab(t, f, 120)
	factoryVerbsOpen(t, a, 4)
	it, _ := a.factoryCursorItem()
	x, y = factoryVerbsHit(t, a, wordAskAt)
	drive(t, a, clickAt(x, y), releaseAt(x, y))
	if got, want := strings.Join(f.said(), " "), "SetGate(4,"+string(factoryNextGate(it.Gate))+")"; got != want {
		t.Fatalf("a press on ask me at asked %q, want %q", got, want)
	}

	f = &factoryFake{}
	a = factoryVerbsLab(t, f, 120)
	factoryVerbsOpen(t, a, 2)
	x, y = factoryVerbsHit(t, a, wordPause)
	drive(t, a, clickAt(x, y), releaseAt(x, y))
	if got := strings.Join(f.said(), " "); got != "Pause(2)" {
		t.Fatalf("a press on pause asked %q", got)
	}
	if !a.fp.open {
		t.Fatal("a press on the column closed the page")
	}
}

// A PRESS ON `stages` WALKS THE STAGE RAIL TO THE FIRST STAGE, where `1-9`
// turn them on and off.
func TestFactoryVerbsStagesRowWalksToTheStages(t *testing.T) {
	a := factoryVerbsLab(t, &factoryFake{}, 120)
	factoryVerbsOpen(t, a, 4)
	x, y := factoryVerbsHit(t, a, wordStages)
	drive(t, a, clickAt(x, y), releaseAt(x, y))
	it, _ := a.factoryCursorItem()
	if rows := a.factoryItemRows(it); rows[a.fp.stage].kind != factoryPageStage || rows[a.fp.stage].at != 0 {
		t.Fatalf("a press on stages left the rail on row %d", a.fp.stage)
	}
}

// THE POINTER RESTING ON A ROW PAINTS THAT ROW, AND ONE ROW, with the
// pointer's ground; off the column the ground is let go.
func TestFactoryVerbsHoverPaintsOneRow(t *testing.T) {
	a := factoryVerbsLab(t, &factoryFake{}, 120)
	a.pal = newPalette(tokens.TrueColor, false)
	factoryVerbsOpen(t, a, 4)
	x, y := factoryVerbsHit(t, a, wordChat)
	drive(t, a, tea.MouseMotionMsg{X: x, Y: y})
	if a.fp.verbHover != wordChat {
		t.Fatalf("the pointer on chat left the hover at %q", a.fp.verbHover)
	}
	ground := factoryGround(a)
	worn := 0
	for i, line := range strings.Split(frame(a), "\n") {
		verbs, ok := factoryVerbsAt(line)
		if !ok {
			continue
		}
		if strings.Contains(verbs, ground) {
			worn++
			if i != y || !strings.Contains(ansi.Strip(verbs), wordChat) {
				t.Fatalf("row %d of the column wears the ground with the pointer on chat at %d: %q", i, y, ansi.Strip(verbs))
			}
		}
	}
	if worn != 1 {
		t.Fatalf("%d rows of the column wear the pointer's ground, want 1", worn)
	}
	drive(t, a, tea.MouseMotionMsg{X: factoryRailW + 4, Y: y})
	if a.fp.verbHover != "" {
		t.Fatalf("the pointer in the pane left the column's hover on %q", a.fp.verbHover)
	}
}

// THE ALIGNMENT AUDIT OF THE COLUMN at 100, 120 and 150: its rule stands at
// one cell on every row, its group names at one cell, its keys end at one
// cell, a `set` row's value starts at one cell, and nothing passes the page's
// right margin.
func TestFactoryAlignVerbs(t *testing.T) {
	for _, width := range []int{100, 120, 150} {
		a := factoryVerbsLab(t, &factoryFake{}, width)
		factoryVerbsOpen(t, a, 4)
		if !a.factoryVerbsDrawn() {
			t.Fatalf("at %d the page drew no column", width)
		}
		body := factoryBodyPlain(a, width, a.height-placeHeadRows-1)
		rule := a.fp.verbX - factoryRuleW
		if end := rule + factoryRuleW + factoryVerbRailW; end != width-factoryMargin {
			t.Errorf("at %d the column ends at %d, not the margin %d", width, end, width-factoryMargin)
		}
		values := map[string]bool{}
		for i, row := range body[a.fp.railTop:] {
			r := []rune(row)
			if len(r) <= rule || string(r[rule]) != "│" {
				t.Errorf("at %d body row %d has no rule at %d: %q", width, a.fp.railTop+i, rule, row)
				continue
			}
			col := string(r[rule+1:])
			if strings.TrimSpace(col) == "" {
				continue
			}
			if end := len([]rune(strings.TrimRight(row, " "))); end > width-factoryMargin {
				t.Errorf("at %d row %d ends at %d, past %d: %q", width, i, end, width-factoryMargin, row)
			}
			name := strings.TrimSpace(col)
			if name == wordGroupDo || name == wordGroupSet || name == wordGroupAlso {
				if x := factoryFirstInk(col); x != factoryVerbLeadW {
					t.Errorf("at %d the group %q stands at %d, not %d", width, name, x, factoryVerbLeadW)
				}
				continue
			}
			if x := factoryFirstInk(col); x != factoryVerbLeadW+factoryVerbIndentW {
				t.Errorf("at %d the row %q starts at %d, not %d", width, name, x, factoryVerbLeadW+factoryVerbIndentW)
			}
			for _, w := range []string{wordAskAt, wordThinking, wordBudget, wordStages} {
				if strings.HasPrefix(name, w+" ") {
					values[itoa(factoryFirstInk(string([]rune(col)[factoryVerbLeadW+factoryVerbIndentW+factoryVerbWordW:])))] = true
				}
			}
			if strings.HasPrefix(name, wordStages+" ") {
				continue
			}
			if end := len([]rune(strings.TrimRight(col, " "))); end != factoryVerbRailW {
				t.Errorf("at %d the key of %q ends at %d, not %d", width, name, end, factoryVerbRailW)
			}
		}
		if len(values) != 1 || !values["0"] {
			t.Errorf("at %d the set rows' values do not start at one cell: %v", width, values)
		}
	}
}
