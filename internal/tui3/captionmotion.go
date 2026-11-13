package tui3

import (
	"math"
	"strings"
	"time"

	"github.com/rivo/uniseg"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// The sweep follows elapsed time, sampled by the existing paint clock. A busy
// terminal may skip pictures without slowing the sign of life. Keeping the
// crest at ordinary reading ink makes the movement visible without a flash.
const (
	shimmerPeriod     = 2 * time.Second
	shimmerMinRadius  = 6.0
	shimmerWidthRatio = 0.20
)

// The sweep follows elapsed time, sampled by the existing paint clock. A busy
// terminal may skip pictures without slowing the sign of life. Keeping the
// crest at ordinary reading ink makes the movement visible without a flash.
func (a *app) shimmer(text string) string {
	if text == "" || a.anyWorkLogoVisible() || a.linear || a.pal.linear || a.pal.profile < tokens.TrueColor {
		return a.pal.narr(text)
	}
	// The sweep follows elapsed time, sampled by the existing paint clock. A busy
	// terminal may skip pictures without slowing the sign of life. Keeping the
	// crest at ordinary reading ink makes the movement visible without a flash.
	elapsed := time.Duration(a.now().UnixNano())
	if !a.turnBegan.IsZero() {
		elapsed = a.now().Sub(a.turnBegan)
	}
	return a.shimmerAt(text, elapsed, shimmerPeriod, 1)
}

// The sweep follows elapsed time, sampled by the existing paint clock. A busy
// terminal may skip pictures without slowing the sign of life. Keeping the
// crest at ordinary reading ink makes the movement visible without a flash.
func (a *app) shimmerAt(text string, elapsed, period time.Duration, strength float64) string {
	if text == "" || a.linear || a.pal.linear || a.pal.profile < tokens.TrueColor {
		return a.pal.narr(text)
	}
	phase := (elapsed%period + period) % period
	width := uniseg.StringWidth(text)
	radius := math.Max(shimmerMinRadius, float64(width)*shimmerWidthRatio)
	center := -radius + float64(phase)/float64(period)*(float64(width)+2*radius)
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
		color := a.pal.shimmerHue(amount * strength)
		// The sweep follows elapsed time, sampled by the existing paint clock. A busy
		// terminal may skip pictures without slowing the sign of life. Keeping the
		// crest at ordinary reading ink makes the movement visible without a flash.
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

// The sweep follows elapsed time, sampled by the existing paint clock. A busy
// terminal may skip pictures without slowing the sign of life. Keeping the
// crest at ordinary reading ink makes the movement visible without a flash.
func (p palette) shimmerHue(amount float64) hue {
	base, ink := p.ramp.narr, p.ramp.ink
	mix := func(a, b uint8) uint8 {
		return uint8(math.Round(float64(a) + amount*(float64(b)-float64(a))))
	}
	h := hue{r: mix(base.r, ink.r), g: mix(base.g, ink.g), b: mix(base.b, ink.b), tier: base.tier}
	return h
}
