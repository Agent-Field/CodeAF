package chat

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/composer"
	"github.com/Agent-Field/aforge-v2/internal/tui2/placeline"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The composer region: three parts, one rectangle.
//
// internal/tui2/composer owns the draft: the rune buffer, the caret, the send
// and newline chords the shell negotiated, the recall ring, bracketed paste,
// and the esc law's first half — a non-empty draft is stashed into the ring,
// never destroyed. This package owns the second half: an esc the composer did
// not consume arrives here as [composer.EscMsg], and only this side knows
// whether the room is streaming and therefore what that esc means (8.2.21).
//
// Above the draft sits internal/tui2/placeline (5.19): one dim row naming where
// this work lands on disk — the question a breadcrumb never answers and the pwd
// a terminal usually gives away for free. Below it sits the meta strip, which
// is 10.5.23's other home: THIS TURN's cost and context, and nothing about
// system health, which belongs to the footer under the region and never mixes.
//
// The three are assembled here rather than by the shell because the shell gives
// a pane its whole rectangle and reserves nothing inside it (pane.go's
// contract). A region that had to be told "you have three rows, one of them is
// mine" would be the shell reaching into a pane, which is exactly the seam the
// v2 layout was built to keep clean.

// composerPane is what the app binds into the composer region.
type composerPane interface {
	tui2.Pane
	tui2.PaneKeys
	tui2.PaneFocus
	// Draft is the text currently in the buffer. The footer's input-state axis
	// (10.5.26) is read off it, and nothing else consults it — the app never
	// reaches into the draft to change it.
	Draft() string
}

// composerOptions is the composer's own option struct, named here so the app
// reads as one package rather than two.
type composerOptions = composer.Options

// composerStack is the assembled region.
type composerStack struct {
	place *placeline.Model
	draft *composer.Model
	meta  *metaStrip
}

var (
	_ tui2.Pane      = (*composerStack)(nil)
	_ tui2.PaneKeys  = (*composerStack)(nil)
	_ tui2.PaneFocus = (*composerStack)(nil)
)

// newComposer builds the region the app binds.
func newComposer(opts composerOptions, place *placeline.Model, meta *metaStrip) composerPane {
	return &composerStack{place: place, draft: composer.New(opts), meta: meta}
}

// Key hands every keystroke to the draft. The place line and the meta strip are
// read-only chrome; neither has an affordance that takes the keyboard.
func (s *composerStack) Key(msg tea.KeyPressMsg) tea.Cmd { return s.draft.Key(msg) }

// Paste is offered only when the draft accepts one, which is how the app's
// paste door finds it (app.go asks the pane for the method rather than assuming
// it).
func (s *composerStack) Paste(msg tea.PasteMsg) tea.Cmd { return s.draft.Paste(msg) }

// Focus flows to both rows that render differently for it: the draft shows its
// caret, and the place line reveals the root's full, unabbreviated path (5.19).
func (s *composerStack) Focus(focused bool) {
	s.draft.Focus(focused)
	if s.place != nil {
		s.place.Focus(focused)
	}
}

// Draft is the current buffer.
func (s *composerStack) Draft() string { return s.draft.Value() }

// Render lays the three parts out from the outside in: the place line takes the
// top edge, the meta strip the bottom, and the draft absorbs whatever is left.
// A region too short for all three loses them in that order — the draft is the
// one part whose absence makes the surface unusable, so it is the last to go.
func (s *composerStack) Render(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	rows := make([]string, 0, height)
	body := height
	wantPlace := height >= 3 && s.place != nil
	wantMeta := height >= 2 && s.meta != nil
	if wantPlace {
		body--
	}
	if wantMeta {
		body--
	}
	if wantPlace {
		rows = append(rows, s.place.Render(width))
	}
	drafted := strings.Split(s.draft.Render(width, body), "\n")
	if len(drafted) == 1 && drafted[0] == "" {
		drafted = nil
	}
	for i := 0; i < body; i++ {
		if i < len(drafted) {
			rows = append(rows, drafted[i])
			continue
		}
		// The draft is shorter than its room. The blank rows are emitted rather
		// than skipped so the meta strip stays welded to the region's bottom
		// edge, one row above the footer, instead of drifting up under a short
		// draft and back down under a long one.
		rows = append(rows, "")
	}
	if wantMeta {
		rows = append(rows, s.meta.render(width))
	}
	return strings.Join(rows, "\n")
}

// -- the meta strip ----------------------------------------------------------

// metaIndent aligns the strip with the draft's text rather than with its
// prompt glyph, so the numbers sit under the words they are about.
const metaIndent = "  "

// metaStrip is the composer's own row of numbers: 10.5.23's cost-and-context
// home, held apart from the footer's health-and-questions home so the two
// never mix.
//
// It shortens rather than wraps, on the same mechanic the footer uses
// ([tokens.FitFooter]) and with money holding the highest priority — 5.9 is
// blunt about why: money "is the one number the user never forgives us for
// hiding", so it is the last cell to leave a narrowing terminal.
//
// The v2 engine seam (engine.go's Commander) carries no per-turn cost and no
// context window yet, so both cells render as the missing-data glyph. That is
// 8.2.20's law applied literally — "missing data renders —, never an estimate" —
// and it is the honest shape: a zero would be a claim that this turn was free.
type metaStrip struct {
	style *tokens.Styler

	// model is the word the answering model goes by.
	model string
	// live says a turn is being answered right now, which is the only time an
	// elapsed cell means anything.
	live bool
	// elapsed is how long the live turn has been running. It ages, which is
	// allowed here and nowhere in the committed transcript: 8.1.2's corollary
	// puts an ageing cell in live regions only, and this strip is one.
	elapsed time.Duration

	// cost and context are the two cells this seam cannot fill yet. The fields
	// exist so the day the engine journals them is a wiring change and not a
	// layout change.
	cost      float64
	haveCost  bool
	used      int64
	window    int64
	haveUsage bool
}

// render draws the strip at width.
func (m *metaStrip) render(width int) string {
	if m == nil || width <= 0 {
		return ""
	}
	type cell struct {
		id       string
		text     string
		token    tokens.Token
		priority int
	}
	cells := make([]cell, 0, 4)
	if model := modelWord(m.model); model != "" {
		cells = append(cells, cell{"model", model, tokens.TextTertiary, 60})
	}
	if m.live {
		cells = append(cells, cell{"elapsed", tokens.Elapsed(m.elapsed), tokens.TextTertiary, 70})
	}
	if m.haveCost {
		cells = append(cells, cell{"cost", tokens.Money(m.cost), tokens.Green, 100})
	} else {
		cells = append(cells, cell{"cost", "$" + tokens.GlyphMissing, tokens.TextTertiary, 100})
	}
	if m.haveUsage {
		cells = append(cells, cell{"ctx", tokens.Gauge(fraction(m.used, m.window)) + " " +
			tokens.Context(m.used, m.window), tokens.ContextToken(m.used, m.window), 80})
	} else {
		cells = append(cells, cell{"ctx", tokens.GlyphMissing + " ctx", tokens.TextTertiary, 80})
	}

	const sep = " " + tokens.GlyphSeparator + " "
	sepWidth := blocks.Width(sep)
	columns := make([]tokens.FooterColumn, len(cells))
	for i, c := range cells {
		w := blocks.Width(c.text)
		if i > 0 {
			w += sepWidth
		}
		columns[i] = tokens.FooterColumn{ID: c.id, MinWidth: w, Priority: c.priority}
	}
	kept := tokens.FitFooter(columns, width-len(metaIndent))
	keep := make(map[string]bool, len(kept))
	for _, c := range kept {
		keep[c.ID] = true
	}

	var out strings.Builder
	out.WriteString(metaIndent)
	written := 0
	for _, c := range cells {
		if !keep[c.id] {
			continue
		}
		if written > 0 {
			out.WriteString(m.paint(sep, tokens.TextTertiary))
		}
		out.WriteString(m.paint(c.text, c.token))
		written++
	}
	if written == 0 {
		return ""
	}
	return out.String()
}

func (m *metaStrip) paint(text string, token tokens.Token) string {
	if m.style == nil || text == "" {
		return text
	}
	return m.style.PaintToken(text, token)
}

// fraction is used/window, clamped, for the one-cell gauge.
func fraction(used, window int64) float64 {
	if window <= 0 || used <= 0 {
		return 0
	}
	return float64(used) / float64(window)
}
