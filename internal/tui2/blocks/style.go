package blocks

// State is the liveness axis of 8.1.6: accent = live, plain = settled,
// dim = chrome. It is orthogonal to [Hue], which carries meaning. A long-lived
// row changes State (colour) continuously and changes shape only at a true
// state transition.
type State uint8

const (
	// StateSettled is plain text: the row is done moving. Zero value, so a
	// bare struct renders as settled.
	StateSettled State = iota
	// StateLive is the accent: something is happening on this row now.
	StateLive
	// StateChrome is dim: separators, meta, fold lines, hints.
	StateChrome
)

// Hue is the five-word colour vocabulary of 5.16. Nothing else is coloured;
// everything else is the grey ramp carried by [State].
type Hue uint8

const (
	// HueNone leaves the grey ramp alone.
	HueNone Hue = iota
	// HueAttention (soft amber) means a human is needed.
	HueAttention
	// HueAlive (soft cyan) means working.
	HueAlive
	// HueMoney (soft green) means money and success.
	HueMoney
	// HueBroken (soft coral) means failed or cancelled.
	HueBroken
	// HueIdentity is the per-task pastel accent. The token layer resolves the
	// actual pastel from the task seed; see [IdentityStyler].
	HueIdentity
)

// Styler is the seam to the token layer. blocks defines the smallest interface
// it needs and never imports internal/tui2/tokens (a parallel builder owns that
// package); the shell injects an implementation. A nil Styler means [Plain].
//
// Paint must not change the printable width of text: it may only wrap text in
// escape sequences. Every width measurement in this package is ANSI-aware, but
// a Styler that inserts printable characters breaks layout.
type Styler interface {
	Paint(text string, state State, hue Hue) string
}

// IdentityStyler is the optional extension a token layer implements to resolve
// the per-task pastel of 5.16. Blocks that carry a task identity type-assert
// for it and fall back to [Hue] painting when it is absent.
type IdentityStyler interface {
	Styler
	// PaintIdentity paints text in the stable pastel derived from seed.
	PaintIdentity(text string, seed uint64, state State) string
}

// Seed turns a task id into the identity seed [Header.GlyphSeed] carries and
// [IdentityStyler.PaintIdentity] resolves. It is FNV-1a/64 over the id bytes —
// allocation-free, and byte-for-byte the same hash the token layer's own
// identity wheel uses, so a glyph seeded from a task id here lands on the SAME
// pastel that layer assigns the task's rail card. The tokens package pins that
// agreement with a test; it is the whole reason the hash is written out rather
// than left to each caller to choose.
//
// An empty id returns zero — "no identity" — which is the honest answer for a
// block that belongs to no task and the value [Header.GlyphSeed] treats as
// unset.
func Seed(taskID string) uint64 {
	if taskID == "" {
		return 0
	}
	const (
		offset = 14695981039346656037
		prime  = 1099511628211
	)
	h := uint64(offset)
	for i := 0; i < len(taskID); i++ {
		h ^= uint64(taskID[i])
		h *= prime
	}
	return h
}

// Plain is the identity Styler: it returns text unchanged. It is the default
// for headless tests and the golden harness, and it is what a nil Styler
// resolves to.
var Plain Styler = plainStyler{}

type plainStyler struct{}

func (plainStyler) Paint(text string, _ State, _ Hue) string { return text }

// styler resolves a possibly-nil Styler.
func styler(s Styler) Styler {
	if s == nil {
		return Plain
	}
	return s
}
