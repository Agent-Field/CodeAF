package tokens

// The state axis (8.1.6) and the hue vocabulary (5.16), and the one function
// that composes them into a [Token].
//
// Three axes exist in this package and they are orthogonal:
//
//   - HUE — what a cell MEANS. Five words, no more.
//   - STATE — how LIVE the cell is: accent = live, plain = settled, dim =
//     chrome. 8.1.6's law, and the reason a long-lived row changes colour
//     rather than shape.
//   - FOCUS — whether the PANE owning the row has the user's attention. A
//     property of the pane, never of the datum; see [Focus].
//
// [ResolveToken] is the whole composition rule, in one place, so no renderer
// ever gets to invent a fourth axis.

// Hue is the five-word colour vocabulary of 5.16. Everything not carrying a hue
// is the three-tier grey ramp of 5.13.
//
// The ordinals are load-bearing: they match the [blocks.Hue] enum one for one so
// the [Styler] seam converts with a numeric cast and no lookup table.
// styler_test.go asserts the correspondence, so a drift on either side fails the
// build's tests rather than mis-colouring a frame.
type Hue uint8

const (
	// HueNone leaves the grey ramp alone. Zero value: an unhued cell is the
	// default, which is the point of a five-word vocabulary.
	HueNone Hue = iota
	// HueAttention (soft amber) means a human is needed: question badges,
	// waiting states, the context meter past its warn point.
	HueAttention
	// HueAlive (soft cyan) means working: live glyphs, the stream caret, the
	// thinking pulse.
	HueAlive
	// HueMoney (soft green) means money and success: cost figures, settled ✓.
	HueMoney
	// HueBroken (soft coral) means failed or cancelled.
	HueBroken
	// HueIdentity is the per-task pastel. It is NOT one colour: the actual
	// pastel comes from the task seed through [IdentityFor], which is why a
	// renderer holding a task id should call [Styler.PaintIdentity] rather than
	// paint HueIdentity. Resolving it without a seed yields the first wheel
	// entry, which is a deliberate, visible placeholder rather than a panic.
	HueIdentity
	hueCount
)

// String names the hue by its meaning, not by its colour — the word is the
// vocabulary; the pastel is only how the word is spoken.
func (h Hue) String() string {
	switch h {
	case HueNone:
		return "none"
	case HueAttention:
		return "attention"
	case HueAlive:
		return "alive"
	case HueMoney:
		return "money"
	case HueBroken:
		return "broken"
	case HueIdentity:
		return "identity"
	}
	return "invalid"
}

// State is the liveness axis of 8.1.6: accent = live, plain = settled,
// dim = chrome.
//
// Ordinals match [blocks.State]; see [Hue] for why that matters.
type State uint8

const (
	// StateSettled is plain text: the row is done moving. Zero value, so a
	// bare struct renders settled — the safe default, because a row wrongly
	// drawn settled is quiet, and a row wrongly drawn live is a lie about
	// liveness (8.1.6).
	StateSettled State = iota
	// StateLive is the accent: something is happening on this row now.
	StateLive
	// StateChrome is dim: separators, meta, fold lines, hints.
	StateChrome
	stateCount
)

// String names the state.
func (s State) String() string {
	switch s {
	case StateSettled:
		return "settled"
	case StateLive:
		return "live"
	case StateChrome:
		return "chrome"
	}
	return "invalid"
}

// String names the focus. contrast_test.go prints it, and the golden harness
// keys on it, so it is part of the API.
func (f Focus) String() string {
	switch f {
	case FocusNormal:
		return "normal"
	case FocusDimmed:
		return "dimmed"
	}
	return "invalid"
}

// ResolveToken composes the hue and state axes into the one token a cell draws
// with. It is the ONLY place the composition rule lives, and the rule is three
// lines long on purpose:
//
//  1. A HUE ALWAYS WINS. A cell that means something keeps meaning it after the
//     row settles — 5.16's green settled ✓ and coral ✕ are settled cells that
//     are still coloured. 8.1.6's "completion settles accent → plain text"
//     governs the row's PROSE, which carries no hue; the glyph beside it does.
//  2. With no hue, the state axis picks the tier: live is cyan, because cyan is
//     the word for alive (5.16) and "accent = live" has to resolve to some
//     accent; settled is the primary grey; chrome is the tertiary grey.
//  3. The SECONDARY tier is never reached from here. A status line is secondary
//     because of WHAT IT IS (5.13's type hierarchy), not because of how live it
//     is — a renderer names [TextSecondary] directly. Deriving it from the
//     state axis would make two different questions share one answer.
//
// It is a pure switch over two small enums: no allocation, no table lookup, and
// nothing for the compiler to fail to inline.
func ResolveToken(h Hue, s State) Token {
	switch h {
	case HueAttention:
		return Amber
	case HueAlive:
		return Cyan
	case HueMoney:
		return Green
	case HueBroken:
		return Coral
	case HueIdentity:
		// Without a seed there is no identity; the first wheel entry is a
		// visible placeholder. Callers holding a task id use IdentityFor.
		return Identity0
	}
	switch s {
	case StateLive:
		return Cyan
	case StateChrome:
		return TextTertiary
	default:
		return TextPrimary
	}
}

// Promote brightens a grey-ramp token one tier: tertiary → secondary →
// primary, and primary stays put. Hues are already the loudest thing on a row
// and are returned unchanged — brightening a pastel would break the contrast
// pairs the gate tested (5.16), and a louder amber does not mean a more urgent
// question.
//
// Two callers, and both are the same idea — a thing that normally recedes has
// briefly earned the eye:
//
//   - 5.22: a chrome control that is also a button brightens one tier on focus,
//     which is why no interactive control lives permanently at the chrome
//     contrast gate.
//   - 5.21's "elapsed that ages": see [ElapsedToken].
func Promote(t Token) Token {
	switch t {
	case TextTertiary:
		return TextSecondary
	case TextSecondary:
		return TextPrimary
	}
	return t
}

// Demote is [Promote]'s inverse for the grey ramp: primary → secondary →
// tertiary, tertiary stays put, hues unchanged. It exists for the one move
// 5.15 needs — pushing chrome down a tier while a scope is entered, so the
// room the user is in outranks the room they came from — and it is deliberately
// NOT how an unfocused pane recedes. That is [FocusDimmed], which dims every
// tier by the same rule instead of collapsing the hierarchy into it.
func Demote(t Token) Token {
	switch t {
	case TextPrimary:
		return TextSecondary
	case TextSecondary:
		return TextTertiary
	}
	return t
}

// Cut marks (12.5.2, the truncation law).
//
// A turn that ended by anything other than its own completion must RENDER
// VISIBLY CUT. That is a rendering decision made above this package, but the
// vocabulary for it lives here so no renderer invents its own dash and its own
// red: the mark is [GlyphCut], and its colour comes from [CutToken] below.

// CutKind is why a stream stopped. It mirrors the reasons a provider can hand
// back (finish_reason) plus the one the user causes.
type CutKind uint8

const (
	// CutNone is a turn that ended by finishing. Nothing is drawn.
	CutNone CutKind = iota
	// CutLengthCap is an output token cap reached mid-answer — the exact shape
	// of session bd3c78ed (12.5), where 600 completion tokens truncated an SVG
	// and the result was journaled as if complete.
	CutLengthCap
	// CutStreamDrop is a connection or provider failure mid-stream.
	CutStreamDrop
	// CutInterrupt is the user pressing esc on a turn they were watching
	// (8.2.21). It is a record of an intent that was carried out.
	CutInterrupt
	cutKindCount
)

// String names the cut kind.
func (c CutKind) String() string {
	switch c {
	case CutNone:
		return "none"
	case CutLengthCap:
		return "length-cap"
	case CutStreamDrop:
		return "stream-drop"
	case CutInterrupt:
		return "interrupt"
	}
	return "invalid"
}

// CutToken is the colour of a cut mark, and the distinction it draws is the
// honest one:
//
//   - A LENGTH CAP or a STREAM DROP is coral. Something broke; the answer on
//     screen is not the answer the model meant to give, and 12.5's whole
//     finding is that an unmarked half-artifact is a lie of omission.
//   - An INTERRUPT is chrome. The user did it on purpose, and nothing is
//     broken. Painting the user's own esc key coral would be the surface
//     scolding someone for using it.
//
// Amber is deliberately not offered: amber means a human is needed (5.16), and
// a cut turn is not a question. The repair doctrine (12.5.3) says the head
// re-produces and says so; it does not stand there asking.
func CutToken(c CutKind) Token {
	switch c {
	case CutLengthCap, CutStreamDrop:
		return Coral
	case CutInterrupt:
		return TextTertiary
	}
	return TextTertiary
}

// CutHue is [CutToken] on the hue axis, for a renderer painting through the
// [Styler] seam rather than naming a token.
func CutHue(c CutKind) Hue {
	switch c {
	case CutLengthCap, CutStreamDrop:
		return HueBroken
	}
	return HueNone
}
