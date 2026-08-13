package palette

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// collect flattens whatever a command produced, expanding a batch, so a test
// can assert on the messages a keypress actually emits.
func collect(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for _, c := range batch {
		out = append(out, collect(c)...)
	}
	return out
}

type recorder struct {
	chosen []Result
	closes int
	dirty  int
}

func (r *recorder) options() Options {
	return Options{
		Invalidate: func() { r.dirty++ },
		OnChoose:   func(res Result) tea.Cmd { r.chosen = append(r.chosen, res); return nil },
		OnClose:    func() tea.Cmd { r.closes++; return nil },
	}
}

// TestEscClosesAndIsConsumed is 8.2.21 resolved for an overlay: while it is
// up, esc is about the overlay and nothing underneath hears it.
func TestEscClosesAndIsConsumed(t *testing.T) {
	rec := &recorder{}
	p := New(rec.options())
	p.SetCatalog(demoCatalog())
	typeText(p, "can")

	p.Key(namedKey(tea.KeyEscape))
	if rec.closes != 1 {
		t.Fatalf("esc closed %d times, want 1", rec.closes)
	}
	if len(rec.chosen) != 0 {
		t.Errorf("esc chose something: %v", rec.chosen)
	}

	// With no callback bound, the same key emits the message instead.
	plain := New(Options{})
	plain.SetCatalog(demoCatalog())
	msgs := collect(plain.Key(namedKey(tea.KeyEscape)))
	if len(msgs) != 1 {
		t.Fatalf("esc emitted %d messages, want 1", len(msgs))
	}
	if _, ok := msgs[0].(CloseMsg); !ok {
		t.Errorf("esc emitted %T, want CloseMsg", msgs[0])
	}
}

func TestEnterYieldsTheSelectedResultThenCloses(t *testing.T) {
	rec := &recorder{}
	p := New(rec.options())
	p.SetCatalog(demoCatalog())

	// Row 0 is the first action of the leading section, in the order the
	// registry seeded it — a decided row, not an arbitrary one (see [filter]).
	first := registry.ForScope(registry.ScopeThread)[0]
	p.Key(namedKey(tea.KeyEnter))
	if len(rec.chosen) != 1 {
		t.Fatalf("enter chose %d results, want 1", len(rec.chosen))
	}
	if got, want := rec.chosen[0], (RunEntry{ID: first.ID}); got != Result(want) {
		t.Errorf("enter yielded %v, want %v", got, want)
	}
	if rec.closes != 1 {
		t.Errorf("enter closed %d times, want 1", rec.closes)
	}
}

func TestEnterOnEachSectionYieldsItsOwnResultShape(t *testing.T) {
	c := demoCatalog()
	cases := []struct {
		query string
		want  Result
	}{
		{"wisp-parity", JumpToRoom{ID: "t-wisp"}},
		{"perf-audit", JumpToRoom{ID: "t-perf"}},
		{"open settings", RunEntry{ID: "slash.settings"}},
		{"work model", OpenSetting{Key: "model.work"}},
	}
	for _, tc := range cases {
		rec := &recorder{}
		p := New(rec.options())
		p.SetCatalog(c)
		typeText(p, tc.query)
		p.Key(namedKey(tea.KeyEnter))
		if len(rec.chosen) != 1 {
			t.Fatalf("query %q chose %d results", tc.query, len(rec.chosen))
		}
		if rec.chosen[0] != tc.want {
			t.Errorf("query %q yielded %v, want %v", tc.query, rec.chosen[0], tc.want)
		}
	}
}

// TestEnterOnADisabledRowDoesNothing: the user asked for something the room
// cannot do, and the honest answer is the reason still on screen beside it.
func TestEnterOnADisabledRowDoesNothing(t *testing.T) {
	c := Catalog{
		Scope:   registry.ScopeThread,
		Actions: []Action{{Entry: mustEntry(t, "slash.model"), Disabled: "settled — ask aforge"}},
	}
	rec := &recorder{}
	p := New(rec.options())
	p.SetCatalog(c)
	if _, ok := p.Selected(); ok {
		t.Fatal("a disabled row reported itself as choosable")
	}
	if cmd := p.Key(namedKey(tea.KeyEnter)); cmd != nil {
		t.Errorf("enter on a disabled row returned a command")
	}
	if len(rec.chosen) != 0 || rec.closes != 0 {
		t.Errorf("enter on a disabled row chose %v and closed %d times", rec.chosen, rec.closes)
	}
}

func TestTypingFiltersAndAsksForARepaint(t *testing.T) {
	rec := &recorder{}
	p := New(rec.options())
	p.SetCatalog(demoCatalog())
	before := p.Count()
	rec.dirty = 0

	typeText(p, "cancel")
	if p.Query() != "cancel" {
		t.Fatalf("query = %q", p.Query())
	}
	if p.Count() >= before {
		t.Errorf("typing narrowed %d rows to %d", before, p.Count())
	}
	if rec.dirty == 0 {
		t.Error("typing never invalidated the frame, so the keystroke would not appear")
	}
}

func TestEditingKeys(t *testing.T) {
	p := New(Options{})
	p.SetCatalog(demoCatalog())

	typeText(p, "open set")
	p.Key(namedKey(tea.KeyBackspace))
	if p.Query() != "open se" {
		t.Errorf("backspace left %q", p.Query())
	}
	p.Key(ctrlKey('w'))
	if p.Query() != "open " {
		t.Errorf("ctrl+w left %q", p.Query())
	}
	p.Key(ctrlKey('u'))
	if p.Query() != "" {
		t.Errorf("ctrl+u left %q", p.Query())
	}
	// Backspace on an empty query is a no-op, never a panic.
	p.Key(namedKey(tea.KeyBackspace))
	if p.Query() != "" {
		t.Errorf("backspace on empty left %q", p.Query())
	}
	// A multi-byte rune leaves as one character, not as one byte.
	typeText(p, "café")
	p.Key(namedKey(tea.KeyBackspace))
	if p.Query() != "caf" {
		t.Errorf("backspace over a multi-byte rune left %q", p.Query())
	}
}

func TestNavigationClampsAtBothEnds(t *testing.T) {
	p := New(Options{})
	p.SetCatalog(demoCatalog())
	p.Render(90, 20)

	for i := 0; i < 5; i++ {
		p.Key(namedKey(tea.KeyUp))
	}
	first, ok := p.Selected()
	if !ok {
		t.Fatal("nothing selected at the top")
	}
	top := registry.ForScope(registry.ScopeThread)[0]
	if got, want := first, Result(RunEntry{ID: top.ID}); got != want {
		t.Errorf("the top of the list is %v, want %v", got, want)
	}
	for i := 0; i < p.Total()+10; i++ {
		p.Key(namedKey(tea.KeyDown))
	}
	if _, ok := p.list.selected(); !ok {
		t.Fatal("the selection fell off the end")
	}
	if p.list.cursor != p.Count()-1 {
		t.Errorf("cursor stopped at %d, want the last row %d", p.list.cursor, p.Count()-1)
	}
}

func TestPageKeysUseTheRenderedHeight(t *testing.T) {
	p := New(Options{})
	p.SetCatalog(demoCatalog())
	p.Render(90, 8)
	p.Key(namedKey(tea.KeyPgDown))
	if p.list.cursor == 0 {
		t.Error("pgdown did not move the selection")
	}
	p.Key(namedKey(tea.KeyPgUp))
	if p.list.cursor != 0 {
		t.Errorf("pgup left the cursor at %d", p.list.cursor)
	}
}

// TestClickRunsTheRowUnderThePointer is 5.22 rule 5: the row IS the button.
func TestClickRunsTheRowUnderThePointer(t *testing.T) {
	rec := &recorder{}
	p := New(rec.options())
	// Rooms only, so the line arithmetic below is about the pointer and not
	// about how many verbs the registry happens to seed today.
	c := demoCatalog()
	c.Actions, c.Settings = []Action{}, nil
	p.SetCatalog(c)
	p.Render(90, 30)

	// bodyTop + 0 is the `rooms` header, + 1 is home, + 2 is wisp-parity.
	msg, pt := clickAt(p.bodyTop + 2)
	p.Mouse(msg, pt)
	if len(rec.chosen) != 1 {
		t.Fatalf("a click on a row chose %d results", len(rec.chosen))
	}
	if got, want := rec.chosen[0], Result(JumpToRoom{ID: "t-wisp"}); got != want {
		t.Errorf("clicked row yielded %v, want %v", got, want)
	}
}

func TestClickOnChromeDoesNothing(t *testing.T) {
	rec := &recorder{}
	p := New(rec.options())
	p.SetCatalog(demoCatalog())
	p.Render(90, 30)

	for _, y := range []int{0, p.bodyTop, -3, 9999} {
		msg, pt := clickAt(y)
		p.Mouse(msg, pt)
	}
	if len(rec.chosen) != 0 || rec.closes != 0 {
		t.Errorf("a click on chrome chose %v and closed %d times", rec.chosen, rec.closes)
	}
}

func TestWheelMovesTheSelectionWithoutChoosing(t *testing.T) {
	rec := &recorder{}
	p := New(rec.options())
	p.SetCatalog(demoCatalog())
	p.Render(90, 30)

	msg, pt := wheelAt(tea.MouseWheelDown)
	p.Mouse(msg, pt)
	if p.list.cursor != wheelStep {
		t.Errorf("a wheel notch moved the cursor to %d, want %d", p.list.cursor, wheelStep)
	}
	if len(rec.chosen) != 0 {
		t.Errorf("a wheel notch chose %v", rec.chosen)
	}
	msg, pt = wheelAt(tea.MouseWheelUp)
	p.Mouse(msg, pt)
	if p.list.cursor != 0 {
		t.Errorf("a wheel notch back left the cursor at %d", p.list.cursor)
	}
}

func TestResetClearsQueryAndSelection(t *testing.T) {
	p := New(Options{})
	p.SetCatalog(demoCatalog())
	typeText(p, "settings")
	p.Key(namedKey(tea.KeyDown))
	p.Reset()
	if p.Query() != "" {
		t.Errorf("Reset left the query %q", p.Query())
	}
	if p.list.cursor != 0 {
		t.Errorf("Reset left the cursor at %d", p.list.cursor)
	}
	if p.Count() != p.Total() {
		t.Errorf("Reset left %d of %d rows filtered in", p.Count(), p.Total())
	}
}

// TestEveryActionRowTeachesADoor is the whole point of 5.22 rule 2: no row may
// be silent about how else to reach it.
func TestEveryActionRowTeachesADoor(t *testing.T) {
	for _, e := range registry.All() {
		if got := accelOf(e, registry.SurfaceDefault); got == "" {
			t.Errorf("entry %s advertises no door at all", e.ID)
		}
		switch {
		case e.Key != "":
			if accelOf(e, registry.SurfaceDefault) != e.Key {
				t.Errorf("entry %s teaches %q, not its key %q", e.ID,
					accelOf(e, registry.SurfaceDefault), e.Key)
			}
		case e.Slash != "":
			if accelOf(e, registry.SurfaceDefault) != "/"+e.Slash {
				t.Errorf("entry %s teaches %q, not its alias", e.ID,
					accelOf(e, registry.SurfaceDefault))
			}
		default:
			if accelOf(e, registry.SurfaceDefault) != askAccel {
				t.Errorf("belt-only entry %s teaches %q, want %q", e.ID,
					accelOf(e, registry.SurfaceDefault), askAccel)
			}
		}
	}
}

// AND THE SAME PROMISE ON THE SURFACE THAT ACTUALLY SHIPS IT.
//
// A composer-first room hands every printable character to the draft, so a row
// teaching a bare letter there is teaching a key that types into the reader's
// sentence. This is the regression for the `?` sheet advertising `t` for the
// thread switcher, `v` for the receipts fold and `y` for copy-answer: every row
// must teach either a chord, a named key, a slash alias, or `ask`.
func TestNoRowTeachesABareLetterInAComposerFirstRoom(t *testing.T) {
	for _, e := range registry.All() {
		if !e.Scope.Has(registry.ScopeThread) {
			continue
		}
		taught := accelOf(e, registry.SurfaceComposerFirst)
		if taught == "" {
			t.Errorf("entry %s advertises no door at all", e.ID)
			continue
		}
		if taught == askAccel || strings.HasPrefix(taught, "/") {
			continue
		}
		if len([]rune(taught)) == 1 {
			t.Errorf("entry %s teaches the bare letter %q in a room where every "+
				"printable key is draft text; it needs a ChordKey", e.ID, taught)
		}
	}
}

// And the chord it teaches is the one the catalog recorded, not a guess.
func TestAComposerFirstRoomTeachesTheRecordedChord(t *testing.T) {
	for _, e := range registry.All() {
		if e.ChordKey == "" {
			continue
		}
		if got := accelOf(e, registry.SurfaceComposerFirst); got != e.ChordKey {
			t.Errorf("entry %s teaches %q, want its recorded chord %q", e.ID, got, e.ChordKey)
		}
	}
}

func TestHeaderShowsTheCountAndTheExit(t *testing.T) {
	p := New(Options{})
	p.SetCatalog(demoCatalog())
	header := strings.Split(p.Render(100, 20), "\n")[0]
	if !strings.Contains(header, searchLabel) || !strings.Contains(header, searchPlaceholder) {
		t.Errorf("the empty header does not say what the field is: %q", header)
	}
	if !strings.Contains(header, escHint) {
		t.Errorf("the header does not advertise the exit: %q", header)
	}
	typeText(p, "can")
	header = strings.Split(p.Render(100, 20), "\n")[0]
	if !strings.Contains(header, "can") {
		t.Errorf("the header does not echo the query: %q", header)
	}
	if !strings.Contains(header, "/") {
		t.Errorf("the header does not carry a match count: %q", header)
	}
}

func TestEmptyStatesTeach(t *testing.T) {
	p := New(Options{})
	if body := strings.Join(rowsOf(p, 60, 10), "\n"); !strings.Contains(body, emptyCatalogText) {
		t.Errorf("an unfilled palette says %q", body)
	}
	p.SetCatalog(demoCatalog())
	typeText(p, "zzzzzz")
	body := strings.Join(rowsOf(p, 60, 10), "\n")
	if !strings.Contains(body, noMatchPrefix) || !strings.Contains(body, "zzzzzz") {
		t.Errorf("an over-filtered palette does not name the query back: %q", body)
	}
}

func mustEntry(t *testing.T, id string) registry.Entry {
	t.Helper()
	e, ok := registry.ByID(id)
	if !ok {
		t.Fatalf("registry entry %q does not exist", id)
	}
	return e
}

// TestTheExitChipIsVerbFirstAndTwoTiers is design-law §16 on the surface that
// carried the reported bug's shape: `esc close` was two words in one grey, and
// a reader could only tell which was the label by already knowing.
func TestTheExitChipIsVerbFirstAndTwoTiers(t *testing.T) {
	styler := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	for _, name := range []string{"palette", "capability"} {
		var header string
		switch name {
		case "palette":
			p := New(Options{Styler: styler})
			p.SetCatalog(demoCatalog())
			header = strings.Split(p.Render(100, 20), "\n")[0]
		default:
			c := NewCapability(Options{Styler: styler})
			c.SetCatalog(demoCatalog())
			header = strings.Split(c.Render(100, 20), "\n")[0]
		}
		plain := ansi.Strip(header)
		if !strings.Contains(plain, "close esc") {
			t.Errorf("%s draws the exit as %q, want verb first", name, plain)
		}
		if strings.Contains(plain, "esc close") {
			t.Errorf("%s still draws the key before its verb: %q", name, plain)
		}
		// Two tiers, not one: the verb is the control and may never live
		// permanently in the dimmest tier (5.22's checklist).
		verbAt := strings.Index(header, closeChip.Verb)
		if verbAt < 0 {
			t.Fatalf("%s lost the verb entirely: %q", name, header)
		}
		if !strings.Contains(header[:verbAt], tokens.TextSecondary.Fg(tokens.TrueColor, tokens.FocusNormal)) {
			t.Errorf("%s paints the exit verb below the secondary tier: %q", name, header)
		}
	}
}

// The key column is the chip's key half, and the ladder that fills it is the
// registry's — one ladder, not a copy per surface.
func TestTheAcceleratorColumnIsTheChipsKeyHalf(t *testing.T) {
	for _, e := range registry.All() {
		want := registry.ChipOn(e, registry.SurfaceDefault).Key
		if want == "" {
			want = registry.AskKey
		}
		if got := accelOf(e, registry.SurfaceDefault); got != want {
			t.Errorf("entry %s teaches %q, the chip says %q", e.ID, got, want)
		}
	}
}

// -- the back grammar (trail.go) ---------------------------------------------

// choicesCatalog is a sub-list: an option's choices, which is the shape the
// reported trap was found in. It carries no actions, because a drilled level is
// showing one thing's choices and not the whole product's verbs.
func choicesCatalog() Catalog {
	return Catalog{
		Actions: []Action{},
		Settings: []SettingRow{
			{Key: "model.work.k3", Label: "claude-k3", Hint: "the fast one"},
			{Key: "model.work.opus", Label: "claude-opus", Hint: "the deep one"},
		},
	}
}

// drilled is a palette standing one level down, having drilled from the root
// through the row named by word.
func drilled(t *testing.T, word string) (*Palette, *recorder) {
	t.Helper()
	rec := &recorder{}
	p := New(rec.options())
	c := demoCatalog()
	c.Drill = func(res Result) bool { _, ok := res.(OpenSetting); return ok }
	p.SetCatalog(c)
	p.Push(word, choicesCatalog())
	if p.Depth() != 1 {
		t.Fatalf("push left the palette at depth %d", p.Depth())
	}
	return p, rec
}

// TestChoosingADrillRowOpensTheSubListInstead: the row that drills does not
// finish the surface, so the wiring gets its result while the palette is still
// up and still has the level it came from.
func TestChoosingADrillRowOpensTheSubListInstead(t *testing.T) {
	rec := &recorder{}
	p := New(rec.options())
	c := demoCatalog()
	c.Drill = func(res Result) bool { _, ok := res.(OpenSetting); return ok }
	// The wiring's real answer: push a level rather than let the surface close.
	rec2 := rec
	p.opts.OnChoose = func(res Result) tea.Cmd {
		rec2.chosen = append(rec2.chosen, res)
		p.Push("model", choicesCatalog())
		return nil
	}
	p.SetCatalog(c)
	typeText(p, "work model")
	p.Key(namedKey(tea.KeyEnter))

	if len(rec.chosen) != 1 {
		t.Fatalf("enter yielded %d results", len(rec.chosen))
	}
	if rec.closes != 0 {
		t.Errorf("a row that opens a sub-list closed the palette %d times", rec.closes)
	}
	if p.Depth() != 1 {
		t.Errorf("the palette is at depth %d, want 1", p.Depth())
	}
	// And an ordinary row still finishes.
	rec.chosen, rec.closes = nil, 0
	p.opts.OnChoose = rec.options().OnChoose
	p.Pop()
	// Popping restores the query the drill was launched from, so the field is
	// cleared before the next search rather than typed onto.
	p.Key(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	typeText(p, "open settings")
	p.Key(namedKey(tea.KeyEnter))
	if rec.closes != 1 {
		t.Errorf("an ordinary row closed %d times, want 1", rec.closes)
	}
}

// TestBackspaceOnAnEmptyFilterPopsOneLevel is the keyboard's way out.
func TestBackspaceOnAnEmptyFilterPopsOneLevel(t *testing.T) {
	p, rec := drilled(t, "model")
	p.Key(namedKey(tea.KeyBackspace))
	if p.Depth() != 0 {
		t.Fatalf("backspace on an empty filter left the palette at depth %d", p.Depth())
	}
	if rec.closes != 0 {
		t.Errorf("going back closed the surface %d times", rec.closes)
	}
	// At the root it is inert rather than destructive: there is nothing below
	// the bottom rung, and a key that closed the palette there would make
	// "delete one letter too many" mean "throw the surface away".
	p.Key(namedKey(tea.KeyBackspace))
	if rec.closes != 0 {
		t.Errorf("backspace at the root closed the surface %d times", rec.closes)
	}
	// And the level really is the root's again.
	if body := strings.Join(rowsOf(p, 100, 200), "\n"); !strings.Contains(body, "rooms") {
		t.Errorf("the root's own groups did not come back:\n%s", body)
	}
}

// TestBackspaceWithTextInTheFilterEditsTheText: the filter is what the reader
// is watching while they type, and a key that sometimes threw the list away
// would make typing feel dangerous.
func TestBackspaceWithTextInTheFilterEditsTheText(t *testing.T) {
	p, _ := drilled(t, "model")
	typeText(p, "opus")
	p.Key(namedKey(tea.KeyBackspace))
	if got := p.Query(); got != "opu" {
		t.Errorf("backspace typed %q, want %q", got, "opu")
	}
	if p.Depth() != 1 {
		t.Fatalf("backspace with text in the filter popped to depth %d", p.Depth())
	}
	// Only once the field is empty does the next press go back.
	for p.Query() != "" {
		p.Key(namedKey(tea.KeyBackspace))
	}
	if p.Depth() != 1 {
		t.Fatalf("emptying the filter popped a level on its own")
	}
	p.Key(namedKey(tea.KeyBackspace))
	if p.Depth() != 0 {
		t.Errorf("the press after the field emptied did not go back")
	}
}

// TestEscClosesFromAnyDepth is 8.2.21 read for depth: esc acts on what you are
// watching, and what a reader watching a drilled palette wants gone is the
// palette — not one rung of it. Two keys with one meaning between them is how
// "press esc until something happens" gets learned.
func TestEscClosesFromAnyDepth(t *testing.T) {
	p, rec := drilled(t, "model")
	p.Push("effort", choicesCatalog())
	if p.Depth() != 2 {
		t.Fatalf("depth is %d, want 2", p.Depth())
	}
	p.Key(namedKey(tea.KeyEscape))
	if rec.closes != 1 {
		t.Fatalf("esc at depth 2 closed %d times, want 1", rec.closes)
	}
	if len(rec.chosen) != 0 {
		t.Errorf("esc chose something: %v", rec.chosen)
	}
}

// TestTheTrailNamesEveryLevelAndOnlyAncestorsAreTargets is the pointer's door,
// and the tier split 5.22's checklist forces on it.
func TestTheTrailNamesEveryLevelAndOnlyAncestorsAreTargets(t *testing.T) {
	p, _ := drilled(t, "model")
	p.Push("effort", choicesCatalog())

	styler := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	p.SetStyler(styler)
	header := strings.Split(p.Render(100, 20), "\n")[headerLine]
	plain := ansi.Strip(header)
	want := trailLead + rootSegment + " " + trailLead + "model " + trailLead + "effort"
	if !strings.Contains(plain, want) {
		t.Fatalf("the trail reads %q, want it to contain %q", plain, want)
	}
	if got, want := p.Trail(), []string{"model", "effort"}; len(got) != len(want) ||
		got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Trail() = %v, want %v", got, want)
	}
	// Two ancestors, two targets — the current level is a label, not a door.
	if len(p.segs) != 2 {
		t.Fatalf("the trail recorded %d click targets, want 2", len(p.segs))
	}
	for _, s := range p.segs {
		if s.depth == p.Depth() {
			t.Errorf("the current level is a click target at depth %d", s.depth)
		}
	}
	// The current segment is the dimmest tier; an ancestor never is.
	at := strings.Index(header, "effort")
	if at < 0 {
		t.Fatal("the current level is not on the line")
	}
	if !strings.Contains(header[:at], tokens.TextTertiary.Fg(tokens.TrueColor, tokens.FocusNormal)) {
		t.Errorf("the current segment is not drawn at the dimmest tier: %q", header)
	}
	root := strings.Index(header, rootSegment)
	if !strings.Contains(header[:root], tokens.TextSecondary.Fg(tokens.TrueColor, tokens.FocusNormal)) {
		t.Errorf("an interactive segment lives in the dimmest tier: %q", header)
	}
}

// TestClickingATrailSegmentJumpsBackToThatLevel: the pointer's whole way out of
// a drilled list.
func TestClickingATrailSegmentJumpsBackToThatLevel(t *testing.T) {
	p, rec := drilled(t, "model")
	p.Push("effort", choicesCatalog())
	p.Render(100, 20)

	// Segment 1 is `model`, the level between the root and where we are.
	seg := p.segs[1]
	if seg.depth != 1 {
		t.Fatalf("segment 1 returns to depth %d", seg.depth)
	}
	p.Mouse(clickAtXY(seg.at, headerLine))
	if p.Depth() != 1 {
		t.Fatalf("clicking `model` left the palette at depth %d", p.Depth())
	}

	// And segment 0 goes all the way home, in one click rather than N.
	p.Render(100, 20)
	p.Mouse(clickAtXY(p.segs[0].at, headerLine))
	if p.Depth() != 0 {
		t.Fatalf("clicking the root left the palette at depth %d", p.Depth())
	}
	if rec.closes != 0 {
		t.Errorf("a trail click closed the surface %d times", rec.closes)
	}
	if len(rec.chosen) != 0 {
		t.Errorf("a trail click chose something: %v", rec.chosen)
	}

	// A click on the header that is not on a segment is chrome and does nothing.
	p.Push("model", choicesCatalog())
	p.Render(100, 20)
	p.Mouse(clickAtXY(99, headerLine))
	if p.Depth() != 1 {
		t.Errorf("a click on header chrome changed the depth to %d", p.Depth())
	}
}

// TestPoppingReturnsTheReaderWhereTheyWereStanding is 7.2 applied to a level: a
// reader who drilled from the middle of a list and came back to the top would
// have been moved by the surface rather than by themselves.
func TestPoppingReturnsTheReaderWhereTheyWereStanding(t *testing.T) {
	rec := &recorder{}
	p := New(rec.options())
	c := demoCatalog()
	c.Drill = func(Result) bool { return false }
	p.SetCatalog(c)
	typeText(p, "wisp")
	p.Key(namedKey(tea.KeyDown))
	before, ok := p.Selected()
	if !ok {
		t.Fatal("nothing selected before the drill")
	}
	query := p.Query()

	p.Push("model", choicesCatalog())
	if p.Query() != "" {
		t.Errorf("the sub-list opened holding the parent's query %q", p.Query())
	}
	p.Pop()

	if p.Query() != query {
		t.Errorf("coming back, the filter reads %q, want %q", p.Query(), query)
	}
	after, ok := p.Selected()
	if !ok {
		t.Fatal("nothing selected after coming back")
	}
	if after != before {
		t.Errorf("coming back moved the selection from %v to %v", before, after)
	}
}

// TestACatalogRefreshDoesNotThrowTheReaderOutOfALevel: a task settling
// elsewhere is not a reason to close the list somebody is standing in (7.2).
func TestACatalogRefreshDoesNotThrowTheReaderOutOfALevel(t *testing.T) {
	p, _ := drilled(t, "model")
	fresh := demoCatalog()
	fresh.Rooms = append(fresh.Rooms, Room{ID: "t-new", Title: "brand-new", Seed: "t-new"})
	p.SetCatalog(fresh)

	if p.Depth() != 1 {
		t.Fatalf("a catalog refresh moved the reader to depth %d", p.Depth())
	}
	if body := strings.Join(rowsOf(p, 100, 60), "\n"); !strings.Contains(body, "claude-k3") {
		t.Errorf("the refresh replaced the level the reader was on:\n%s", body)
	}
	// And the new facts are there when they come back.
	p.Pop()
	if body := strings.Join(rowsOf(p, 100, 200), "\n"); !strings.Contains(body, "brand-new") {
		t.Errorf("the root came back stale:\n%s", body)
	}
}

// TestResetClearsTheDrillStack: a palette that reopened three levels down is
// the reopened-holding-a-query surprise with the list changed as well.
func TestResetClearsTheDrillStack(t *testing.T) {
	p, _ := drilled(t, "model")
	p.Reset()
	if p.Depth() != 0 {
		t.Errorf("reset left the palette at depth %d", p.Depth())
	}
	if len(p.Trail()) != 0 {
		t.Errorf("reset left a trail: %v", p.Trail())
	}
}
