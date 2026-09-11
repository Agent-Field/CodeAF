package tui3

import (
	"math"
	"strings"
	"time"

	"github.com/rivo/uniseg"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The sweep follows elapsed time, sampled by the existing paint clock. A busy
// terminal may skip pictures without slowing the sign of life. The two-second
// travel and cosine feather follow Codex's terminal summary shimmer; keeping
// the crest at ordinary reading ink makes the movement visible without a flash.
// https://github.com/openai/codex/blob/main/codex-rs/tui/src/summary_shimmer.rs
const (
	shimmerPeriod     = 2 * time.Second
	shimmerMinRadius  = 6.0
	shimmerWidthRatio = 0.20
)

// shimmer paints the one moving highlight a collapsed live caption owns.
//
// THE SHIMMER IS THE SPINNER, RELOCATED. Opening the work returns its animation
// budget to the tool rows immediately. The letters never move, and the smooth
// bell reaches ordinary answer ink only at its crest. Measuring whole graphemes
// in terminal cells keeps accents and joined emoji intact under the highlight.
func (a *app) shimmer(text string) string {
	if text == "" || a.linear || a.pal.linear || a.pal.profile < tokens.TrueColor {
		return a.pal.narr(text)
	}
	// A turn supplies a stable origin when it has one. Rooms can run while the
	// conversation is idle, so they use the same clock's absolute phase instead.
	// Neither rendering nor opening a disclosure starts another timer.
	elapsed := time.Duration(a.now().UnixNano())
	if !a.turnBegan.IsZero() {
		elapsed = a.now().Sub(a.turnBegan)
	}
	phase := (elapsed%shimmerPeriod + shimmerPeriod) % shimmerPeriod
	width := uniseg.StringWidth(text)
	radius := math.Max(shimmerMinRadius, float64(width)*shimmerWidthRatio)
	center := -radius + float64(phase)/float64(shimmerPeriod)*(float64(width)+2*radius)
	var b strings.Builder
	graphemes := uniseg.NewGraphemes(text)
	cell := 0
	start, offset := 0, 0
	var previous hue
	for graphemes.Next() {
		word, cells := graphemes.Str(), graphemes.Width()
		distance := math.Abs(float64(cell) + float64(cells)/2 - center)
		amount := 0.0
		if distance < radius {
			amount = (1 + math.Cos(math.Pi*distance/radius)) / 2
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
