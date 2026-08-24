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
	// bodyInk is [Styler.WithBodyInk]'s override, precomputed per focus: the
	// foreground sequence [TextPrimary] resolves to on THIS Styler, or "" in
	// both slots when the tier keeps the palette's own value. Empty is therefore
	// the whole of "no override", which is what every Styler built by
	// [NewStyler] and [NewStylerIn] is.
	bodyInk [focusCount]string
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
//
// It COPIES rather than reconstructing, which it did not have to do before
// [Styler.WithBodyInk] existed: a reconstruction goes through [NewStylerIn], and
// [NewStylerIn] knows nothing about an override, so a pane that lost focus would
// silently get the palette's own body tier back. Copying is exactly equivalent
// for everything that used to be re-derived — neither `enabled` nor `upgrading`
// depends on the focus.
func (s *Styler) WithFocus(f Focus) *Styler {
	if f >= focusCount {
		f = FocusNormal
	}
	if f == s.focus {
		return s
	}
	out := *s
	out.focus = f
	return &out
}

// WithGlyphSet returns a Styler identical to s but drawing in the given tier.
// It mirrors [Styler.WithFocus] — copy included, for the same reason — and it is
// what a settings sheet's live preview renders its two sample lines through.
func (s *Styler) WithGlyphSet(g GlyphSet) *Styler {
	if g >= glyphSetCount {
		g = Plain
	}
	if g == s.glyphs {
		return s
	}
	out := *s
	out.glyphs = g
	out.upgrading = g != Plain
	return &out
}

// WithBodyInk returns a Styler that paints the body tier — [TextPrimary] — in a
// STATED colour rather than in this palette's own.
//
// ── WHY THIS SEAM EXISTS: ONE SURFACE, ONE BODY WHITE ──
//
// internal/tui3 authors its own quieter palette and then renders a model's
// markdown through internal/tui2/prose, which is the only markdown renderer in
// the tree and deliberately resolves every colour on the row from this package
// (prose's own package comment says why it will not seat a second colour
// authority). The consequence was two whites on one screen: prose painted the
// reply's body at [TextPrimary] #E6E6F0 while everything tui3 drew around it
// wore tui3's own, dimmer ink. The brightest thing on the surface was therefore
// the thing a person reads most — about 14:1 against a dark terminal where the
// accent beside it sat at 9.6:1 — which is glare, and halation that makes the
// strokes read heavier than they are.
//
// The fix is not a second renderer and not a copy of the ramp. It is that the
// COLOUR AUTHORITY a caller hands prose may be asked to say the body tier in the
// caller's own voice. One Styler, one answer to "which white", and the ladder's
// shape untouched: an h1 is still the top of the grey ramp and still bold, an h3
// still steps down through [Demote], a fenced block still lands on the code ramp
// and an inline span still stands on the [Sheet]. What moves is the VALUE at the
// top of the ramp, so everything standing on it moves together — which is the
// point, because a tier the body alone left behind would simply relocate the
// glare onto the headings.
//
// A Styler built by [NewStyler] or [NewStylerIn] carries no override, so every
// construction site in this tree — the whole v2 surface included — keeps
// rendering exactly the bytes it rendered.
//
// ── WHAT IT COSTS PER PROFILE ──
//
// The override is a COLOUR, so it exists only where the profile has colours to
// spend. [TrueColor] takes it exactly. [ANSI256] takes its nearest cube-or-grey
// neighbour, computed here by the same [nearest256] this package's own table is
// built with — a caller whose palette rounded the same hex to a different index
// would be two whites again on the majority profile, and one resolver is what
// stops that. [ANSI16] and [NoColor] take NOTHING and fall back to the token:
// the sixteen are the user's own theme, so there is no honest form for an
// authored hex there (the wall [Token.UnderlineColor] meets from the other
// side), and a profile told to write no SGR at all writes none.
//
// The DIMMED variant is derived rather than asked for, by the one rule the whole
// palette is dimmed with ([DimTowardGround]). A caller states the colour it
// reads at; an unfocused pane then recedes by the same law every other token
// obeys, instead of staying at full strength because nobody thought about it.
//
// CONTRAST IS THE CALLER'S TO JUSTIFY. contrast_test.go gates [Pairings], and a
// colour that is not in the table is not in that walk — so the caller who states
// one owes it the check its own palette owes. internal/tui3 pins its body ink
// with a contrast law of its own for exactly this reason.
func (s *Styler) WithBodyInk(c Color) *Styler {
	out := *s
	out.bodyInk = [focusCount]string{
		inkSeq(s.profile, c),
		inkSeq(s.profile, Mix(c, groundBase, DimTowardGround)),
	}
	return &out
}

// Fg is the foreground sequence this Styler paints a token with, and it is THE
// ONE DOOR: every painter here and in internal/tui2/prose asks it rather than
// reaching past to [Token.Fg], so an override stated once is honoured everywhere
// a row is assembled rather than on whichever paths somebody remembered.
//
// A nil Styler and an out-of-range token both answer "", which is the same
// nothing [NoColor] answers: there is no colour to write, and no reason to panic
// on the way to writing none.
func (s *Styler) Fg(t Token) string {
	if s == nil || t >= tokenCount {
		return ""
	}
	if t == TextPrimary {
		if seq := s.bodyInk[s.focus]; seq != "" {
			return seq
		}
	}
	return t.Fg(s.profile, s.focus)
}

// inkSeq spells one authored colour as a foreground sequence for a profile, and
// answers "" where the profile has no honest form for an authored colour at all.
// See [Styler.WithBodyInk] for which profiles those are and why.
func inkSeq(p Profile, c Color) string {
	switch p {
	case TrueColor:
		return "\x1b[38;2;" + itoa(int(c.R)) + ";" + itoa(int(c.G)) + ";" + itoa(int(c.B)) + "m"
	case ANSI256:
		return "\x1b[38;5;" + itoa(int(nearest256(c))) + "m"
	}
	return ""
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
	// Through [Styler.Fg] rather than [Token.Fg], so a band drawn under the body
	// tier keeps the body tier this Styler actually paints.
	fgSeq := s.Fg(fg)
	bgSeq := bg.Bg(s.profile, s.focus)
	b.Grow(len(fgSeq) + len(bgSeq) + len(text) + len(sgrResetAll))
	b.WriteString(bgSeq)
	b.WriteString(fgSeq)
	b.WriteString(text)
	b.WriteString(sgrResetAll)
	return b.String()
}

// PaintRowOn lays an ALREADY-PAINTED row onto a raised ground.
//
// It is the door a card needs and [Styler.PaintOn] cannot be: PaintOn takes one
// span, and a card's row is a dozen spans a dozen renderers painted — a header
// grammar, a markdown pass, a fold hint — none of which knows it is standing on
// a plane. Asking each of them to thread a ground through would be threading a
// background into every renderer in the tree so that one block kind could have
// a floor.
//
// IT WORKS BECAUSE THE PACKAGE'S OWN RESET IS FOREGROUND-ONLY. [Styler.paint]
// closes a span with SGR 39, which clears the colour it set and nothing else, so
// a background armed before the row survives every span inside it. There is
// exactly one sequence in this package that does clear a background — the
// [sgrResetAll] PaintOn writes — and it is re-armed here rather than left to
// punch a hole in the plane: a code span inside a card's body is a real case,
// and a card whose ground stopped halfway along a row would be a rendering bug
// nobody could see the cause of. One known sequence, one repair, both stated.
//
// Printable width is untouched, which is the blocks contract this obeys like
// every other painter here. An empty row is returned as it came: a background
// around nothing is bytes for no reason, and a zero-width painted string makes
// every "is this row blank" check downstream answer wrong.
func (s *Styler) PaintRowOn(row string, ground Token) string {
	if !s.enabled || row == "" || ground >= tokenCount || !ground.IsSurface() {
		return row
	}
	bg := ground.Bg(s.profile, s.focus)
	if bg == "" {
		return row
	}
	if strings.Contains(row, sgrResetAll) {
		row = strings.ReplaceAll(row, sgrResetAll, sgrResetFg+bg)
	}
	var b strings.Builder
	b.Grow(len(bg) + len(row) + len(sgrResetBg))
	b.WriteString(bg)
	b.WriteString(row)
	b.WriteString(sgrResetBg)
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

// sgrResetFg and sgrResetBg are the two halves of [sgrResetAll], named because
// [Styler.PaintRowOn] needs to spend them separately.
const (
	sgrResetFg = "\x1b[39m"
	sgrResetBg = "\x1b[49m"
)

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
	seq := s.Fg(t)
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
