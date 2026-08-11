package chat

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/composer"
	"github.com/Agent-Field/aforge-v2/internal/tui2/placeline"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
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

	// mode reports what the composer is bound to right now (5.15). It is a
	// function rather than a field because the binding is the app's state and
	// this region renders it; a copy here would be a second truth that ages by
	// one keystroke.
	mode func() composerBind
	// hud draws the bounded live summary above the draft (8.2.8). It returns
	// the rows it wants and never more than it is offered; nil means this frame
	// has a rail and does not need one.
	hud func(width, height int) []string
}

var (
	_ tui2.Pane      = (*composerStack)(nil)
	_ tui2.PaneKeys  = (*composerStack)(nil)
	_ tui2.PaneFocus = (*composerStack)(nil)
)

// newComposer builds the region the app binds.
func newComposer(opts composerOptions, place *placeline.Model, meta *metaStrip) *composerStack {
	return &composerStack{place: place, draft: composer.New(opts), meta: meta}
}

// Key hands every keystroke to the draft. The place line and the meta strip are
// read-only chrome; neither has an affordance that takes the keyboard.
//
// A DISABLED composer takes nothing. 5.15's one rule is that a settled row's
// composer is disabled, and a disabled composer that quietly accepted a draft
// nobody could send would be the affordance lying in the most frustrating way
// available: the reader types a paragraph and only then finds out.
func (s *composerStack) Key(msg tea.KeyPressMsg) tea.Cmd {
	if s.disabled() {
		return nil
	}
	return s.draft.Key(msg)
}

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
	// The bounded HUD (8.2.8) borrows from the DRAFT and from nothing else, and
	// never takes its last row. The place line names the ground and the meta
	// strip carries the money; a summary that displaced either would have spent
	// a permanent row on a transient fact. What it cannot fit, its fold line
	// accounts for — that is what "bounded" means here.
	var summary []string
	if s.hud != nil && body > 1 {
		if summary = s.hud(width, body-1); len(summary) > 0 {
			body -= len(summary)
		}
	}
	if wantPlace {
		rows = append(rows, s.place.Render(width))
	}
	rows = append(rows, summary...)
	drafted := strings.Split(s.renderDraft(width, body), "\n")
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

// -- what the composer is bound to (5.15) ------------------------------------

// steerPlaceholder is the steer line's idle hint (5.11: "steer — one-way").
const steerPlaceholder = "steer — one-way"

// chatPlaceholder mirrors internal/tui2/composer's own idle hint. It is
// duplicated rather than imported because that package deliberately does not
// make it configurable — see its comment — so this is the one string this file
// has to know to be able to replace it.
//
// REQUESTED SEAM: composer.Options should carry Prompt and Placeholder. A room
// whose composer means something different needs to SAY so, and the two glyphs
// 5.11 spends on exactly that distinction currently have to be swapped from
// outside.
const chatPlaceholder = "Type a message"

// bind reports what this region is bound to, defaulting to an ordinary chat so
// a stack built without an app behind it is still a composer.
func (s *composerStack) bind() composerBind {
	if s.mode == nil {
		return composerBind{mode: rail.ComposerChat}
	}
	return s.mode()
}

func (s *composerStack) disabled() bool { return s.bind().mode == rail.ComposerDisabled }

// renderDraft draws the draft in whatever mode the selected row bound.
//
// A DISABLED composer is not a greyed-out text field: it is a sentence saying
// why, drawn where the draft would have been (5.20 rule 3). Nothing takes the
// keyboard, no caret is shown, and no prompt glyph promises a send.
//
// A STEER composer is the ordinary draft with its two visible words changed —
// the prompt glyph and the idle hint — because everything else about typing one
// line of text is the same and forking the editor would fork its bugs. The
// glyph swap is width-preserving by construction: both prompts are single-cell
// glyphs from the same table (5.17), so the row's fit is unchanged.
func (s *composerStack) renderDraft(width, body int) string {
	if body <= 0 || width <= 0 {
		return ""
	}
	bind := s.bind()
	if bind.mode == rail.ComposerDisabled {
		note := bind.note
		if note == "" {
			note = "this work is settled — ask aforge about it"
		}
		line := blocks.Truncate(tokens.GlyphMissing+" "+note, width)
		if s.draft != nil {
			line = paintDim(s.styler(), line)
		}
		rows := make([]string, body)
		rows[0] = line
		return strings.Join(rows, "\n")
	}
	drawn := s.draft.Render(width, body)
	if bind.mode != rail.ComposerSteer {
		return drawn
	}
	head, rest, hasRest := strings.Cut(drawn, "\n")
	head = strings.Replace(head, tokens.GlyphPromptChat, tokens.GlyphPromptSteer, 1)
	if strings.TrimSpace(s.draft.Value()) == "" && strings.Contains(head, chatPlaceholder) {
		// The hint only changes when the replacement still fits. A row that was
		// already cut to width keeps the words it has rather than growing past
		// the edge to say something friendlier.
		if grown := strings.Replace(head, chatPlaceholder, steerPlaceholder, 1); blocks.Width(grown) <= width {
			head = grown
		}
	}
	if !hasRest {
		return head
	}
	return head + "\n" + rest
}

// styler is the region's painter, at the focus the draft is drawn with.
func (s *composerStack) styler() *tokens.Styler {
	if s.meta == nil {
		return nil
	}
	return s.meta.style
}

// paintDim draws chrome-tier text, or plain text when there is no profile.
func paintDim(style *tokens.Styler, text string) string {
	if style == nil {
		return text
	}
	return style.PaintToken(text, tokens.TextTertiary)
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
