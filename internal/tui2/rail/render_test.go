package rail

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The shipping gate: at every width from 1 to 110, in every mode, at every
// cursor position, on both focus states and every terminal profile, a render
// produces at most the rows it was given room for, never a row wider than the
// pane, never a newline inside a row, and never a panic.
//
// 110 is past [tokens.RailAtWidth] on purpose: the breakpoint is a rendering
// decision and the sweep must cross it, and 1 is there because a compositor
// under pressure will hand out a one-column rectangle before it hands out none.
func TestRenderNeverOverflowsAndNeverPanics(t *testing.T) {
	scenes := map[string]*Model{
		"home":     New(scene()),
		"task":     entered(t),
		"crowd":    New(crowd(40, LifeQueued, LifeWorking, LifeFailed, LifeSettled)),
		"settled":  New(crowd(40, LifeSettled, LifeFailed)),
		"lone":     New(&fakeSource{scopes: map[string]Scope{}}),
		"stressed": New(stress()),
	}
	modes := []Mode{ModeAuto, ModeRail, ModeList, ModeHUD}
	heights := []int{1, 2, 3, 5, 9, 24}

	// Two sweeps that cross: every width against one profile, and every
	// profile and focus against the widths where something changes — the
	// gutter floor, the rail breakpoint, and both extremes.
	sweeps := []struct {
		profile tokens.Profile
		focus   tokens.Focus
		widths  []int
	}{
		{tokens.TrueColor, tokens.FocusNormal, allWidths(1, 110)},
		{tokens.TrueColor, tokens.FocusDimmed, cornerWidths()},
		{tokens.NoColor, tokens.FocusNormal, cornerWidths()},
		{tokens.NoColor, tokens.FocusDimmed, cornerWidths()},
		{tokens.ANSI16, tokens.FocusNormal, cornerWidths()},
		{tokens.ANSI16, tokens.FocusDimmed, cornerWidths()},
		{tokens.ANSI256, tokens.FocusNormal, cornerWidths()},
		{tokens.ANSI256, tokens.FocusDimmed, cornerWidths()},
	}

	for name, m := range scenes {
		for _, sweep := range sweeps {
			v := NewView(tokens.NewStyler(sweep.profile, sweep.focus))
			for _, mode := range modes {
				for _, cursor := range cursorSamples(m.Len()) {
					m.Select(cursor)
					for _, width := range sweep.widths {
						for _, height := range heights {
							lines := v.Render(m, mode, width, height)
							if len(lines) > height {
								t.Fatalf("%s/%v/%v/%v w=%d h=%d: %d lines",
									name, sweep.profile, sweep.focus, mode, width, height, len(lines))
							}
							for i, line := range lines {
								if w := blocks.Width(line); w > width {
									t.Fatalf("%s/%v/%v/%v w=%d h=%d line %d is %d cells: %q",
										name, sweep.profile, sweep.focus, mode, width, height, i, w, line)
								}
								if strings.ContainsAny(line, "\n\r") {
									t.Fatalf("%s w=%d line %d smuggled a newline: %q",
										name, width, i, line)
								}
							}
						}
					}
				}
			}
		}
	}
}

func allWidths(lo, hi int) []int {
	out := make([]int, 0, hi-lo+1)
	for w := lo; w <= hi; w++ {
		out = append(out, w)
	}
	return out
}

// cornerWidths are the widths where a layout decision changes: the extremes,
// both sides of the gutter floor, a rail, and both sides of the breakpoint.
func cornerWidths() []int {
	return []int{1, 2, gutterFloor - 1, gutterFloor, 20, tokens.RailWidth, 60,
		tokens.RailAtWidth - 1, tokens.RailAtWidth, 110}
}

// cursorSamples covers the ends and the middle of a scope rather than every
// row: the fold and the band care about first, last and somewhere-inside.
func cursorSamples(n int) []int {
	switch {
	case n <= 0:
		return []int{0}
	case n <= 4:
		return allWidths(0, n-1)
	}
	return []int{0, 1, n / 2, n - 2, n - 1}
}

// 5.14's "never shown" tier, enforced rather than intended: no node ID, no seq,
// no journal internal reaches a row.
func TestNeverRendersIdentifiers(t *testing.T) {
	forbidden := []string{idWisp, idClean, idPerf, idH2, "{", "\"", "seq", "node_id"}
	for _, m := range []*Model{New(scene()), entered(t)} {
		v := plainView()
		for cursor := 0; cursor < m.Len(); cursor++ {
			m.Select(cursor)
			for _, mode := range []Mode{ModeRail, ModeList, ModeHUD} {
				for _, width := range []int{28, 60, 90, 110} {
					for _, line := range v.Render(m, mode, width, 30) {
						for _, bad := range forbidden {
							if strings.Contains(line, bad) {
								t.Fatalf("row leaked %q: %q", bad, line)
							}
						}
					}
				}
			}
		}
	}
}

// 5.9's anatomy, pinned. Colourless so the assertion is about layout — which
// is also why the selected row wears ▎ here: at [tokens.NoColor] there is no
// band to draw, so the cursor is carried by the gutter marker
// ([tokens.SelectionMarker]) and the row is not padded to a ground it does not
// have.
func TestCardAnatomy(t *testing.T) {
	m := New(scene())
	m.Select(1) // wisp-parity, focused, so the card expands in place
	got := plainView().Render(m, ModeRail, 28, 24)
	want := []string{
		" ● aforge",
		"────────────────────────────",
		"▎◐ wisp-parity            ›",
		"   reworking NavCtx after t…",
		"   K3 · ▁ · $8.65 · 41m",
		" ? data-clean             ›",
		"   needs a key for the vend…",
		"   Q3 · $0.44 · 6m",
		" ✓ perf-audit",
		"   wrote the report and sto…",
		"   K3 · $2.10 · 52m",
	}
	assertLines(t, got, want)
}

// The task scope of 5.15's second wireframe, in §3's connector grammar: scope
// header, orchestrator, the hairline at the room boundary, then the PLAN as a
// tree — ├─ while there are siblings below, ╰─ on the last of them, and the │
// guide running down beside a branch that is not finished, which is what puts
// NavCtx2 under KeyCutter rather than merely two spaces to the right of it.
func TestTaskScopeAnatomy(t *testing.T) {
	m := entered(t)
	m.Select(4) // KeyCutter, blocked behind H2
	got := plainView().Render(m, ModeRail, 28, 24)
	want := []string{
		"‹ wisp-parity       (4 of 5)",
		" ◐ orchestrator           ›",
		"   dropping H2 — 3 steps re…",
		"   K3 · 17%/1M · $8.65",
		"────────────────────────────",
		" ├─ ✓ XhrSyn             12m",
		" ├─ ◐ H2                 28m",
		" ├─ ◐ T3Infra            21m",
		"▎└─ ⚑ KeyCutter",
		"      waits: H2",
		// The tree spends three cells a level, so a depth-1 row in 28 columns
		// pays for its place in the plan with the cell the ladder ranks lowest.
		// Money is what is left, which is 5.9's promise kept under pressure.
		"    └─ ◐ NavCtx2       $0.37",
	}
	assertLines(t, got, want)
}

// §3's connector grammar on a shape with real depth, and the two things it has
// to get right: a branch that still has siblings below it keeps its │ running
// down past its own children, and the last child AT EVERY LEVEL draws the
// corner. Both are facts about a row's neighbours, worked out from the depths
// alone (sizeGuides) — no source states them, so no source can state them
// wrongly.
//
// And it is a JOB's grammar only. The home rail is a list of jobs, rooms and
// doors that are siblings of nothing; a tree drawn over it would claim a
// parentage those rows do not have.
func TestThePlanIsDrawnAsAConnectorTreeAndOnlyInsideAJob(t *testing.T) {
	deep := []Row{
		{ID: "task", Kind: RowSurface, Name: "orchestrator", Composer: ComposerChat},
		{ID: "a", Kind: RowStep, Name: "Alpha", Life: LifeWorking},
		{ID: "a1", Kind: RowWorker, Depth: 1, Name: "AlphaOne", Life: LifeSettled},
		{ID: "a2", Kind: RowStep, Depth: 1, Name: "AlphaTwo", Life: LifeWorking},
		{ID: "a2x", Kind: RowWorker, Depth: 2, Name: "AlphaTwoDeep", Life: LifeQueued},
		{ID: "b", Kind: RowStep, Name: "Beta", Life: LifeQueued},
	}
	src := &fakeSource{scopes: map[string]Scope{
		HomeScopeID: {ID: HomeScopeID, Title: "aforge", Rows: []Row{
			{ID: "home", Kind: RowSurface, Name: "aforge", Composer: ComposerChat},
			{ID: "task", Kind: RowTask, Name: "wisp-parity", Life: LifeWorking},
		}},
		"task": {ID: "task", Title: "wisp-parity", Rows: deep},
	}}

	m := New(src)
	home := copyOf(plainView().Render(m, ModeList, 60, 20))
	for _, glyph := range []string{tokens.GlyphTreeBranch, tokens.GlyphTreeLast} {
		if countLinesContaining(home, glyph) > 0 {
			t.Fatalf("the home rail drew a tree:\n%s", strings.Join(home, "\n"))
		}
	}

	if _, ok := m.SelectID("task"); !ok {
		t.Fatal("the task is missing")
	}
	if ev := m.Enter(); ev.Kind != EventScopeEntered {
		t.Fatalf("enter = %+v", ev)
	}
	m.Select(0)
	got := plainView().Render(m, ModeList, 60, 20)
	want := []string{
		"‹ wisp-parity",
		"▎● orchestrator                                           ›",
		"────────────────────────────────────────────────────────────",
		" ├─ ◐ Alpha",
		" │  ├─ ✓ AlphaOne",
		" │  └─ ◐ AlphaTwo",
		" │     └─ ○ AlphaTwoDeep",
		" └─ ○ Beta",
	}
	assertLines(t, got, want)
}

// 5.15: the same rows, the same selection semantics, rendered as a full-pane
// list. Same model, same width, two named surfaces — identical bytes.
func TestRailAndListAreTheSameRows(t *testing.T) {
	m := New(scene())
	v := plainView()
	for cursor := 0; cursor < m.Len(); cursor++ {
		m.Select(cursor)
		rail := copyOf(v.Render(m, ModeRail, 80, 20))
		list := copyOf(v.Render(m, ModeList, 80, 20))
		assertLines(t, list, rail)
	}
}

// 5.16: selection is a background band, not a foreground colour — and tokens'
// contrast law (Legal) forbids a band under a dimmed pane, which marks its
// cursor with the ▎ accent rail instead.
func TestSelectionIsABandWhenFocusedAndAnAccentWhenNot(t *testing.T) {
	m := New(scene())
	m.Select(1)

	focused := copyOf(colourView(tokens.FocusNormal).Render(m, ModeRail, 28, 20))
	if !strings.Contains(focused[2], "\x1b[48") {
		t.Fatalf("the selected row carries no background band: %q", focused[2])
	}
	if strings.Contains(focused[0], "\x1b[48") {
		t.Fatalf("an unselected row carries a band: %q", focused[0])
	}
	if strings.Contains(strings.Join(focused, ""), tokens.GlyphAccentRail) {
		t.Fatal("a focused pane drew the accent rail as well as the band")
	}

	dimmed := copyOf(colourView(tokens.FocusDimmed).Render(m, ModeRail, 28, 20))
	if strings.Contains(strings.Join(dimmed, ""), "\x1b[48") {
		t.Fatal("a dimmed pane drew a selection band; tokens.Legal forbids the pairing")
	}
	if !strings.HasPrefix(dimmed[2], "\x1b") || !strings.Contains(dimmed[2], tokens.GlyphAccentRail) {
		t.Fatalf("a dimmed pane did not mark its cursor with the accent rail: %q", dimmed[2])
	}
}

// THE DEFECT: at `--color none` the rail lost selection entirely. Selection was
// conveyed only by the background tint, the no-colour profile writes no
// background, and so not one cell differed between the selected row and its
// neighbours — the screenshot harness caught two frames that were byte-identical
// except for the cursor the user could not see.
//
// The law being kept is 5.16's degradation philosophy, stated in tokens'
// [tokens.NoColor]: "every state that colour carries also has a glyph (5.17),
// which is why this profile is a degradation and not a failure". The answer is
// [tokens.SelectionMarker] — the same ▎ accent rail an unfocused pane already
// uses, for the same reason (there is no ground to raise).
//
// The test is written as the invariant rather than as one profile's bytes: at
// EVERY profile, and at both focus states, a selected row must differ from an
// unselected one in at least one cell. A future profile, a future band token
// and a future marker all have to keep it.
func TestSelectionIsVisibleAtEveryProfile(t *testing.T) {
	profiles := []tokens.Profile{tokens.NoColor, tokens.ANSI16, tokens.ANSI256, tokens.TrueColor}
	focuses := []tokens.Focus{tokens.FocusNormal, tokens.FocusDimmed}
	modes := []Mode{ModeRail, ModeList}
	// Widths at and above the gutter floor: below it there is no column to
	// spend on a marker and no ground to raise either, and the renderer says so
	// by [gutterFor] returning zero.
	widths := []int{gutterFloor, 20, tokens.RailWidth, 60, 110}

	for _, profile := range profiles {
		for _, focus := range focuses {
			v := NewView(tokens.NewStyler(profile, focus))
			for _, mode := range modes {
				for _, width := range widths {
					m := New(scene())
					// Row 1 is wisp-parity, and its first line is line 2 of the
					// map in both renders — surface row, hairline, then members.
					// Selecting it expands the card underneath, which is why the
					// comparison is of that ONE line and not of the pane.
					m.Select(1)
					selected := copyOf(v.Render(m, mode, width, 24))
					m.Select(2)
					elsewhere := copyOf(v.Render(m, mode, width, 24))
					if len(selected) < 3 || len(elsewhere) < 3 {
						t.Fatalf("%s/%s %s w=%d: nothing rendered", profile, focus, mode, width)
					}
					// Trailing spaces are trimmed on purpose: a band that
					// resolved to nothing still pads its row to the pane's edge,
					// and a column of invisible spaces is not an affordance.
					// That padding is exactly what made the defect look like a
					// difference to a byte comparison while looking like nothing
					// to a reader.
					a := strings.TrimRight(selected[2], " ")
					b := strings.TrimRight(elsewhere[2], " ")
					if a == b {
						t.Fatalf("%s/%s %s w=%d: the selected row is indistinguishable from the same row unselected — "+
							"selection is invisible at this profile:\n %q",
							profile, focus, mode, width, a)
					}
				}
			}
		}
	}
}

// THE DEFECT (12.11.2, found in a screenshot and closed first in
// internal/tui2/palette): at 16 colours the band falls back to SGR 7, which
// swaps the two colours the terminal is currently using — so every tier colour
// the row wrote INSIDE the band landed on its background, and the selected row
// came out striped: one inverted block per span, with the uncoloured padding
// runs between them left plain. Reverse video is defined against the terminal's
// own two colours, so the only honest reading is ONE reversed run.
//
// The assertion is the shape rather than the bytes: whatever spans the row is
// built from, no tier foreground may appear inside a reversed band.
func TestAReversedBandIsOneRun(t *testing.T) {
	if tokens.ANSI16.SelectionStyle() != tokens.SelectionReverse {
		t.Skip("16 colours no longer reverses; this test guards that path")
	}
	v := NewView(tokens.NewStyler(tokens.ANSI16, tokens.FocusNormal))
	m := New(scene())
	m.Select(1)
	for _, mode := range []Mode{ModeRail, ModeList} {
		var selected string
		for _, line := range copyOf(v.Render(m, mode, 40, 24)) {
			if strings.Contains(line, tokens.Reverse(tokens.ANSI16)) {
				selected = line
				break
			}
		}
		if selected == "" {
			t.Fatalf("%s: no row carries the reversed band", mode)
		}
		for _, tok := range []tokens.Token{tokens.TextPrimary, tokens.TextSecondary, tokens.TextTertiary} {
			if strings.Contains(selected, tok.Fg(tokens.ANSI16, tokens.FocusNormal)) {
				t.Errorf("%s: a %s foreground is written inside the reversed band: %q", mode, tok, selected)
			}
		}
	}
}

// 5.16: the identity pastel appears in the glyph and in the selection band
// tint, and nowhere else — an accent hue never colourises text, and the band is
// the scope's ("which room am I in"), not the row's.
func TestIdentityLivesOnlyInTheGlyphAndTheBand(t *testing.T) {
	v := colourView(tokens.FocusNormal)

	home := New(scene())
	home.Select(1)
	line := copyOf(v.Render(home, ModeRail, 28, 20))[2]
	ident := tokens.IdentityFor(idWisp).Fg(tokens.TrueColor, tokens.FocusNormal)
	if strings.Count(line, ident) != 1 {
		t.Fatalf("the identity pastel is not on exactly the glyph: %q", line)
	}
	if !strings.Contains(line, tokens.Band.Bg(tokens.TrueColor, tokens.FocusNormal)) {
		t.Fatalf("the home band is tinted; identity answers which ROOM (5.16): %q", line)
	}

	task := entered(t)
	task.Select(2)
	inside := copyOf(v.Render(task, ModeRail, 28, 20))[6]
	tint := tokens.BandFor(tokens.IdentityFor(idWisp)).Bg(tokens.TrueColor, tokens.FocusNormal)
	if !strings.Contains(inside, tint) {
		t.Fatalf("a task scope's band is not tinted with its identity: %q", inside)
	}
}

// THE DEFECT: [View.identityOr] painted the pastel at every profile, but
// [tokens.Profile.IdentityDistinct] is false below 256 colours — the eight
// pastels collapse onto six chromatic slots there, so two rooms could be handed
// the same hue by a cell whose whole job is to tell them apart. 5.20's rule is
// that a lying identity is worse than none, so 16 colours gets the fallback the
// row would have worn with no identity at all.
//
// The glyph is read through the SGR immediately before it, so the assertion is
// about the cell that carries the identity and not about the row: at 16 colours
// the name beside it happens to resolve to the same bright cyan, which is
// exactly the collapse this test exists to keep off the glyph.
func TestIdentityIsWithheldBelow256Colours(t *testing.T) {
	for _, profile := range []tokens.Profile{tokens.ANSI16, tokens.ANSI256, tokens.TrueColor} {
		v := NewView(tokens.NewStyler(profile, tokens.FocusNormal))
		m := New(scene())
		// The surface row holds the cursor, so wisp-parity's glyph is painted
		// with its own foreground rather than swallowed by a selection band.
		m.Select(0)
		line := copyOf(v.Render(m, ModeRail, 40, 24))[2]
		got := sgrBefore(line, tokens.GlyphWorking)
		ident := tokens.IdentityFor(idWisp).Fg(profile, tokens.FocusNormal)
		want := ident
		if !profile.IdentityDistinct() {
			want = tokens.TextPrimary.Fg(profile, tokens.FocusNormal)
			if want == ident {
				t.Fatalf("%s: the fallback and the pastel are the same bytes; this test proves nothing", profile)
			}
		}
		if got != want {
			t.Errorf("%s: the glyph is painted %q, want %q (IdentityDistinct=%v): %q",
				profile, got, want, profile.IdentityDistinct(), line)
		}
	}
}

// 5.16: no two ADJACENT rail cards share a hue, and a card's hue does not
// change because a sibling appeared.
func TestAdjacentCardsNeverShareAnIdentity(t *testing.T) {
	a, b := collidingIDs(t)
	src := &fakeSource{scopes: map[string]Scope{
		HomeScopeID: {ID: HomeScopeID, Title: "aforge", Rows: []Row{
			{ID: "home", Kind: RowSurface, Name: "aforge"},
			{ID: a, Kind: RowTask, Name: "alpha", Life: LifeWorking},
			{ID: b, Kind: RowTask, Name: "bravo", Life: LifeWorking},
		}},
	}}
	v := colourView(tokens.FocusNormal)
	lines := copyOf(v.Render(New(src), ModeRail, 28, 20))
	first, second := sgrBefore(lines[2], tokens.GlyphWorking), sgrBefore(lines[3], tokens.GlyphWorking)
	if first == "" || first == second {
		t.Fatalf("adjacent cards share the pastel %q:\n%s", first, strings.Join(lines, "\n"))
	}
}

// collidingIDs finds two ids the stable hash sends to the same wheel entry, so
// the adjacency rule has something to resolve.
func collidingIDs(t *testing.T) (string, string) {
	t.Helper()
	seen := map[tokens.Token]string{}
	for i := 0; i < 4096; i++ {
		id := "task-" + strconv.Itoa(i)
		h := tokens.IdentityFor(id)
		if prev, ok := seen[h]; ok {
			return prev, id
		}
		seen[h] = id
	}
	t.Fatal("no two ids collided on an eight-hue wheel, which is impossible")
	return "", ""
}

// sgrBefore is the colour a glyph was painted in: the SGR sequence immediately
// preceding it on the row.
func sgrBefore(line, glyph string) string {
	at := strings.Index(line, glyph)
	if at <= 0 {
		return ""
	}
	head := line[:at]
	i := strings.LastIndex(head, "\x1b[")
	if i < 0 || !strings.HasSuffix(head, "m") {
		return ""
	}
	return head[i:]
}

// 8.1.6: shape encodes the state CATEGORY and changes only at a true
// transition. Nothing on a durable row ever animates.
func TestGlyphChangesOnlyWithState(t *testing.T) {
	cases := []struct {
		row  Row
		want string
	}{
		{Row{Kind: RowTask, Name: "x", Life: LifeQueued}, tokens.GlyphQueued},
		{Row{Kind: RowTask, Name: "x", Life: LifeWorking}, tokens.GlyphWorking},
		{Row{Kind: RowTask, Name: "x", Life: LifeSettled}, tokens.GlyphSettled},
		{Row{Kind: RowTask, Name: "x", Life: LifeFailed}, tokens.GlyphFailed},
		{Row{Kind: RowTask, Name: "x", Life: LifeCancelled}, tokens.GlyphFailed},
		{Row{Kind: RowTask, Name: "x", Life: LifePaused}, tokens.GlyphPaused},
		{Row{Kind: RowTask, Name: "x", Life: LifeWorking, Questions: 1}, tokens.GlyphNeedsHuman},
		{Row{Kind: RowTask, Name: "x", Life: LifeQueued, WaitsOn: []string{"y"}}, tokens.GlyphWaitsOn},
	}
	v := plainView()
	for _, c := range cases {
		m := New(oneRow(c.row))
		line := v.Render(m, ModeRail, 28, 8)[2]
		if !strings.Contains(line, c.want) {
			t.Fatalf("%v row drew %q, want the glyph %q", c.row.Life, line, c.want)
		}
	}
	// The shared-clock spinner belongs to transient tool rows, never here.
	m := New(scene())
	all := strings.Join(v.Render(m, ModeRail, 28, 30), "")
	for _, frame := range tokens.SpinnerFrames {
		if strings.Contains(all, frame) {
			t.Fatalf("a durable row animated with %q (8.1.6)", frame)
		}
	}
}

// 5.11: the mark is a preview of the composer the row will bind, so it can
// never promise a prompt the row will not give.
func TestComposerMarkPreviewsTheComposer(t *testing.T) {
	cases := []struct {
		row  Row
		want string
	}{
		{Row{Kind: RowTask, Name: "x", Composer: ComposerChat}, tokens.GlyphPromptChat},
		{Row{Kind: RowTask, Name: "x", Composer: ComposerSteer}, tokens.GlyphPromptSteer},
		{Row{Kind: RowTask, Name: "x", Composer: ComposerChat, Life: LifeSettled}, ""},
		{Row{Kind: RowTask, Name: "x", Composer: ComposerNone}, ""},
	}
	v := plainView()
	for _, c := range cases {
		line := v.Render(New(oneRow(c.row)), ModeRail, 28, 8)[2]
		marks := strings.Count(line, tokens.GlyphPromptChat) + strings.Count(line, tokens.GlyphPromptSteer)
		if c.want == "" {
			if marks != 0 {
				t.Fatalf("row %+v drew a composer mark: %q", c.row, line)
			}
			continue
		}
		if !strings.Contains(line, c.want) {
			t.Fatalf("row %+v drew %q, want %q", c.row, line, c.want)
		}
	}
}

// 5.9: money is always visible; the rest can truncate on narrow rails.
func TestMoneySurvivesEveryNarrowing(t *testing.T) {
	row := Row{
		Kind: RowTask, Name: "wisp-parity", Composer: ComposerChat, Life: LifeWorking,
		Meta: Telemetry{
			Model: "K3", Effort: "high", Cost: 8.65, HasCost: true,
			ContextUsed: 800_000, ContextWindow: 1_000_000,
			Elapsed: 41 * time.Minute, HasElapsed: true,
			Workers: 4, HasWorkers: true,
		},
	}
	v := plainView()
	for width := 12; width <= 110; width++ {
		lines := v.Render(New(oneRow(row)), ModeRail, width, 8)
		meta := lines[len(lines)-1]
		if !strings.Contains(meta, "$8.65") {
			t.Fatalf("w=%d dropped the money: %q", width, meta)
		}
	}
}

// 5.17: the one-cell gauge is the ambient form and the precise reading is the
// wider one; 8.2.17's dual threshold turns it amber.
func TestContextDegradesToTheGaugeAndWarnsInAmber(t *testing.T) {
	calm := Telemetry{
		Model: "K3", Cost: 1, HasCost: true,
		ContextUsed: 100_000, ContextWindow: 1_000_000,
		Elapsed: 41 * time.Minute, HasElapsed: true,
		Workers: 4, HasWorkers: true,
	}
	hot := calm
	hot.ContextUsed = 900_000
	v := plainView()
	wide := strings.Join(v.Render(New(oneRow(Row{Kind: RowTask, Name: "x", Meta: calm})), ModeList, 90, 8), "\n")
	if !strings.Contains(wide, "10%/1M") {
		t.Fatalf("wide context = %q, want the precise reading", wide)
	}
	narrow := strings.Join(v.Render(New(oneRow(Row{Kind: RowTask, Name: "x", Meta: calm})), ModeRail, 28, 8), "\n")
	if !strings.Contains(narrow, tokens.Gauge(0.1)) {
		t.Fatalf("narrow context = %q, want the one-cell gauge", narrow)
	}
	colour := colourView(tokens.FocusNormal)
	amber := strings.Join(colour.Render(New(oneRow(Row{Kind: RowTask, Name: "x", Meta: hot})), ModeRail, 28, 8), "\n")
	if !strings.Contains(amber, tokens.Amber.Fg(tokens.TrueColor, tokens.FocusNormal)) {
		t.Fatalf("a context past its warn point is not amber: %q", amber)
	}
}

// 12.5.2, the truncation law: a cut row renders visibly cut, and the mark
// survives after the words have gone.
func TestCutRowsRenderVisiblyCut(t *testing.T) {
	row := Row{
		Kind: RowTask, Name: "diagram", Status: "drawing the architecture",
		Life: LifeSettled, Cut: tokens.CutLengthCap,
	}
	v := plainView()
	wide := v.Render(New(oneRow(row)), ModeList, 90, 8)[3]
	if !strings.Contains(wide, tokens.GlyphCut) || !strings.Contains(wide, "output cap") {
		t.Fatalf("wide cut row = %q", wide)
	}
	narrow := v.Render(New(oneRow(row)), ModeRail, 20, 8)[3]
	if !strings.Contains(narrow, tokens.GlyphCut) {
		t.Fatalf("narrow cut row lost its mark: %q", narrow)
	}
	// An interrupt is the user's own doing and is not painted as breakage.
	row.Cut = tokens.CutInterrupt
	colour := colourView(tokens.FocusNormal)
	line := colour.Render(New(oneRow(row)), ModeList, 90, 8)[3]
	if strings.Contains(line, tokens.Coral.Fg(tokens.TrueColor, tokens.FocusNormal)) {
		t.Fatalf("an interrupt was painted as broken: %q", line)
	}
}

// 12.5.1, the artifact law: a deliverable is referenced by its path, and a path
// is cut in the middle because the filename is the information (5.21).
func TestArtifactRowsReferenceTheirPath(t *testing.T) {
	row := Row{
		Kind: RowTask, Name: "diagram", Life: LifeSettled,
		Artifact: Ref{Path: "docs/architecture/2026-08/overview.svg"},
	}
	v := plainView()
	m := New(oneRow(row))
	m.Select(1)
	wide := strings.Join(v.Render(m, ModeList, 90, 8), "\n")
	if !strings.Contains(wide, "docs/architecture/2026-08/overview.svg") {
		t.Fatalf("the artifact path is missing:\n%s", wide)
	}
	narrow := strings.Join(v.Render(m, ModeRail, 28, 8), "\n")
	if !strings.Contains(narrow, "overview.svg") {
		t.Fatalf("a narrow rail tail-truncated the path:\n%s", narrow)
	}
}

// §14: the shape of a job is a CENSUS and never a fraction. `9/30` promises a
// denominator a replan can invalidate; `9✓ 21○` cannot go stale, costs the same
// cells whatever the plan's size, and drops off a narrow rail before the money
// does. And a job with no parts says nothing at all rather than "1" or
// "atomic" — every one of its counts is the whole of it.
func TestAJobsShapeIsACensusAndNeverAFraction(t *testing.T) {
	row := Row{Kind: RowTask, Name: "big", Life: LifeWorking,
		Meta: Telemetry{Cost: 8.65, HasCost: true,
			Counts: StateCounts{Done: 9, Queued: 21}}}
	m := New(oneRow(row))
	m.Select(1)
	v := plainView()

	wide := strings.Join(copyOf(v.Render(m, ModeList, 90, 12)), "\n")
	if !strings.Contains(wide, "21"+tokens.GlyphQueued+" 9"+tokens.GlyphSettled) {
		t.Fatalf("the census is missing:\n%s", wide)
	}
	for _, banned := range []string{"9/30", "/30", "30 workers", "atomic"} {
		if strings.Contains(wide, banned) {
			t.Fatalf("the card said %q:\n%s", banned, wide)
		}
	}
	// The census is the first cell a squeezed row gives up (prioCounts), and the
	// money is the last thing standing (5.9).
	narrow := strings.Join(copyOf(v.Render(m, ModeList, 16, 12)), "\n")
	if strings.Contains(narrow, tokens.GlyphQueued) {
		t.Fatalf("the census outlived the column it was borrowing:\n%s", narrow)
	}
	if !strings.Contains(narrow, "$8.65") {
		t.Fatalf("the money was dropped before the census:\n%s", narrow)
	}

	silent := New(oneRow(Row{Kind: RowTask, Name: "one hand", Life: LifeWorking}))
	silent.Select(1)
	frame := strings.Join(copyOf(v.Render(silent, ModeList, 90, 12)), "\n")
	for _, banned := range []string{"atomic", "1 worker", "0/1", "1/1"} {
		if strings.Contains(frame, banned) {
			t.Fatalf("a one-part job described its own shape as %q:\n%s", banned, frame)
		}
	}
}

// 8.2.8: the HUD is bounded, carries the live summary, and is not the map — no
// surface row, no scope header, no cursor.
func TestHUDIsBoundedAndCarriesOnlyLiveWork(t *testing.T) {
	m := New(crowd(40, LifeWorking, LifeSettled, LifeQueued, LifeFailed))
	v := plainView()
	lines := v.Render(m, ModeHUD, 60, 40)
	if len(lines) > tokens.HUDRowCap {
		t.Fatalf("HUD drew %d rows, cap is %d", len(lines), tokens.HUDRowCap)
	}
	if strings.Contains(strings.Join(lines, "\n"), "aforge") {
		t.Fatal("the HUD drew the scope's surface row; it is not the map (5.15)")
	}
	if !strings.Contains(lines[0], "more") {
		t.Fatalf("the HUD hid rows without accounting for them: %q", lines[0])
	}

	settled := New(crowd(6, LifeSettled))
	if got := v.Render(settled, ModeHUD, 60, 8); len(got) != 0 {
		t.Fatalf("a settled scope produced a HUD: %v", got)
	}
}

func TestHUDShowsAQuestionEvenOnSettledWork(t *testing.T) {
	src := oneRow(Row{Kind: RowTask, Name: "asked", Life: LifeSettled, Questions: 1})
	lines := plainView().Render(New(src), ModeHUD, 40, 8)
	if len(lines) != 1 || !strings.Contains(lines[0], tokens.GlyphNeedsHuman) {
		t.Fatalf("HUD = %v, want the open question", lines)
	}
}

// The gutter is reserved in both focus states so nothing shifts sideways when
// the pane gains or loses focus.
func TestFocusDoesNotShiftTheLayout(t *testing.T) {
	m := New(scene())
	m.Select(2)
	normal := copyOf(plainView().Render(m, ModeRail, 28, 20))
	dim := copyOf(NewView(tokens.NewStyler(tokens.NoColor, tokens.FocusDimmed)).Render(m, ModeRail, 28, 20))
	if len(normal) != len(dim) {
		t.Fatalf("focus changed the row count: %d vs %d", len(normal), len(dim))
	}
	for i := range normal {
		// The gutter cell itself is the one difference: a space when the pane
		// is focused (the band marks the row), the accent rail when it is not.
		if a, b := withoutGutter(normal[i]), withoutGutter(dim[i]); a != b {
			t.Fatalf("line %d shifted with focus:\n normal %q\n dimmed %q", i, normal[i], dim[i])
		}
	}
}

// 12.10.6's second finding, at the rail's door. The sanitiser PRESERVES SGR
// and remaps it into the palette — the right trade for a colour terminal, where
// a worker's own red is information it meant to carry. At tokens.NoColor it is
// the wrong trade: the surface has promised no escapes, and the readers of that
// promise are a dumb pipe and a golden file, both of which read a surviving SGR
// as corruption.
//
// The promise is total, so the test is: at NoColor, no rendered cell contains
// an ESC, whatever the prose brought with it. At a colour profile the same
// prose keeps its colour, because erasing it there would be a different bug.
func TestNoColourEmitsNoEscapesEvenWhenTheProseBroughtSome(t *testing.T) {
	row := Row{
		Kind:   RowTask,
		Name:   "\x1b[31mred name\x1b[0m",
		Status: "\x1b[1;32mit went green\x1b[m",
		Life:   LifeWorking,
	}
	src := oneRow(row)

	for _, mode := range []Mode{ModeRail, ModeList, ModeHUD} {
		plain := plainView().Render(New(src), mode, 60, 12)
		for i, line := range plain {
			if strings.ContainsRune(line, 0x1b) {
				t.Fatalf("%s line %d carried an escape at the no-colour profile: %q", mode, i, line)
			}
		}
		coloured := colourView(tokens.FocusNormal).Render(New(src), mode, 60, 12)
		if !strings.ContainsRune(strings.Join(coloured, ""), 0x1b) {
			t.Fatalf("%s: a colour profile lost the prose's own colour entirely: %v", mode, coloured)
		}
	}
}

// Model-written prose reaches this surface as names and status lines. It may
// not move the cursor or smuggle a second row.
func TestProseCannotEscapeItsRow(t *testing.T) {
	row := Row{
		Kind: RowTask, Name: "evil\x1b[2Jname",
		Status: "first line\nsecond line\ttabbed",
		Life:   LifeWorking,
	}
	lines := plainView().Render(New(oneRow(row)), ModeList, 60, 8)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "\x1b[2J") {
		t.Fatalf("an escape sequence survived: %q", joined)
	}
	if len(lines) != 4 {
		t.Fatalf("a newline in a status became %d lines: %v", len(lines), lines)
	}
}

func TestPaneIsUsableAtItsZeroValue(t *testing.T) {
	var p *Pane
	if got := p.Render(20, 5); got != "" {
		t.Fatalf("a nil Pane rendered %q", got)
	}
	empty := &Pane{}
	if got := empty.Render(20, 5); got != "" {
		t.Fatalf("a Pane with no model rendered %q", got)
	}
	live := &Pane{Model: New(scene())}
	out := live.Render(28, 6)
	if out == "" || strings.Count(out, "\n") > 5 {
		t.Fatalf("pane render = %q", out)
	}
	if live.View == nil {
		t.Fatal("the pane did not build itself a view")
	}
}

func TestModeForCrossesTheDocumentedBreakpoint(t *testing.T) {
	if got := ModeFor(tokens.RailAtWidth - 1); got != ModeList {
		t.Fatalf("below the breakpoint = %v", got)
	}
	if got := ModeFor(tokens.RailAtWidth); got != ModeRail {
		t.Fatalf("at the breakpoint = %v", got)
	}
}

// A render is a pure function of the model and the size: two calls with the
// same arguments produce the same bytes, and a repaint allocates almost
// nothing once the buffers are warm.
func TestRenderIsPureAndAllocationLean(t *testing.T) {
	m := New(scene())
	m.Select(1)
	v := colourView(tokens.FocusNormal)
	first := copyOf(v.Render(m, ModeRail, 28, 20))
	second := copyOf(v.Render(m, ModeRail, 28, 20))
	assertLines(t, second, first)

	allocs := testing.AllocsPerRun(50, func() {
		v.Render(m, ModeRail, 28, 20)
	})
	// One allocation per emitted line is the floor — a rendered line IS a
	// string — and the formatted numbers on a card's third line are the rest.
	// The budget is per LINE rather than absolute so the assertion keeps
	// meaning something when the scene grows; what it catches is a render that
	// starts allocating per CELL or per frame-sized buffer.
	if allocs > float64(5*len(first)) {
		t.Fatalf("a repaint allocated %.0f times for %d lines", allocs, len(first))
	}
}

// entered returns a model already inside the wisp-parity scope.
func entered(t *testing.T) *Model {
	t.Helper()
	m := New(scene())
	if _, ok := m.SelectID(idWisp); !ok {
		t.Fatal("wisp-parity is missing from the scene")
	}
	if ev := m.Enter(); ev.Kind != EventScopeEntered {
		t.Fatalf("enter = %+v", ev)
	}
	return m
}

// oneRow builds a home scope with a single member, for the law tests that want
// one card and nothing else on screen.
func oneRow(r Row) *fakeSource {
	return &fakeSource{scopes: map[string]Scope{
		HomeScopeID: {ID: HomeScopeID, Title: "aforge", Rows: []Row{
			{ID: "home", Kind: RowSurface, Name: "aforge", Composer: ComposerChat},
			r,
		}},
	}}
}

// stress is the awkward scope: empty names, long prose, deep indent, every
// optional field present at once.
func stress() *fakeSource {
	long := strings.Repeat("a very long status that keeps going ", 6)
	rows := []Row{
		{ID: "s", Kind: RowSurface, Name: "", Status: long},
		{ID: "1", Kind: RowTask, Name: "", Status: long, Life: LifeWorking, Questions: 12,
			Composer: ComposerSteer, Cut: tokens.CutStreamDrop,
			Artifact: Ref{Path: strings.Repeat("nested/", 12) + "file.svg"},
			WaitsOn:  []string{"one", "two", "three", "four"},
			Workers:  make([]Row, 20),
			Meta: Telemetry{
				Model: strings.Repeat("model", 6), Effort: "high", Boosted: true,
				Cost: 12345.678, HasCost: true,
				ContextUsed: 1 << 40, ContextWindow: 1,
				Elapsed: 400 * 24 * time.Hour, Estimate: time.Second, HasElapsed: true,
				Counts: StateCounts{Queued: 1 << 30, Running: 1 << 30, Done: 1 << 30,
					Failed: 1 << 30, Cancelled: 1 << 30},
			}},
		{ID: "2", Kind: RowWorker, Depth: 4, Name: "deep", Life: LifeCancelled},
		{ID: "3", Kind: RowStep, Name: "one hand", Life: LifePaused,
			Meta: Telemetry{Cost: -1, HasCost: true}},
	}
	return &fakeSource{scopes: map[string]Scope{
		HomeScopeID: {ID: HomeScopeID, Title: "", Rows: rows},
	}}
}

// withoutGutter drops the leading gutter cell and any trailing pad, leaving the
// content a focus change must not move.
func withoutGutter(line string) string {
	line = strings.TrimRight(line, " ")
	if line == "" {
		return line
	}
	r := []rune(line)
	return strings.TrimRight(string(r[1:]), " ")
}

func copyOf(lines []string) []string {
	out := make([]string, len(lines))
	copy(out, lines)
	return out
}

func assertLines(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d:\n got:\n%s\nwant:\n%s",
			len(got), len(want), strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d:\n got %q\nwant %q", i, got[i], want[i])
		}
	}
}

// mergedRoom is the shape a pty screenshot caught: an entered room whose source
// has ONE word for the scope and for its surface row. chat's taskScope names row
// 0 after the task, the homes package names row 0 after the home, and
// Scope.normalize fills the name in from the title when a source leaves it
// empty — so this is not one source's habit, it is the shape the rail has to
// render well.
func mergedRoom() *fakeSource {
	const room = "Permanent Aforge spine"
	f := &fakeSource{scopes: map[string]Scope{
		HomeScopeID: {ID: HomeScopeID, Title: "aforge", Rows: []Row{
			{ID: "home", Kind: RowSurface, Name: "aforge", Composer: ComposerChat},
			{ID: "task", Kind: RowTask, Name: room, Life: LifeWorking, Seed: "task"},
		}},
		"task": {ID: "task", Title: room, Seed: "task", Rows: []Row{
			{
				ID: "task", Kind: RowSurface, Name: room,
				Status: "1 part running", Life: LifeWorking,
				Composer: ComposerSteer, Seed: "task",
			},
		}},
	}}
	return f
}

func enteredMerged(t *testing.T) *Model {
	t.Helper()
	m := New(mergedRoom())
	if _, ok := m.SelectID("task"); !ok {
		t.Fatal("the task is missing from the scene")
	}
	if ev := m.Enter(); ev.Kind != EventScopeEntered {
		t.Fatalf("enter = %+v", ev)
	}
	return m
}

// THE DEFECT: an entered room said its own name three times — scope header, row
// 0, and the detail card in the main pane. 5.15's wireframe says the header and
// row 0 are for two different things ("‹ wisp-parity" names the ROOM, "●
// orchestrator" says what row 0 IS), and when a source has only one word for
// both the rail draws one row rather than the word twice.
func TestAnEnteredRoomSaysItsNameOnce(t *testing.T) {
	const room = "Permanent Aforge spine"
	for _, mode := range []Mode{ModeRail, ModeList} {
		lines := copyOf(plainView().Render(enteredMerged(t), mode, 40, 20))
		if n := countLinesContaining(lines, room); n != 1 {
			t.Fatalf("%s: the room named itself %d times, want once:\n%s",
				mode, n, strings.Join(lines, "\n"))
		}
		// Every affordance the merge absorbed is still on the row it merged into.
		first := lines[0]
		for _, want := range []string{
			tokens.GlyphScopeUp,     // the way out is still clickable (5.15)
			tokens.GlyphWorking,     // the lifecycle glyph (5.17)
			room,                    // the room, named once
			tokens.GlyphPromptSteer, // 5.11's composer mark, still promised
		} {
			if !strings.Contains(first, want) {
				t.Fatalf("%s: the merged row lost %q: %q", mode, want, first)
			}
		}
		// And the surface row's own lines survive under it.
		if len(lines) < 2 || !strings.Contains(lines[1], "1 part running") {
			t.Fatalf("%s: the surface row lost its status line:\n%s", mode, strings.Join(lines, "\n"))
		}
	}
}

// The merge fires ONLY on the collision. A room whose source gave row 0 a word
// of its own gets 5.15's wireframe exactly: the header names the room, row 0
// says what it is, and both keep their line.
func TestAScopeWithItsOwnSurfaceWordKeepsBothLines(t *testing.T) {
	lines := copyOf(plainView().Render(entered(t), ModeRail, 28, 24))
	if len(lines) < 2 {
		t.Fatalf("task scope rendered %d lines", len(lines))
	}
	if !strings.HasPrefix(strings.TrimSpace(lines[0]), tokens.GlyphScopeUp) {
		t.Fatalf("the scope header is gone: %q", lines[0])
	}
	if !strings.Contains(lines[0], "wisp-parity") {
		t.Fatalf("the header stopped naming the room: %q", lines[0])
	}
	if !strings.Contains(lines[1], "orchestrator") {
		t.Fatalf("row 0 stopped saying what it is: %q", lines[1])
	}
	if strings.Contains(lines[1], tokens.GlyphScopeUp) {
		t.Fatalf("row 0 absorbed a header it did not collide with: %q", lines[1])
	}
}

// Home has no header to merge, so nothing changes there: row 0 is the only
// place the word `aforge` can live and it keeps it.
func TestHomeIsUntouchedByTheMerge(t *testing.T) {
	lines := copyOf(plainView().Render(New(mergedRoom()), ModeRail, 40, 20))
	if strings.Contains(strings.Join(lines, "\n"), tokens.GlyphScopeUp) {
		t.Fatalf("home drew a scope-up glyph:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[0], "aforge") {
		t.Fatalf("home lost its surface row: %q", lines[0])
	}
}

func countLinesContaining(lines []string, want string) int {
	n := 0
	for _, line := range lines {
		if strings.Contains(line, want) {
			n++
		}
	}
	return n
}

// The merge removes ONE LINE — the header that was saying the row's own word —
// and touches nothing else. A screenshot of an entered room showed a single row
// where a card's worth of rows had been, and the reading was that the merge had
// eaten the scope. It had not: the room's own source was handing the rail one
// bare row. This pins the difference so the two failures can never be confused
// again — whatever a scope contains, merging costs it the header line and no
// other line.
func TestTheMergeCostsTheHeaderLineAndNothingElse(t *testing.T) {
	const room = "Permanent Aforge spine"
	rich := []Row{
		{
			ID: "task", Kind: RowSurface, Name: room,
			Status: "1 part running", Life: LifeWorking, Composer: ComposerSteer,
			Meta: Telemetry{Cost: 8.65, HasCost: true, Atomic: true},
		},
		{ID: "task/a", Kind: RowWorker, Depth: 1, Name: "XhrSyn", Life: LifeSettled},
		{ID: "task/b", Kind: RowWorker, Depth: 1, Name: "H2", Life: LifeWorking},
		{ID: "task/c", Kind: RowWorker, Depth: 1, Name: "KeyCutter", Life: LifeQueued,
			WaitsOn: []string{"H2"}},
	}

	merging := &fakeSource{scopes: map[string]Scope{
		HomeScopeID: {ID: HomeScopeID, Title: "aforge", Rows: []Row{
			{ID: "home", Kind: RowSurface, Name: "aforge", Composer: ComposerChat},
			{ID: "task", Kind: RowTask, Name: room, Life: LifeWorking},
		}},
		"task": {ID: "task", Title: room, Rows: rich},
	}}
	// The same scope, with a word of its own for row 0: no merge, both lines.
	named := &fakeSource{scopes: map[string]Scope{}}
	for id, sc := range merging.scopes {
		named.scopes[id] = sc
	}
	distinct := append([]Row(nil), rich...)
	distinct[0].Name = "orchestrator"
	named.scopes["task"] = Scope{ID: "task", Title: room, Rows: distinct}

	enter := func(src *fakeSource) []string {
		m := New(src)
		if _, ok := m.SelectID("task"); !ok {
			t.Fatal("the task is missing")
		}
		if ev := m.Enter(); ev.Kind != EventScopeEntered {
			t.Fatalf("enter = %+v", ev)
		}
		return copyOf(plainView().Render(m, ModeRail, 30, 24))
	}

	mergedLines := enter(merging)
	namedLines := enter(named)

	if len(namedLines)-len(mergedLines) != 1 {
		t.Fatalf("the merge cost %d lines, want exactly 1:\nmerged:\n%s\nnamed:\n%s",
			len(namedLines)-len(mergedLines),
			strings.Join(mergedLines, "\n"), strings.Join(namedLines, "\n"))
	}
	// Every row the scope carries still has its line, and so does the surface's
	// own status. Only the header's separate line is gone.
	for _, want := range []string{"1 part running", "XhrSyn", "H2", "KeyCutter", "waits:"} {
		if countLinesContaining(mergedLines, want) == 0 {
			t.Fatalf("the merge ate %q:\n%s", want, strings.Join(mergedLines, "\n"))
		}
	}
	// The hairline at the room boundary survives too (5.13): there are members
	// to separate from the surface.
	if countLinesContaining(mergedLines, tokens.GlyphTreeDash) == 0 {
		t.Fatalf("the merge ate the room boundary:\n%s", strings.Join(mergedLines, "\n"))
	}
}

// THE DEFECT (7.2, caught in a pty investigation): selecting a card expanded it
// from three rows to about ten — summary, progress, per-worker rows, the lot —
// and the fold, which budgets in LINES, paid for those rows out of the cards
// around it. The neighbours slid into `… 7 more`, so the card a reader was
// about to click moved out from under the pointer. 7.2's stable-order law was
// kept to the letter (nothing re-sorted) and broken where it is felt: a row that
// MOVES because the cursor rested one row above it is the same betrayal.
//
// The invariant, stated so that neither half of the fix can be dropped: at any
// size, in either map rendering, previewing a row that was already on screen
// leaves every OTHER line of the frame exactly as it was — same rows, same
// order, same bytes, same chrome. Only the previewed card's own lines appear,
// directly beneath it.
//
// The one line deliberately excluded is the scope header's `(2 of 5)` counter,
// which IS the cursor's own readout and is supposed to move with it.
func TestPreviewingACardNeverMovesTheOtherRows(t *testing.T) {
	scenes := map[string]*Model{
		"home":     New(scene()),
		"task":     entered(t),
		"crowd":    New(crowd(12, LifeQueued, LifeWorking, LifeFailed, LifeSettled)),
		"settled":  New(crowd(12, LifeSettled, LifeFailed)),
		"stressed": New(stress()),
	}
	for name, m := range scenes {
		for _, mode := range []Mode{ModeRail, ModeList} {
			for _, width := range []int{28, tokens.RailWidth, 60, 110} {
				// Every height from "two lines and a prayer" up past the tallest
				// scene here: the reserve has to hold at each of them, which is
				// what stops a cap from being an invariant only on big screens.
				for height := 2; height <= 26; height++ {
					v := plainView()
					m.Select(0)
					base := frameOf(v, v.Render(m, mode, width, height))
					for _, row := range rowsShown(base) {
						m.Select(row)
						got := frameOf(v, v.Render(m, mode, width, height))
						assertSameRows(t, others(got, row), others(base, row),
							"%s/%v w=%d h=%d row=%d", name, mode, width, height, row)
					}
					m.Select(0)
				}
			}
		}
	}
}

// railLine is one frame line with the row it was drawn for, read back through
// the shipped hit-test table so the assertion is about the map the pointer
// lands on and not about a second description of it.
type railLine struct {
	row  int
	text string
}

// frameOf reads the last frame back as its lines tagged with the row each was
// drawn for: the model row where there is one, [markScopeUp] for the scope
// header, [markChrome] for the hairline and the fold line.
func frameOf(v *View, lines []string) []railLine {
	out := make([]railLine, 0, len(lines))
	for y, line := range lines {
		key := int(markChrome)
		if y == v.upLine {
			key = int(markScopeUp)
		} else if row, ok := v.RowAt(y); ok {
			key = row
		}
		out = append(out, railLine{row: key, text: line})
	}
	return out
}

// rowsShown is which model rows a frame drew, surface excluded. It is the set
// the invariant above quantifies over: a row the fold had already hidden is a
// different question, because the cursor must never fold away (fold.go) and so
// selecting a hidden row necessarily reveals it.
func rowsShown(frame []railLine) []int {
	var out []int
	for _, l := range frame {
		if l.row <= 0 {
			continue
		}
		if n := len(out); n > 0 && out[n-1] == l.row {
			continue
		}
		out = append(out, l.row)
	}
	return out
}

// others is the frame without the two rows whose own selection state differs
// between the two renders — the row under test and the surface — and without
// the scope header, which carries the `(2 of 5)` counter.
func others(frame []railLine, sel int) []railLine {
	out := make([]railLine, 0, len(frame))
	for _, l := range frame {
		if l.row == int(markScopeUp) || l.row == 0 || l.row == sel {
			continue
		}
		out = append(out, l)
	}
	return out
}

func assertSameRows(t *testing.T, got, want []railLine, format string, args ...any) {
	t.Helper()
	where := fmt.Sprintf(format, args...)
	if len(got) != len(want) {
		t.Fatalf("%s: the preview left %d other lines, want %d:\n got:\n%s\nwant:\n%s",
			where, len(got), len(want), showRows(got), showRows(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: line %d moved:\n got %d %q\nwant %d %q",
				where, i, got[i].row, got[i].text, want[i].row, want[i].text)
		}
	}
}

func showRows(lines []railLine) string {
	var b strings.Builder
	for _, l := range lines {
		fmt.Fprintf(&b, "%3d %q\n", l.row, l.text)
	}
	return b.String()
}

// The cap itself, which is what makes the reserve affordable: no row anywhere
// grows by more than [previewLines] when the cursor arrives, so the rail spends
// at most two lines of its budget keeping the picture still.
func TestAPreviewNeverGrowsARowByMoreThanTwoLines(t *testing.T) {
	v := plainView()
	for name, m := range map[string]*Model{
		"home": New(scene()), "task": entered(t), "stressed": New(stress()),
	} {
		for i, r := range m.Rows() {
			if got := v.previewOf(r); got < 0 || got > previewLines {
				t.Fatalf("%s row %d (%q) previews %d lines, want 0..%d",
					name, i, r.Name, got, previewLines)
			}
		}
	}
}

// What the preview's lines are spent on, and what they are not. The summaries a
// card has to offer — the census of its parts and its money — are CELLS on the
// collapsed card's own line 3, so a reader gets them without pausing; the one
// thing selection buys is the deliverable, which needs a path and therefore a
// line. The per-worker rows are a LIST: they have no bounded form, so 5.9's full
// depth keeps them one room away.
func TestThePreviewCarriesTheSummariesAndNotTheTree(t *testing.T) {
	card := Row{
		ID: "big", Kind: RowTask, Name: "wisp-parity", Life: LifeWorking,
		Status:   "reworking NavCtx",
		Artifact: Ref{Path: "docs/perf/report.md"},
		Workers: []Row{
			{ID: "w1", Name: "H2Probe", Life: LifeWorking},
			{ID: "w2", Name: "KeyCutter", Life: LifeQueued},
			{ID: "w3", Name: "XhrSyn", Life: LifeSettled},
		},
		Meta: Telemetry{Cost: 8.65, HasCost: true,
			Counts: StateCounts{Running: 1, Queued: 1, Done: 1}},
	}
	m := New(oneRow(card))
	v := plainView()

	m.Select(0)
	collapsed := copyOf(v.Render(m, ModeList, 90, 24))
	m.Select(1)
	focused := copyOf(v.Render(m, ModeList, 90, 24))
	if grew := len(focused) - len(collapsed); grew != 1 {
		t.Fatalf("the preview grew the frame by %d lines, want 1:\n%s",
			grew, strings.Join(focused, "\n"))
	}

	// The census and the money are on the card whether it is looked at or not.
	census := "1" + tokens.GlyphWorking + " 1" + tokens.GlyphQueued + " 1" + tokens.GlyphSettled
	for _, want := range []string{census, "$8.65"} {
		if !strings.Contains(strings.Join(collapsed, "\n"), want) {
			t.Fatalf("the COLLAPSED card dropped %q:\n%s", want, strings.Join(collapsed, "\n"))
		}
	}
	frame := strings.Join(focused, "\n")
	if !strings.Contains(frame, "report.md") {
		t.Fatalf("the preview dropped the deliverable:\n%s", frame)
	}
	for _, gone := range []string{"H2Probe", "KeyCutter", "XhrSyn"} {
		if strings.Contains(frame, gone) {
			t.Fatalf("the preview drew the worker row %q; that is the room's job:\n%s", gone, frame)
		}
	}
}

// -- the rail's one motion: the clock (§11 "numbers tick") --------------------

// A RUNNING CARD'S CLOCK COUNTS BETWEEN SNAPSHOTS, AND A SETTLED ONE DOES NOT.
//
// The reported defect was "nothing moves while work runs". The rail's honest
// answer is NOT a spinner — §18.2 names a rail card as the example of where a
// moving glyph is forbidden, "a lie about liveness" on a durable object — it is
// the elapsed cell, which used to be measured inside the snapshot and therefore
// froze between journal moves. A worker inside a tool call journals nothing at
// all, so "between journal moves" was minutes.
func TestARunningCardsClockCountsBetweenSnapshots(t *testing.T) {
	model := New(scene())
	clock := blocks.NewClock(0)
	base := time.Unix(1700000000, 0)
	clock.Latch(base)
	model.SetClock(clock)

	view := NewView(nil)
	draw := func(at time.Time) string {
		clock.Latch(at)
		return strings.Join(view.Render(model, ModeRail, 40, 24), "\n")
	}
	first := draw(base)
	later := draw(base.Add(90 * time.Second))
	if first == later {
		t.Fatalf("ninety seconds passed and the rail drew the same bytes:\n%s", first)
	}
	// The fixture's running card is at 41m; a minute and a half on says so.
	if !strings.Contains(first, "41m") {
		t.Fatalf("the snapshot's own figure is not on the card:\n%s", first)
	}
	if !strings.Contains(later, "42m") {
		t.Fatalf("the running card's clock did not count on:\n%s", later)
	}
	// A repaint at the same instant is byte-identical: the rail costs nothing
	// between the seconds it has something to say (8.1.3).
	if again := draw(base.Add(90 * time.Second)); again != later {
		t.Fatalf("two renders of one instant disagree:\n%s\n%s", later, again)
	}
}

// A SETTLED ROW'S CLOCK IS A FINISHED MEASUREMENT. Ageing it would be the
// surface inventing time nobody spent (8.2.20).
func TestASettledCardsClockStandsStill(t *testing.T) {
	settled := &fakeSource{scopes: map[string]Scope{
		HomeScopeID: {ID: HomeScopeID, Title: "aforge", Rows: []Row{
			{ID: "home", Kind: RowSurface, Name: "aforge", Life: LifeSettled},
			{ID: "job-done", Kind: RowTask, Name: "perf-audit", Life: LifeSettled,
				Meta: Telemetry{Elapsed: 3 * time.Minute, HasElapsed: true}},
		}},
	}}
	model := New(settled)
	clock := blocks.NewClock(0)
	base := time.Unix(1700000000, 0)
	clock.Latch(base)
	model.SetClock(clock)

	view := NewView(nil)
	draw := func(at time.Time) string {
		clock.Latch(at)
		return strings.Join(view.Render(model, ModeRail, 40, 24), "\n")
	}
	first := draw(base)
	if later := draw(base.Add(time.Hour)); later != first {
		t.Fatalf("a settled card's clock ran for an hour it did not spend:\n%s\n%s", first, later)
	}
}

// NO CLOCK, NO DRIFT. A headless render — a golden, a test, a host that never
// handed a clock over — draws exactly what it always drew.
func TestARailWithNoClockDrawsWhatTheSnapshotSaid(t *testing.T) {
	model := New(scene())
	view := NewView(nil)
	first := strings.Join(view.Render(model, ModeRail, 40, 24), "\n")
	if !strings.Contains(first, "41m") {
		t.Fatalf("a clockless rail changed the snapshot's figure:\n%s", first)
	}
	if model.Drift() != 0 {
		t.Fatalf("a clockless model drifted by %v", model.Drift())
	}
}

// A REFRESH RESTARTS THE DRIFT. The rows it loads were measured in the snapshot
// it just read, so ageing them by the time since the PREVIOUS snapshot would
// double-count every interval.
func TestARefreshRestartsTheDrift(t *testing.T) {
	model := New(scene())
	clock := blocks.NewClock(0)
	base := time.Unix(1700000000, 0)
	clock.Latch(base)
	model.SetClock(clock)

	clock.Latch(base.Add(90 * time.Second))
	if model.Drift() != 90*time.Second {
		t.Fatalf("drift is %v, want 90s", model.Drift())
	}
	model.Refresh()
	if model.Drift() != 0 {
		t.Fatalf("a fresh snapshot is already %v old", model.Drift())
	}
}
