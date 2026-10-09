package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── THE STAGE ROWS SHOW THEIR LOOPS ─────────────────────────────────────────
//
// Owner's layout, 2026-10-09: each step row says its loop in one line,
// `test ↻ 1/3 until clean`: `↻ 2` before the run, `↻ 1/2` while it runs, what
// it loops until where that fits, `+` where someone other than the recipe set
// it, and `?` where the run asks you.

// factoryLineWith is the line of text that carries words, and "" for none.
func factoryLineWith(text, words string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, words) {
			return line
		}
	}
	return ""
}

// factoryStageRowsAt is item id's page at width, its stage rows' rail cells
// by name, plain, and the whole body for a failure message.
func factoryStageRowsAt(t *testing.T, a *app, id, width int) (map[string]string, []string) {
	t.Helper()
	a.width = width
	factoryOn(t, a, id)
	drive(t, a, key("enter"))
	defer drive(t, a, key("esc"))
	it, _ := a.factoryCursorItem()
	body := factoryBodyPlain(a, width, 40)
	out := map[string]string{}
	for _, row := range body {
		left, _, div := factorySplitAt(row)
		if div < 0 {
			continue
		}
		if div != factoryItemColW {
			t.Fatalf("at %d the column is %d cells wide, not %d: %q", width, div, factoryItemColW, row)
		}
		if f := strings.Fields(left); len(f) >= 2 && strings.HasPrefix(left, factorySpaces(factoryMargin+factoryNestW)) {
			for _, st := range factoryStages(a.fp.snap, it) {
				if f[1] == st.Name {
					out[st.Name] = left
				}
			}
		}
	}
	return out, body
}

// A STEP OF TWO ROUNDS SAYS `↻ 2` BEFORE THE RUN AND `↻ 1/2` WHILE IT RUNS,
// what it loops until beside it, and the steps the run asks you at wear `?`.
func TestFactoryStageRowsShowTheirLoops(t *testing.T) {
	a := factoryVerbLab(t, &factoryFake{})
	loop := a.icon(tokens.GLoop)
	asks := a.factoryAsksMark()
	// Before the run: item 3 is queued with nothing run.
	rows, body := factoryStageRowsAt(t, a, 3, 120)
	for name, want := range map[string][]string{
		"approve": {asks}, "test": {loop + " 2", wordUntil + " green"}, "review": {loop + " 2", wordUntil + " clean"}, "proof": {asks},
	} {
		row, ok := rows[name]
		if !ok {
			t.Fatalf("no %s row in the column:\n%s", name, strings.Join(body, "\n"))
		}
		for _, w := range want {
			if !strings.Contains(row, w) {
				t.Errorf("before the run the %s row does not say %q: %q", name, w, row)
			}
		}
	}
	if strings.Contains(rows["write"], loop) || strings.Contains(rows["write"], wordUntil) {
		t.Errorf("a one-round step says a loop: %q", rows["write"])
	}
	// During the run: item 2 runs review on its first of two rounds.
	rows, _ = factoryStageRowsAt(t, a, 2, 120)
	if !strings.Contains(rows["review"], loop+" 1/2") {
		t.Errorf("the running review row does not say %q: %q", loop+" 1/2", rows["review"])
	}
	if !strings.Contains(rows["proof"], asks) {
		t.Errorf("the proof row, where the run asks for the sign-off, does not wear %q: %q", asks, rows["proof"])
	}
}

// AN APPROVE STEP, A PERSON, ALWAYS ASKS; an item with an approve step takes
// the sign-off after its last stage that runs, and one with none ships
// itself, so asks at no stage of its own.
func TestFactoryStageRowAsksWhereTheRunStops(t *testing.T) {
	approve := factory.Stage{Name: factory.ApproveName, Kind: factory.StageGate, On: true}
	it := factory.Item{}
	views := []factoryStageView{
		{stage: factory.Stage{Name: "plan", On: true}},
		{stage: approve},
		{stage: factory.Stage{Name: "proof", On: true}},
	}
	for at, want := range []bool{false, true, false} {
		if got := factoryStageAsks(it, views, at); got != want {
			t.Errorf("with no approve step on the item, stage %s asks %v, not %v", views[at].stage.Name, got, want)
		}
	}
	it.Stages = []factory.Stage{{Name: "plan", On: true}, approve, {Name: "proof", On: true}}
	for at, want := range []bool{false, true, true} {
		if got := factoryStageAsks(it, views, at); got != want {
			t.Errorf("with an approve step, stage %s asks %v, not %v", views[at].stage.Name, got, want)
		}
	}
	views[2].off = true
	if factoryStageAsks(it, views, 2) || !factoryStageAsks(it, views, 1) {
		t.Error("a stage switched off asks, or the last stage that runs does not take the sign-off")
	}
}

// A STEP SOMEONE OTHER THAN THE RECIPE SET WEARS `+`, dim; `?` stands beside
// it where the run also asks there.
func TestFactoryStageRowAddedByAnother(t *testing.T) {
	f := &factoryFake{}
	factoryShapeItem(f, 2, func(it *factory.Item) {
		it.Stages = append([]factory.Stage(nil), it.Stages...)
		for i := range it.Stages {
			switch it.Stages[i].Name {
			case "neaten":
				it.Stages[i].By, it.Stages[i].Why = factory.ByManager, "the change touches the billing cache"
			case "write":
				it.Stages[i].By = factory.ByRecipe
			}
		}
	})
	a := factoryVerbLab(t, f)
	rows, body := factoryStageRowsAt(t, a, 2, 120)
	if !strings.HasSuffix(strings.TrimSpace(rows["neaten"]), a.factoryAddedMark()) {
		t.Errorf("the neaten row the manager set does not wear %q:\n%s", a.factoryAddedMark(), strings.Join(body, "\n"))
	}
	if strings.Contains(rows["write"], a.factoryAddedMark()) {
		t.Errorf("the recipe's write row wears the added mark: %q", rows["write"])
	}
}

// THE AUDIT: at 100, 120 and 150 the column stays [factoryItemColW] wide, and
// every step row keeps its name whole however much it has to say.
func TestFactoryAlignStageLoops(t *testing.T) {
	a := factoryVerbLab(t, &factoryFake{})
	for _, width := range []int{100, 120, 150} {
		for _, id := range []int{2, 3} {
			rows, _ := factoryStageRowsAt(t, a, id, width)
			for name, row := range rows {
				if r := []rune(row); len(r) != factoryItemColW {
					t.Errorf("at %d item %d's %s row is %d cells: %q", width, id, name, len(r), row)
				}
				if f := strings.Fields(row); len(f) < 2 || f[1] != name {
					t.Errorf("at %d item %d's %s row cut its name: %q", width, id, name, row)
				}
			}
		}
	}
}
