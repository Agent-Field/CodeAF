package palette

import (
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

// TestMatchedCharactersBrightenOneTier is the fzf highlight (5.18, 5.21) and
// the 5.22 checklist item about tiers, checked on the spans themselves so the
// assertion cannot be satisfied by an accident of escape-sequence ordering.
func TestMatchedCharactersBrightenOneTier(t *testing.T) {
	var l lineBuf
	l.reset(40)
	pos := appendPositions(nil, "cancel", "cnl")
	l.addMatched("cancel", tokens.TextSecondary, pos, 0)

	var bright, base []string
	for _, s := range l.spans {
		switch s.tok {
		case tokens.TextPrimary:
			bright = append(bright, s.text)
		case tokens.TextSecondary:
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
		if s.text == ellipsis && s.tok != tokens.TextSecondary {
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
