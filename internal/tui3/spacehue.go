package tui3

import (
	"math"
	"sync"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── A SPACE'S COLOUR, GENERATED ─────────────────────────────────────────────
//
// A space is told apart by a colour, and the colours are generated rather
// than picked from a list, so that the tenth space is as easy to tell from the
// other nine as the second is from the first.
//
// THE COLOURS LIVE IN OKLCH, where equal steps of hue look like equal steps.
// A space keeps only its hue angle and a lightness tier; the lightness and
// chroma come from the tier and the ladder being drawn on, so one space reads
// right on a dark terminal and a light one.
//
// THE HUES THAT ALREADY MEAN SOMETHING ARE KEPT OUT. Amber is a question
// waiting, the live ink is work running, red is a failure and the accent is the
// cursor; a space in any of those would say a second thing in a colour that
// already says one. Their hues are read off the ramp being drawn with, never
// written down here, so a theme that moves them moves the bands with them.
//
// EACH NEW SPACE TAKES THE FARTHEST HUE: of every allowed degree, the one whose
// nearest used hue is farthest away. The second space lands opposite the
// first, the third splits the widest gap, and so on; past six the tiers
// alternate so neighbours differ in lightness as well.

// spaceHueSpec is a space's colour as stored: a hue angle in degrees and a
// lightness tier, 0 or 1.
type spaceHueSpec struct {
	Hue  float64
	Tier int
}

// spaceHueBand is how many degrees either side of a meaningful hue no space
// may take.
const spaceHueBand = 25

// spaceHueTierFree is how many spaces are all drawn at tier 0 before the tiers
// start to alternate.
const spaceHueTierFree = 6

// ── OKLAB AND OKLCH ─────────────────────────────────────────────────────────

func srgbToLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func linearToSRGB(c float64) float64 {
	if c <= 0.0031308 {
		return 12.92 * c
	}
	return 1.055*math.Pow(c, 1/2.4) - 0.055
}

// rgbToOKLab is an 8-bit sRGB colour in OKLab.
func rgbToOKLab(r, g, b uint8) (L, A, B float64) {
	lr, lg, lb := srgbToLinear(float64(r)/255), srgbToLinear(float64(g)/255), srgbToLinear(float64(b)/255)
	l := math.Cbrt(0.4122214708*lr + 0.5363325363*lg + 0.0514459929*lb)
	m := math.Cbrt(0.2119034982*lr + 0.6806995451*lg + 0.1073969566*lb)
	s := math.Cbrt(0.0883024619*lr + 0.2817188376*lg + 0.6299787005*lb)
	return 0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		0.0259040371*l + 0.7827717662*m - 0.8086757660*s
}

// oklchToSRGB is an OKLCH colour as sRGB channels in 0..1, which may fall
// outside that range when the colour is outside the gamut.
func oklchToSRGB(L, C, h float64) (r, g, b float64) {
	A, B := C*math.Cos(h*math.Pi/180), C*math.Sin(h*math.Pi/180)
	l := L + 0.3963377774*A + 0.2158037573*B
	m := L - 0.1055613458*A - 0.0638541728*B
	s := L - 0.0894841775*A - 1.2914855480*B
	l, m, s = l*l*l, m*m*m, s*s*s
	r = linearToSRGB(4.0767416621*l - 3.3077115913*m + 0.2309699292*s)
	g = linearToSRGB(-1.2684380046*l + 2.6097574011*m - 0.3413193965*s)
	b = linearToSRGB(-0.0041960863*l - 0.7034186147*m + 1.7076147010*s)
	return r, g, b
}

// inGamut reports whether three sRGB channels are all drawable.
func inGamut(r, g, b float64) bool {
	const eps = 1e-4
	return r >= -eps && r <= 1+eps && g >= -eps && g <= 1+eps && b >= -eps && b <= 1+eps
}

func to8(c float64) uint8 {
	return uint8(math.Round(math.Min(math.Max(c, 0), 1) * 255))
}

// oklabHue is the OKLCH hue of an 8-bit colour, in degrees 0..360.
func oklabHue(r, g, b uint8) float64 {
	_, A, B := rgbToOKLab(r, g, b)
	h := math.Atan2(B, A) * 180 / math.Pi
	if h < 0 {
		h += 360
	}
	return h
}

// hueGap is the distance between two hues around the circle, 0..180.
func hueGap(a, b float64) float64 {
	d := math.Mod(math.Abs(a-b), 360)
	if d > 180 {
		d = 360 - d
	}
	return d
}

// ── THE GENERATOR ───────────────────────────────────────────────────────────

// spaceReservedHues is the hues the palette already spends on meaning: the
// question's amber, the running ink, the failure red and the cursor's accent.
func spaceReservedHues(p palette) []float64 {
	return spaceReservedFrom(p.ramp)
}

func spaceReservedFrom(r ramp) []float64 {
	out := make([]float64, 0, 4)
	for _, h := range []hue{r.warn, r.live, r.bad, r.accent} {
		out = append(out, oklabHue(h.r, h.g, h.b))
	}
	return out
}

// spaceHueAllowed reports whether a hue is clear of every reserved band.
func spaceHueAllowed(h float64, reserved []float64) bool {
	for _, r := range reserved {
		if hueGap(h, r) < spaceHueBand {
			return false
		}
	}
	return true
}

// spaceTierFor is the tier the n-th space (from zero) is drawn at.
func spaceTierFor(n int) int {
	if n < spaceHueTierFree {
		return 0
	}
	return 1 - (n-spaceHueTierFree)%2
}

// nextSpaceHue is the colour for a new space beside the used ones: the allowed
// whole degree farthest from every used hue, the lowest such degree on a tie.
func nextSpaceHue(used []spaceHueSpec, reserved []float64) spaceHueSpec {
	best, bestGap := -1.0, -1.0
	for d := 0; d < 360; d++ {
		h := float64(d)
		if !spaceHueAllowed(h, reserved) {
			continue
		}
		gap := 1000.0
		for _, u := range used {
			gap = math.Min(gap, hueGap(h, u.Hue))
		}
		if gap > bestGap {
			best, bestGap = h, gap
		}
	}
	if best < 0 {
		best = 0
	}
	return spaceHueSpec{Hue: best, Tier: spaceTierFor(len(used))}
}

// spaceHueChoices is k colours a new space could take, best first: each the
// farthest from the used hues and from the choices before it. They are what
// the new-space card offers as swatches and what shuffle walks through.
func spaceHueChoices(used []spaceHueSpec, reserved []float64, k int) []spaceHueSpec {
	tier := spaceTierFor(len(used))
	seen := append([]spaceHueSpec(nil), used...)
	out := make([]spaceHueSpec, 0, k)
	for len(out) < k {
		next := nextSpaceHue(seen, reserved)
		next.Tier = tier
		out = append(out, next)
		seen = append(seen, next)
	}
	return out
}

// ── DRAWING ONE ─────────────────────────────────────────────────────────────

// spaceGround is the luminance of the background a space colour must read on:
// the one the terminal reported, else the middle of the assumed dark range, or
// white on the light ladder (styles.go's THE GLARE LAW names both).
func spaceGround(p palette) float64 {
	switch {
	case p.measured:
		return luminanceOf(p.ground.r, p.ground.g, p.ground.b)
	case p.light:
		return luminanceOf(0xFF, 0xFF, 0xFF)
	}
	return luminanceOf(0x1A, 0x1B, 0x26)
}

// spaceLight reports whether the palette is drawing on a light background.
func spaceLight(p palette) bool {
	if p.measured {
		return luminanceOf(p.ground.r, p.ground.g, p.ground.b) > 0.18
	}
	return p.light
}

// spaceRGB is a space colour as 8-bit sRGB for this palette: the tier's
// lightness and chroma, the chroma eased until the colour is drawable, and the
// lightness moved away from the background until it reads at 3:1.
func spaceRGB(p palette, h spaceHueSpec) (uint8, uint8, uint8) {
	light := spaceLight(p)
	L, C := 0.80, 0.11
	if h.Tier == 1 {
		L, C = 0.68, 0.13
	}
	if light {
		L, C = 0.55, 0.13
		if h.Tier == 1 {
			L = 0.45
		}
	}
	ground := spaceGround(p)
	for step := 0; step < 40; step++ {
		c := C
		r, g, b := oklchToSRGB(L, c, h.Hue)
		for !inGamut(r, g, b) && c > 0 {
			c = math.Max(c-0.005, 0)
			r, g, b = oklchToSRGB(L, c, h.Hue)
		}
		r8, g8, b8 := to8(r), to8(g), to8(b)
		if contrastRatio(luminanceOf(r8, g8, b8), ground) >= 3 || L <= 0.05 || L >= 0.98 {
			return r8, g8, b8
		}
		if light {
			L -= 0.02
		} else {
			L += 0.02
		}
	}
	r, g, b := oklchToSRGB(L, 0, h.Hue)
	return to8(r), to8(g), to8(b)
}

// xterm256Lab is the OKLab of every cube and grey index, computed once.
var (
	xterm256Once sync.Once
	xterm256Lab  [256][3]float64
)

// nearest256Lab is the closest xterm-256 index to a colour by OKLab distance,
// over the cube and the grey ramp and never the first sixteen, which are the
// person's own theme.
func nearest256Lab(r, g, b uint8) uint8 {
	xterm256Once.Do(func() {
		for i := 16; i < 256; i++ {
			var cr, cg, cb int
			if i < 232 {
				n := i - 16
				cr, cg, cb = cubeLevels[n/36], cubeLevels[(n/6)%6], cubeLevels[n%6]
			} else {
				v := 8 + 10*(i-232)
				cr, cg, cb = v, v, v
			}
			L, A, B := rgbToOKLab(uint8(cr), uint8(cg), uint8(cb))
			xterm256Lab[i] = [3]float64{L, A, B}
		}
	})
	L, A, B := rgbToOKLab(r, g, b)
	best, bestD := 16, math.Inf(1)
	for i := 16; i < 256; i++ {
		c := xterm256Lab[i]
		d := (L-c[0])*(L-c[0]) + (A-c[1])*(A-c[1]) + (B-c[2])*(B-c[2])
		if d < bestD {
			best, bestD = i, d
		}
	}
	return uint8(best)
}

// spaceHueOf is a space colour as a hue this palette can paint with.
func spaceHueOf(p palette, h spaceHueSpec) hue {
	r, g, b := spaceRGB(p, h)
	return hue{r: r, g: g, b: b, idx: nearest256Lab(r, g, b), tier: flat}
}

// spaceInk is the pen a space's colour is drawn with, or nil where there is no
// colour to draw: a terminal under 256 colours, NO_COLOR, or the ASCII floor.
// A caller given nil draws the space's initial, dim, instead of a dot.
func (p palette) spaceInk(h spaceHueSpec) func(string) string {
	if p.ascii || p.profile < tokens.ANSI256 {
		return nil
	}
	hh := spaceHueOf(p, h)
	return func(s string) string { return p.paint(s, hh) }
}
