package settings

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Provenance is 5.20's honesty duty made visible: a surface that shows a value
// owes the reader where the value came from, and the three answers are a
// built-in default, something saved in this profile, and an environment pin.

func TestProvenanceMovesFromDefaultToSavedOnTheWrite(t *testing.T) {
	s := newSheet(t, func(o *Options) { o.Debounce = -1 })
	r := s.gotoRow(t, config.KeyDailyBudget)

	if kind, text := s.provenance(r); kind != sourceDefault || text != "default" {
		t.Fatalf("an untouched row reads %v/%q, want the built-in default", kind, text)
	}
	if frame := s.Render(80, 20); !strings.Contains(frame, "built-in default") {
		t.Fatalf("the detail block does not say where the value came from:\n%s", frame)
	}

	s.press(namedKey(tea.KeyEnter))
	s.press(ctrlKey('u'))
	s.typeText("31")
	s.press(namedKey(tea.KeyEnter))

	r = s.gotoRow(t, config.KeyDailyBudget)
	if kind, _ := s.provenance(r); kind != sourceSaved {
		t.Fatalf("provenance is %v after a write, want saved", kind)
	}
	frame := s.Render(80, 20)
	if !strings.Contains(frame, "saved") {
		t.Fatalf("the row does not say it is saved now:\n%s", frame)
	}

	// A fresh sheet over the same profile reads the same provenance from the
	// registry rather than from this session's memory of having written it.
	second := New(Options{Registry: config.NewSettings(config.SettingsOptions{ProfileDir: s.dir})})
	row := second.rows[0]
	for _, candidate := range second.rows {
		if candidate.setting.Key == config.KeyDailyBudget {
			row = candidate
		}
	}
	if kind, _ := second.provenance(row); kind != sourceSaved {
		t.Fatalf("a fresh sheet reads %v, want saved", kind)
	}
}

// An environment pin takes the row away from the user, and the row says so:
// the variable is named, the value renders in amber (the hue for "a human is
// implicated"), enter refuses in words, and nothing is staged.
func TestEnvironmentPinnedRowIsReadOnlyAndNamesThePin(t *testing.T) {
	t.Setenv("AFORGE_DAILY_BUDGET", "9")

	s := newSheet(t)
	r := s.gotoRow(t, config.KeyDailyBudget)

	kind, name := s.provenance(r)
	if kind != sourcePinned || name != "AFORGE_DAILY_BUDGET" {
		t.Fatalf("provenance = %v/%q, want the pin named", kind, name)
	}
	if s.editable(r) {
		t.Fatal("a pinned row must not be editable")
	}

	s.press(namedKey(tea.KeyEnter))
	if s.editing || s.picking {
		t.Fatal("a pinned row opened an editor")
	}
	if len(s.pending) != 0 {
		t.Fatalf("a pinned row staged %v", s.pending)
	}
	refusal := s.failed[r.setting.Key]
	if !strings.Contains(refusal, "AFORGE_DAILY_BUDGET") {
		t.Fatalf("refusal %q does not name the variable that owns the row", refusal)
	}

	frame := s.Render(90, 24)
	if !strings.Contains(frame, "AFORGE_DAILY_BUDGET") {
		t.Fatalf("the pin is not on screen:\n%s", frame)
	}
	if !strings.Contains(frame, "read-only") {
		t.Fatalf("the row does not say it is read-only:\n%s", frame)
	}
	if !strings.Contains(frame, "$9") {
		t.Fatalf("the row does not show the pinned value:\n%s", frame)
	}

	// The verbs are gone too — an action strip that offered "enter edit" on a
	// row enter cannot edit would be an affordance that lies (5.22 rule 5).
	if strings.Contains(frame, "enter edit") && strings.Contains(frame, "read-only") {
		line := ""
		for _, candidate := range strings.Split(frame, "\n") {
			if strings.Contains(candidate, "read-only") {
				line = candidate
			}
		}
		if strings.Contains(line, "enter edit") {
			t.Fatalf("the pinned row still offers an edit verb: %q", line)
		}
	}
}

// The pin renders in amber and nothing else on the sheet is coloured by state,
// because amber has exactly one meaning (5.16).
func TestPinnedValueIsTheOnlyHueOnAnOtherwiseGreyRow(t *testing.T) {
	t.Setenv("AFORGE_ATTRIBUTION", "0")
	s := newSheet(t, func(o *Options) {
		o.Styler = tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	})
	r := s.gotoRow(t, config.KeyAttribution)

	line := s.rowLine(r, 0, false, 20, 80)
	if !strings.Contains(line, tokens.Amber.Fg(tokens.TrueColor, tokens.FocusNormal)) {
		t.Fatalf("a pinned value is not painted amber: %q", line)
	}
	for _, forbidden := range []tokens.Token{tokens.Green, tokens.Coral, tokens.Cyan} {
		if strings.Contains(line, forbidden.Fg(tokens.TrueColor, tokens.FocusNormal)) {
			t.Fatalf("row line carries %v, which has no meaning here: %q", forbidden, line)
		}
	}
}

// A row whose store this surface cannot inspect renders the missing-data glyph
// rather than guessing "default" (10.2.8).
func TestUninspectableStoreRendersMissingRatherThanAGuess(t *testing.T) {
	s := newSheet(t)
	r := s.gotoRow(t, config.KeySplitPct)

	if kind, _ := s.provenance(r); kind != sourceUnknown {
		t.Fatalf("provenance = %v, want unknown for a row kept beside the graph", kind)
	}
	chip, _ := s.chip(r)
	if chip != tokens.GlyphMissing {
		t.Fatalf("chip = %q, want the missing-data glyph", chip)
	}

	// Once THIS session has written it, the guess is no longer a guess.
	s.Model.debounce = -1
	s.press(namedKey(tea.KeyEnter))
	s.press(ctrlKey('u'))
	s.typeText("55")
	s.press(namedKey(tea.KeyEnter))

	r = s.gotoRow(t, config.KeySplitPct)
	if kind, _ := s.provenance(r); kind != sourceSaved {
		t.Fatalf("provenance = %v after this session wrote the row, want saved", kind)
	}
	if s.split != 55 {
		t.Fatalf("the divider seam took %d, want 55", s.split)
	}
}

// Live preview (8.2.19): a row that changes how the surface renders shows one
// sample line, and the sample moves with the value.
func TestPreviewShowsOneSampleLineThatFollowsTheValue(t *testing.T) {
	s := newSheet(t, func(o *Options) { o.Debounce = -1 })
	r := s.gotoRow(t, config.KeyLinearMode)

	before := s.preview(r, 60)
	if before == "" {
		t.Fatal("the linear-mode row has no preview")
	}
	if strings.Contains(before, "\n") {
		t.Fatalf("a preview is one line, got %q", before)
	}
	if !strings.Contains(s.Render(80, 20), before) {
		t.Fatal("the preview is not on screen")
	}

	s.press(typeRune(' '))
	after := s.preview(r, 60)
	if after == before {
		t.Fatalf("the preview did not follow the value: still %q", after)
	}

	// A row with nothing to preview shows nothing — a preview of a dollar
	// amount would be decoration.
	if sample := s.preview(s.gotoRow(t, config.KeyDailyBudget), 60); sample != "" {
		t.Fatalf("a money row grew a preview: %q", sample)
	}
}

func TestSplitPreviewTracksTheDivider(t *testing.T) {
	if narrow, wide := splitPreview("30%"), splitPreview("85%"); narrow == wide {
		t.Fatalf("the divider preview does not move: %q", narrow)
	}
	if got := splitPreview("not a number"); got != "" {
		t.Fatalf("an unreadable value previewed %q, want nothing", got)
	}
}

// The gate mechanism (8.2.19), proved against an injected dependency, because
// internal/config has none of its own yet.
func TestGatedRowAppearsTheMomentItsParentFlips(t *testing.T) {
	restore := gates
	t.Cleanup(func() { gates = restore })
	gates = map[string]gate{
		config.KeyTenureAfter: func(value func(string) (string, bool)) bool {
			on, _ := value(config.KeyProposeSkills)
			return on == "on"
		},
	}

	s := newSheet(t) // debounce is an hour: the gate must not wait for the write
	if !s.listed(config.KeyTenureAfter) {
		t.Fatal("the parent is on by default, so the gated row should be listed")
	}

	s.gotoRow(t, config.KeyProposeSkills)
	s.press(typeRune(' ')) // parent off
	if s.listed(config.KeyTenureAfter) {
		t.Fatal("the gated row is still listed after its parent went off")
	}
	if len(s.pending) == 0 {
		t.Fatal("the gate must read the live value, not wait for the write")
	}

	s.gotoRow(t, config.KeyProposeSkills)
	s.press(typeRune(' ')) // parent back on
	if !s.listed(config.KeyTenureAfter) {
		t.Fatal("the gated row did not come back when its parent flipped on")
	}
}

// Linear mode (10.1.5) keeps the selection legible without a background fill.
func TestLinearModeMarksTheSelectionWithoutABand(t *testing.T) {
	styler := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	band := tokens.Band.Bg(tokens.TrueColor, tokens.FocusNormal)

	normal := newSheet(t, func(o *Options) { o.Styler = styler })
	if frame := normal.Render(80, 20); !strings.Contains(frame, band) {
		t.Fatal("the ordinary sheet does not draw the selection band")
	}

	linear := newSheet(t, func(o *Options) { o.Styler = styler; o.Linear = true })
	frame := linear.Render(80, 20)
	if strings.Contains(frame, band) {
		t.Fatalf("linear mode still paints a background fill:\n%q", frame)
	}
	if !strings.Contains(frame, tokens.GlyphAccentRail) {
		t.Fatal("linear mode dropped the fill without keeping the marker")
	}
}
