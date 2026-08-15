package tui3

import (
	"os"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/tui2/prose"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Markdown on this surface is internal/tui2/prose, unchanged.
//
// A model answers in markdown whether or not anyone asked it to, and there is
// exactly one renderer in this tree that turns that into rows without handing a
// second colour authority a seat on the screen — goldmark's parser walked into
// internal/tui2/tokens. Adapting it here is four lines and one cached Styler;
// re-deriving it would be a second heading ladder, a second table fitter and a
// second answer to what a 16-colour terminal may draw, all drifting from the
// first the week after they were written.
//
// What this file owns is therefore only the two things prose leaves to a
// caller: which Styler paints, and which measure the prose wraps to.
//
// CODE FENCES STAY ON THE TOKENS RAMP, and this is the whole of D11's "else"
// branch. Decision 11 asks for a pastel chroma style (catppuccin-mocha) on
// fenced code IF it can be had without editing internal/tui2 — and it cannot:
// prose.Options is Width, Measure and *tokens.Styler, with no theme parameter
// and no seam for one. Behind it, prose/code.go BUILDS its chroma style out of
// the tokens ramp and maps every chroma token onto a tokens.CodeSlot, so there
// is no style to swap from this side even in principle — the theme is the token
// layer. Adopting mocha here would mean adding an option to prose, which is the
// one edit this slice is not allowed to make and would in any case hand the
// surface a second colour authority — exactly what prose's own package comment
// rejects glamour for.
//
// The cost is small and the floor is already right: the tokens ramp is muted by
// construction, so a fence renders quiet next to styles.go's pastels rather
// than clashing with them. If the theme is wanted later it is one option on
// prose.Options and one line here, and it belongs to whoever owns tui2.

var (
	stylerOnce sync.Once
	styler     *tokens.Styler
)

// markdownStyler is the surface's painter, built once per process.
//
// Once, because a Styler is immutable and every escape sequence in it was
// computed at package initialization by the palette table: rebuilding one per
// render would redo that work on every frame of a stream and produce the same
// bytes each time. Lazily, because construction reads the environment, and a
// package-level var would fix the profile at init — before a test or a caller
// that sets NO_COLOR for a subprocess has had a word.
//
// The profile comes from [tokens.DetectProfile], the same door cmd/aforge opens
// for the v2 surface, so both surfaces answer "what can this terminal say" from
// one decision table rather than from two guesses. The glyph tier stays
// [tokens.Plain]: that axis is opt-out-able elsewhere in the tree by a flag this
// surface does not have yet, and the plain tier is a designed floor, not a
// degradation. Focus is normal — this surface has one pane, so there is nothing
// for a dimmed one to recede behind.
func markdownStyler() *tokens.Styler {
	stylerOnce.Do(func() {
		styler = tokens.NewStyler(tokens.DetectProfile(os.Getenv), tokens.FocusNormal)
	})
	return styler
}

// renderMarkdown renders model-written markdown into screen rows, one string
// per row, each at most width printable cells, with the package's typographic
// hierarchy applied: headings promoted by tier (accent, h1 bold), fenced code
// chroma-highlighted, tables fitted (truncate, never wrap), bold/italic, and
// hanging list indents.
//
// It caches nothing: a caller redraws on turn settle and a few times a second
// while a reply streams, and a cache keyed on text that is still growing is a
// cache that is wrong at exactly the moments anyone is looking at it. The one
// thing held across calls is the Styler, which is a value and not a result.
//
// Untrusted text needs no laundering here — prose.Render is itself the
// chokepoint, running every byte through internal/sanitize with the token
// layer's ANSI-16 remap and then stripping the SGR that survives, so a reply
// cannot paint itself a heading. Sanitizing first would only mean doing it
// twice.
func renderMarkdown(text string, width int) []string {
	return renderMarkdownWith(markdownStyler(), text, width)
}

// renderMarkdownWith is [renderMarkdown] against a stated Styler. It is the
// whole body, split off because the profile is detected from the environment
// exactly once per process: a test that wants to see what a truecolor terminal
// gets cannot ask for one afterwards, and a test that raced the detection to
// set NO_COLOR would be a test whose result depended on which test ran first.
func renderMarkdownWith(st *tokens.Styler, text string, width int) []string {
	if width < 1 {
		width = 1
	}
	return prose.Render(text, prose.Options{
		Width: width,
		// The hard ceiling is the pane; the reading length is prose's own
		// constant, clamped to the ceiling by prose. A 200-column window is a
		// wide window, not a wide sentence — and naming the constant rather
		// than a number of our own keeps one definition of a measure in the
		// tree.
		Measure: prose.DefaultMeasure,
		Styler:  st,
	})
}
