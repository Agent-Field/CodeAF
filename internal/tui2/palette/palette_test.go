package palette

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
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

	// Row 0 is home.
	p.Key(namedKey(tea.KeyEnter))
	if len(rec.chosen) != 1 {
		t.Fatalf("enter chose %d results, want 1", len(rec.chosen))
	}
	if got, want := rec.chosen[0], (JumpToRoom{ID: rail.HomeScopeID}); got != Result(want) {
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
	if got, want := first, Result(JumpToRoom{ID: rail.HomeScopeID}); got != want {
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
	p.SetCatalog(demoCatalog())
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
		if got := accelOf(e); got == "" {
			t.Errorf("entry %s advertises no door at all", e.ID)
		}
		switch {
		case e.Key != "":
			if accelOf(e) != e.Key {
				t.Errorf("entry %s teaches %q, not its key %q", e.ID, accelOf(e), e.Key)
			}
		case e.Slash != "":
			if accelOf(e) != "/"+e.Slash {
				t.Errorf("entry %s teaches %q, not its alias", e.ID, accelOf(e))
			}
		default:
			if accelOf(e) != askAccel {
				t.Errorf("belt-only entry %s teaches %q, want %q", e.ID, accelOf(e), askAccel)
			}
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
