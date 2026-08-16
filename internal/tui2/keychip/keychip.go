// Package keychip paints the verb·key chip — the one renderer for every surface
// that draws an action word beside the key that reaches it.
//
// §16's VERB·KEY CHIP is "one renderer, every surface" and this is the renderer:
// the word that names the act is the word the eye lands on and the key reads as
// annotation because it is drawn as one — which is why `esc close` was a bug and
// not a style.
//
// THE BUG THAT NAMED THE RULE was a settings sheet whose hint read `esc close`.
// Two greys, two words, and nothing in the row saying which one is the label and
// which is the thing to press, so a reader parses it by already knowing the
// answer — the definition of the memory test 5.22 exists to remove. Verb-first
// fixes it structurally rather than by explanation.
//
// internal/registry owns the DATA half of that law: the order of the two words
// and the fallback ladder that decides what the key half says at all
// ([registry.Chip]). It cannot own the paint — the dependency runs tui →
// registry, and a Styler arriving there would drag a terminal renderer behind
// every consumer of the catalog, including the future web surface. So the paint
// lives here, and it was being written once per surface: the footer's chipRuns,
// the composer's hint chips, the consent dialog's key strip, the empty room's
// teaching rows and a task card's confirm line were five spellings of one
// two-tier rule, and two of them had already drifted into key-first.
//
// WHY ITS OWN PACKAGE and not a function in blocks or tokens. It needs
// [registry.Chip], and registry drags the store and provider packages behind it;
// blocks is the leaf every renderer depends on and must stay one, and tokens is
// the vocabulary underneath it. A package of its own is the only place both
// halves can meet without inverting an edge somebody else depends on.
package keychip

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Span is one painted run of a chip line: the text, and the tier it wears.
//
// It is spans rather than a finished string because the surfaces differ in what
// they do with them — the footer attaches a click id to each run, the consent
// strip paints on a ground, the teaching row indents — and a renderer handed one
// pre-painted string can only take it or leave it. What must NOT differ is which
// run gets which tier, and that is what this package decides.
type Span struct {
	Text string
	Tok  tokens.Token
}

// Sep is the separator between chips on one line: the telemetry dot, at the
// dimmest tier, exactly as every other list of cells in the product spells it.
const Sep = " " + tokens.GlyphSeparator + " "

// Of is one chip in the two tiers §16's grammar asks for: the VERB first at
// `verb`, the key after it at [tokens.TextTertiary].
//
// The verb tier is the CALLER's because it is a fact about the surface — a
// footer's verbs sit at secondary against a busy row, a teaching row's sit
// brighter because the row is the lesson — and what the caller does not get to
// choose is the ORDER, or that the key is one tier down. The verb is never at
// the dimmest tier: an interactive chip may not live permanently in the tier the
// eye skips (5.22's amendment).
//
// A chip with no verb is its bare key, which is a real row: a belt-only verb is
// reached through the user's own words and has no key by construction, and a
// key with no verb is what an overlay's raw exit hint has always been. A chip
// with neither is no spans at all rather than an empty run.
func Of(chip registry.Chip, verb tokens.Token) []Span {
	if chip.Empty() {
		if chip.Key == "" {
			return nil
		}
		return []Span{{Text: chip.Key, Tok: tokens.TextTertiary}}
	}
	out := make([]Span, 0, 2)
	out = append(out, Span{Text: chip.Verb, Tok: verb})
	if chip.Key != "" {
		out = append(out, Span{Text: registry.ChipGap + chip.Key, Tok: tokens.TextTertiary})
	}
	return out
}

// Line is a row of chips, joined by [Sep] at the dimmest tier.
//
// The separator is chrome and the chips are not, which is the whole reason the
// join is here rather than at each call site: a surface that painted its whole
// strip at one tier would flatten the two-tier rule the chips were built to
// state, and that is precisely what the consent dialog's key strip did.
//
// An empty chip contributes nothing AND no separator, so a strip that drops a
// verb it cannot honestly offer does not leave a dangling dot behind it.
func Line(chips []registry.Chip, verb tokens.Token) []Span {
	out := make([]Span, 0, len(chips)*3)
	for _, chip := range chips {
		spans := Of(chip, verb)
		if len(spans) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, Span{Text: Sep, Tok: tokens.TextTertiary})
		}
		out = append(out, spans...)
	}
	return out
}

// Width is how many cells [Line] will take, so a surface can decide what fits
// before it paints anything. It measures the same spans Line builds, through the
// same ruler the renderer lays rows out with.
func Width(chips []registry.Chip) int {
	total := 0
	for _, span := range Line(chips, tokens.TextSecondary) {
		total += blocks.Width(span.Text)
	}
	return total
}

// Text is [Line] as one plain string, for a surface that carries a message
// rather than a row of spans — a status line, a `--color none` render, a test.
// It is the same words in the same order; only the tiers are gone.
func Text(chips ...registry.Chip) string {
	var b strings.Builder
	for _, span := range Line(chips, tokens.TextSecondary) {
		b.WriteString(span.Text)
	}
	return b.String()
}
