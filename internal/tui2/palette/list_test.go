package palette

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// demoCatalog is the shared fixture: two live rooms (one of them home, one
// carrying open questions), one settled room that must land under `history`,
// the registry's own actions for a room scope, and two settings rows one of
// which the environment holds.
func demoCatalog() Catalog {
	return Catalog{
		Scope: registry.ScopeThread,
		Title: "wisp-parity",
		Rooms: []Room{
			{ID: rail.HomeScopeID, Title: "aforge", Attention: rail.AttnQueued, Summary: "the main room", Key: "1"},
			{ID: "t-wisp", Title: "wisp-parity", Seed: "t-wisp", Attention: rail.AttnQuestion,
				Questions: 2, Summary: "parity sweep, 3 workers", Key: "2"},
			{ID: "t-perf", Title: "perf-audit", Seed: "t-perf", Attention: rail.AttnSettled,
				Summary: "settled 20m ago"},
		},
		Settings: []SettingRow{
			{Key: "model.work", Label: "work model", Hint: "the model that does the work", Value: "K3"},
			{Key: "budget.daily", Label: "daily budget", Hint: "today's dollar limit", Value: "$20.00", Pinned: "AFORGE_BUDGET"},
		},
	}
}

func newTestPalette(t *testing.T, c Catalog) *Palette {
	t.Helper()
	p := New(Options{})
	p.SetCatalog(c)
	return p
}

// rowsOf renders and returns the body lines, without the header and blank.
func rowsOf(p *Palette, width, height int) []string {
	frame := p.Render(width, height)
	if frame == "" {
		return nil
	}
	lines := strings.Split(frame, "\n")
	if len(lines) <= p.bodyTop {
		return nil
	}
	return lines[p.bodyTop:]
}

func TestSectionsRenderInDocumentedOrder(t *testing.T) {
	p := newTestPalette(t, demoCatalog())
	lines := rowsOf(p, 100, 60)

	want := []string{"rooms", "actions", "history", "settings"}
	var seen []string
	for _, line := range lines {
		for _, w := range want {
			if strings.HasPrefix(line, w) {
				seen = append(seen, w)
			}
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("saw section headers %v, want %v\n%s", seen, want, strings.Join(lines, "\n"))
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("section order %v, want %v", seen, want)
		}
	}
}

// TestSettledRoomsLandUnderHistory is 5.18's split, applied to the palette:
// live rooms are direct targets, settled ones are reference.
func TestSettledRoomsLandUnderHistory(t *testing.T) {
	rows := buildRows(nil, demoCatalog())
	for _, r := range rows {
		switch r.verb {
		case "aforge", "wisp-parity":
			if r.sec != sectionRooms {
				t.Errorf("%s is live but landed in section %d", r.verb, r.sec)
			}
		case "perf-audit":
			if r.sec != sectionHistory {
				t.Errorf("perf-audit is settled but landed in section %d", r.sec)
			}
		}
	}
}

func TestActionsAreFilteredByScope(t *testing.T) {
	for _, scope := range []registry.Scope{registry.ScopeThread, registry.ScopeNode, registry.ScopeTalk} {
		c := demoCatalog()
		c.Scope = scope
		rows := buildRows(nil, c)
		n := 0
		for _, r := range rows {
			if r.sec != sectionActions {
				continue
			}
			n++
			id := r.result.(RunEntry).ID
			e, ok := registry.ByID(id)
			if !ok {
				t.Fatalf("row %q carries an id no registry entry has", id)
			}
			if !e.Scope.Has(scope) {
				t.Errorf("entry %s is out of scope %d but rendered", id, scope)
			}
		}
		if want := len(registry.ForScope(scope)); n != want {
			t.Errorf("scope %d rendered %d actions, registry has %d", scope, n, want)
		}
	}
}

// TestZeroScopeShowsEverything: a caller that has not said where it is has not
// said "nowhere", and a palette that silently showed no actions would be the
// discoverability failure 5.22 exists to remove.
func TestZeroScopeShowsEverything(t *testing.T) {
	c := demoCatalog()
	c.Scope = 0
	n := 0
	for _, r := range buildRows(nil, c) {
		if r.sec == sectionActions {
			n++
		}
	}
	if n != registry.Len() {
		t.Errorf("zero scope rendered %d actions, catalog has %d", n, registry.Len())
	}
}

func TestExplicitActionsAreUsedVerbatim(t *testing.T) {
	e, ok := registry.ByID("slash.help")
	if !ok {
		t.Fatal("slash.help must exist")
	}
	c := demoCatalog()
	c.Scope = registry.ScopeNode
	c.Actions = []Action{{Entry: e}}
	n := 0
	for _, r := range buildRows(nil, c) {
		if r.sec == sectionActions {
			n++
		}
	}
	if n != 1 {
		t.Errorf("explicit Actions rendered %d rows, want 1", n)
	}
}

// TestDisabledRowsNameTheirReason is 5.20 rule 3 in one assertion: the action
// is still listed, the reason is on screen, no accelerator is advertised, and
// enter would do nothing.
func TestDisabledRowsNameTheirReason(t *testing.T) {
	const reason = "settled — ask aforge"
	c := demoCatalog()
	c.Reason = func(id string) string {
		if id == "slash.model" {
			return reason
		}
		return ""
	}
	p := newTestPalette(t, c)
	typeText(p, "model")

	var line string
	for _, l := range rowsOf(p, 100, 60) {
		if strings.Contains(l, reason) {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("the disabled row is missing its reason entirely:\n%s", strings.Join(rowsOf(p, 100, 60), "\n"))
	}
	if !strings.Contains(line, "choose model") {
		t.Errorf("the disabled row dropped its verb: %q", line)
	}
	e, _ := registry.ByID("slash.model")
	if strings.Contains(line, "/"+e.Slash) {
		t.Errorf("a disabled affordance advertised its accelerator: %q", line)
	}

	// And it cannot be chosen.
	var rows []row
	rows = buildRows(rows, c)
	for i := range rows {
		if rows[i].disabled != "" && rows[i].enabled() {
			t.Errorf("row %q is disabled and choosable at once", rows[i].verb)
		}
	}
}

// TestPinnedSettingIsDisabledWithItsVariable: a setting the environment holds
// is one this surface cannot change, and offering it anyway is the affordance
// lying (5.22 rule 5).
func TestPinnedSettingIsDisabledWithItsVariable(t *testing.T) {
	for _, r := range buildRows(nil, demoCatalog()) {
		if r.sec != sectionSettings || r.verb != "daily budget" {
			continue
		}
		if r.disabled == "" {
			t.Fatal("a pinned setting rendered as available")
		}
		if !strings.Contains(r.disabled, "AFORGE_BUDGET") {
			t.Errorf("the reason does not name the variable: %q", r.disabled)
		}
		return
	}
	t.Fatal("the pinned setting row never rendered")
}

// TestWidthSweep is the production bar: every line fits, nothing panics, at
// every width from one column to well past a rail-plus-transcript frame, and
// at every height a dialog can be given.
func TestWidthSweep(t *testing.T) {
	c := demoCatalog()
	c.Rooms = append(c.Rooms, Room{
		ID:      "t-long",
		Title:   "a task word nobody would ever choose but which exists anyway",
		Seed:    "t-long",
		Summary: "日本語 mixed with a very long summary line that keeps going well past any sane column budget",
	})
	c.Settings = append(c.Settings, SettingRow{Key: "x", Label: "café résumé", Hint: "accented", Value: "—"})

	styler := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	for _, query := range []string{"", "a", "can", "café", "日本", "zzzz"} {
		for w := 1; w <= 110; w++ {
			for _, h := range []int{1, 2, 3, 5, 12, 40} {
				p := New(Options{Styler: styler})
				p.SetCatalog(c)
				typeText(p, query)
				frame := p.Render(w, h)
				lines := strings.Split(frame, "\n")
				if frame == "" {
					lines = nil
				}
				if len(lines) > h {
					t.Fatalf("query %q w=%d h=%d: %d lines", query, w, h, len(lines))
				}
				for i, line := range lines {
					if got := blocks.Width(line); got > w {
						t.Fatalf("query %q w=%d h=%d line %d is %d cells: %q", query, w, h, i, got, line)
					}
				}

				capPane := NewCapability(Options{Styler: styler})
				capPane.SetCatalog(c)
				capFrame := capPane.Render(w, h)
				capLines := strings.Split(capFrame, "\n")
				if capFrame == "" {
					capLines = nil
				}
				if len(capLines) > h {
					t.Fatalf("capability w=%d h=%d: %d lines", w, h, len(capLines))
				}
				for i, line := range capLines {
					if got := blocks.Width(line); got > w {
						t.Fatalf("capability w=%d h=%d line %d is %d cells: %q", w, h, i, got, line)
					}
				}
			}
		}
	}
}

func TestZeroSizesRenderNothing(t *testing.T) {
	p := newTestPalette(t, demoCatalog())
	for _, size := range [][2]int{{0, 10}, {10, 0}, {-4, -4}, {0, 0}} {
		if got := p.Render(size[0], size[1]); got != "" {
			t.Errorf("Render(%d,%d) = %q, want empty", size[0], size[1], got)
		}
	}
	c := NewCapability(Options{})
	c.SetCatalog(demoCatalog())
	if got := c.Render(0, 0); got != "" {
		t.Errorf("capability Render(0,0) = %q", got)
	}
}

// TestAcceleratorOutranksDescription: the palette exists to teach the
// accelerator, so the description is what width pressure takes first.
func TestAcceleratorOutranksDescription(t *testing.T) {
	c := Catalog{Scope: registry.ScopeThread}
	p := newTestPalette(t, c)
	typeText(p, "settings")

	e, _ := registry.ByID("slash.settings")
	wide := strings.Join(rowsOf(p, 100, 20), "\n")
	if !strings.Contains(wide, e.Description) || !strings.Contains(wide, e.Key) {
		t.Fatalf("a wide palette must show both the description and the key:\n%s", wide)
	}
	narrow := strings.Join(rowsOf(p, 30, 20), "\n")
	if !strings.Contains(narrow, e.Key) {
		t.Errorf("a narrow palette dropped the accelerator it exists to teach:\n%s", narrow)
	}
	if strings.Contains(narrow, e.Description) {
		t.Errorf("a 30-column palette kept a full description:\n%s", narrow)
	}
}

// TestColumnsDoNotDanceWhileTyping is 5.21's "width-stable everything" applied
// to the one surface that redraws on every keystroke: the columns are measured
// from the WHOLE catalog, not from the filtered set, so narrowing the list can
// never shift the text under the user's eye.
func TestColumnsDoNotDanceWhileTyping(t *testing.T) {
	const desc = "open every setting in one place"
	p := newTestPalette(t, demoCatalog())

	columnOf := func() int {
		for _, line := range rowsOf(p, 90, 60) {
			if at := strings.Index(line, desc); at >= 0 {
				return at
			}
		}
		t.Fatalf("the settings action never rendered")
		return -1
	}
	before := columnOf()
	for _, q := range []string{"o", "op", "ope", "open s"} {
		p.Reset()
		typeText(p, q)
		if got := columnOf(); got != before {
			t.Errorf("query %q moved the description column from %d to %d", q, before, got)
		}
	}
}

// TestMatchedCharactersStandOneTierAboveTheRest is the fzf highlight (5.18,
// 5.21), checked on the spans themselves so the assertion cannot be satisfied
// by an accident of escape-sequence ordering.
//
// The relationship is what is pinned, not the direction: the matched letters
// read one tier above the letters around them. They get there by the surround
// DROPPING rather than by the match rising, because the columns now carry the
// hierarchy and a verb already sits at the top tier — see [lineBuf.addMatched].
func TestMatchedCharactersStandOneTierAboveTheRest(t *testing.T) {
	var l lineBuf
	l.reset(40)
	pos := appendPositions(nil, "cancel", "cnl")
	l.addMatched("cancel", tokens.TextSecondary, pos, 0)

	var bright, base []string
	for _, s := range l.spans {
		switch s.tok {
		case tokens.TextSecondary:
			bright = append(bright, s.text)
		case tokens.TextTertiary:
			base = append(base, s.text)
		default:
			t.Fatalf("unexpected token %v on span %q", s.tok, s.text)
		}
	}
	if strings.Join(bright, "") != "cnl" {
		t.Errorf("highlighted %q, want the matched letters c, n and l", bright)
	}
	if strings.Join(base, "") != "ace" {
		t.Errorf("left %q at the base tier, want the unmatched letters", base)
	}
	// Painting may add escape sequences and never printable cells.
	if got := blocks.Width(strings.Join(joinSpans(l.spans), "")); got != len("cancel") {
		t.Errorf("highlighting changed the printable width to %d", got)
	}
}

func joinSpans(spans []span) []string {
	out := make([]string, len(spans))
	for i := range spans {
		out[i] = spans[i].text
	}
	return out
}

// TestHighlightNeverPaintsIntoTheEllipsis: a truncated row highlights only
// letters still on screen.
func TestHighlightNeverPaintsIntoTheEllipsis(t *testing.T) {
	var l lineBuf
	l.reset(4)
	pos := appendPositions(nil, "cancel everything", "cg")
	l.addMatched("cancel everything", tokens.TextSecondary, pos, 0)
	for _, s := range l.spans {
		if s.text == ellipsis && s.tok != tokens.TextTertiary {
			t.Errorf("the cut mark was highlighted")
		}
	}
	if got := blocks.Width(strings.Join(joinSpans(l.spans), "")); got > 4 {
		t.Errorf("truncated line is %d cells, want at most 4", got)
	}
}

// TestSelectionIsAnIdentityTintedBand is 5.16: selection is a background band,
// and a room with an identity tints it.
func TestSelectionIsAnIdentityTintedBand(t *testing.T) {
	c := demoCatalog()
	rows := buildRows(nil, c)
	for _, r := range rows {
		switch r.verb {
		case "aforge":
			if r.band != tokens.Band {
				t.Errorf("home has no identity but drew a tinted band")
			}
		case "wisp-parity":
			if r.band == tokens.Band {
				t.Errorf("an identified room drew the untinted band")
			}
			if _, ok := tokens.IdentityIndex(r.band); !ok {
				t.Errorf("the band %v is not on the identity wheel", r.band)
			}
		}
	}

	p := New(Options{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)})
	p.SetCatalog(c)
	body := rowsOf(p, 80, 30)
	var selected string
	for _, line := range body {
		if strings.Contains(line, "aforge") {
			selected = line
			break
		}
	}
	if selected == "" {
		t.Fatal("the first room never rendered")
	}
	if !strings.Contains(selected, tokens.Band.Bg(tokens.TrueColor, tokens.FocusNormal)) {
		t.Errorf("the selected row carries no band background: %q", selected)
	}
}

// TestSelectionSurvivesACatalogRefresh: a task settling elsewhere must not move
// the row under the user's hand (7.2).
func TestSelectionSurvivesACatalogRefresh(t *testing.T) {
	c := demoCatalog()
	p := newTestPalette(t, c)
	p.Key(namedKey(tea.KeyDown))
	p.Key(namedKey(tea.KeyDown))
	before, ok := p.Selected()
	if !ok {
		t.Fatal("nothing selected")
	}

	// A new room appears at the top of the list.
	c.Rooms = append([]Room{{ID: "t-new", Title: "brand-new", Seed: "t-new", Attention: rail.AttnWorking}}, c.Rooms...)
	p.SetCatalog(c)

	after, ok := p.Selected()
	if !ok {
		t.Fatal("the selection vanished across a refresh")
	}
	if before != after {
		t.Errorf("selection moved from %v to %v across a refresh", before, after)
	}
}

// TestRoomGlyphCarriesItsMeaning: a room with a blocked human is amber, a
// working room is cyan, and a room with no semantic word spends the slot on
// its identity pastel.
func TestRoomGlyphCarriesItsMeaning(t *testing.T) {
	cases := []struct {
		room Room
		want tokens.Token
	}{
		{Room{ID: "a", Attention: rail.AttnQuestion, Seed: "a"}, tokens.Amber},
		{Room{ID: "b", Attention: rail.AttnWorking, Seed: "b"}, tokens.Cyan},
		{Room{ID: "c", Attention: rail.AttnSettled, Seed: "c"}, tokens.Green},
		{Room{ID: "d", Attention: rail.AttnFailed, Seed: "d"}, tokens.Coral},
		{Room{ID: "e", Attention: rail.AttnQueued, Seed: "e"}, tokens.IdentityFor("e")},
		{Room{ID: "", Attention: rail.AttnQueued}, tokens.TextPrimary},
	}
	for _, c := range cases {
		if got := roomGlyphToken(&c.room); got != c.want {
			t.Errorf("room %+v glyph token = %v, want %v", c.room, got, c.want)
		}
	}
}

// TestQuestionChipIsAmberAndOnlyWhenNeeded: 5.16 — amber only ever means a
// human is actually needed.
func TestQuestionChipIsAmberAndOnlyWhenNeeded(t *testing.T) {
	p := newTestPalette(t, demoCatalog())
	body := strings.Join(rowsOf(p, 90, 30), "\n")
	if !strings.Contains(body, chipText(2)) {
		t.Errorf("the room with two open questions has no count chip:\n%s", body)
	}
	if strings.Contains(body, chipText(0)) {
		t.Errorf("a chip was drawn for zero questions:\n%s", body)
	}
}

// TestModelProseCannotEscapeARow: room titles and summaries are model-written
// and must not be able to move the cursor or smuggle a second line in.
func TestModelProseCannotEscapeARow(t *testing.T) {
	c := Catalog{Rooms: []Room{{
		ID:      "evil",
		Title:   "wisp\x1b[2J\x1b[H",
		Summary: "line one\nline two\r\x1b]0;title\x07",
	}}}
	p := newTestPalette(t, c)
	frame := p.Render(80, 20)
	if strings.Contains(frame, "\x1b[2J") || strings.Contains(frame, "\x1b]0;") {
		t.Errorf("a control sequence survived into the frame: %q", frame)
	}
	for _, line := range strings.Split(frame, "\n") {
		if strings.ContainsAny(line, "\r\v\f") {
			t.Errorf("a row smuggled a line break: %q", line)
		}
	}
}

// -- the sheet's own paint (the aesthetics lane) ------------------------------

// styledPalette is the fixture for the paint tests: the same catalog, bound to a
// real profile, because everything below is about bytes a headless Styler never
// writes.
func styledPalette(t *testing.T, profile tokens.Profile) *Palette {
	t.Helper()
	p := New(Options{Styler: tokens.NewStyler(profile, tokens.FocusNormal)})
	p.SetCatalog(demoCatalog())
	return p
}

// TestEveryRowStandsOnTheSheetsGround is 12.11's owed half, closed here: the
// dialog's boundary was drawn around a panel that painted no ground, so the
// sheet and the transcript it floats over had the same floor — the terminal's —
// and two rooms with one floor read as one room (12.13's wall finding, on the
// other axis).
func TestEveryRowStandsOnTheSheetsGround(t *testing.T) {
	p := styledPalette(t, tokens.TrueColor)
	const width, height = 70, 20
	ground := tokens.Sheet.Bg(tokens.TrueColor, tokens.FocusNormal)
	band := tokens.Band.Bg(tokens.TrueColor, tokens.FocusNormal)
	lines := strings.Split(p.Render(width, height), "\n")
	if len(lines) != height {
		t.Fatalf("the sheet rendered %d of its %d rows; the rest show the room behind it", len(lines), height)
	}
	for i, line := range lines {
		if strings.Contains(line, band) || strings.Contains(line, tokens.Reverse(tokens.TrueColor)) {
			continue // the selected row carries the band instead
		}
		if !strings.Contains(line, ground) {
			t.Errorf("row %d has no ground under it: %q", i, line)
		}
		if got := blocks.Width(line); got != width {
			t.Errorf("row %d paints %d of %d cells; the rest is the room behind it", i, got, width)
		}
	}
}

// TestNoGroundBelow256: at 16 colours the only raised background is the
// terminal theme's bright black and PaintOn's fallback is reverse video, so a
// sheet drawn there would be a slab. The boundary survives as the hairline the
// chrome draws (internal/tui2/dialogchrome).
func TestNoGroundBelow256(t *testing.T) {
	for _, profile := range []tokens.Profile{tokens.NoColor, tokens.ANSI16} {
		p := styledPalette(t, profile)
		out := p.Render(60, 12)
		if strings.Contains(out, tokens.Sheet.Bg(profile, tokens.FocusNormal)) &&
			tokens.Sheet.Bg(profile, tokens.FocusNormal) != "" {
			t.Errorf("%v painted a ground it cannot draw honestly", profile)
		}
	}
}

// TestSelectionIsMarkedAtEveryProfile is 12.11.2's invariant, applied to the
// surface it was not applied to: at NoColor this list drew no band (there are no
// bytes to spend) and had no other carrier, so not one cell differed between the
// selected row and its neighbours.
func TestSelectionIsMarkedAtEveryProfile(t *testing.T) {
	for _, profile := range []tokens.Profile{tokens.NoColor, tokens.ANSI16, tokens.ANSI256, tokens.TrueColor} {
		p := styledPalette(t, profile)
		lines := rowsOf(p, 70, 30)
		marked := 0
		for _, line := range lines {
			if strings.Contains(line, tokens.GlyphAccentRail) {
				marked++
			}
		}
		if marked != 1 {
			t.Errorf("%v: %d rows carry the selection marker, want exactly 1", profile, marked)
		}
	}
}

// TestTheThreeColumnsAreThreeTiers is the correction this lane was opened for:
// a row is a title, the status line about it, and the key it answers to (5.13),
// and drawn at one tier they are a wall of grey.
func TestTheThreeColumnsAreThreeTiers(t *testing.T) {
	rows := buildRows(nil, demoCatalog())
	var live *row
	for i := range rows {
		if rows[i].sec == sectionActions && rows[i].disabled == "" {
			live = &rows[i]
			break
		}
	}
	if live == nil {
		t.Fatal("no live action row in the fixture")
	}
	verb, desc, accel := rowTokens(live, false)
	if verb != tokens.TextPrimary || desc != tokens.TextSecondary || accel != tokens.TextTertiary {
		t.Errorf("columns are %v/%v/%v, want primary/secondary/tertiary", verb, desc, accel)
	}
	// 5.16: "selection is a background band, not a foreground color — text
	// keeps its tier color". Only the accelerator moves, and only because
	// 5.22's checklist forbids an interactive chip from living permanently in
	// the dimmest tier.
	selVerb, selDesc, selAccel := rowTokens(live, true)
	if selVerb != verb || selDesc != desc {
		t.Errorf("selection changed the text tiers to %v/%v", selVerb, selDesc)
	}
	if selAccel != tokens.Promote(accel) {
		t.Errorf("the selected row's accelerator is %v, want one tier up", selAccel)
	}
}

// TestGroupsAreRuledAndSpaced: 5.13 allows exactly one mark at a boundary and
// names it — a hairline — and 12.11 read the same sentence to give the dialog
// its own two rules. A group inside the sheet is the smaller boundary of the
// same kind.
func TestGroupsAreRuledAndSpaced(t *testing.T) {
	p := newTestPalette(t, demoCatalog())
	lines := rowsOf(p, 80, 120)
	ruled := 0
	for i, line := range lines {
		if !strings.Contains(line, tokens.GlyphTreeDash) {
			continue
		}
		ruled++
		if i == 0 {
			continue // the first group opens the list and needs no gap above it
		}
		if strings.TrimSpace(lines[i-1]) != "" {
			t.Errorf("group header %q has no blank line above it", strings.TrimSpace(line))
		}
	}
	if ruled != 4 {
		t.Errorf("%d group headers carry a rule, want one per section (4)", ruled)
	}
}

// TestAClippedListSaysHowMuchIsHidden: 5.20's affordance rule cuts both ways.
// A `?` sheet showing thirteen of thirty verbs with no mark is not a short list,
// it is a list lying about its length — on the one surface built to answer
// "what can this room do" without the reader guessing.
func TestAClippedListSaysHowMuchIsHidden(t *testing.T) {
	p := newTestPalette(t, demoCatalog())
	total := p.Total()
	lines := rowsOf(p, 80, 8)
	last := strings.TrimSpace(lines[len(lines)-1])
	if !strings.HasSuffix(last, "more") {
		t.Fatalf("a clipped list ends with %q, want a count of what is hidden", last)
	}
	shown := 0
	for _, line := range lines[:len(lines)-1] {
		if text := strings.TrimSpace(line); text != "" &&
			!strings.Contains(line, tokens.GlyphTreeDash) {
			shown++
		}
	}
	if got := last; !strings.HasPrefix(got, strconv.Itoa(total-shown)) {
		t.Errorf("the cue says %q with %d rows on screen of %d", got, shown, total)
	}
	// A list that fits says nothing at all: a cue on a complete list is chrome
	// that answers no question.
	full := rowsOf(p, 80, 120)
	for _, line := range full {
		if strings.HasSuffix(strings.TrimSpace(line), "more") {
			t.Errorf("an unclipped list still claims rows are hidden: %q", line)
		}
	}
}

// TestNoColumnTakesMoreThanAThird: the verb column is measured against the
// CATALOG, so one long room title would otherwise set the column for every row
// and push every description into an ellipsis.
func TestNoColumnTakesMoreThanAThird(t *testing.T) {
	c := demoCatalog()
	c.Rooms = append(c.Rooms, Room{ID: "t-long", Title: strings.Repeat("verylongname", 3),
		Attention: rail.AttnQueued, Summary: "a room with an outlier for a name"})
	p := newTestPalette(t, c)
	for _, width := range []int{40, 60, 80, 100} {
		if got := p.list.layout(width).verbW; got > width/3 && got > verbColMin {
			t.Errorf("at width %d the verb column is %d cells, past the third", width, got)
		}
	}
}

// TestAReversedBandIsOneRun is the 16-colour defect a screenshot found: SGR 7
// swaps the colours in use, so the tier colours the row wrote inside it landed
// on its BACKGROUND and the selected row came out striped — one inverted block
// per span with the padding between them uninverted.
func TestAReversedBandIsOneRun(t *testing.T) {
	p := styledPalette(t, tokens.ANSI16)
	if tokens.ANSI16.SelectionStyle() != tokens.SelectionReverse {
		t.Skip("16 colours no longer reverses; this test guards that path")
	}
	var selected string
	for _, line := range rowsOf(p, 70, 30) {
		if strings.Contains(line, tokens.Reverse(tokens.ANSI16)) {
			selected = line
			break
		}
	}
	if selected == "" {
		t.Fatal("no row carries the reversed band")
	}
	for _, tok := range []tokens.Token{tokens.TextPrimary, tokens.TextSecondary, tokens.TextTertiary} {
		if strings.Contains(selected, tok.Fg(tokens.ANSI16, tokens.FocusNormal)) {
			t.Errorf("a %s foreground is written inside the reversed band: %q", tok, selected)
		}
	}
}
