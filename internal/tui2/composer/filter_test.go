package composer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// -- opening and closing ------------------------------------------------------

func TestFilter_OpensOnTypedAt(t *testing.T) {
	m := withTargets(t, Options{})
	m.Key(charKey('@'))
	if !m.filter.open {
		t.Fatalf("typing @ at the start of a draft must open the filter")
	}
	if len(m.filter.hits) != 3 {
		t.Fatalf("hits = %d on an empty needle, want every target", len(m.filter.hits))
	}
	if m.Value() != "@" {
		t.Fatalf("Value() = %q: the @ is ordinary draft text", m.Value())
	}
}

func TestFilter_NeverOpensWithoutSomethingToAddress(t *testing.T) {
	for name, targets := range map[string]func() []Target{
		"nil options": nil,
		"empty list":  func() []Target { return nil },
		"wordless":    func() []Target { return []Target{{ID: "x"}} },
	} {
		m := New(Options{Targets: targets})
		m.Key(charKey('@'))
		if m.filter.open {
			t.Fatalf("%s: a filter opened onto nothing", name)
		}
	}
}

func TestFilter_NarrowsAsYouType(t *testing.T) {
	m := withTargets(t, Options{})
	m.Key(charKey('@'))
	typeString(m, "wi")
	if len(m.filter.hits) != 2 {
		t.Fatalf("hits = %d for %q, want wisp-parity and wire-up", len(m.filter.hits), "wi")
	}
	typeString(m, "sp")
	if len(m.filter.hits) != 1 {
		t.Fatalf("hits = %d for %q, want only wisp-parity", len(m.filter.hits), "wisp")
	}
	if got, _ := m.filter.selected(); got.ID != "t-wisp" {
		t.Fatalf("selected = %q, want t-wisp", got.ID)
	}
}

func TestFilter_MatchesTitleAsWellAsWord(t *testing.T) {
	m := withTargets(t, Options{})
	m.Key(charKey('@'))
	typeString(m, "hotloop")
	if len(m.filter.hits) != 1 {
		t.Fatalf("hits = %d, want the row whose TITLE matches", len(m.filter.hits))
	}
	if got, _ := m.filter.selected(); got.ID != "t-perf" {
		t.Fatalf("selected = %q, want t-perf by its title", got.ID)
	}
}

func TestFilter_LiveRowsRankAheadOfSettledOnes(t *testing.T) {
	m := withTargets(t, Options{})
	m.Key(charKey('@'))
	typeString(m, "wi")
	first := m.filter.rows[m.filter.hits[0].idx].target
	last := m.filter.rows[m.filter.hits[len(m.filter.hits)-1].idx].target
	if first.Settled || !last.Settled {
		t.Fatalf("order = live? %v then settled? %v, want live first (5.18)", !first.Settled, last.Settled)
	}
}

func TestFilter_EscClosesAndLeavesTheDraftAlone(t *testing.T) {
	m := withTargets(t, Options{})
	m.Key(charKey('@'))
	typeString(m, "wi")
	if cmd := m.Key(escKey()); cmd != nil {
		t.Fatalf("esc against an open filter must be consumed, not handed on")
	}
	if m.filter.open {
		t.Fatalf("esc did not close the filter")
	}
	if m.Value() != "@wi" {
		t.Fatalf("Value() = %q, want the typed text untouched by the close", m.Value())
	}
	// The esc law resumes exactly where it was: the NEXT esc is the composer's
	// own, stashing the draft rather than closing anything.
	if cmd := m.Key(escKey()); cmd != nil {
		t.Fatalf("the second esc must be the stash branch of the esc law")
	}
	if m.Value() != "" {
		t.Fatalf("Value() = %q, want the draft stashed by the second esc", m.Value())
	}
	m.Key(upKey())
	if m.Value() != "@wi" {
		t.Fatalf("stash lost: %q", m.Value())
	}
}

func TestFilter_ClosesWhenTheNeedleStopsBeingOne(t *testing.T) {
	cases := map[string]func(*Model){
		"space typed":       func(m *Model) { m.Key(charKey(' ')) },
		"newline typed":     func(m *Model) { m.Key(altEnterKey()) },
		"backspaced past @": func(m *Model) { m.Key(backspaceKey()); m.Key(backspaceKey()); m.Key(backspaceKey()) },
		"cursor walks left": func(m *Model) { m.Key(leftKey()); m.Key(leftKey()); m.Key(leftKey()) },
		"cursor jumps home": func(m *Model) { m.Key(homeKey()) },
		"draft replaced whole": func(m *Model) {
			m.Key(escKey())
			m.Key(escKey())
			m.Key(upKey())
		},
	}
	for name, act := range cases {
		m := withTargets(t, Options{NewlineKeys: []string{"alt+enter"}})
		m.Key(charKey('@'))
		typeString(m, "wi")
		act(m)
		if m.filter.open {
			t.Errorf("%s: filter still open", name)
		}
	}
}

func TestFilter_ArrowsWalkTheCandidatesAndClamp(t *testing.T) {
	m := withTargets(t, Options{})
	m.Key(charKey('@'))
	if m.filter.sel != 0 {
		t.Fatalf("sel = %d on open, want the first row", m.filter.sel)
	}
	m.Key(upKey())
	if m.filter.sel != 0 {
		t.Fatalf("sel = %d, want ↑ to clamp at the top rather than wrap", m.filter.sel)
	}
	m.Key(downKey())
	m.Key(downKey())
	m.Key(downKey())
	if m.filter.sel != len(m.filter.hits)-1 {
		t.Fatalf("sel = %d, want ↓ to clamp at the last row", m.filter.sel)
	}
	// The draft is untouched by walking the list: ↑/↓ belong to the filter
	// while it is open, not to the recall ring.
	if m.Value() != "@" {
		t.Fatalf("Value() = %q, want the arrows to have gone to the list", m.Value())
	}
}

// -- completing ---------------------------------------------------------------

func TestFilter_EnterCompletesTheHighlightedRow(t *testing.T) {
	m := withTargets(t, Options{})
	m.Key(charKey('@'))
	typeString(m, "wisp")
	m.Key(enterKey())
	if m.Value() != "@wisp-parity " {
		t.Fatalf("Value() = %q, want the completed token plus its closing space", m.Value())
	}
	if m.filter.open {
		t.Fatalf("completing must close the filter")
	}
	if len(m.mentions) != 1 {
		t.Fatalf("mentions = %d after completion, want 1", len(m.mentions))
	}
}

func TestFilter_TabCompletesToo(t *testing.T) {
	m := withTargets(t, Options{})
	m.Key(charKey('@'))
	typeString(m, "perf")
	m.Key(tabKey())
	if m.Value() != "@perf-audit " {
		t.Fatalf("Value() = %q, want tab to complete like enter (5.18)", m.Value())
	}
}

func TestFilter_CompletesInTheMiddleOfProse(t *testing.T) {
	m := withTargets(t, Options{})
	typeString(m, "ask ")
	complete(m, "perf")
	typeString(m, "about the loop")
	if m.Value() != "ask @perf-audit about the loop" {
		t.Fatalf("Value() = %q", m.Value())
	}
	if len(m.mentions) != 1 || m.mentions[0].start != 4 {
		t.Fatalf("mentions = %+v, want one token at rune 4", m.mentions)
	}
}

func TestFilter_EnterSendsWhenNothingMatches(t *testing.T) {
	// A filter showing "no task by that name" must not swallow the send of a
	// draft the user has finished typing (5.20: no dead-air sends).
	var sent []string
	m := withTargets(t, Options{OnSubmit: func(s string) { sent = append(sent, s) }})
	m.Key(charKey('@'))
	typeString(m, "zzzz")
	if len(m.filter.hits) != 0 {
		t.Fatalf("hits = %d, want none", len(m.filter.hits))
	}
	m.Key(enterKey())
	if len(sent) != 1 || sent[0] != "@zzzz" {
		t.Fatalf("sent = %v, want the draft to have gone out as prose", sent)
	}
}

// -- the scorer ---------------------------------------------------------------

func TestScorePrefersEarlyAndPrefix(t *testing.T) {
	prefix, ok := score("wisp-parity", "wi")
	if !ok {
		t.Fatal("wisp-parity should match wi")
	}
	scattered, ok := score("rewrite in place", "wi")
	if !ok {
		t.Fatal("rewrite in place should match wi")
	}
	if prefix >= scattered {
		t.Errorf("prefix scored %d, scattered scored %d: lower is better and prefix must win", prefix, scattered)
	}
	if prefix >= 0 {
		t.Errorf("a leading match must take the flat bonus and go negative: got %d", prefix)
	}
}

// TestScoreAgreesWithRegistry pins this package's restated scorer against the
// registry's own over the registry's own catalog, exactly as
// internal/tui2/palette does. The `@` filter and the ctrl+k palette must never
// start disagreeing about which row best answers a query — 5.22's "one
// registry, six surfaces" is a fact or it is a claim.
func TestScoreAgreesWithRegistry(t *testing.T) {
	for _, query := range []string{"", "c", "can", "model", "task", "open", "zzz"} {
		want := registry.FuzzyMatch(registry.ScopeAny, query)
		needle := lower(query)
		got := make(map[string]int, len(want))
		matched := 0
		for _, e := range registry.ForScope(registry.ScopeAny) {
			verbScore, verbOK := score(lower(e.Verb), needle)
			descScore, descOK := score(lower(e.Description), needle)
			switch {
			case verbOK && descOK:
				got[e.ID] = min(verbScore, descScore)
			case verbOK:
				got[e.ID] = verbScore
			case descOK:
				got[e.ID] = descScore
			default:
				continue
			}
			matched++
		}
		if matched != len(want) {
			t.Fatalf("query %q: matched %d entries, registry matched %d", query, matched, len(want))
		}
		for _, mt := range want {
			if s, ok := got[mt.Entry.ID]; !ok || s != mt.Score {
				t.Errorf("query %q: entry %s scored %d here, %d in the registry", query, mt.Entry.ID, s, mt.Score)
			}
		}
	}
}

func TestAppendPositionsWalksTheSameMatchAsScore(t *testing.T) {
	cases := []struct {
		haystack, needle string
		want             []int32
	}{
		{"wisp-parity", "wi", []int32{0, 1}},
		// Greedy, like the scorer: the 'p' found is "wisp"'s, not "parity"'s.
		{"wisp-parity", "wpy", []int32{0, 3, 10}},
		{"wisp-parity", "", nil},
		{"wisp-parity", "zz", nil},
	}
	for _, c := range cases {
		got := appendPositions(nil, c.haystack, c.needle)
		if len(got) != len(c.want) {
			t.Errorf("appendPositions(%q, %q) = %v, want %v", c.haystack, c.needle, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("appendPositions(%q, %q) = %v, want %v", c.haystack, c.needle, got, c.want)
				break
			}
		}
	}
}

// -- what the open filter draws ----------------------------------------------

func TestFilter_RenderShowsCandidatesAndTheHistoryGroup(t *testing.T) {
	m := withTargets(t, Options{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)})
	m.Focus(true)
	m.Key(charKey('@'))
	typeString(m, "wi")
	out := ansi.Strip(m.Render(60, 6))

	for _, want := range []string{"wisp-parity", "wire-up", historyGroup, historyGroupNote} {
		if !strings.Contains(out, want) {
			t.Fatalf("render is missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "rust agent browser parity") {
		t.Fatalf("render dropped the title column:\n%s", out)
	}
	// The draft keeps the LAST row; the list opens over it, never under it (8).
	rows := strings.Split(out, "\n")
	if !strings.Contains(rows[len(rows)-1], "@wi") {
		t.Fatalf("last row = %q, want the draft", rows[len(rows)-1])
	}
}

// TestFilter_OpensUpwardBestMatchNearestTheDraft is 8's flip, stated as
// geometry: every candidate row sits ABOVE the draft's own row, and the row the
// user is about to choose is the one their eye is already on.
func TestFilter_OpensUpwardBestMatchNearestTheDraft(t *testing.T) {
	m := withTargets(t, Options{Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)})
	m.Focus(true)
	m.Key(charKey('@'))
	typeString(m, "wi")
	rows := strings.Split(ansi.Strip(m.Render(60, 1+m.HintRows())), "\n")
	if len(rows) < 3 {
		t.Fatalf("the list did not open:\n%s", strings.Join(rows, "\n"))
	}
	draft := rows[len(rows)-1]
	if !strings.Contains(draft, "@wi") {
		t.Fatalf("the draft is not the bottom row: %q", draft)
	}
	best := m.filter.rows[m.filter.hits[0].idx].target.Word
	if nearest := rows[len(rows)-2]; !strings.Contains(nearest, best) {
		t.Fatalf("the row nearest the draft is %q, want the best match %q", nearest, best)
	}
	// The `history` heading names the rows under it, and settled targets rank
	// after live ones — so the heading is the TOP of the block.
	if !strings.Contains(rows[0], historyGroup) {
		t.Fatalf("first row = %q, want the history heading above its group", rows[0])
	}
}

// TestFilter_TheHistoryHeadingIsAlwaysOverItsGroup: the heading is pinned to
// the top of the window, so however far the list scrolls, a settled row is
// never drawn without the word that says settled rows are addressed ABOUT.
func TestFilter_TheHistoryHeadingIsAlwaysOverItsGroup(t *testing.T) {
	targets := make([]Target, 0, 10)
	for i := 0; i < 4; i++ {
		targets = append(targets, Target{ID: "live" + string(rune('a'+i)), Word: "task-" + string(rune('a'+i))})
	}
	for i := 0; i < 6; i++ {
		targets = append(targets, Target{ID: "old" + string(rune('a'+i)), Word: "done-" + string(rune('a'+i)), Settled: true})
	}
	m := withTargets(t, Options{Targets: func() []Target { return targets },
		Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)})
	m.Focus(true)
	m.Key(charKey('@'))
	for step := 0; step < len(targets); step++ {
		rows := strings.Split(ansi.Strip(m.Render(60, 1+m.HintRows())), "\n")
		settled := false
		for _, row := range rows {
			if strings.Contains(row, "done-") {
				settled = true
			}
		}
		if settled && !strings.Contains(rows[0], historyGroup) {
			t.Fatalf("step %d drew a settled row with no heading over it:\n%s", step, strings.Join(rows, "\n"))
		}
		m.Key(downKey())
	}
}

// TestFilter_SelectionWalksUpTheScreen: ↓ walks toward the less relevant, which
// on an upward list is toward the top of the screen — and the selection never
// scrolls away.
func TestFilter_SelectionWalksUpTheScreen(t *testing.T) {
	m := withTargets(t, Options{Styler: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)})
	m.Focus(true)
	m.Key(charKey('@'))
	height := 1 + m.HintRows()
	first := strings.Split(ansi.Strip(m.Render(60, height)), "\n")
	m.Key(downKey())
	second := strings.Split(ansi.Strip(m.Render(60, height)), "\n")

	sel := func(rows []string) int {
		for i, row := range rows {
			if strings.HasPrefix(row, padded(60, tokens.GlyphAccentRail)) {
				return i
			}
		}
		return -1
	}
	a, b := sel(first), sel(second)
	if a < 0 || b < 0 {
		t.Fatalf("no selected row drawn: %v / %v", first, second)
	}
	if b >= a {
		t.Fatalf("the selection moved from row %d to row %d; ↓ must walk up an upward list", a, b)
	}
}

func TestFilter_RenderLightsTheTypedCharacters(t *testing.T) {
	sty := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	plain := withTargets(t, Options{Styler: sty})
	plain.Key(charKey('@'))
	before := plain.Render(60, 6)

	lit := withTargets(t, Options{Styler: sty})
	lit.Key(charKey('@'))
	typeString(lit, "wisp")
	after := lit.Render(60, 6)

	if before == after {
		t.Fatalf("a typed needle changed nothing on screen; no match highlight is being drawn")
	}
	// The highlight is a tier promotion inside the word, so the word's own row
	// must carry more than one foreground sequence.
	var row string
	for _, r := range strings.Split(after, "\n") {
		if strings.Contains(ansi.Strip(r), "wisp-parity") {
			row = r
			break
		}
	}
	if row == "" {
		t.Fatalf("no row rendered the matched word:\n%s", after)
	}
	if n := strings.Count(row, "\x1b[38;2;"); n < 3 {
		t.Fatalf("row has %d foreground runs, want the matched characters painted apart from the rest: %q", n, row)
	}
}

func TestFilter_RenderSaysSoWhenNothingMatches(t *testing.T) {
	m := withTargets(t, Options{})
	m.Key(charKey('@'))
	typeString(m, "zzzz")
	out := ansi.Strip(m.Render(60, 6))
	if !strings.Contains(out, noMatch) {
		t.Fatalf("render = %q, want an honest empty state", out)
	}
}

func TestFilter_NeverTakesTheDraftsLastRow(t *testing.T) {
	m := withTargets(t, Options{})
	m.Key(charKey('@'))
	out := m.Render(40, 1)
	if strings.Contains(out, "\n") {
		t.Fatalf("Render(40,1) = %q, want the single row to stay the draft's", out)
	}
	if !strings.Contains(ansi.Strip(out), "@") {
		t.Fatalf("Render(40,1) = %q, want the draft", out)
	}
}

func TestFilter_RenderIsBoundedAndScrollsToTheSelection(t *testing.T) {
	many := make([]Target, 0, 12)
	for i := 0; i < 12; i++ {
		many = append(many, Target{ID: string(rune('a' + i)), Word: "task-" + string(rune('a'+i)), Seed: uint64(i)})
	}
	m := withTargets(t, Options{Targets: func() []Target { return many }})
	m.Key(charKey('@'))
	out := m.Render(60, 20)
	if got := len(strings.Split(out, "\n")); got > 1+maxFilterRows {
		t.Fatalf("rendered %d rows, want the list bounded to %d plus the draft", got, maxFilterRows)
	}
	for i := 0; i < 11; i++ {
		m.Key(downKey())
	}
	last := ansi.Strip(m.Render(60, 20))
	if !strings.Contains(last, "task-l") {
		t.Fatalf("the selected row scrolled off screen:\n%s", last)
	}
}

// -- width sweep ---------------------------------------------------------------

// TestFilter_WidthSweepNeverPanicsOrOverflows walks every width from 1 to 110
// against every state this lane adds — filter open with and without matches,
// and a completed token showing its chip — because the composer's rectangle is
// given to it and a pane that overruns one is a broken frame, not a clipped
// word.
func TestFilter_WidthSweepNeverPanicsOrOverflows(t *testing.T) {
	sty := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	states := map[string]func(*Model){
		"filter open, all rows": func(m *Model) { m.Key(charKey('@')) },
		"filter open, narrowed": func(m *Model) { m.Key(charKey('@')); typeString(m, "wi") },
		"filter open, no match": func(m *Model) { m.Key(charKey('@')); typeString(m, "zzzz") },
		"completed live token":  func(m *Model) { complete(m, "wisp") },
		"completed settled":     func(m *Model) { complete(m, "wire") },
		"token plus prose": func(m *Model) {
			complete(m, "wisp")
			typeString(m, "please skip H2 and report back with the numbers")
		},
	}
	for name, act := range states {
		for _, height := range []int{1, 2, 3, 6, 20} {
			for width := 1; width <= 110; width++ {
				m := withTargets(t, Options{Styler: sty})
				m.Focus(width%2 == 0)
				act(m)
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("%s: Render(%d,%d) panicked: %v", name, width, height, r)
						}
					}()
					lines := strings.Split(m.Render(width, height), "\n")
					if len(lines) > height {
						t.Fatalf("%s: Render(%d,%d) returned %d lines", name, width, height, len(lines))
					}
					for i, line := range lines {
						if w := ansi.StringWidth(line); w > width {
							t.Fatalf("%s: Render(%d,%d) line %d has width %d: %q", name, width, height, i, w, line)
						}
					}
				}()
			}
		}
	}
}

// TestFilter_KeystrokeIsAllocationLean pins the per-keystroke budget the brief
// sets. Ranking reuses the hit and needle buffers, so typing into an open
// filter must not allocate per target — only the small lowercased needle the
// scorer indexes.
func TestFilter_KeystrokeIsAllocationLean(t *testing.T) {
	many := make([]Target, 0, 64)
	for i := 0; i < 64; i++ {
		many = append(many, Target{ID: "id", Word: "task-parity-run", Title: "a title with words in it", Seed: uint64(i)})
	}
	m := withTargets(t, Options{Targets: func() []Target { return many }})
	m.Key(charKey('@'))
	typeString(m, "ta")
	got := testing.AllocsPerRun(200, func() {
		m.Key(charKey('s'))
		m.Key(backspaceKey())
	})
	if got > 8 {
		t.Fatalf("%.0f allocations per keystroke pair over %d targets, want a bounded handful", got, len(many))
	}
}
