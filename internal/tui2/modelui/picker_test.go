package modelui

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

func TestPickerIsAPane(t *testing.T) {
	t.Parallel()
	var p any = New(Options{})
	if _, ok := p.(tui2.Pane); !ok {
		t.Error("Picker is not a tui2.Pane")
	}
	if _, ok := p.(tui2.PaneKeys); !ok {
		t.Error("Picker is not a tui2.PaneKeys")
	}
	if _, ok := p.(tui2.PaneMouse); !ok {
		t.Error("Picker is not a tui2.PaneMouse")
	}
}

// 5.23: the surface shows the five roles as five rows — not a model catalog.
func TestTheFirstLevelIsAlwaysTheFiveRolesInLadderOrder(t *testing.T) {
	t.Parallel()
	for _, c := range []Catalog{{}, sampleCatalog()} {
		p := newPicker(c)
		if p.Level() != LevelRoles {
			t.Fatalf("picker opened at level %v, want the roles", p.Level())
		}
		if got := p.Total(); got != 5 {
			t.Fatalf("first level has %d rows, want the five roles", got)
		}
		rendered := lines(p, 70, 12)
		var seen []string
		for _, role := range store.ModelRoles() {
			line, ok := find(rendered, RoleWord(role))
			if !ok {
				t.Fatalf("role %q is missing from:\n%s", RoleWord(role), strings.Join(rendered, "\n"))
			}
			seen = append(seen, line)
		}
		// Ladder order, which is the order the doc names them in.
		last := -1
		for _, line := range seen {
			at := indexOfLine(rendered, line)
			if at <= last {
				t.Fatalf("roles are out of ladder order:\n%s", strings.Join(rendered, "\n"))
			}
			last = at
		}
	}
}

func TestARoleRowIsAChipWithItsProvenance(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())
	rendered := lines(p, 70, 12)

	voice, ok := find(rendered, RoleWord(store.RoleOrchestrate))
	if !ok {
		t.Fatalf("no voice row in:\n%s", strings.Join(rendered, "\n"))
	}
	if !strings.Contains(voice, "claude-sonnet-4") {
		t.Errorf("voice row does not name its model: %q", voice)
	}
	if strings.Contains(voice, "anthropic/") {
		t.Errorf("voice row leaked a provider id: %q", voice)
	}
	if !strings.Contains(voice, "global") {
		t.Errorf("voice row does not say where the binding came from: %q", voice)
	}
	if !strings.Contains(voice, tokens.Gauge(0.05)) {
		t.Errorf("voice row has no context gauge: %q", voice)
	}

	hands, _ := find(rendered, RoleWord(store.RoleWork))
	if !strings.Contains(hands, tokens.GlyphBoosted) {
		t.Errorf("the boosted role does not carry the boost mark: %q", hands)
	}
	if !strings.Contains(hands, "task") {
		t.Errorf("hands row does not say its binding is the task's: %q", hands)
	}

	// An unresolved role is unbound and says so, rather than being hidden.
	architect, _ := find(rendered, RoleWord(store.RolePlan))
	if !strings.Contains(architect, tokens.GlyphMissing) || !strings.Contains(architect, "unbound") {
		t.Errorf("an unbound role does not read as unbound: %q", architect)
	}
}

func TestOpeningARoleShowsTheCatalogOneLevelDeeper(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())
	openRole(p, store.RoleWork)

	if p.Level() != LevelModels || p.Role() != store.RoleWork {
		t.Fatalf("after enter: level %v role %q, want the models of work", p.Level(), p.Role())
	}
	rendered := lines(p, 70, 12)
	// The header names the role and the scope the binding will land in.
	if !strings.Contains(rendered[0], RoleWord(store.RoleWork)) || !strings.Contains(rendered[0], "this task") {
		t.Errorf("header does not name the role and its scope: %q", rendered[0])
	}
	for _, want := range []string{"claude-sonnet-4", "gpt-oss-120b", "gpt-oss-20b", "qwen3.5-9b"} {
		if _, ok := find(rendered, want); !ok {
			t.Errorf("model %q missing from:\n%s", want, strings.Join(rendered, "\n"))
		}
	}
	// The bound one carries the settled mark, and nothing else does.
	bound, _ := find(rendered, "gpt-oss-120b")
	if !strings.Contains(bound, tokens.GlyphSettled) {
		t.Errorf("the bound model is not marked: %q", bound)
	}
	other, _ := find(rendered, "qwen3.5-9b")
	if strings.Contains(other, tokens.GlyphSettled) {
		t.Errorf("an unbound model is marked as bound: %q", other)
	}
}

func TestChoosingAModelEmitsSetRoleAndCloses(t *testing.T) {
	t.Parallel()
	var got Result
	closed := 0
	p := New(Options{
		OnChoose: func(r Result) tea.Cmd { got = r; return nil },
		OnClose:  func() tea.Cmd { closed++; return nil },
	})
	p.SetCatalog(sampleCatalog())
	openRole(p, store.RoleVerify)
	p.Key(namedKey(tea.KeyDown)) // past "claude-sonnet-4" to "gpt-oss-120b"
	p.Key(namedKey(tea.KeyEnter))

	want := SetRole{Role: store.RoleVerify, ModelSlug: "openai/gpt-oss-120b", Scope: store.TaskScope("t-1")}
	if got != Result(want) {
		t.Fatalf("emitted %v, want %v", got, want)
	}
	if closed != 1 {
		t.Fatalf("closed %d times, want once", closed)
	}
}

func TestTheResultCarriesTheSlugAndNeverTheWord(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())
	openRole(p, store.RoleScribe)
	res, ok := p.Selected()
	if !ok {
		t.Fatal("nothing selected on the models level")
	}
	set, ok := res.(SetRole)
	if !ok {
		t.Fatalf("selected result is %T, want SetRole", res)
	}
	if set.ModelSlug != "anthropic/claude-sonnet-4" {
		t.Fatalf("result slug = %q, want the provider id verbatim", set.ModelSlug)
	}
	if set.Target() != string(store.RoleScribe) {
		t.Fatalf("Target = %q, want the role", set.Target())
	}
}

// A scope binds nothing → there is nothing to inherit back to, and offering the
// clear anyway would be an affordance that does nothing (5.22 rule 5).
func TestTheInheritRowAppearsOnlyWhereABindingExists(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())

	openRole(p, store.RoleWork) // BoundHere
	if _, ok := find(lines(p, 70, 12), inheritVerb); !ok {
		t.Fatal("a role bound at this scope offers no way to unbind it")
	}
	var got Result
	p.opts.OnChoose = func(r Result) tea.Cmd { got = r; return nil }
	p.cursor = 0
	p.Key(namedKey(tea.KeyEnter))
	want := ClearRole{Role: store.RoleWork, Scope: store.TaskScope("t-1")}
	if got != Result(want) {
		t.Fatalf("emitted %v, want %v", got, want)
	}

	p.Reset()
	openRole(p, store.RoleScribe) // resolved from a default, bound nowhere here
	if _, ok := find(lines(p, 70, 12), inheritVerb); ok {
		t.Fatal("a role with no binding here offers to clear one anyway")
	}
}

// 5.20 rule 3. The reason is the wiring's words and it is on the row.
func TestADisabledRowNamesItsReasonAndRefusesEnter(t *testing.T) {
	t.Parallel()
	c := sampleCatalog()
	c.Roles[1].Disabled = "no live work to move"
	chosen, closed := 0, 0
	p := New(Options{
		OnChoose: func(Result) tea.Cmd { chosen++; return nil },
		OnClose:  func() tea.Cmd { closed++; return nil },
	})
	p.SetCatalog(c)

	rendered := lines(p, 70, 12)
	hands, _ := find(rendered, RoleWord(store.RoleWork))
	if !strings.Contains(hands, "no live work to move") {
		t.Fatalf("the disabled role does not say why: %q", hands)
	}
	// The reason takes the description column, so the chip is not also there
	// claiming the binding is live.
	if strings.Contains(hands, tokens.GlyphBoosted) {
		t.Errorf("a disabled row still advertises its boost: %q", hands)
	}
	if strings.Contains(hands, "task") {
		t.Errorf("a disabled row still advertises its provenance column: %q", hands)
	}

	openRole(p, store.RoleWork)
	if p.Level() != LevelRoles {
		t.Fatal("enter on a disabled row opened it anyway")
	}
	if chosen != 0 || closed != 0 {
		t.Fatalf("a disabled row emitted %d results and closed %d times", chosen, closed)
	}
}

func TestASurfaceWideRefusalDisablesEveryRow(t *testing.T) {
	t.Parallel()
	c := sampleCatalog()
	c.Disabled = "visitor window — bindings cannot take effect"
	p := newPicker(c)
	rendered := lines(p, 78, 12)
	for _, role := range store.ModelRoles() {
		line, _ := find(rendered, RoleWord(role))
		if !strings.Contains(line, "visitor window") {
			t.Errorf("role %q does not carry the refusal: %q", RoleWord(role), line)
		}
	}
	openRole(p, store.RoleWork)
	if p.Level() != LevelRoles {
		t.Fatal("a refused surface still opened a role")
	}
}

func TestADisabledModelRefusesEnterAndKeepsItsReason(t *testing.T) {
	t.Parallel()
	c := sampleCatalog()
	c.Models[3].Disabled = "no API key for this provider"
	chosen := 0
	p := New(Options{OnChoose: func(Result) tea.Cmd { chosen++; return nil }})
	p.SetCatalog(c)
	openRole(p, store.RoleWork)
	p.Key(namedKey(tea.KeyEnd))

	rendered := lines(p, 70, 12)
	row, _ := find(rendered, "qwen3.5-9b")
	if !strings.Contains(row, "no API key") {
		t.Fatalf("the disabled model does not say why: %q", row)
	}
	if strings.Contains(row, "33K") {
		t.Errorf("a disabled row still advertises its window: %q", row)
	}
	if _, ok := p.Selected(); ok {
		t.Error("a disabled row reports a result")
	}
	p.Key(namedKey(tea.KeyEnter))
	if chosen != 0 {
		t.Fatalf("enter on a disabled model emitted %d results", chosen)
	}
}

// 12.3.5: effort has no journal axis. It is shown, and the surface says why it
// cannot be set rather than offering a control that would do nothing.
func TestEffortIsShownAndSaidToBeUnsettable(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())
	rendered := lines(p, 78, 12)
	clerk, _ := find(rendered, RoleWord(store.RoleScribe))
	if !strings.Contains(clerk, tokens.GlyphSeparator+" low") {
		t.Fatalf("the effort in the slug is not on the chip: %q", clerk)
	}

	openRole(p, store.RoleScribe)
	note, ok := find(lines(p, 78, 12), "rides the slug")
	if !ok {
		t.Fatalf("the models level does not say why effort cannot be set:\n%s",
			strings.Join(lines(p, 78, 12), "\n"))
	}
	if !strings.Contains(note, "shown, not set") {
		t.Errorf("the effort note is not a refusal: %q", note)
	}

	// A role whose slug carries no effort gets no note about one.
	p.Reset()
	openRole(p, store.RoleWork)
	if _, ok := find(lines(p, 78, 12), "rides the slug"); ok {
		t.Error("a role with no effort in its slug is told about effort anyway")
	}
}

// TestEscClosesFromEitherLevel is the inversion of the rule this surface used
// to hold, and it is 8.2.21 read for depth (internal/tui2/palette/trail.go):
// esc acts on what you are WATCHING, and what a reader watching a drilled
// picker wants gone is the picker, not one rung of it. Two keys with one
// meaning between them is how "press esc until something happens" gets learned.
func TestEscClosesFromEitherLevel(t *testing.T) {
	t.Parallel()
	closed := 0
	p := New(Options{OnClose: func() tea.Cmd { closed++; return nil }})
	p.SetCatalog(sampleCatalog())

	openRole(p, store.RoleVerify)
	p.Key(namedKey(tea.KeyEscape))
	if closed != 1 {
		t.Fatalf("esc on the models level closed %d times, want once", closed)
	}
	// It closed instead of unwinding: the level is untouched, because the
	// picker does not own the plane it is drawn on and Reset is what the wiring
	// calls on the way back in.
	if p.Level() != LevelModels {
		t.Fatalf("esc on the models level walked back to %v as well as closing", p.Level())
	}
	p.Key(namedKey(tea.KeyEscape))
	if closed != 2 {
		t.Fatalf("esc on the roles level closed %d times, want a second close", closed)
	}
}

// TestBackspaceOnAnEmptyQueryGoesBackOneLevel: the ladder esc gave up is
// carried by the key a shell path prompt and every file picker already teach.
func TestBackspaceOnAnEmptyQueryGoesBackOneLevel(t *testing.T) {
	t.Parallel()
	closed := 0
	p := New(Options{OnClose: func() tea.Cmd { closed++; return nil }})
	p.SetCatalog(sampleCatalog())

	openRole(p, store.RoleVerify)
	if cmd := p.Key(namedKey(tea.KeyBackspace)); cmd != nil || closed != 0 {
		t.Fatal("backspace on an empty query asked the shell to close")
	}
	if p.Level() != LevelRoles {
		t.Fatal("backspace on an empty query did not go back")
	}
	// And it lands on the row it came from — popping is a return, not a fresh
	// open (trail.go). Here the whole of the roles level's place is that row.
	if r, _ := p.selected(); r.role != store.RoleVerify {
		t.Fatalf("came back to %q, want the role that was open", r.role)
	}
	// On the roles level there is no query and nothing above to pop to, so the
	// key does nothing at all rather than closing the surface behind it.
	p.Key(namedKey(tea.KeyBackspace))
	if p.Level() != LevelRoles || closed != 0 {
		t.Fatalf("backspace on the roles level left level %v and %d closes", p.Level(), closed)
	}
}

// TestBackspaceWithTextEditsTheQuery is the other half of the same rule: a key
// that sometimes deleted a letter and sometimes threw away the list would make
// typing feel dangerous.
func TestBackspaceWithTextEditsTheQuery(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())
	openRole(p, store.RoleWork)

	typeText(p, "oss")
	p.Key(namedKey(tea.KeyBackspace))
	if p.Query() != "os" || p.Level() != LevelModels {
		t.Fatalf("backspace with text left query %q on level %v", p.Query(), p.Level())
	}
	// Down to empty, one letter at a time, and the level survives the last one:
	// the press that EMPTIES the field is an edit, not an ascent.
	p.Key(namedKey(tea.KeyBackspace))
	p.Key(namedKey(tea.KeyBackspace))
	if p.Query() != "" || p.Level() != LevelModels {
		t.Fatalf("emptying the field left query %q on level %v", p.Query(), p.Level())
	}
	p.Key(namedKey(tea.KeyBackspace))
	if p.Level() != LevelRoles {
		t.Fatal("the press after the field emptied did not go back")
	}
}

func TestTypingFiltersTheModelsAndNotTheRoles(t *testing.T) {
	t.Parallel()
	repaints := 0
	p := New(Options{Invalidate: func() { repaints++ }})
	p.SetCatalog(sampleCatalog())

	typeText(p, "oss")
	if p.Query() != "" || p.Count() != 5 {
		t.Fatalf("typing on the roles level filtered it: query %q, %d rows", p.Query(), p.Count())
	}

	openRole(p, store.RoleWork)
	before := repaints
	typeText(p, "oss")
	if p.Query() != "oss" {
		t.Fatalf("query = %q, want %q", p.Query(), "oss")
	}
	if p.Count() != 2 {
		t.Fatalf("%d rows survive %q, want the two gpt-oss models", p.Count(), "oss")
	}
	if repaints <= before {
		t.Error("typing did not ask for a repaint; the character would never appear")
	}
	// The note matches too — a user searching "cheap" is searching the words
	// the wiring wrote.
	p.Key(ctrlKey('u'))
	typeText(p, "cheap")
	if p.Count() != 2 {
		t.Fatalf("%d rows survive %q, want the two the note describes", p.Count(), "cheap")
	}
}

func TestEditingKeys(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())
	openRole(p, store.RoleWork)

	typeText(p, "gpt oss")
	p.Key(namedKey(tea.KeyBackspace))
	if p.Query() != "gpt os" {
		t.Fatalf("backspace left %q", p.Query())
	}
	p.Key(ctrlKey('w'))
	if p.Query() != "gpt " {
		t.Fatalf("ctrl+w left %q", p.Query())
	}
	p.Key(ctrlKey('u'))
	if p.Query() != "" {
		t.Fatalf("ctrl+u left %q", p.Query())
	}
	// A bare modifier types nothing.
	p.Key(tea.KeyPressMsg{Mod: tea.ModCtrl})
	if p.Query() != "" {
		t.Fatalf("a modifier typed %q", p.Query())
	}
}

func TestOverFilteringAndEmptinessTeachDifferentThings(t *testing.T) {
	t.Parallel()
	p := newPicker(Catalog{})
	openRole(p, store.RoleWork)
	if _, ok := find(lines(p, 60, 8), "no models to offer yet"); !ok {
		t.Error("an empty catalog does not say it is empty")
	}
	p2 := newPicker(sampleCatalog())
	openRole(p2, store.RoleWork)
	typeText(p2, "zzz")
	if _, ok := find(lines(p2, 60, 8), "no model matches zzz"); !ok {
		t.Error("an over-filtered list does not name the query back")
	}
}

func TestNavigationClampsAtBothEnds(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())
	for i := 0; i < 20; i++ {
		p.Key(namedKey(tea.KeyUp))
	}
	if p.cursor != 0 {
		t.Fatalf("cursor ran off the top to %d", p.cursor)
	}
	for i := 0; i < 20; i++ {
		p.Key(namedKey(tea.KeyDown))
	}
	if p.cursor != 4 {
		t.Fatalf("cursor = %d, want the last of five", p.cursor)
	}
	p.Key(namedKey(tea.KeyHome))
	if p.cursor != 0 {
		t.Fatalf("home left the cursor at %d", p.cursor)
	}
}

func TestClickRunsTheRowUnderThePointerAndChromeDoesNothing(t *testing.T) {
	t.Parallel()
	var got Result
	p := New(Options{OnChoose: func(r Result) tea.Cmd { got = r; return nil }})
	p.SetCatalog(sampleCatalog())
	lines(p, 70, 12)

	// A click on the header opens nothing.
	p.Mouse(clickAt(0))
	if p.Level() != LevelRoles {
		t.Fatal("a click on chrome opened a role")
	}
	// A click on the third body row opens the hands.
	p.Mouse(clickAt(p.bodyTop + 2))
	if p.Level() != LevelModels || p.Role() != store.RoleWork {
		t.Fatalf("click opened %v/%q, want the work role's models", p.Level(), p.Role())
	}
	lines(p, 70, 12)
	p.Mouse(clickAt(p.bodyTop + 1)) // past the inherit row, onto the first model
	if _, ok := got.(SetRole); !ok {
		t.Fatalf("a click on a model emitted %v, want a SetRole", got)
	}
}

func TestWheelMovesTheSelectionWithoutChoosing(t *testing.T) {
	t.Parallel()
	chosen := 0
	p := New(Options{OnChoose: func(Result) tea.Cmd { chosen++; return nil }})
	p.SetCatalog(sampleCatalog())
	lines(p, 70, 12)
	p.Mouse(wheelAt(tea.MouseWheelDown))
	if p.cursor != wheelStep {
		t.Fatalf("wheel moved to %d, want %d", p.cursor, wheelStep)
	}
	if chosen != 0 {
		t.Fatal("a wheel notch chose a row")
	}
}

func TestChoosingWithNoCallbacksEmitsMessages(t *testing.T) {
	t.Parallel()
	p := New(Options{})
	p.SetCatalog(sampleCatalog())
	openRole(p, store.RoleWork)
	p.cursor = 1 // past inherit
	msg := drain(p.Key(namedKey(tea.KeyEnter)))
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("enter produced %T, want a batch of choose and close", msg)
	}
	var sawChoose, sawClose bool
	for _, cmd := range batch {
		switch m := drain(cmd).(type) {
		case ChooseMsg:
			sawChoose = true
			if _, ok := m.Result.(SetRole); !ok {
				t.Errorf("ChooseMsg carries %T", m.Result)
			}
		case CloseMsg:
			sawClose = true
		}
	}
	if !sawChoose || !sawClose {
		t.Fatalf("choose=%v close=%v, want both", sawChoose, sawClose)
	}
}

func TestResetReturnsToTheRolesAndClearsTheQuery(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())
	openRole(p, store.RoleWork)
	typeText(p, "oss")
	p.Reset()
	if p.Level() != LevelRoles || p.Query() != "" || p.cursor != 0 {
		t.Fatalf("after reset: level %v query %q cursor %d", p.Level(), p.Query(), p.cursor)
	}
}

// A catalog arriving while the user is three keystrokes into a decision must
// not move the surface under their hand (7.2).
func TestACatalogRefreshKeepsTheLevelAndTheSelection(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())
	openRole(p, store.RoleWork)
	p.Key(namedKey(tea.KeyEnd))
	before, ok := p.Selected()
	if !ok {
		t.Fatal("nothing selected")
	}

	c := sampleCatalog()
	// Something finished elsewhere and a row was inserted above the selection.
	c.Models = append([]ModelOption{{Slug: "new/model-1", Window: 8_000}}, c.Models...)
	p.SetCatalog(c)

	if p.Level() != LevelModels || p.Role() != store.RoleWork {
		t.Fatalf("a refresh threw the user back to %v", p.Level())
	}
	after, ok := p.Selected()
	if !ok || after != before {
		t.Fatalf("selection moved from %v to %v", before, after)
	}
}

// The production bar: every line fits, nothing panics, at every width and
// height a terminal can produce.
func TestWidthSweepNeverOverflowsAndNeverPanics(t *testing.T) {
	t.Parallel()
	c := sampleCatalog()
	c.Roles = append(c.Roles, RoleRow{
		Role:     store.RolePlan,
		Model:    "a-vendor/an-extremely-long-model-name-nobody-would-ship:high",
		Source:   store.RoleFromPin,
		Disabled: "pinned at this node — a pin outranks a binding",
	})
	c.Models = append(c.Models, ModelOption{
		Slug: "彼ら/日本語のモデル", Window: 1_000_000, Note: "wide runes in every column",
	})
	// Linear mode is swept too: it spends an extra left column ([markerCol]),
	// which is exactly the kind of change that walks a row off a narrow frame.
	for _, opts := range []Options{
		{},
		{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)},
		{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal), Linear: true},
	} {
		p := New(opts)
		p.SetCatalog(c)
		for _, level := range []Level{LevelRoles, LevelModels} {
			if level == LevelModels {
				p.Reset()
				openRole(p, store.RoleWork)
				typeText(p, "o")
			}
			for w := 1; w <= 120; w++ {
				for _, h := range []int{1, 2, 3, 5, 12, 40} {
					out := p.Render(w, h)
					rows := strings.Split(out, "\n")
					if out == "" {
						rows = nil
					}
					if len(rows) > h {
						t.Fatalf("level %v at %dx%d rendered %d lines", level, w, h, len(rows))
					}
					for i, line := range rows {
						if got := blocks.Width(line); got > w {
							t.Fatalf("level %v at %dx%d line %d is %d cells: %q", level, w, h, i, got, line)
						}
					}
				}
			}
		}
	}
}

func TestZeroSizesRenderNothing(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())
	for _, size := range [][2]int{{0, 10}, {10, 0}, {0, 0}, {-4, 10}, {10, -4}} {
		if got := p.Render(size[0], size[1]); got != "" {
			t.Errorf("Render(%d,%d) = %q, want nothing", size[0], size[1], got)
		}
	}
}

func TestColumnsDoNotDanceWhileTyping(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())
	openRole(p, store.RoleWork)
	at := func() int {
		row, _ := find(lines(p, 70, 12), "gpt-oss-120b")
		return strings.Index(row, "cheap, fast")
	}
	before := at()
	typeText(p, "oss")
	if after := at(); after != before {
		t.Fatalf("the description column moved from %d to %d while typing", before, after)
	}
}

func TestSelectionBrightensAndDisabledRowsDoNot(t *testing.T) {
	t.Parallel()
	c := sampleCatalog()
	c.Roles[1].Disabled = "no live work to move"
	p := New(Options{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)})
	p.SetCatalog(c)

	rendered := lines(p, 70, 12)
	selected := rendered[p.bodyTop]
	if !strings.Contains(selected, tokens.Band.Bg(tokens.TrueColor, tokens.FocusNormal)) &&
		!strings.Contains(selected, tokens.Reverse(tokens.TrueColor)) {
		t.Errorf("the selected row carries no band: %q", selected)
	}
	disabled, _ := find(rendered, "no live work to move")
	if strings.Contains(disabled, tokens.TextPrimary.Fg(tokens.TrueColor, tokens.FocusNormal)) {
		t.Errorf("a disabled row was promoted into the primary tier: %q", disabled)
	}
}

// TestLinearPaintsNoGroundAndNoBand is 10.1.5, and it is the same bargain
// internal/tui2/settings and internal/tui2/consentui already strike: the ground
// and the band are both background fills, a fill is what a screen reader cannot
// see and what a high-contrast terminal may render as a solid block, and neither
// carries a fact — so both go. What may NOT go is the selection, so the ▎ of
// [markerCol] takes it over, and it is checked on both levels because the
// picker's two depths draw their rows through one function and would fail
// together or not at all.
func TestLinearPaintsNoGroundAndNoBand(t *testing.T) {
	t.Parallel()
	styler := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	ground := tokens.Sheet.Bg(tokens.TrueColor, tokens.FocusNormal)
	band := tokens.Band.Bg(tokens.TrueColor, tokens.FocusNormal)

	open := func(linear bool) *Picker {
		p := New(Options{Styler: styler, Linear: linear})
		p.SetCatalog(sampleCatalog())
		return p
	}

	// The control: without it a picker that stopped rendering would pass every
	// assertion below.
	ordinary := strings.Join(lines(open(false), 70, 12), "\n")
	if !strings.Contains(ordinary, ground) || !strings.Contains(ordinary, band) {
		t.Fatalf("the ordinary picker draws no ground or no band, so this test measures nothing:\n%q", ordinary)
	}

	for _, c := range []struct {
		name string
		open func() *Picker
	}{
		{"roles", func() *Picker { return open(true) }},
		{"models", func() *Picker {
			p := open(true)
			openRole(p, store.RoleWork)
			return p
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			rendered := lines(c.open(), 70, 12)
			frame := strings.Join(rendered, "\n")
			if strings.Contains(frame, ground) {
				t.Errorf("linear mode still paints the sheet's ground:\n%q", frame)
			}
			if strings.Contains(frame, band) {
				t.Errorf("linear mode still paints the selection band:\n%q", frame)
			}
			marked := 0
			for _, line := range rendered {
				if strings.Contains(line, tokens.GlyphAccentRail) {
					marked++
				}
			}
			if marked != 1 {
				t.Errorf("%d rows carry the selection marker, want exactly 1:\n%q", marked, frame)
			}
		})
	}
}

// 5.14: never show ids. A scope's target is an id.
func TestTheScopeIsAWordAndNeverAnID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		scope store.BindingScope
		want  string
	}{
		{store.ScopeGlobal, "everywhere"},
		{store.TaskScope("t-01H8XYZ"), "this task"},
		{store.NodeScope("n-01H8XYZ"), "this worker"},
		{store.BindingScope(""), "everywhere"},
	}
	for _, c := range cases {
		p := newPicker(Catalog{Scope: c.scope, Models: []ModelOption{{Slug: "x/y"}}})
		rendered := lines(p, 70, 12)
		if !strings.Contains(rendered[0], c.want) {
			t.Errorf("scope %q reads %q, want %q", c.scope, rendered[0], c.want)
		}
		if strings.Contains(rendered[0], "01H8XYZ") {
			t.Errorf("scope %q leaked its target id: %q", c.scope, rendered[0])
		}
	}
}

// An invalid scope must not become a binding nobody can find or clear.
func TestAnInvalidScopeDegradesToGlobal(t *testing.T) {
	t.Parallel()
	p := newPicker(Catalog{
		Scope:  store.BindingScope("task:"),
		Models: []ModelOption{{Slug: "x/y"}},
	})
	openRole(p, store.RoleWork)
	res, ok := p.Selected()
	if !ok {
		t.Fatal("nothing selected")
	}
	if got := res.(SetRole).Scope; got != store.ScopeGlobal {
		t.Fatalf("an invalid scope emitted %q, want global", got)
	}
}

// Every overlay advertises its own door (5.22), and it advertises it as a
// verb·key chip: the VERB first, the key after it (§16 THE VERB·KEY CHIP). This
// header read "esc close" until the chip grammar landed — a key and a verb in
// one tier, which a reader can only parse by already knowing the answer.
//
// It is the SAME chip on both levels now. The models level used to say `back
// esc`, which was the one key in the product printed with two different
// promises; esc closes from either level, so it advertises one thing.
func TestTheHeaderAdvertisesItsExitAsAVerbThenItsKey(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())
	roles := lines(p, 90, 12)[0]
	if !strings.Contains(roles, closeChip.String()) {
		t.Errorf("the roles level does not advertise its door: %q", roles)
	}
	openRole(p, store.RoleWork)
	models := lines(p, 90, 12)[0]
	if !strings.Contains(models, closeChip.String()) {
		t.Errorf("the models level does not advertise its door: %q", models)
	}
	for _, line := range []string{roles, models} {
		if strings.Contains(line, "esc close") || strings.Contains(line, "esc back") {
			t.Errorf("the door is spelled key-first: %q", line)
		}
		// The chip that promised esc would only go back is gone with the
		// behaviour it described.
		if strings.Contains(line, "back esc") {
			t.Errorf("the header still advertises esc as back: %q", line)
		}
	}
}

// TestTheModelsHeaderDrawsTheTrailAndOnlyTheAncestorIsADoor is the drill
// grammar (internal/tui2/palette/trail.go) on this picker: the header's first
// words are the path the reader walked, the ancestor step is a click target
// back to it, and the step they are standing on is a label.
func TestTheModelsHeaderDrawsTheTrailAndOnlyTheAncestorIsADoor(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())

	// The roles level is depth zero: it draws its label and records no target,
	// because a one-step path is a label pretending to be navigation.
	roles := lines(p, 90, 12)[0]
	if !strings.Contains(roles, rolesLabel) {
		t.Fatalf("the roles header does not name the level: %q", roles)
	}
	if len(p.segs) != 0 {
		t.Errorf("the roles level recorded %d click targets, want none", len(p.segs))
	}

	openRole(p, store.RoleWork)
	header := lines(p, 90, 12)[0]
	want := trailLead + rolesLabel + trailSep + trailLead + RoleWord(store.RoleWork)
	if !strings.Contains(header, want) {
		t.Fatalf("the trail reads %q, want it to contain %q", header, want)
	}

	// One ancestor, one target, on exactly the cells the ancestor word drew.
	if len(p.segs) != 1 {
		t.Fatalf("the trail recorded %d click targets, want 1", len(p.segs))
	}
	seg := p.segs[0]
	if seg.level != LevelRoles {
		t.Errorf("the trail's ancestor returns to %v, want the roles", seg.level)
	}
	if at, w := blocks.Width(trailLead), blocks.Width(rolesLabel); seg.at != at || seg.w != w {
		t.Errorf("the ancestor is recorded at %d+%d, want %d+%d", seg.at, seg.w, at, w)
	}
	// The step the reader is standing on opens nothing: clicking where you
	// already are must do nothing rather than rebuild the list under the
	// pointer.
	current := blocks.Width(trailLead + rolesLabel + trailSep + trailLead)
	for x := current; x < current+blocks.Width(RoleWord(store.RoleWork)); x++ {
		if _, hit := hitAt(p.segs, x); hit {
			t.Fatalf("the current step is a click target at column %d", x)
		}
	}
}

// The two steps are drawn in two tiers, and that split is 5.22's checklist
// rather than a preference: the ancestor is a DOOR and an interactive chip may
// never live permanently in the dimmest tier, while the current step opens
// nothing and is a label.
func TestTheTrailSpendsATierOnWhatIsADoor(t *testing.T) {
	t.Parallel()
	p := New(Options{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)})
	p.SetCatalog(sampleCatalog())
	openRole(p, store.RoleWork)
	header := lines(p, 90, 12)[0]

	door := tokens.TextSecondary.Fg(tokens.TrueColor, tokens.FocusNormal) + rolesLabel
	if !strings.Contains(header, door) {
		t.Errorf("the ancestor step is not drawn as a door: %q", header)
	}
	label := tokens.TextTertiary.Fg(tokens.TrueColor, tokens.FocusNormal) + RoleWord(store.RoleWork)
	if !strings.Contains(header, label) {
		t.Errorf("the current step is not drawn at the dimmest tier: %q", header)
	}
}

// TestClickingTheTrailGoesBackToThatLevel: the pointer's whole way out of a
// drilled list, which this surface did not have at all — the header was not a
// target and esc was the only exit from the level.
func TestClickingTheTrailGoesBackToThatLevel(t *testing.T) {
	t.Parallel()
	repaints := 0
	p := New(Options{Invalidate: func() { repaints++ }})
	p.SetCatalog(sampleCatalog())
	openRole(p, store.RoleVerify)
	lines(p, 90, 12)

	// The lead is not the word: a click before the ancestor step is chrome.
	p.Mouse(clickAtXY(0, headerLine))
	if p.Level() != LevelModels {
		t.Fatal("a click on the trail's lead left the level")
	}
	// And neither is the step the reader is standing on.
	current := blocks.Width(trailLead + rolesLabel + trailSep + trailLead)
	p.Mouse(clickAtXY(current, headerLine))
	if p.Level() != LevelModels {
		t.Fatal("a click on the current step left the level")
	}

	before := repaints
	door := p.segs[0].at
	p.Mouse(clickAtXY(door, headerLine))
	if p.Level() != LevelRoles {
		t.Fatalf("clicking %q left the picker on %v", rolesLabel, p.Level())
	}
	// It is the same return the key takes: the row that was open is the row the
	// reader lands on.
	if r, _ := p.selected(); r.role != store.RoleVerify {
		t.Fatalf("came back to %q, want the role that was open", r.role)
	}
	if repaints <= before {
		t.Error("the jump did not ask for a repaint; the old level would stay on screen")
	}
	// Back at the roles level the target is gone with the trail that drew it,
	// so the same column is chrome again rather than a stale hit table.
	lines(p, 90, 12)
	if len(p.segs) != 0 {
		t.Fatalf("the roles level kept %d click targets from the level below", len(p.segs))
	}
	if cmd := p.Mouse(clickAtXY(door, headerLine)); cmd != nil {
		t.Error("a click on the roles header did something")
	}
}

// The two halves are drawn in two tiers, which is the other half of the rule: a
// verb and its key in one grey are two unmarked words.
func TestTheDoorChipSpendsTwoTiers(t *testing.T) {
	t.Parallel()
	p := New(Options{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)})
	p.SetCatalog(sampleCatalog())
	header := lines(p, 70, 12)[0]
	verb := tokens.TextSecondary.Fg(tokens.TrueColor, tokens.FocusNormal) + closeChip.Verb
	key := tokens.TextTertiary.Fg(tokens.TrueColor, tokens.FocusNormal) + registry.ChipGap + closeChip.Key
	if !strings.Contains(header, verb) {
		t.Errorf("the verb is not at the brighter tier: %q", header)
	}
	if !strings.Contains(header, key) {
		t.Errorf("the key is not one tier down: %q", header)
	}
}

// A width that cannot hold the chip keeps the COUNT, because esc answers
// everywhere and the count is stated nowhere else. The trail is drawn on the
// same budget and takes cells from the same line, so the degradation is the
// existing one, walked further out: count wins over chip, still.
func TestTheCountOutlivesTheDoorChip(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())
	openRole(p, store.RoleWork)
	var sawBoth, sawCountAlone bool
	for w := 24; w <= 110; w++ {
		header := lines(p, w, 10)[0]
		count := strings.Contains(header, "/"+strconv.Itoa(p.Total()))
		chip := strings.Contains(header, closeChip.String())
		switch {
		case chip:
			if !count {
				t.Fatalf("at width %d the chip outlived the count: %q", w, header)
			}
			sawBoth = true
		case count:
			sawCountAlone = true
		}
	}
	if !sawBoth || !sawCountAlone {
		t.Errorf("the tail never used both steps: both=%v count-alone=%v", sawBoth, sawCountAlone)
	}
}

// The chip is the row's fact and the provenance word is its qualifier, so the
// provenance goes first under width pressure. A row that kept "global" and
// dropped the model would be answering "where from" about a value it no longer
// shows.
func TestTheModelOutlivesTheProvenanceColumn(t *testing.T) {
	t.Parallel()
	p := newPicker(sampleCatalog())
	for w := 20; w <= 70; w++ {
		row, ok := find(lines(p, w, 10), RoleWord(store.RoleScribe))
		if !ok {
			continue
		}
		hasModel := strings.Contains(row, "gpt-oss") || strings.Contains(row, "gpt-os…") ||
			strings.Contains(row, "gpt-…") || strings.Contains(row, "g…")
		if strings.Contains(row, "default") && !hasModel {
			t.Fatalf("at width %d the row kept its provenance and lost its model: %q", w, row)
		}
	}
}

func indexOfLine(rendered []string, want string) int {
	for i, line := range rendered {
		if line == want {
			return i
		}
	}
	return -1
}
