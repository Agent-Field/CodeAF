package tui3

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// factoryFixing fixes the stages named for the test's length, through the
// accessor's hook (factory_stagefixed.go), until the field exists.
func factoryFixing(t *testing.T, names ...string) {
	t.Helper()
	fixed := map[string]bool{}
	for _, n := range names {
		fixed[n] = true
	}
	stageFixedFor = func(s factory.Stage) bool { return fixed[s.Name] }
	t.Cleanup(func() { stageFixedFor = nil })
}

// THE ONE SENTENCE, spelled out once here so a change to it is a change a
// person sees in this test.
func TestFactoryFixedWordsAreTheSentence(t *testing.T) {
	if got, want := factoryFixedWords("security"), "security is fixed by the recipe · change .codeaf/factory.md to change it"; got != want {
		t.Fatalf("the refusal is %q, want %q", got, want)
	}
}

// factorySettingsLine is the settings pane's row for the stage named name, as
// the body draws it, and the next stage row under it.
func factorySettingsLine(t *testing.T, body []string, name, next string) (string, string) {
	t.Helper()
	var row, after string
	for _, line := range body {
		// THE PANE IS RIGHT OF THE RULE; the rail left of it names stages too.
		_, line, div := factorySplitAt(line)
		if div < 0 {
			continue
		}
		f := strings.Fields(line)
		for i := 0; i+1 < len(f); i++ {
			if f[i+1] == name && len([]rune(f[i])) == 1 && row == "" {
				row = line
			}
			if f[i+1] == next && len([]rune(f[i])) == 1 && after == "" {
				after = line
			}
		}
	}
	if row == "" || after == "" {
		t.Fatalf("no settings rows for %s and %s:\n%s", name, next, strings.Join(body, "\n"))
	}
	return row, after
}

// A FIXED STAGE WEARS THE LOCK IN THE SETTINGS PANE where its `on` stands, in
// the same column, and its digit asks no door: the note line says the
// sentence. A stage beside it still turns.
func TestFactorySettingsPaneLocksAFixedStage(t *testing.T) {
	factoryFixing(t, "review")
	for _, width := range factoryAlignWidths {
		f := &factoryFake{}
		a := factoryVerbsLab(t, f, width)
		factoryVerbsOpen(t, a, 8)
		factoryRowNamed(t, a, wordFacetSettings)
		body := factoryExactBody(t, a, width, 40)
		lock := a.icon(tokens.GLocked)
		row, next := factorySettingsLine(t, body, "review", "neaten")
		if !strings.Contains(row, lock) || strings.HasSuffix(strings.TrimSpace(row), wordOn) {
			t.Fatalf("at %d the fixed row is %q, not the lock", width, row)
		}
		// THE LOCK STANDS IN THE `on` COLUMN (the alignment audit).
		if at, want := ansi.StringWidth(row[:strings.Index(row, lock)]), ansi.StringWidth(next[:strings.LastIndex(next, wordOn)]); at != want {
			t.Fatalf("at %d the lock stands at %d, the next row's on at %d:\n%s\n%s", width, at, want, row, next)
		}
		f.said()
		drive(t, a, key("4"))
		if got := f.said(); len(got) != 0 {
			t.Fatalf("at %d the fixed stage's digit asked %v", width, got)
		}
		if want := factoryFixedWords("review"); a.pageMsg != want {
			t.Fatalf("at %d the note says %q, want %q", width, a.pageMsg, want)
		}
		drive(t, a, key("5"))
		if got := strings.Join(f.said(), " "); got != "SetStage(8,4,false)" {
			t.Fatalf("at %d the stage beside it asked %q", width, got)
		}
	}
}

// THE RECIPE PAGE REFUSES THE SAME WAY: the fixed stage's digit, `w` on it and
// `s` words that name it change nothing and say the sentence, and its pane
// shows the lock with its ask dim.
func TestFactoryRecipePageRefusesAFixedStage(t *testing.T) {
	factoryFixing(t, "plan")
	f := newSettingsFake()
	a := factorySettingsLab(t, f, 150)
	drive(t, a, key("]"), key("E"))
	if a.fp.recipe == nil {
		t.Fatal("E opened no recipe page")
	}
	before := factory.CopyStages(a.fp.recipe.stages())
	if before[0].Name != "plan" || !before[0].On {
		t.Fatalf("the recipe's first stage is %+v", before[0])
	}
	body := strings.Join(factoryExactBody(t, a, 150, 14), "\n")
	if !strings.Contains(body, a.icon(tokens.GLocked)+" plan "+wordFixedByRecipe) {
		t.Fatalf("the pane shows no lock:\n%s", body)
	}
	want := factoryFixedWords("plan")
	drive(t, a, key("1"))
	if !a.fp.recipe.stages()[0].On || a.pageMsg != want {
		t.Fatalf("1 on a fixed stage: on=%v note=%q", a.fp.recipe.stages()[0].On, a.pageMsg)
	}
	a.pageMsg = ""
	drive(t, a, key("w"))
	if a.fp.act.ask != nil || a.pageMsg != want {
		t.Fatalf("w on a fixed stage opened %+v, note %q", a.fp.act.ask, a.pageMsg)
	}
	a.pageMsg = ""
	drive(t, a, key("s"))
	factoryType(t, a, "plan: guess")
	drive(t, a, key("enter"))
	if got := a.fp.recipe.stages(); len(got) != len(before) || got[0].Ask != before[0].Ask || a.pageMsg != want {
		t.Fatalf("s naming a fixed stage left %d stages, ask %q, note %q", len(got), got[0].Ask, a.pageMsg)
	}
	// A STAGE THE FILE DOES NOT FIX STILL TURNS.
	drive(t, a, key("2"))
	if a.fp.recipe.stages()[1].On {
		t.Fatal("2 did not switch the second stage off")
	}
}

// factoryOfferFake is [factoryFake] with the first offer's doors, keeping
// `not now` the way the store does: across windows.
type factoryOfferFake struct {
	*factoryFake
	away   map[string]bool
	branch string
	write  bool
}

func (f *factoryOfferFake) seam() factory.Seam {
	s := f.factoryFake.seam()
	s.RecipeOffer = func(_ context.Context, repo string) (bool, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return !f.away[repo], nil
	}
	if f.write {
		s.WriteRecipe = func(_ context.Context, repo string) (string, error) {
			return f.branch, f.rec("WriteRecipe", repo)
		}
	}
	s.RecipeNotNow = func(repo string) error {
		f.mu.Lock()
		f.away[repo] = true
		f.mu.Unlock()
		return f.rec("RecipeNotNow", repo)
	}
	return s
}

// factoryOfferLab is a window over the offer fake, its first read folded.
func factoryOfferLab(t *testing.T, f *factoryOfferFake) *app {
	t.Helper()
	a := placeApp(t)
	a.factory = f.seam()
	a.pal = newPalette(tokens.NoColor, false)
	a.width, a.height = 200, 44
	if cmd := a.showPage(pageFactory); cmd != nil {
		drive(t, a, runCmd(cmd)...)
	}
	if !a.at(pageFactory) || !a.fp.loaded {
		t.Fatal("the factory place did not open over the offer fake")
	}
	return a
}

const (
	factoryOfferHead = "no recipe in codeaf yet"
	factoryOfferAsk  = "write the recipe into the repo so the team shares it? [y] write it · [n] not now"
)

// A REPOSITORY WITH NO RECIPE FILE IS OFFERED ONE ONCE: the landed item's
// repository is offered at the bottom of the right column, `n` puts it away
// through the store, and neither this window's next read nor the next window
// offers it again.
func TestFactoryFirstRecipeOfferIsAskedOnce(t *testing.T) {
	f := &factoryOfferFake{factoryFake: &factoryFake{}, away: map[string]bool{}, write: true, branch: "main"}
	a := factoryOfferLab(t, f)
	// THE OFFER'S SENTENCE IS LONG; on the landed item's page the right
	// column holds it whole, as it holds the habit offer's.
	factoryOn(t, a, 9)
	drive(t, a, key("enter"))
	text := factoryFrameText(a)
	for _, want := range []string{factoryOfferHead, factoryOfferAsk} {
		if !strings.Contains(text, want) {
			t.Fatalf("the offer is missing %q:\n%s", want, text)
		}
	}
	if hint := factoryHint(a); !strings.HasPrefix(hint, "y write it · n not now") {
		t.Fatalf("the hint under the offer is %q", hint)
	}
	f.said()
	drive(t, a, key("n"))
	if got := strings.Join(f.said(), " "); got != "RecipeNotNow(agentfield/codeaf)" {
		t.Fatalf("n on the offer asked %q", got)
	}
	if a.fp.act.recipeOffer != "" || a.fp.act.ask != nil {
		t.Fatalf("n left the offer %q or opened %+v", a.fp.act.recipeOffer, a.fp.act.ask)
	}
	drive(t, a, runCmd(a.factoryRead())...)
	if text := factoryFrameText(a); strings.Contains(text, factoryOfferHead) {
		t.Fatalf("the next read offered it again:\n%s", text)
	}
	b := factoryOfferLab(t, f)
	if text := factoryFrameText(b); strings.Contains(text, factoryOfferHead) || b.fp.act.recipeOffer != "" {
		t.Fatalf("the next window offered it again:\n%s", text)
	}
}

// `y` WRITES IT through WriteRecipe and the note line says where.
func TestFactoryFirstRecipeOfferWritesOnY(t *testing.T) {
	f := &factoryOfferFake{factoryFake: &factoryFake{}, away: map[string]bool{}, write: true, branch: "main"}
	a := factoryOfferLab(t, f)
	if a.fp.act.recipeOffer != "agentfield/codeaf" {
		t.Fatalf("the offer stands for %q", a.fp.act.recipeOffer)
	}
	f.said()
	drive(t, a, key("y"))
	if got := strings.Join(f.said(), " "); got != "WriteRecipe(agentfield/codeaf)" {
		t.Fatalf("y on the offer asked %q", got)
	}
	if want := "recipe written · .codeaf/factory.md on main"; a.pageMsg != want {
		t.Fatalf("the note says %q, want %q", a.pageMsg, want)
	}
	if text := factoryFrameText(a); strings.Contains(text, factoryOfferHead) {
		t.Fatalf("y left the offer drawn:\n%s", text)
	}
}

// THE OFFER NEVER SHOWS WHERE THE WRITE DOOR IS ABSENT, and `y` and `n` are
// then the floor's own keys.
func TestFactoryFirstRecipeOfferNeedsTheWriteDoor(t *testing.T) {
	f := &factoryOfferFake{factoryFake: &factoryFake{}, away: map[string]bool{}}
	a := factoryOfferLab(t, f)
	if text := factoryFrameText(a); strings.Contains(text, factoryOfferHead) || a.fp.act.recipeOffer != "" {
		t.Fatalf("an offer with no write door:\n%s", text)
	}
}
