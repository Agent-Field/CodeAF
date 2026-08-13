package palette

import (
	"image"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The switcher's laws, asserted on the surface rather than on its state.
//
// Every test here drives the component the way the shell drives it — a key
// message, a mouse message, a Render at a width — because the thing this
// surface has to be right about is what a person sees before they choose which
// conversation to open, and a field is not that.

// fixtureThreads is three conversations in the order the wiring hands them
// over: newest activity first, with one delivery nobody has read.
func fixtureThreads() []Thread {
	return []Thread{
		{ID: "chat-1", Name: "the wisp parity push",
			LeftAt: "the diff is ready when you are", When: "2h"},
		{ID: "chat-2", Name: "importer rewrite",
			LeftAt: "parked on the schema question", When: "yesterday", Unseen: true},
		{ID: "chat-3", Name: "", LeftAt: "", When: "aug 3"},
	}
}

func newFixtureSwitcher(t *testing.T, opts Options) *Switcher {
	t.Helper()
	s := NewSwitcher(opts)
	s.SetThreads(fixtureThreads())
	return s
}

// plainRows renders the sheet and strips it, which is what every assertion
// about words rather than colours wants.
func plainRows(pane interface{ Render(int, int) string }, width, height int) []string {
	return strings.Split(ansi.Strip(pane.Render(width, height)), "\n")
}

// Every thread is listed, with the two columns 5.2 asks for and the relative
// time behind them — and the one door at the foot of the list.
func TestTheSwitcherListsEveryThreadAndTheDoor(t *testing.T) {
	s := newFixtureSwitcher(t, Options{})
	frame := strings.Join(plainRows(s, 90, 14), "\n")
	for _, want := range []string{
		"the wisp parity push", "left at: the diff is ready when you are", "2h",
		"importer rewrite", "left at: parked on the schema question", "yesterday",
		NewThreadWord,
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the switcher is missing %q:\n%s", want, frame)
		}
	}
}

// A thread the scribe has not named yet says it is new, and NEVER shows an id.
// 13.3.4 puts identifiers in the never-shown tier and a switcher is exactly the
// place a reader would meet one.
func TestAnUnnamedThreadSaysSoAndNeverShowsAnID(t *testing.T) {
	s := newFixtureSwitcher(t, Options{})
	frame := strings.Join(plainRows(s, 90, 14), "\n")
	if strings.Contains(frame, "chat-1") || strings.Contains(frame, "chat-3") {
		t.Fatalf("a session id reached a cell:\n%s", frame)
	}
	if !strings.Contains(frame, UnnamedThread) {
		t.Fatalf("the unnamed thread has no name at all:\n%s", frame)
	}
}

// A thread with nothing said in it draws no `left at:` lead. Chrome announcing
// an absence is chrome spending cells on nothing.
func TestAThreadWithNothingSaidDrawsNoLeftAtLead(t *testing.T) {
	s := NewSwitcher(Options{})
	s.SetThreads([]Thread{{ID: "chat-1", Name: "quiet one", When: "3d"}})
	frame := strings.Join(plainRows(s, 90, 10), "\n")
	if strings.Contains(frame, strings.TrimSpace(leftAtLead)) {
		t.Fatalf("an empty thread drew the lead word anyway:\n%s", frame)
	}
}

// THE ONE ORNAMENT (5.1 law 3). The dot appears exactly where a delivery landed
// unseen, and nowhere else — no counts, no badges, no second mark.
func TestTheUnseenDotIsTheOnlyOrnament(t *testing.T) {
	s := newFixtureSwitcher(t, Options{})
	rows := plainRows(s, 90, 14)
	dots := 0
	for _, row := range rows {
		dots += strings.Count(row, tokens.GlyphStepDone)
	}
	if dots != 1 {
		t.Fatalf("the sheet carries %d ornaments, want exactly 1:\n%s", dots,
			strings.Join(rows, "\n"))
	}
	for _, row := range rows {
		if strings.Contains(row, tokens.GlyphStepDone) &&
			!strings.Contains(row, "importer rewrite") {
			t.Fatalf("the dot landed on the wrong row: %q", row)
		}
	}
}

// The thread the window is already in never dots, whatever the wiring says
// about it: you are looking at whatever landed there.
func TestTheCurrentThreadNeverDots(t *testing.T) {
	s := NewSwitcher(Options{})
	s.SetThreads([]Thread{{ID: "chat-1", Name: "here", Unseen: true, Current: true}})
	frame := strings.Join(plainRows(s, 90, 10), "\n")
	if strings.Contains(frame, tokens.GlyphStepDone) {
		t.Fatalf("the thread the reader is in wears the unseen dot:\n%s", frame)
	}
}

// Typeahead narrows as you type, and the header's count says so — which is what
// tells a reader their query is working.
func TestTypingFiltersTheThreads(t *testing.T) {
	s := newFixtureSwitcher(t, Options{})
	if got := s.Count(); got != 4 {
		t.Fatalf("the unfiltered list holds %d rows, want 3 threads and the door", got)
	}
	typeSwitcher(s, "import")
	if got := s.Query(); got != "import" {
		t.Fatalf("the query is %q", got)
	}
	frame := strings.Join(plainRows(s, 90, 14), "\n")
	if strings.Contains(frame, "the wisp parity push") {
		t.Fatalf("the filter kept a row that does not match:\n%s", frame)
	}
	if !strings.Contains(frame, "importer rewrite") {
		t.Fatalf("the filter dropped the row that does match:\n%s", frame)
	}
}

// The `left at:` line is matched too, which is what makes searching for a thing
// somebody SAID find the conversation they said it in.
func TestTypingMatchesTheLeftAtLine(t *testing.T) {
	s := newFixtureSwitcher(t, Options{})
	typeSwitcher(s, "schema")
	frame := strings.Join(plainRows(s, 90, 14), "\n")
	if !strings.Contains(frame, "importer rewrite") {
		t.Fatalf("a search over what was said found nothing:\n%s", frame)
	}
}

// An over-filtered list names the query back, so the failure a reader sees is
// their own typo — and the door to start a thread survives it, because a reader
// who searched for a conversation that does not exist is precisely the reader
// who wants to begin one.
func TestAnOverFilteredListNamesTheQueryAndKeepsTheDoor(t *testing.T) {
	s := newFixtureSwitcher(t, Options{})
	typeSwitcher(s, "zzz")
	frame := strings.Join(plainRows(s, 90, 14), "\n")
	if !strings.Contains(frame, "zzz") {
		t.Fatalf("the empty state does not name the query:\n%s", frame)
	}
}

// ↑↓ move and enter chooses, and what comes back is the thread's own id.
func TestArrowsMoveAndEnterSwitches(t *testing.T) {
	var chosen Result
	s := newFixtureSwitcher(t, Options{
		OnChoose: func(r Result) tea.Cmd { chosen = r; return nil },
		OnClose:  func() tea.Cmd { return nil },
	})
	s.Key(tea.KeyPressMsg{Code: tea.KeyDown})
	s.Key(tea.KeyPressMsg{Code: tea.KeyEnter})
	switch got := chosen.(type) {
	case SwitchThread:
		if got.ID != "chat-2" {
			t.Fatalf("enter after one arrow chose %q, want the second thread", got.ID)
		}
	default:
		t.Fatalf("enter yielded %T (%v), want a SwitchThread", chosen, chosen)
	}
}

// esc closes and chooses nothing. An overlay is what the reader is watching, so
// the key is consumed here whatever is happening underneath (8.2.21).
func TestEscClosesAndChoosesNothing(t *testing.T) {
	closed, chosen := false, false
	s := newFixtureSwitcher(t, Options{
		OnChoose: func(Result) tea.Cmd { chosen = true; return nil },
		OnClose:  func() tea.Cmd { closed = true; return nil },
	})
	s.Key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !closed {
		t.Fatal("esc did not close the switcher")
	}
	if chosen {
		t.Fatal("esc chose a thread on the way out")
	}
}

// EVERY BARE LETTER IS A LETTER. The body of this surface is a search field, so
// a bare `n` filters — it does not mint a conversation — and a reader can search
// for a thread whose name begins with one.
func TestABareLetterAlwaysReachesTheFilter(t *testing.T) {
	var chosen Result
	s := newFixtureSwitcher(t, Options{
		OnChoose: func(r Result) tea.Cmd { chosen = r; return nil },
		OnClose:  func() tea.Cmd { return nil },
	})
	typeSwitcher(s, "n")
	if chosen != nil {
		t.Fatalf("a bare letter minted a thread from a search field: %v", chosen)
	}
	if got := s.Query(); got != "n" {
		t.Fatalf("`n` did not reach the filter: query is %q", got)
	}
}

// The door's chord means the same thing from any filter state, which is what a
// chord is for — and it is the chord the row itself advertises.
func TestTheDoorsChordMintsAThreadFromAnyState(t *testing.T) {
	var chosen Result
	s := newFixtureSwitcher(t, Options{
		OnChoose: func(r Result) tea.Cmd { chosen = r; return nil },
		OnClose:  func() tea.Cmd { return nil },
	})
	typeSwitcher(s, "impor")
	s.Key(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	if _, ok := chosen.(NewThread); !ok {
		t.Fatalf("%s yielded %T (%v), want NewThread", NewThreadKey, chosen, chosen)
	}
}

// PARITY. Every row the switcher draws is reachable by the pointer, through the
// same call enter makes — 5.22 rule 5's "the row IS the button", asserted per
// row rather than once.
func TestEveryRowIsClickableAndAgreesWithEnter(t *testing.T) {
	for _, target := range []struct {
		name string
		want string
	}{
		{"the wisp parity push", "chat-1"},
		{"importer rewrite", "chat-2"},
	} {
		t.Run(target.name, func(t *testing.T) {
			var byKey, byClick Result
			keyed := newFixtureSwitcher(t, Options{
				OnChoose: func(r Result) tea.Cmd { byKey = r; return nil },
				OnClose:  func() tea.Cmd { return nil },
			})
			clicked := newFixtureSwitcher(t, Options{
				OnChoose: func(r Result) tea.Cmd { byClick = r; return nil },
				OnClose:  func() tea.Cmd { return nil },
			})

			rows := plainRows(clicked, 90, 14)
			y := -1
			for i, row := range rows {
				if strings.Contains(row, target.name) {
					y = i
				}
			}
			if y < 0 {
				t.Fatalf("%q is not on the sheet:\n%s", target.name, strings.Join(rows, "\n"))
			}
			clicked.Mouse(tea.MouseClickMsg{Button: tea.MouseLeft, X: 4, Y: y},
				image.Point{X: 4, Y: y})

			keyed.Select(target.want)
			keyed.Key(tea.KeyPressMsg{Code: tea.KeyEnter})

			if byKey != byClick {
				t.Fatalf("the pointer answered %v and the keyboard %v", byClick, byKey)
			}
			if got, ok := byClick.(SwitchThread); !ok || got.ID != target.want {
				t.Fatalf("clicking %q yielded %v", target.name, byClick)
			}
		})
	}
}

// The `new thread` row is a button too, and it yields the same result its key
// does — one door, two hands.
func TestClickingTheNewThreadRowMintsOne(t *testing.T) {
	var chosen Result
	s := newFixtureSwitcher(t, Options{
		OnChoose: func(r Result) tea.Cmd { chosen = r; return nil },
		OnClose:  func() tea.Cmd { return nil },
	})
	rows := plainRows(s, 90, 14)
	y := -1
	for i, row := range rows {
		if strings.Contains(row, NewThreadWord) && strings.Contains(row, NewThreadKey) {
			y = i
		}
	}
	if y < 0 {
		t.Fatalf("the new-thread door is not on the sheet:\n%s", strings.Join(rows, "\n"))
	}
	s.Mouse(tea.MouseClickMsg{Button: tea.MouseLeft, X: 4, Y: y}, image.Point{X: 4, Y: y})
	if _, ok := chosen.(NewThread); !ok {
		t.Fatalf("clicking the door yielded %T (%v)", chosen, chosen)
	}
}

// Select points the cursor at one thread, which is the door a task page's
// attribution row opens.
func TestSelectPointsTheCursorAtAThread(t *testing.T) {
	s := newFixtureSwitcher(t, Options{})
	if !s.Select("chat-2") {
		t.Fatal("Select did not find a thread that is on the list")
	}
	result, ok := s.Selected()
	if !ok {
		t.Fatal("the cursor rests on nothing after a Select")
	}
	if got, want := result.(SwitchThread).ID, "chat-2"; got != want {
		t.Fatalf("the cursor rests on %q, want %q", got, want)
	}
	if s.Select("nobody") {
		t.Fatal("Select claimed to find a thread that is not on the list")
	}
}

// LINEAR MODE IS A PLAIN NUMBERED LIST, and fully functional: the ordinals are
// drawn, the selection is still carried by a printable cell, and every key
// still does what it does in the ordinary rendering (10.1.5).
func TestLinearModeNumbersTheRowsAndStillSwitches(t *testing.T) {
	var chosen Result
	s := NewSwitcher(Options{
		Linear:   true,
		OnChoose: func(r Result) tea.Cmd { chosen = r; return nil },
		OnClose:  func() tea.Cmd { return nil },
	})
	s.SetThreads(fixtureThreads())
	// The verb column is capped ([verbColMax]) and a long name is cut there like
	// any other, so the assertion is on the ORDINAL and the head of each name —
	// which is what a numbered list has to get right.
	frame := strings.Join(plainRows(s, 120, 14), "\n")
	for _, want := range []string{"1. the wisp", "2. importer rewrite", "3. " + UnnamedThread} {
		if !strings.Contains(frame, want) {
			t.Fatalf("linear mode did not number the rows:\n%s", frame)
		}
	}
	// The selection marker survives, which is what linear mode keeps when it
	// drops the band.
	if !strings.Contains(frame, tokens.GlyphAccentRail) {
		t.Fatalf("linear mode dropped the selection marker as well as the band:\n%s", frame)
	}
	s.Key(tea.KeyPressMsg{Code: tea.KeyDown})
	s.Key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got, ok := chosen.(SwitchThread); !ok || got.ID != "chat-2" {
		t.Fatalf("linear mode answered enter with %v", chosen)
	}
}

// A window with no threads at all says so and teaches the way out of it. An
// unwired surface and an empty one must never look alike.
func TestAnEmptySwitcherTeachesTheDoor(t *testing.T) {
	s := NewSwitcher(Options{})
	s.SetThreads(nil)
	frame := strings.Join(plainRows(s, 90, 10), "\n")
	if !strings.Contains(frame, NewThreadWord) {
		t.Fatalf("an empty switcher offers no way to start a thread:\n%s", frame)
	}
}

// The width sweep every surface in this package pays: no row wider than the
// width it was asked for, no panic, at any size.
func TestTheSwitcherNeverOverflowsAtAnyWidth(t *testing.T) {
	s := newFixtureSwitcher(t, Options{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)})
	for width := 1; width <= 110; width++ {
		for height := 1; height <= 12; height++ {
			rows := strings.Split(s.Render(width, height), "\n")
			if len(rows) != height {
				t.Fatalf("at %dx%d the sheet drew %d rows", width, height, len(rows))
			}
			for _, row := range rows {
				if got := ansi.StringWidth(ansi.Strip(row)); got > width {
					t.Fatalf("at %dx%d a row is %d cells wide: %q", width, height, got, row)
				}
			}
		}
	}
}

// typeSwitcher drives text through the component's own key path, which is where
// the empty-filter rule for `n` lives.
func typeSwitcher(s *Switcher, text string) {
	for _, r := range text {
		s.Key(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}
