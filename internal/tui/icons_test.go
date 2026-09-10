package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// TestTheTierIsDecidedOnceFromTheSettingAndTheTerminal states the arithmetic in
// icons.go as a table rather than as a paragraph: the Display row wins when a
// person has said something, and what the terminal can be trusted with answers
// when they have not.
func TestTheTierIsDecidedOnceFromTheSettingAndTheTerminal(t *testing.T) {
	for _, c := range []struct {
		name string
		mode string
		auto tokens.GlyphSet
		want tokens.GlyphSet
	}{
		{"a fresh window has detected nothing and draws the floor", "", tokens.Plain, tokens.Plain},
		{"auto takes the terminal's answer", config.IconsAuto, tokens.NerdFont, tokens.NerdFont},
		{"auto takes a veto too", config.IconsAuto, tokens.Plain, tokens.Plain},
		{"rich overrides a veto, because no terminal reports its font", config.IconsPlain + "x", tokens.Plain, tokens.Plain},
		{"rich is the person's own say", config.IconsRich, tokens.Plain, tokens.NerdFont},
		{"plain is the person's own say", config.IconsPlain, tokens.NerdFont, tokens.Plain},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := &Model{iconMode: c.mode, iconAuto: c.auto}
			if got := m.iconSet(); got != c.want {
				t.Fatalf("iconSet() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestTheDoorDetectsAndAnEmbedderKeepsTheFloor pins the seam a test suite rests
// on. [tokens.DetectGlyphSet] turns the nerd-font tier ON for any terminal it
// cannot rule out, so a window that detected at construction would draw every
// mark as a private-use codepoint in every test in this package — invisible in
// the frames they log and impossible to write down in an assertion. The
// detection therefore happens at [RunWithCommander] and nowhere else.
func TestTheDoorDetectsAndAnEmbedderKeepsTheFloor(t *testing.T) {
	m := New(&fakeBackend{}, "floor")
	if m.icons != tokens.Plain {
		t.Fatalf("a window nobody opened a terminal for draws %v, want the plain floor", m.icons)
	}
	m.detectIcons(func(name string) string {
		if name == "TERM" {
			return "xterm-256color"
		}
		return ""
	})
	if m.icons != tokens.NerdFont {
		t.Fatalf("an ordinary terminal got %v, want the nerd-font tier", m.icons)
	}
	m.detectIcons(func(name string) string {
		if name == "TERM" {
			return "linux"
		}
		return ""
	})
	if m.icons != tokens.Plain {
		t.Fatalf("the Linux console got %v, and it cannot draw private use at all", m.icons)
	}
}

// TestEveryMarkOnOneFrameComesFromOneRepertoire is the bug the law exists for,
// asserted rather than described: a person with a patched font saw proper icons
// beside their tool calls and bare geometric shapes beside their tasks, because
// one of the two was a literal. Flip this window to the nerd-font tier and NO
// mark it draws may still be the plain floor's.
func TestEveryMarkOnOneFrameComesFromOneRepertoire(t *testing.T) {
	m := New(&fakeBackend{}, "one-repertoire")
	m.iconAuto = tokens.NerdFont
	m.settleIcons()

	drawn := []string{
		ansi.Strip(briefItemGlyph(m.icons, store.BriefDone)),
		ansi.Strip(briefItemGlyph(m.icons, store.BriefFailure)),
		ansi.Strip(briefItemGlyph(m.icons, store.BriefCancelled)),
		ansi.Strip(briefItemGlyph(m.icons, briefWaitingKind)),
		ansi.Strip(cardPartGlyph(m.icons, store.Done)),
		ansi.Strip(cardPartGlyph(m.icons, store.Running)),
		ansi.Strip(cardPartGlyph(m.icons, store.Pending)),
		ansi.Strip(historyStatusGlyph(m.icons, store.Failed)),
		ansi.Strip(m.cardGlyph(jobCard{State: cardQuestion})),
		ansi.Strip(m.cardGlyph(jobCard{State: cardCompiling})),
		m.icon(mediaSlotOf(t, "clip.mp4")),
		m.icon(mediaSlotOf(t, "brief.pdf")),
		m.icon(attachmentSlot("/tmp/chart.png")),
		activityLegend(m.icons),
	}
	// The floor's bytes for every slot the tier actually MOVES. A geometry slot
	// — the composer's chevron, the separators, the tree corners — has no icon
	// by design and correctly draws the same character in every tier, so a gate
	// that failed on those would be a gate that refuses the design.
	floor := map[rune]string{}
	for _, b := range tokens.Vocabulary() {
		if b.NerdFont == "" {
			continue
		}
		for _, r := range b.Plain {
			floor[r] = b.Name
		}
	}
	for _, cell := range drawn {
		for _, r := range cell {
			if name, stranded := floor[r]; stranded {
				t.Errorf("%q still draws %U (%s's plain byte) on a patched terminal: a mark "+
					"spelled as a literal draws the plain floor forever "+
					"(docs/design/icons/DESIGN.md)", cell, r, name)
			}
		}
	}
}

// TestAThreadAlreadyDrawnIsRedrawnInTheNewTier is the cache half of the same
// promise, driven through the real render. A settled message is kept as the
// BYTES it rendered to and only the pane's width is outside its key, so a person
// who changed the Display row would otherwise watch the new tier arrive on the
// live tail while the conversation above it stayed in the old one.
func TestAThreadAlreadyDrawnIsRedrawnInTheNewTier(t *testing.T) {
	model, _, _ := newSettingsModel(t)
	seq := int64(7)
	model.messages = []store.Message{{
		Seq: seq, Role: store.RoleAgent, Body: "While you were away.",
		Brief: &store.Brief{Items: []store.BriefItem{
			{Kind: store.BriefDone, Body: "The market report landed."},
		}},
	}}
	model.briefExpanded[seq] = true
	model.refreshChat()
	if drawn := ansi.Strip(model.renderMessages()); !strings.Contains(drawn, tokens.GlyphSettled) {
		t.Fatalf("the plain floor's settled mark is not on the first draw:\n%s", drawn)
	}

	row, ok := model.settingsRegistry.Row(config.KeyIcons)
	if !ok {
		t.Fatal("no step-icons row")
	}
	if cmd := model.applySetting(row, config.IconsRich); cmd != nil {
		t.Fatalf("applying the row returned a command: %v", cmd)
	}
	drawn := ansi.Strip(model.renderMessages())
	if strings.Contains(drawn, tokens.GlyphSettled) {
		t.Fatalf("the conversation kept the old tier's settled mark after the row moved:\n%s", drawn)
	}
	if !strings.Contains(drawn, tokens.NerdFont.Glyph(tokens.GSettled)) {
		t.Fatalf("the conversation did not take the new tier's settled mark:\n%s", drawn)
	}
}

func mediaSlotOf(t *testing.T, path string) tokens.GlyphID {
	t.Helper()
	slot, ok := mediaSlot(path)
	if !ok {
		t.Fatalf("no chip kind for %q", path)
	}
	return slot
}

// TestChangingTheDisplayRowMovesEveryMarkAtOnce is the other half of the same
// promise, driven through the real settings row. The row's own hint says "The
// change applies immediately", and nothing else in this window re-reads it.
func TestChangingTheDisplayRowMovesEveryMarkAtOnce(t *testing.T) {
	model, _, _ := newSettingsModel(t)
	if model.iconMode != config.IconsAuto {
		t.Fatalf("a fresh profile reads %q, want %q", model.iconMode, config.IconsAuto)
	}
	row, ok := model.settingsRegistry.Row(config.KeyIcons)
	if !ok {
		t.Fatal("no step-icons row")
	}
	model.helpLines = []string{"a legend drawn in the old tier"}
	if cmd := model.applySetting(row, config.IconsRich); cmd != nil {
		t.Fatalf("applying the row returned a command: %v", cmd)
	}
	if model.icons != tokens.NerdFont {
		t.Fatalf("the surface draws %v after the row said rich", model.icons)
	}
	if model.helpLines != nil {
		t.Fatal("the help legend kept its old tier's lines")
	}
	if got := ansi.Strip(cardPartGlyph(model.icons, store.Done)); strings.Contains(got, tokens.GlyphSettled) {
		t.Fatalf("a settled part still draws the plain floor's %q", got)
	}
	if cmd := model.applySetting(row, config.IconsPlain); cmd != nil {
		t.Fatalf("applying the row returned a command: %v", cmd)
	}
	if model.icons != tokens.Plain {
		t.Fatalf("the surface draws %v after the row said plain", model.icons)
	}
	if got := ansi.Strip(cardPartGlyph(model.icons, store.Done)); got != tokens.GlyphSettled {
		t.Fatalf("a settled part draws %q, want the plain floor's %q", got, tokens.GlyphSettled)
	}
}
