package tokens

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
)

// The consumer seam.
//
// internal/tui2/blocks declares the smallest interface it needs — a Paint that
// takes text, a state and a hue — and deliberately never imports this package,
// so the two could be built in parallel. This file closes the seam from THIS
// side, so the assembly wave writes
//
//	transcript.SetStyler(tokens.NewStyler(profile, tokens.FocusNormal))
//
// and not an adapter type in a third package. The import edge runs
// tokens → blocks, which is the direction that keeps blocks a leaf; blocks does
// not gain a dependency by being depended on.
//
// The two enums are declared on both sides with matching ordinals (see [Hue]),
// so the conversion below is a numeric cast the compiler folds away.
// styler_test.go pins the correspondence, and a compile-time assertion pins the
// interface, so both halves of the seam fail loudly rather than silently.

var (
	_ blocks.Styler         = (*Styler)(nil)
	_ blocks.IdentityStyler = (*Styler)(nil)
)

// Styler paints text with this package's palette for one terminal profile and
// one pane focus. It is immutable after construction and safe to share across
// goroutines: every field is read-only and every escape sequence it writes was
// computed once, at package initialization, by the palette table.
//
// A Styler is a value worth caching per pane, not per frame — construct one
// when the profile or the pane's focus changes, and hold it.
type Styler struct {
	profile Profile
	focus   Focus
	glyphs  GlyphSet
	// enabled is the profile check hoisted out of the hot path: under NoColor
	// every Paint is the identity function and must not touch the palette at
	// all.
	enabled bool
	// upgrading is the same hoist for the glyph tier, and it is a SEPARATE
	// question from enabled: the tier is orthogonal to colour, so a NoColor
	// terminal with a patched font still draws icons and a truecolor terminal
	// on the plain tier still draws the 5.17 floor.
	upgrading bool
}

// NewStyler returns a Styler for a terminal profile and a pane focus, on the
// plain glyph tier. Its signature is unchanged and always will be: every
// construction site written before the tier existed keeps compiling and keeps
// rendering exactly what it rendered, which is the whole contract of an
// enhancement ladder.
//
// An out-of-range profile or focus is clamped rather than panicking. This is
// the one place in the package that forgives bad input, and the reason is that
// its argument usually comes from a terminal probe: a render must not die
// because an emulator lied about itself, and the honest degradation for
// "capability unknown" is no colour.
func NewStyler(p Profile, f Focus) *Styler {
	return NewStylerIn(p, f, Plain)
}

// NewStylerIn is [NewStyler] with the third axis: the glyph repertoire tier
// (12.7). It composes with profile and focus and changes neither — the tier
// decides which character lands in a cell, never how many cells a line takes,
// which token tints it, or where a segment sits.
func NewStylerIn(p Profile, f Focus, g GlyphSet) *Styler {
	if p >= profileCount {
		p = NoColor
	}
	if f >= focusCount {
		f = FocusNormal
	}
	if g >= glyphSetCount {
		g = Plain
	}
	return &Styler{
		profile:   p,
		focus:     f,
		glyphs:    g,
		enabled:   p != NoColor,
		upgrading: g != Plain,
	}
}

// Profile reports the terminal profile this Styler paints for.
func (s *Styler) Profile() Profile { return s.profile }

// Focus reports the pane focus this Styler paints for.
func (s *Styler) Focus() Focus { return s.focus }

// GlyphSet reports the glyph repertoire tier this Styler draws in. A nil
// Styler reports [Plain] — see [Styler.Glyph] for why that is safe here and
// not for painting.
func (s *Styler) GlyphSet() GlyphSet {
	if s == nil {
		return Plain
	}
	return s.glyphs
}

// WithFocus returns a Styler identical to s but painting at the given focus.
// Dimming is a property of the pane (8.3), so a compositor that has just lost
// focus swaps one small value rather than re-resolving every row.
func (s *Styler) WithFocus(f Focus) *Styler {
	if f >= focusCount {
		f = FocusNormal
	}
	if f == s.focus {
		return s
	}
	return NewStylerIn(s.profile, f, s.glyphs)
}

// WithGlyphSet returns a Styler identical to s but drawing in the given tier.
// It mirrors [Styler.WithFocus], and it is what a settings sheet's live preview
// renders its two sample lines through.
func (s *Styler) WithGlyphSet(g GlyphSet) *Styler {
	if g >= glyphSetCount {
		g = Plain
	}
	if g == s.glyphs {
		return s
	}
	return NewStylerIn(s.profile, s.focus, g)
}

// Glyph is the EXPLICIT door to the vocabulary: it resolves a slot in this
// Styler's tier. Every consumer can use it, and the six slots whose plain side
// is ASCII — "?" needs-human, "=" paused, "$" spend, "/" folder and the diff
// signs — have no other door, because those characters are things a user types
// and the automatic path must never rewrite one (12.7 D.3).
//
// A nil Styler resolves the plain glyph rather than panicking, and that
// forgiveness is deliberate where [Styler.Paint]'s is not. A nil Styler is a
// real state in this tree — a block built before a profile was chosen holds one
// — and a consumer adopting the tier replaces a package-level CONSTANT with
// this call. If the call could panic where the constant could not, adoption
// would be a one-token edit that changes when a renderer crashes, and every
// consumer would have to grow a nil check for a lookup that reads no colour and
// makes no decision. Painting is different: it must produce escape bytes, and
// there is no honest answer to "which colour" without a profile.
func (s *Styler) Glyph(id GlyphID) string {
	if s == nil {
		return Plain.Glyph(id)
	}
	return s.glyphs.Glyph(id)
}

// PaintGlyph resolves a slot in this tier and paints it on the state and hue
// axes, which is the whole grammar of a glyph cell in one call.
func (s *Styler) PaintGlyph(id GlyphID, state blocks.State, hue blocks.Hue) string {
	return s.Paint(s.glyphs.Glyph(id), state, hue)
}

// Paint implements [blocks.Styler]: it wraps text in the escape sequences for
// the token that state and hue resolve to.
//
// It obeys blocks' contract that painting must not change printable width —
// only escape sequences are added, never a printable byte. Empty text is
// returned untouched, because an escape pair around nothing is bytes the
// terminal parses for no reason, and a zero-width painted string would make
// every "is this row empty" check downstream answer wrong.
func (s *Styler) Paint(text string, state blocks.State, hue blocks.Hue) string {
	return s.paint(text, ResolveToken(Hue(hue), State(state)))
}

// PaintIdentity implements [blocks.IdentityStyler]: it paints text in the
// stable pastel of a task seed, resolved through the same 8-hue wheel the rail
// assigns from (5.16). The state axis is accepted and ignored for the hue —
// identity is identity whether the task is running or settled, which is the
// whole point of a stable accent — but a chrome-state caller gets the tertiary
// grey, because chrome is structure and structure has no identity.
func (s *Styler) PaintIdentity(text string, seed uint64, state blocks.State) string {
	if State(state) == StateChrome {
		return s.paint(text, TextTertiary)
	}
	return s.paint(text, Identity(int(seed%IdentityCount)))
}

// PaintToken is the direct door for a renderer that already knows its token —
// the grey-ramp tiers, a band, a promoted cell — and does not need the hue and
// state axes to resolve one for it.
func (s *Styler) PaintToken(text string, t Token) string {
	if t >= tokenCount {
		return text
	}
	return s.paint(text, t)
}

// PaintOn draws text in fg over the background bg. It is the selection band of
// 5.16 ("selection is a background band, not a foreground colour"): the row
// keeps its tier colour and gains a raised ground.
//
// Under a profile whose [Profile.SelectionStyle] is [SelectionReverse] there is
// no trustworthy raised background, so the band becomes SGR 7 — the honest
// fallback, defined relative to whatever the terminal's own colours are.
// [Legal] governs which pairs may be drawn at all, and the contrast gate has
// measured every one of them.
//
// Under [SelectionMarker] it returns the text unpainted, and that is the whole
// of what this function may do: painting must not change printable width (the
// blocks contract), and a marker is a CELL. A renderer whose profile answers
// [SelectionMarker] therefore has to draw [GlyphAccentRail] in its gutter — ask
// [Profile.SelectionStyle] before drawing a row, not this function after.
func (s *Styler) PaintOn(text string, fg, bg Token) string {
	text = s.upgrade(text)
	if !s.enabled || text == "" {
		return text
	}
	if s.profile.SelectionStyle() == SelectionReverse {
		return Reverse(s.profile) + s.paint(text, fg) + Reset(s.profile)
	}
	if fg >= tokenCount || bg >= tokenCount {
		return text
	}
	var b strings.Builder
	fgSeq := fg.Fg(s.profile, s.focus)
	bgSeq := bg.Bg(s.profile, s.focus)
	b.Grow(len(fgSeq) + len(bgSeq) + len(text) + len(sgrResetAll))
	b.WriteString(bgSeq)
	b.WriteString(fgSeq)
	b.WriteString(text)
	b.WriteString(sgrResetAll)
	return b.String()
}

// Token resolves the hue and state axes to a token without painting, for a
// caller that wants the colour value itself (a lipgloss style, a swatch).
func (s *Styler) Token(state blocks.State, hue blocks.Hue) Token {
	return ResolveToken(Hue(hue), State(state))
}

// sgrResetAll is the reset written after a painted span. Foreground and
// background are cleared separately rather than with SGR 0, so painting a cell
// never silently clears a caller's bold or underline on the same row.
const sgrResetAll = "\x1b[39;49m"

// upgrade is the glyph tier's whole automatic path (12.7 D.2, rule (a)): a
// painted cell that is exactly one rune, and is an auto-upgradable slot's plain
// glyph, becomes this tier's icon. Everything else passes through untouched —
// never a substring rewrite, never an ASCII slot, never a lead rune inside a
// longer string.
//
// The warrant is the header grammar: blocks paints the glyph cell as its own
// span, so it arrives here as a whole one-rune string. That is how four
// consumer packages written before the tier existed get the tier without an
// edit. What it costs on the hot path is one branch under the plain tier, and
// under the nerd-font tier one rune decode plus a binary search over two dozen
// entries — a bounded rune check, not a map probe per cell.
func (s *Styler) upgrade(text string) string {
	if !s.upgrading {
		return text
	}
	return s.glyphs.Upgrade(text)
}

// paint is the one place a painted string is assembled. It is a single
// allocation — the Builder is sized exactly — and the escape sequences it
// concatenates were computed once by buildTable, so no frame ever formats one.
func (s *Styler) paint(text string, t Token) string {
	text = s.upgrade(text)
	if !s.enabled || text == "" {
		return text
	}
	seq := t.Fg(s.profile, s.focus)
	if seq == "" {
		return text
	}
	const reset = "\x1b[39m"
	var b strings.Builder
	b.Grow(len(seq) + len(text) + len(reset))
	b.WriteString(seq)
	b.WriteString(text)
	b.WriteString(reset)
	return b.String()
}
