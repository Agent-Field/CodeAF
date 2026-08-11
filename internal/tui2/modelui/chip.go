package modelui

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The chip. One line of 5.23's grammar and nothing else:
//
//	⟨role word⟩ ⟨model word⟩ ⟨ctx gauge⟩
//
// with two riders the same section and 5.10 put on the same chip: the boost
// glyph, because boost is a transient escalation of the work binding and not a
// second concept, and the reasoning effort, because it rides the model.
//
// Full form, widest first:
//
//	hands claude-sonnet-4 ⇡ · high ▄
//	voice gpt-oss-120b ▄
//	claude-sonnet-4 ▄            (Role empty: the row already said which)
//
// THE DROP LADDER. A chip is embedded in the tightest lines the product has —
// a status line, a room header, a rail worker row four columns from the edge —
// so it must shrink rather than overflow, and it must shrink in a stated order
// rather than in whatever order the code happens to concatenate:
//
//	1. the effort suffix — provenance, and display-only anyway (12.3.5)
//	2. the role word — the chip's context is usually already on the line
//	3. the gauge — one cell of health, and its absence says nothing false
//	4. the boost glyph
//	5. the model word truncates, and is the last thing standing
//
// Boost outranks the gauge deliberately: the gauge is a number that will be
// there again next frame, and the boost mark is the only place on the whole
// screen that says the work role is currently running somewhere other than
// where the settings say it is.
//
// WIDTH STABILITY. Render(w) never returns more than w cells, for every w and
// every field combination — width_test.go sweeps it. The gauge is exactly one
// cell whenever it is drawn (both [tokens.Gauge] and [tokens.GlyphMissing] are
// single-cell by the glyph table's own measured gate), so a row of chips whose
// numbers are moving does not shuffle its columns.

// Span is one painted run of a chip: text and the token that colours it.
//
// It exists so a consumer that is already assembling a line — the status line's
// cell fitter, the rail's card painter — can fold a chip's cells into its own
// buffer instead of re-parsing the escape sequences [Chip.Render] would have
// produced. The chip renders; the line belongs to whoever drew it.
type Span struct {
	Text  string
	Token tokens.Token
}

// Chip is one model chip's facts plus the painter it uses.
//
// The zero value renders nothing at all rather than a placeholder: a chip with
// no model and no role is a chip about nothing, and drawing "—" for it would
// claim there is a binding here whose value is unknown.
type Chip struct {
	// Role is the role this chip governs. Empty (or unrecognized) draws no role
	// word, which is what a row whose first column already names the role wants.
	Role store.ModelRole

	// Model is the resolved model slug — what the next provider call will
	// actually use, pin and scope ladder already applied. It is rendered as a
	// model word, never as the slug ([ModelWord]).
	Model string

	// Boosted says the work-role binding is in its transient escalation. It
	// draws [tokens.GlyphBoosted]; see the amendment in the glyph table for why
	// that is ⇡ and not 5.10's ⚡ (⚡ measures two cells and fails 5.17's own
	// width law).
	Boosted bool

	// Used and Window are the context reading behind the gauge: tokens spent
	// against the model's window, the denominator being catalog.ContextLength
	// for [Chip.Model]. A zero Window with a non-zero Used draws
	// [tokens.GlyphMissing] — the window is unknown, which is not the same as
	// empty (10.2.8). Both zero draws no gauge cell at all: nothing is known,
	// and an unknown that takes a column is a column spent on nothing.
	Used, Window int64

	// Styler paints. A nil Styler renders plain text, which is what the golden
	// harness, a NoColor terminal and every test here want.
	Styler *tokens.Styler

	// subject records that this chip stands for a binding even though it prints
	// no role word — set by [Chip.WithoutRole]. It is why a role row with
	// nothing bound draws "—" instead of drawing nothing: the row IS about a
	// binding, and a blank cell where a model goes reads as "no opinion" rather
	// than as "unbound".
	subject bool
}

// WithoutRole returns a copy that prints no role word, for a row whose own
// first column already names the role — a settings row, a rail worker row.
// The chip still knows it is about a binding, so a missing model renders as the
// missing mark rather than as nothing at all.
func (c Chip) WithoutRole() Chip {
	c.Role, c.subject = "", true
	return c
}

// partGap is the separator between the chip's parts. One space, never a
// separator glyph: the chip is three words, and a telemetry separator between
// them would make it read as a stat strip rather than as a sentence.
const partGap = " "

// Text is the chip's full form as plain text, with nothing dropped. It is what
// [Chip.Width] measures and what a caller wanting the chip's own sentence for a
// log line or a receipt should ask for.
func (c Chip) Text() string {
	var b strings.Builder
	for _, s := range c.Spans(0) {
		b.WriteString(s.Text)
	}
	return b.String()
}

// Width is how many cells the full form wants.
func (c Chip) Width() int { return blocks.Width(c.Text()) }

// MinWidth is the narrowest useful chip: the model word alone, with everything
// else already dropped. A consumer budgeting columns can ask this to decide
// whether to draw the chip at all — below it the chip would be an ellipsis, and
// an ellipsis where a model name goes teaches nothing.
func (c Chip) MinWidth() int { return blocks.Width(c.modelWord()) }

// Spans is the chip's cells, already reduced to fit width. A width of zero or
// less means "no budget" and yields the full form.
//
// The returned slice is freshly allocated; a caller rendering per frame should
// prefer [Chip.AppendSpans] with its own reused buffer.
func (c Chip) Spans(width int) []Span { return c.AppendSpans(nil, width) }

// AppendSpans appends the chip's cells to dst and returns it.
func (c Chip) AppendSpans(dst []Span, width int) []Span {
	role, model, boost, effort, gauge := c.fit(width)
	add := func(text string, tok tokens.Token) {
		if text == "" {
			return
		}
		if len(dst) > 0 {
			dst = append(dst, Span{Text: partGap, Token: tokens.TextTertiary})
		}
		dst = append(dst, Span{Text: text, Token: tok})
	}
	add(role, tokens.TextTertiary)
	// The model word is the chip's identity, so it sits one tier above the
	// telemetry around it. A consumer that wants the whole chip to recede dims
	// its pane, which is the axis dimming lives on (8.1.6).
	add(model, tokens.TextSecondary)
	// Boost is cyan — 5.16's "alive", which is what a transient escalation is.
	// It is deliberately NOT amber: amber means a human is needed, and a boosted
	// role needs nobody.
	add(boost, tokens.Cyan)
	add(effort, tokens.TextTertiary)
	add(gauge, tokens.ContextToken(c.Used, c.Window))
	return dst
}

// Render paints the chip into at most width cells.
func (c Chip) Render(width int) string {
	if width <= 0 {
		return ""
	}
	var line lineBuf
	var buf strings.Builder
	line.reset(width)
	profile, focus := tokens.NoColor, tokens.FocusNormal
	if c.Styler != nil {
		profile, focus = c.Styler.Profile(), c.Styler.Focus()
	}
	for _, s := range c.Spans(width) {
		line.add(s.Text, s.Token)
	}
	return line.emit(&buf, profile, focus, width, false, tokens.Band)
}

// fit resolves the five parts against a width budget, walking the drop ladder.
// A width of zero or less is "no budget" and returns the full form.
func (c Chip) fit(width int) (role, model, boost, effort, gauge string) {
	role, model = c.roleWord(), c.modelWord()
	if c.Boosted {
		boost = tokens.GlyphBoosted
	}
	if word := Effort(c.Model); word != "" {
		effort = tokens.GlyphSeparator + partGap + word
	}
	gauge = c.gaugeCell()
	if width <= 0 {
		return role, model, boost, effort, gauge
	}
	for partsWidth(role, model, boost, effort, gauge) > width {
		switch {
		case effort != "":
			effort = ""
		case role != "":
			role = ""
		case gauge != "":
			gauge = ""
		case boost != "":
			boost = ""
		default:
			// Only the model word is left, and it is still too wide. Truncate
			// leaves the ellipsis mark; a word that cannot hold even that comes
			// back empty, and an empty chip is a chip that honestly did not fit.
			model = blocks.Truncate(model, width)
			if blocks.Width(model) > width {
				model = ""
			}
			return role, model, boost, effort, gauge
		}
	}
	return role, model, boost, effort, gauge
}

// partsWidth is the assembled width: every present part plus one gap between
// each adjacent pair.
func partsWidth(parts ...string) int {
	total, seen := 0, 0
	for _, part := range parts {
		if part == "" {
			continue
		}
		if seen > 0 {
			total += len(partGap)
		}
		total += blocks.Width(part)
		seen++
	}
	return total
}

func (c Chip) roleWord() string { return c.Role.Word() }

// modelWord is the chip's model cell. An empty slug draws [tokens.GlyphMissing]
// ONLY when the chip is otherwise about something — a role, a boost, a context
// reading. A chip that knows nothing draws nothing.
func (c Chip) modelWord() string {
	if word := ModelWord(c.Model); word != "" {
		return word
	}
	if c.Role.Valid() || c.subject || c.Boosted || c.Window > 0 || c.Used > 0 {
		return tokens.GlyphMissing
	}
	return ""
}

// gaugeCell is one cell of context, or nothing.
func (c Chip) gaugeCell() string {
	switch {
	case c.Window > 0:
		used := c.Used
		if used < 0 {
			used = 0
		}
		return tokens.Gauge(float64(used) / float64(c.Window))
	case c.Used > 0:
		// Tokens were spent against a window nobody can size. That is a known
		// unknown and it says so; inventing a fraction here would render "plenty
		// of room" for a model whose window is simply not in the catalog.
		return tokens.GlyphMissing
	}
	return ""
}
