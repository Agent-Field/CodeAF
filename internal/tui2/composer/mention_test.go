package composer

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// -- helpers for the `@` grammar's tests -------------------------------------

func tabKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyTab} }
func ctrlEnterKey() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}
}

// testTargets is the fixture every test in this lane addresses: two live tasks
// (one wanting a human) and one settled, with two words sharing a prefix so the
// fuzzy ranking and the longest-match rule both have something to be wrong
// about.
func testTargets() []Target {
	return []Target{
		{ID: "t-wisp", Word: "wisp-parity", Title: "rust agent browser parity", Seed: 3},
		{ID: "t-perf", Word: "perf-audit", Title: "profile the hot loop", Seed: 5, Attention: true},
		{ID: "t-wire", Word: "wire-up", Title: "landed last week", Seed: 7, Settled: true},
	}
}

// withTargets builds a composer wired to a fixed target list.
func withTargets(t *testing.T, opts Options) *Model {
	t.Helper()
	if opts.Targets == nil {
		opts.Targets = testTargets
	}
	return New(opts)
}

// complete opens the filter by typing "@" plus query and takes the highlighted
// row, the way a person completes a mention.
func complete(m *Model, query string) {
	m.Key(charKey('@'))
	typeString(m, query)
	m.Key(enterKey())
}

// -- derivation ---------------------------------------------------------------

func TestMention_DerivedFromTypedText(t *testing.T) {
	m := withTargets(t, Options{})
	typeString(m, "@wisp-parity")
	if len(m.mentions) != 1 {
		t.Fatalf("mentions = %d, want 1 for a fully typed task word", len(m.mentions))
	}
	if got := m.mentions[0].target.ID; got != "t-wisp" {
		t.Fatalf("mention target = %q, want t-wisp", got)
	}
	if m.mentions[0].start != 0 || m.mentions[0].length != len("@wisp-parity") {
		t.Fatalf("mention span = %+v, want the whole token", m.mentions[0])
	}
}

func TestMention_NeedsAWordBoundary(t *testing.T) {
	m := withTargets(t, Options{})
	typeString(m, "mail@wisp-parity")
	if len(m.mentions) != 0 {
		t.Fatalf("mentions = %d, want 0: an @ mid-word is an address, not an address", len(m.mentions))
	}
	if m.filter.open {
		t.Fatalf("filter opened on a mid-word @")
	}
}

func TestMention_LongestWordWins(t *testing.T) {
	m := withTargets(t, Options{Targets: func() []Target {
		return []Target{
			{ID: "short", Word: "wisp"},
			{ID: "long", Word: "wisp-parity"},
		}
	}})
	typeString(m, "@wisp-parity ok")
	if len(m.mentions) != 1 || m.mentions[0].target.ID != "long" {
		t.Fatalf("mentions = %+v, want the longer word to win", m.mentions)
	}
}

func TestMention_PartialWordIsNotAToken(t *testing.T) {
	m := withTargets(t, Options{})
	typeString(m, "@wisp-par")
	if len(m.mentions) != 0 {
		t.Fatalf("mentions = %d, want 0 while the word is still half typed", len(m.mentions))
	}
}

func TestMention_VanishesWithItsTarget(t *testing.T) {
	live := testTargets()
	m := withTargets(t, Options{Targets: func() []Target { return live }})
	complete(m, "wisp")
	if len(m.mentions) != 1 {
		t.Fatalf("mentions = %d after completion, want 1", len(m.mentions))
	}
	// The rail forgets the task. The token stops being a token on the next
	// edit — an affordance may not outlive the thing it addresses.
	live = nil
	m.Key(charKey('x'))
	if len(m.mentions) != 0 {
		t.Fatalf("mentions = %d after the target disappeared, want 0", len(m.mentions))
	}
}

func TestMention_NilTargetsNeverDerives(t *testing.T) {
	m := New(Options{})
	typeString(m, "@wisp-parity hello")
	if len(m.mentions) != 0 || m.filter.open {
		t.Fatalf("a composer with no Targets must know nothing about mentions")
	}
}

// -- atomicity ----------------------------------------------------------------

func TestMention_BackspaceRemovesTheWholeToken(t *testing.T) {
	m := withTargets(t, Options{})
	complete(m, "wisp")
	if m.Value() != "@wisp-parity " {
		t.Fatalf("Value() = %q, want the completed token and its closing space", m.Value())
	}
	m.Key(backspaceKey()) // the trailing space
	if m.Value() != "@wisp-parity" {
		t.Fatalf("Value() = %q, want the space gone and the token intact", m.Value())
	}
	m.Key(backspaceKey()) // the token, in one keypress
	if m.Value() != "" {
		t.Fatalf("Value() = %q, want one backspace to take the whole token", m.Value())
	}
}

func TestMention_DeleteForwardRemovesTheWholeToken(t *testing.T) {
	m := withTargets(t, Options{})
	complete(m, "wisp")
	m.cursor = 0
	m.Key(deleteKey())
	if m.Value() != " " {
		t.Fatalf("Value() = %q, want the token gone and its closing space left", m.Value())
	}
}

func TestMention_BackspaceInsideTheTokenIsOrdinary(t *testing.T) {
	// The cursor is not at a boundary, so there is no object to delete — one
	// rune goes, and the span stops being a mention because the text no longer
	// names a target. Both halves matter: nothing is protected that the user
	// did not point at, and nothing keeps claiming to be a token once it is not.
	m := withTargets(t, Options{})
	complete(m, "wisp")
	m.cursor = 6 // inside "@wisp-|parity"
	m.Key(backspaceKey())
	if m.Value() != "@wispparity " {
		t.Fatalf("Value() = %q, want a single-rune delete", m.Value())
	}
	if len(m.mentions) != 0 {
		t.Fatalf("mentions = %d, want 0: the text no longer names a target", len(m.mentions))
	}
}

// -- survival through the stash and the ring ----------------------------------

func TestMention_SurvivesEscStashAndRestore(t *testing.T) {
	m := withTargets(t, Options{})
	complete(m, "wisp")
	typeString(m, "skip H2")
	want := m.Value()

	if cmd := m.Key(escKey()); cmd != nil {
		t.Fatalf("esc against a non-empty draft must still be consumed by the esc law")
	}
	if m.Value() != "" {
		t.Fatalf("draft not stashed: %q", m.Value())
	}
	m.Key(upKey())
	if m.Value() != want {
		t.Fatalf("restored draft = %q, want %q", m.Value(), want)
	}
	if len(m.mentions) != 1 || m.mentions[0].target.ID != "t-wisp" {
		t.Fatalf("mentions = %+v after restore, want the token to come back live", m.mentions)
	}
}

func TestMention_SurvivesTheHistoryRing(t *testing.T) {
	var sent []Dispatch
	m := withTargets(t, Options{OnDispatch: func(d Dispatch) { sent = append(sent, d) }})
	complete(m, "wisp")
	typeString(m, "skip H2")
	m.Key(enterKey())
	if len(sent) != 1 {
		t.Fatalf("dispatches = %d, want 1", len(sent))
	}

	m.Key(upKey())
	if m.Value() != "@wisp-parity skip H2" {
		t.Fatalf("recalled draft = %q", m.Value())
	}
	if len(m.mentions) != 1 {
		t.Fatalf("mentions = %d after recall, want the token to be live again", len(m.mentions))
	}
	// And it is a real token again, not just matching text: one backspace at
	// its edge takes the whole thing.
	m.cursor = m.mentions[0].end()
	m.Key(backspaceKey())
	if m.Value() != " skip H2" {
		t.Fatalf("Value() = %q, want the recalled token to be atomic too", m.Value())
	}
}

func TestMention_SendTrimsWithoutLosingTheToken(t *testing.T) {
	var got Dispatch
	m := withTargets(t, Options{OnDispatch: func(d Dispatch) { got = d }})
	typeString(m, "  ")
	complete(m, "wisp")
	typeString(m, "ping  ")
	m.Key(enterKey())
	if got.Text != "@wisp-parity ping" {
		t.Fatalf("dispatch text = %q, want the trimmed draft with its token", got.Text)
	}
	if got.TargetID != "t-wisp" {
		t.Fatalf("dispatch target = %q, want t-wisp even with leading whitespace", got.TargetID)
	}
}
