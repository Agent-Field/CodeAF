package tui3

import (
	"math"
	"strings"

	"github.com/rivo/uniseg"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// These frame slots belong to the existing paint clock, including its remote
// stride. PROGRESS HAS ONE CLOCK: a caption never starts a timer of its own.
// A slow sweep and a quiet rest signal ongoing work without asking to be read
// again. The long period is for ambient progress, never for a key or disclosure.
const (
	shimmerSweep  = 72
	shimmerRest   = 24
	shimmerPeriod = shimmerSweep + shimmerRest
	shimmerRadius = 8.0
	shimmerLift   = 0.35
)

// shimmer paints the one moving highlight a collapsed live caption owns.
//
// THE SHIMMER IS THE SPINNER, RELOCATED. Opening the work returns its animation
// budget to the tool rows immediately. The letters never move, and the smooth
// bell stays well below answer ink even at its crest. Measuring whole graphemes
// in terminal cells keeps accents and joined emoji intact under the highlight.
func (a *app) shimmer(text string) string {
	phase := a.paints % shimmerPeriod
	if text == "" || a.linear || a.pal.linear || a.pal.profile < tokens.TrueColor ||
		phase == 0 || phase >= shimmerSweep-1 {
		return a.pal.narr(text)
	}
	width := uniseg.StringWidth(text)
	center := -shimmerRadius + float64(phase)/float64(shimmerSweep-1)*(float64(width)+2*shimmerRadius)
	var b strings.Builder
	graphemes := uniseg.NewGraphemes(text)
	cell := 0
	start, offset := 0, 0
	var previous hue
	for graphemes.Next() {
		word, cells := graphemes.Str(), graphemes.Width()
		distance := math.Abs(float64(cell) + float64(cells)/2 - center)
		amount := 0.0
		if distance < shimmerRadius {
			amount = shimmerLift * (1 + math.Cos(math.Pi*distance/shimmerRadius)) / 2
		}
		color := a.pal.shimmerHue(amount)
		// Adjacent clusters with the same colour share one escape pair. Most of
		// the line is the quiet prefix or suffix, so per-character escapes there
		// would spend terminal bandwidth on pixels that have not changed.
		if offset > start && color != previous {
			b.WriteString(a.pal.paint(text[start:offset], previous))
			start = offset
		}
		previous = color
		offset += len(word)
		cell += cells
	}
	b.WriteString(a.pal.paint(text[start:offset], previous))
	return b.String()
}

// shimmerHue follows the current theme's reading ladder rather than flashing
// white. Its feather is continuous in truecolour. Lower-colour and accessible
// tiers stay still: nearest 256-colour matches can jump between unrelated hues,
// and a switch in font weight cannot express a gentle colour transition either.
func (p palette) shimmerHue(amount float64) hue {
	base, ink := p.ramp.narr, p.ramp.ink
	mix := func(a, b uint8) uint8 {
		return uint8(math.Round(float64(a) + amount*(float64(b)-float64(a))))
	}
	h := hue{r: mix(base.r, ink.r), g: mix(base.g, ink.g), b: mix(base.b, ink.b), tier: base.tier}
	return h
}
