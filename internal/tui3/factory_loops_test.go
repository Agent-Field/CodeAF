package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── THE STAGE ROWS SHOW THEIR LOOPS ─────────────────────────────────────────
//
// Owner decision, 2026-10-08: the item page's left column shows each stage's
// loop in a cell or two (`×2` before the run, `1/2` during and after, `+`
// where someone other than the recipe set it, `?` where the run asks you),
// and the story's open head says the rest.

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
		if div != factoryRailW {
			t.Fatalf("at %d the rail is %d cells wide, not %d: %q", width, div, factoryRailW, row)
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

// A STAGE OF TWO ROUNDS SAYS `×2` BEFORE THE RUN AND `1/2` WHILE IT RUNS, in
// one column, and the stages the run asks you at wear `?` in the glyph column
// at the rail's last cell.
func TestFactoryStageRowsShowTheirLoops(t *testing.T) {
	a := factoryVerbLab(t, &factoryFake{})
	asks := a.factoryAsksMark()
	glyphAt := factoryRailW - factoryStageGlyphW
	roundsEnd := glyphAt - 1
	cell := func(row string, from, to int) string {
		r := []rune(row)
		return strings.TrimSpace(string(r[from:to]))
	}
	// Before the run: item 3 is queued with nothing run.
	rows, body := factoryStageRowsAt(t, a, 3, 120)
	t.Logf("before the run at 120:\n%s", strings.Join(body[3:12], "\n"))
	for name, want := range map[string][2]string{
		"plan": {"", ""}, "approve": {"", asks}, "write": {"", ""}, "test": {"×2", ""}, "review": {"×2", ""}, "neaten": {"", ""}, "proof": {"", asks},
	} {
		row, ok := rows[name]
		if !ok {
			t.Fatalf("no %s row on the rail:\n%s", name, strings.Join(body, "\n"))
		}
		if want[0] != "" && cell(row, roundsEnd-factoryStageRoundsW, roundsEnd) != want[0] {
			t.Errorf("the %s row's rounds are not %q right-aligned before the glyph: %q", name, want[0], row)
		}
		if got := cell(row, glyphAt, factoryRailW); got != want[1] {
			t.Errorf("the %s row's glyph is %q, not %q: %q", name, got, want[1], row)
		}
	}
	// During the run: item 2 runs review on its first of two rounds.
	rows, body = factoryStageRowsAt(t, a, 2, 120)
	t.Logf("during the run at 120:\n%s", strings.Join(body[3:12], "\n"))
	if got := cell(rows["review"], roundsEnd-factoryStageRoundsW, roundsEnd); got != "1/2" {
		t.Errorf("the running review row's rounds are %q, not 1/2: %q", got, rows["review"])
	}
	if strings.Contains(rows["review"], "×2") {
		t.Errorf("the running review row says its most rounds beside its round: %q", rows["review"])
	}
	if got := cell(rows["proof"], glyphAt, factoryRailW); got != asks {
		t.Errorf("the proof row, where the run asks for the sign-off, wears %q: %q", got, rows["proof"])
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

// A STAGE SOMEONE OTHER THAN THE RECIPE SET WEARS `+`, dim, in the glyph
// column; `?` wins the one cell where the run also asks there. Its reason is
// the dim `why:` line under its open head.
func TestFactoryStageRowAddedByAnother(t *testing.T) {
	f := &factoryFake{}
	factoryShapeItem(f, 2, func(it *factory.Item) {
		it.Stages = append([]factory.Stage(nil), it.Stages...)
		for i := range it.Stages {
			switch it.Stages[i].Name {
			case "neaten", "proof":
				it.Stages[i].By, it.Stages[i].Why = factory.ByManager, "the change touches the billing cache"
			case "write":
				it.Stages[i].By = factory.ByRecipe
			}
		}
	})
	a := factoryVerbLab(t, f)
	rows, body := factoryStageRowsAt(t, a, 2, 120)
	glyph := func(name string) string {
		return strings.TrimSpace(string([]rune(rows[name])[factoryRailW-factoryStageGlyphW:]))
	}
	if glyph("neaten") != a.factoryAddedMark() {
		t.Errorf("the neaten row the manager set does not wear %q:\n%s", a.factoryAddedMark(), strings.Join(body, "\n"))
	}
	if glyph("proof") != a.factoryAsksMark() || glyph("write") != "" {
		t.Errorf("proof wears %q and write %q", glyph("proof"), glyph("write"))
	}
	factoryOn(t, a, 2)
	it, _ := a.factoryCursorItem()
	stages := factoryStages(a.fp.snap, it)
	neaten, _ := factoryStageNamed(stages, "neaten")
	brief := a.factoryTLBrief(neaten, 120)
	if len(brief) != 2 || ansi.Strip(brief[1]) != wordWhyLabel+" the change touches the billing cache" || brief[1] != a.pal.dim(ansi.Strip(brief[1])) {
		t.Errorf("the why line is not dim under the ask: %q", brief)
	}
}

// THE OPEN HEAD SAYS THE LOOP WHOLE and the folded one keeps the strip's
// words; under the open head the first line is the stage's ask, dim.
func TestFactoryTLHeadSaysTheLoop(t *testing.T) {
	a := factoryVerbLab(t, &factoryFake{})
	factoryOn(t, a, 2)
	it, _ := a.factoryCursorItem()
	stages := factoryStages(a.fp.snap, it)
	open := ansi.Strip(a.factoryTLHead(it, 4, factoryMarkRunning, stages, true))
	want := a.factorySpin() + " review · round 1 of 2 · until clean · per finding"
	if !strings.HasPrefix(open, want) {
		t.Errorf("the open head is %q, not %q…", open, want)
	}
	if folded := ansi.Strip(a.factoryTLHead(it, 4, factoryMarkRunning, stages, false)); !strings.HasPrefix(folded, a.factorySpin()+" review 1/2") || strings.Contains(folded, wordUntil) {
		t.Errorf("the folded head is %q", folded)
	}
	if head := ansi.Strip(a.factoryTLHead(it, 0, factoryMarkDone, stages, true)); strings.Contains(head, wordRound) || !strings.Contains(head, "plan · until done") {
		t.Errorf("a one-round stage's open head is %q", head)
	}
	if head := ansi.Strip(a.factoryTLHead(it, 2, factoryMarkDone, stages, true)); !strings.Contains(head, "write · until done · per file") {
		t.Errorf("the write's open head is %q", head)
	}
	story := a.factoryTLStory(it, 120)
	for i, l := range story {
		if l.kind == factoryTLHead && l.phase == 4 {
			if i+1 >= len(story) || strings.TrimSpace(ansi.Strip(story[i+1].left)) != wordAskLabel+" read it as a stranger would" {
				t.Fatalf("the line under the open review head is not its ask: %q", ansi.Strip(story[i+1].left))
			}
			return
		}
	}
	t.Fatal("no review head in the story")
}

// WHAT CHANGED ABOUT THE STAGES IS THE STORY'S FIRST LINE, dim and whole on
// one line, and an item nobody changed draws none.
func TestFactoryTLAdaptedLineFirst(t *testing.T) {
	a := factoryVerbLab(t, &factoryFake{})
	story := a.factoryTLStory(*factoryPaneItem(t, a, 10), 120)
	if len(story) < 2 || story[0].left != a.pal.dim(factoryAdaptedWords) || story[1].left != "" {
		t.Fatalf("the story does not open on the adapted line and a blank: %q", story[0].left)
	}
	if first := ansi.Strip(a.factoryTLStory(*factoryPaneItem(t, a, 2), 120)[0].left); strings.Contains(first, "why:") {
		t.Fatalf("an item nobody changed opens on %q", first)
	}
}

// THE AUDIT: at 100, 120 and 150 the rail stays [factoryRailW] wide, every
// stage row's rounds end in one cell and its glyph stands in one cell.
func TestFactoryAlignStageLoops(t *testing.T) {
	a := factoryVerbLab(t, &factoryFake{})
	for _, width := range []int{100, 120, 150} {
		for _, id := range []int{2, 3} {
			rows, _ := factoryStageRowsAt(t, a, id, width)
			for name, row := range rows {
				r := []rune(row)
				if len(r) != factoryRailW {
					t.Errorf("at %d item %d's %s row is %d cells: %q", width, id, name, len(r), row)
					continue
				}
				if c := r[factoryRailW-factoryStageGlyphW-1]; c != ' ' {
					t.Errorf("at %d item %d's %s row has no air before its glyph: %q", width, id, name, row)
				}
				if rounds := strings.TrimSpace(string(r[factoryRailW-factoryStageGlyphW-1-factoryStageRoundsW : factoryRailW-factoryStageGlyphW-1])); strings.ContainsAny(rounds, "×/") && rounds != "×2" && rounds != "1/2" {
					t.Errorf("at %d item %d's %s rounds are out of their column: %q", width, id, name, row)
				}
			}
		}
	}
}
