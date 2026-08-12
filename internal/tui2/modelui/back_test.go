package modelui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ONE CLICK, ONE LIST.
//
// The reported defect: "when I click to choose a model I don't get an OpenRouter
// list, rather some weird lists like voice, architect, skeptic". Two faults in
// one sentence — the surface opened on the ROLE list when the reader had already
// named a role, and the role list was spelled in words nobody was taught. These
// are the tests for the first; word_test.go holds the second.

// backing installs a path above the picker and records what it is asked to go
// back to, rather than performing it: this package holds words and never learns
// what they open.
func backing(p *Picker, trail ...string) *[]int {
	asked := &[]int{}
	p.SetBack(trail, func(depth int) tea.Cmd {
		*asked = append(*asked, depth)
		return nil
	})
	return asked
}

// A settings row that names one slot opens that slot's catalog. Not the five
// role rows with the right one selected — the models, with their price, window
// and score, which is the list the reader asked for.
func TestOpeningARoleShowsItsCatalogInOneStep(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())
	p.Open(store.RoleWork)

	if p.Level() != LevelModels {
		t.Fatalf("the picker is at level %v, want one role's models", p.Level())
	}
	if p.Role() != store.RoleWork {
		t.Fatalf("the picker opened role %q, want %q", p.Role(), store.RoleWork)
	}
	rendered := lines(p, 90, 12)
	for _, want := range []string{"claude-sonnet-4", "gpt-oss-120b", "qwen3.5-9b"} {
		if _, ok := find(rendered, want); !ok {
			t.Fatalf("%q is not on the models level:\n%s", want, strings.Join(rendered, "\n"))
		}
	}
	// And the cursor is on what the role is running RIGHT NOW, which is the
	// only way the ✓ is visible without scrolling.
	res, ok := p.Selected()
	if !ok {
		t.Fatal("the opened level has no choosable row under the cursor")
	}
	if set, is := res.(SetRole); !is || set.ModelSlug != "openai/gpt-oss-120b" {
		t.Fatalf("the cursor landed on %+v, want the model the role is bound to", res)
	}
}

// A slot outside the ladder is not a role whose catalog could be shown, so the
// surface stays where it is rather than opening a level with nothing in it and
// no honest word at its head.
func TestOpeningANonRoleLeavesThePickerWhereItIs(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())
	p.Open(store.ModelRole("image"))
	if p.Level() != LevelRoles {
		t.Fatalf("an unknown slot opened level %v, want the roles", p.Level())
	}
	p.Open("")
	if p.Level() != LevelRoles {
		t.Fatal("an empty slot opened a level")
	}
}

// A CATALOG THAT NEVER ARRIVED IS SAID, NOT SUBSTITUTED. The old door answered
// an empty catalog by leaving the reader on the five role words, which is
// exactly what they reported seeing instead of models. The honest answer is a
// sentence.
func TestAMissingCatalogSaysSoRatherThanShowingTheRoleWords(t *testing.T) {
	t.Parallel()
	c := sampleCatalog()
	c.Models = nil
	p := newPicker(c)
	// A role with no binding at this scope, so the level is empty rather than
	// holding the one row that clears a binding.
	p.Open(store.RoleVerify)

	rendered := lines(p, 90, 12)
	if _, ok := find(rendered, "no models to offer yet"); !ok {
		t.Fatalf("an empty catalog did not say so:\n%s", strings.Join(rendered, "\n"))
	}
	// The role list is one ascent away and is not what a reader looking for
	// models is left staring at.
	for _, role := range store.ModelRoles() {
		if role == store.RoleVerify {
			// The open role's own word is on the trail, which is where you are.
			continue
		}
		if _, ok := find(rendered[1:], RoleWord(role)); ok {
			t.Errorf("the empty models level is showing the role word %q", RoleWord(role))
		}
	}
}

// THE PATH ABOVE THE PICKER. Reached from the palette through the settings
// sheet, the trail starts at the palette and not halfway along the reader's own
// route.
func TestADrilledPickerDrawsThePathAboveItself(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())
	backing(p, "all", "settings")

	header := lines(p, 100, 12)[headerLine]
	want := trailLead + "all" + trailSep + trailLead + "settings" + trailSep + trailLead + rolesLabel
	if !strings.Contains(header, want) {
		t.Fatalf("the roles header reads %q, want it to contain %q", header, want)
	}
	if len(p.segs) != 2 {
		t.Fatalf("the roles level recorded %d click targets, want the two rungs above it", len(p.segs))
	}

	p.Open(store.RoleWork)
	header = lines(p, 100, 12)[headerLine]
	want += trailSep + trailLead + RoleWord(store.RoleWork)
	if !strings.Contains(header, want) {
		t.Fatalf("the models header reads %q, want it to contain %q", header, want)
	}
	if len(p.segs) != 3 {
		t.Fatalf("the models level recorded %d click targets, want two rungs plus the roles", len(p.segs))
	}
}

// Backspace walks ONE rung, wherever the reader is standing: from the models
// level to the roles, and from the roles level out of the picker entirely. The
// key means one thing at every depth, which is what makes it a grammar.
func TestBackspaceWalksOneRungAtEveryDepth(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())
	asked := backing(p, "all", "settings")
	p.Open(store.RoleWork)

	p.Key(namedKey(tea.KeyBackspace))
	if p.Level() != LevelRoles {
		t.Fatalf("backspace on the models level left it at %v", p.Level())
	}
	if len(*asked) != 0 {
		t.Fatalf("backspace left the picker when it had a level to ascend: %v", *asked)
	}

	p.Key(namedKey(tea.KeyBackspace))
	if len(*asked) != 1 || (*asked)[0] != 1 {
		t.Fatalf("backspace at the roles level asked for %v, want the rung directly above (1)", *asked)
	}

	// With text in the filter it is still an erase.
	p.Open(store.RoleWork)
	typeText(p, "claude")
	p.Key(namedKey(tea.KeyBackspace))
	if p.Query() != "claud" {
		t.Fatalf("backspace mid-filter left %q, want it to erase a letter", p.Query())
	}
	if len(*asked) != 1 {
		t.Fatalf("backspace mid-filter threw the level away: %v", *asked)
	}
}

// left is the arrow that already meant "out of this", and it walks the same
// ladder past the top of this surface.
func TestLeftAtTheRoleLevelLeavesThePicker(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())
	asked := backing(p, "all")
	p.Key(namedKey(tea.KeyLeft))
	if len(*asked) != 1 || (*asked)[0] != 0 {
		t.Fatalf("left at the roles level asked for %v, want depth 0", *asked)
	}
}

// A picker nobody drilled into keeps its own top: left and backspace there ask
// for nothing, because there is nothing above it.
func TestAnUndrilledPickerHasNothingAboveIt(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())
	if cmd := p.Key(namedKey(tea.KeyBackspace)); cmd != nil {
		t.Error("backspace on an undrilled roles level asked to go somewhere")
	}
	if cmd := p.Key(namedKey(tea.KeyLeft)); cmd != nil {
		t.Error("left on an undrilled roles level asked to go somewhere")
	}
	header := lines(p, 90, 12)[headerLine]
	if !strings.Contains(header, rolesLabel) || len(p.segs) != 0 {
		t.Fatalf("an undrilled roles header is %q with %d targets, want the bare label and none",
			header, len(p.segs))
	}
}

// The pointer walks the same path the keys do, and each word lands on its own
// rung — including the one this surface owns, which ascends inside it.
func TestClickingThePathAboveLeavesThePicker(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())
	asked := backing(p, "all", "settings")
	p.Open(store.RoleWork)
	lines(p, 100, 12)

	for i, seg := range p.segs {
		msg, at := clickAtXY(seg.at, headerLine)
		p.Mouse(msg, at)
		switch {
		case i < 2:
			if len(*asked) != i+1 || (*asked)[i] != i {
				t.Fatalf("clicking step %d asked for %v", i, *asked)
			}
		default:
			if p.Level() != LevelRoles {
				t.Fatal("clicking the roles step did not ascend inside the picker")
			}
			if len(*asked) != 2 {
				t.Fatalf("clicking the picker's own step left the picker: %v", *asked)
			}
		}
		p.Open(store.RoleWork)
		lines(p, 100, 12)
	}
}

// ESC IS NOT PART OF THE LADDER, at any depth and with any path above. What a
// reader watching a drilled picker wants gone is the picker.
func TestEscClosesTheWholeFlowFromADrilledPicker(t *testing.T) {
	t.Parallel()
	closed := 0
	p := New(Options{OnClose: func() tea.Cmd { closed++; return nil }})
	p.SetCatalog(sampleCatalog())
	asked := backing(p, "all", "settings")
	p.Open(store.RoleWork)

	p.Key(namedKey(tea.KeyEscape))
	if closed != 1 || len(*asked) != 0 {
		t.Fatalf("esc closed %d times and went back %v, want 1 and none", closed, *asked)
	}
}

// The ancestors above this surface are DOORS and sit at the secondary tier; the
// step the reader is standing on opens nothing and sits at the dimmest. 5.22's
// checklist: an interactive chip may never live permanently in the dimmest tier.
func TestThePathAboveIsInkedAsDoors(t *testing.T) {
	t.Parallel()
	p := New(Options{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)})
	p.SetCatalog(sampleCatalog())
	backing(p, "all")
	header := lines(p, 100, 12)[headerLine]

	door := tokens.TextSecondary.Fg(tokens.TrueColor, tokens.FocusNormal) + "all"
	if !strings.Contains(header, door) {
		t.Errorf("the rung above is not drawn as a door: %q", header)
	}
	label := tokens.TextTertiary.Fg(tokens.TrueColor, tokens.FocusNormal) + rolesLabel
	if !strings.Contains(header, label) {
		t.Errorf("the current step is not drawn at the dimmest tier: %q", header)
	}
}

// The mark this surface draws the path with is the palette's own, so the three
// surfaces draw one path and not three.
func TestThePickersTrailMarkIsThePalettesOwn(t *testing.T) {
	t.Parallel()
	if want := tokens.GlyphPromptChat + " "; trailLead != want {
		t.Fatalf("the picker's trail lead is %q, want the palette's %q", trailLead, want)
	}
	if blocks.Width(trailSep) != 1 {
		t.Fatalf("the trail separator is %d cells, want one", blocks.Width(trailSep))
	}
}
