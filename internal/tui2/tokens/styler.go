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
	// enabled is the profile check hoisted out of the hot path: under NoColor
	// every Paint is the identity function and must not touch the palette at
	// all.
	enabled bool
}

// NewStyler returns a Styler for a terminal profile and a pane focus.
//
// An out-of-range profile or focus is clamped rather than panicking. This is
// the one place in the package that forgives bad input, and the reason is that
// its argument usually comes from a terminal probe: a render must not die
// because an emulator lied about itself, and the honest degradation for
// "capability unknown" is no colour.
func NewStyler(p Profile, f Focus) *Styler {
	if p >= profileCount {
		p = NoColor
	}
	if f >= focusCount {
		f = FocusNormal
	}
	return &Styler{profile: p, focus: f, enabled: p != NoColor}
}

// Profile reports the terminal profile this Styler paints for.
func (s *Styler) Profile() Profile { return s.profile }

// Focus reports the pane focus this Styler paints for.
func (s *Styler) Focus() Focus { return s.focus }

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
	return NewStyler(s.profile, f)
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
func (s *Styler) PaintOn(text string, fg, bg Token) string {
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

// paint is the one place a painted string is assembled. It is a single
// allocation — the Builder is sized exactly — and the escape sequences it
// concatenates were computed once by buildTable, so no frame ever formats one.
func (s *Styler) paint(text string, t Token) string {
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
