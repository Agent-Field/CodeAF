package tui3

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// shapingItem is an item waiting on the shaping question with n stages on.
func shapingItem(n int) factory.Item {
	it := factory.Item{
		State:    factory.StateNeedsYou,
		QKind:    "plan",
		Question: "run these stages? manager set review: thorough on security, code and architecture · why: touches the call row",
		Adapted:  []string{"manager set review: thorough on security, code and architecture", "why: touches the call row"},
	}
	for i := 0; i < n; i++ {
		st := factory.Stage{Name: fmt.Sprintf("s%d", i+1), Ask: fmt.Sprintf("the ask of stage %d, said in a sentence that runs on and on and on", i+1), On: true}
		switch i {
		case 0:
			st = factory.Stage{Name: "read", Ask: "the diff and its claims", On: true}
		case 1:
			st = factory.Stage{Name: "review", Ask: "thorough on security, code and architecture", On: true}
		}
		it.Stages = append(it.Stages, st)
	}
	return it
}

func shapingPlain(a *app, it factory.Item, measure, room int) []string {
	var out []string
	for _, l := range a.factoryShapingBlock(it, measure, room) {
		out = append(out, strings.TrimRight(ansi.Strip(l), " "))
	}
	return out
}

// THE SHAPING QUESTION IS DRAWN AS THE STAGES, not one long line.
func TestFactoryShapingQuestionIsTheStages(t *testing.T) {
	a := factoryPlaceLab(t)
	a.factory = (&factoryFake{}).seam()
	icon := ansi.Strip(a.icon(tokens.GNeedsHuman))
	for _, w := range []int{120, 100} {
		rows := shapingPlain(a, shapingItem(3), w, 12)
		want := []string{
			icon + " run these stages?",
			"  1 read · the diff and its claims",
			"  2 review · thorough on security, code and architecture",
			"  3 s3 · the ask of stage 3, said in a sentence that runs on and on and on",
			"  manager set review: thorough on security, code and architecture · why: touches the call row",
			"  y run · n keep the recipe · a in words",
		}
		if strings.Join(rows, "\n") != strings.Join(want, "\n") {
			t.Fatalf("at %d columns:\n%s\nwant:\n%s", w, strings.Join(rows, "\n"), strings.Join(want, "\n"))
		}
		for _, r := range rows {
			if ansi.StringWidth(r) > w {
				t.Fatalf("a row overruns %d: %q", w, r)
			}
		}
	}
	t.Log("\n" + strings.Join(shapingPlain(a, shapingItem(3), 120, 12), "\n"))
}

// A PANE TOO SHORT FOR THE STAGES CUTS THE ASKS, then ends on `… N more`.
func TestFactoryShapingQuestionCutsToMore(t *testing.T) {
	a := factoryPlaceLab(t)
	a.factory = (&factoryFake{}).seam()
	for _, w := range []int{120, 100} {
		rows := shapingPlain(a, shapingItem(12), w, 8)
		if len(rows) != 8 {
			t.Fatalf("at %d: %d rows, want 8:\n%s", w, len(rows), strings.Join(rows, "\n"))
		}
		if rows[1] != "  1 read" || rows[4] != "  4 s4" || rows[5] != "  … 8 more" {
			t.Fatalf("at %d the asks are not cut and the stages not ended on a count:\n%s", w, strings.Join(rows, "\n"))
		}
		if !strings.HasPrefix(rows[7], "  y run") {
			t.Fatalf("the keys are not last: %q", rows[7])
		}
	}
	// Twelve stages with room to spare keep every ask.
	rows := shapingPlain(a, shapingItem(12), 120, 20)
	if len(rows) != 12+3 || !strings.Contains(rows[2], "thorough on security") || strings.Contains(strings.Join(rows, "\n"), "more") {
		t.Fatalf("a roomy pane cut the stages:\n%s", strings.Join(rows, "\n"))
	}
}

// A STAGE SWITCHED OFF IS NOT DRAWN AND KEEPS ITS NUMBER; other plan questions
// are drawn as they were.
func TestFactoryShapingOnlyForTheShapingQuestion(t *testing.T) {
	a := factoryPlaceLab(t)
	it := shapingItem(3)
	it.Stages[1].On = false
	rows := shapingPlain(a, it, 120, 12)
	if strings.Join(rows[1:3], "|") != "  1 read · the diff and its claims|  3 s3 · the ask of stage 3, said in a sentence that runs on and on and on" {
		t.Fatalf("an off stage is drawn or renumbered:\n%s", strings.Join(rows, "\n"))
	}
	it.Question = "plan is ready · go, or change it?"
	if factoryIsShaping(it) {
		t.Fatal("another plan question is read as the shaping one")
	}
	it.Question, it.QKind = "run these stages?", "rounds"
	if factoryIsShaping(it) {
		t.Fatal("another kind is read as the shaping one")
	}
}

// THE PEEK'S QUESTION LINE IS `? run these stages?`, the stages under it.
func TestFactoryShapingPeekQuestionLine(t *testing.T) {
	a := factoryPlaceLab(t)
	a.factory = (&factoryFake{}).seam()
	lines := a.factoryPeekQuestion(shapingItem(3), 70, 12)
	if got := ansi.Strip(lines[0]); got != ansi.Strip(a.icon(tokens.GNeedsHuman))+" run these stages?" {
		t.Fatalf("the peek's question line is %q", got)
	}
	if got := ansi.Strip(a.factoryItemQuestion(shapingItem(3), 120)); !strings.Contains(got, " run these stages?") || strings.Contains(got, "manager set") {
		t.Fatalf("the item head's question is %q", got)
	}
}
