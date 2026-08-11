package rail

import (
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

// 5.9's anatomy, pinned. Colourless so the assertion is about layout.
func TestCardAnatomy(t *testing.T) {
	m := New(scene())
	m.Select(1) // wisp-parity, focused, so the card expands in place
	got := plainView().Render(m, ModeRail, 28, 24)
	want := []string{
		" ● aforge",
		"────────────────────────────",
		" ◐ wisp-parity            › ",
		"   reworking NavCtx after t…",
		"   K3 · ▁ · $8.65 · 41m     ",
		"   ●◐◐⚑○  1/5               ",
		"   ◐ H2          $0.37 · 28m",
		"   ◐ NavCtx2      $0.12 · 5m",
		" ? data-clean             ›",
		"   needs a key for the vend…",
		"   Q3 · $0.44 · 6m",
		" ✓ perf-audit",
		"   wrote the report and sto…",
		"   K3 · $2.10 · 52m",
	}
	assertLines(t, got, want)
}

// The task scope of 5.15's second wireframe: scope header, orchestrator, the
// hairline at the room boundary, then the DAG with its waits-on structure.
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
		" ✓ XhrSyn                12m",
		" ◐ H2                    28m",
		" ◐ T3Infra               21m",
		" ⚑ KeyCutter                ",
		"   waits on H2              ",
		"   ◐ NavCtx2      $0.37 · 5m",
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

// 5.21: step dots map 1:1 to steps, and fall back to the gauge form rather than
// lying about how many steps there are.
func TestStepDotsFallBackToTheGaugeWhenTheyDoNotFit(t *testing.T) {
	steps := make([]Step, 30)
	for i := range steps {
		if i < 9 {
			steps[i].Life = LifeSettled
		}
	}
	row := Row{Kind: RowTask, Name: "big", Life: LifeWorking, Steps: steps}
	m := New(oneRow(row))
	m.Select(1)
	v := plainView()
	narrow := strings.Join(v.Render(m, ModeRail, 28, 12), "\n")
	if strings.Count(narrow, tokens.GlyphStepDone) > 1 {
		t.Fatalf("thirty dots were drawn in 28 columns:\n%s", narrow)
	}
	if !strings.Contains(narrow, "9/30") {
		t.Fatalf("the progress numbers are missing:\n%s", narrow)
	}
	wide := dotsRow(v.Render(m, ModeList, 90, 12))
	if strings.Count(wide, tokens.GlyphStepDone) != 9 {
		t.Fatalf("wide dots = %d, want 9: %q", strings.Count(wide, tokens.GlyphStepDone), wide)
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
			Steps:    make([]Step, 40),
			Workers:  make([]Row, 20),
			Meta: Telemetry{
				Model: strings.Repeat("model", 6), Effort: "high", Boosted: true,
				Cost: 12345.678, HasCost: true,
				ContextUsed: 1 << 40, ContextWindow: 1,
				Elapsed: 400 * 24 * time.Hour, Estimate: time.Second, HasElapsed: true,
				Workers: 1 << 30, HasWorkers: true,
			}},
		{ID: "2", Kind: RowWorker, Depth: 4, Name: "deep", Life: LifeCancelled},
		{ID: "3", Kind: RowStep, Name: "atomic", Life: LifePaused,
			Meta: Telemetry{Atomic: true, Cost: -1, HasCost: true}},
	}
	return &fakeSource{scopes: map[string]Scope{
		HomeScopeID: {ID: HomeScopeID, Title: "", Rows: rows},
	}}
}

// dotsRow finds the step-dot line, which is the only row carrying a progress
// fraction.
func dotsRow(lines []string) string {
	for _, l := range lines {
		if strings.Contains(l, "/") {
			return l
		}
	}
	return ""
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
