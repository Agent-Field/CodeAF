package tui3

import (
	"math"

	tea "charm.land/bubbletea/v2"
)

// ── THE MEASURED GROUND ─────────────────────────────────────────────────────
//
// styles.go authors two ladders against an ASSUMED background, because for four
// waves there was no way to learn the real one: a constructor may not block on a
// round trip that a terminal is free never to answer. This file is the other
// half of that sentence. The round trip is still a round trip — but it is an
// EVENT now, asked for in [app.Init] and answered, if it is answered at all, on
// the same message lane every keystroke arrives on. Nothing waits for it, so the
// law the constructor keeps is untouched.
//
// What arrives is one colour: the terminal's own background. Everything in this
// file is what that one colour buys, and it is all arithmetic — pure functions
// from a measured ground to a ladder, with a single thin hook at the bottom that
// swaps the app's palette and drops the paint that was cached against the old
// one.
//
// THE PALETTE IS STILL CLOSED. This file computes colours; it does not AUTHOR
// any. There is not one hex literal below, and there cannot be: every value here
// is derived from a hue styles.go already owns or from the ground the terminal
// reported. The two files together are still the only pair in the package that
// may hold an escape sequence, and only styles.go writes one.

// measuredGround is the terminal's own background, as the terminal reported it.
//
// It is a type of its own rather than three loose bytes because it is the one
// fact everything in this file is a function of, and a signature that says
// "measuredGround" cannot be handed an ink by accident — which, in a package
// whose whole vocabulary is colours, is a mistake worth making impossible.
type measuredGround struct{ r, g, b uint8 }

// ── WHAT THE GROUND STEPS AIM AT ────────────────────────────────────────────
//
// These are not new numbers. They are the numbers styles.go's GROUND LADDER
// already states as its aim, read off the middle of the range it had to assume —
// the cursor step just above the point a flat tint stops being perceptible, the
// selected step half a step louder, and the marked span louder again because it
// is transient and covers many rows at once.
//
// The difference is what they are measured AGAINST. Authored, they were an aim
// that landed differently on every terminal: one notch louder on a blacker
// screen, one notch quieter on a lighter one, and close to nothing on a tinted
// page. Derived, they are what the step IS, on whatever ground the terminal
// turned out to have. That is the whole of what the reply buys for this ladder.
const (
	groundCursorRatio   = 1.17
	groundSelectedRatio = 1.37
	groundMarkRatio     = 1.98
)

// band is a range one reading tier may sit in, in WCAG contrast against the
// measured ground.
//
// A BAND RATHER THAN A POINT, AND A VALUE IN BAND IS NOT TOUCHED. That rule is
// styles.go's own — it is why two steps of the light ladder survived the retune
// that authored the other four — and it is the whole reason this file is a
// correction rather than a second palette. On the terminals the authored ladder
// was aimed at, every reading tier already lands inside its band and the reply
// changes nothing a person can see. It is the grounds the authors could not aim
// at that move: a pure black screen, where the body was climbing past fifteen to
// one and reading as a glare, and a tinted page, where the whole light ladder
// was sliding under the floor at once.
type band struct{ low, high float64 }

// The three reading tiers and the bands they are held to.
//
// They are a LADDER by construction (styles.go) — the body, the surface's second
// voice, and the surface talking about itself — so the three bands are disjoint
// and ordered, and that is what keeps the ladder a ladder no matter which end of
// its band each tier gets pushed to.
//
//	ink    9.5-12   the body. The ceiling is the important half: light text on a
//	                black screen at fifteen to one is the halation complaint every
//	                terminal palette eventually gets, and the authored ink hits it
//	                on exactly the ground nobody could aim at
//	muted  4.5-7.5  the second voice. The floor is the one number here borrowed
//	                from outside — 4.5:1 is what every accessibility guideline
//	                asks of text a person is expected to read, and the authored
//	                light muted sits under it on a white page
//	dim    2.6-4.4  the surface's own murmur. It is allowed to be quiet, which is
//	                its whole job, but a tier that has dropped under about 2.6:1
//	                is not quiet, it is gone
var (
	inkBand   = band{9.5, 12.0}
	mutedBand = band{4.5, 7.5}
	dimBand   = band{2.6, 4.4}
)

// signalFloor is the least contrast a SIGNAL hue may have against the measured
// ground before the whole signal set is moved.
//
// It is deliberately below the reading tiers' floors. A signal is not a
// paragraph — it is a plus sign, a cross, a single word — and styles.go spends
// real care keeping the set quiet on purpose. What this floor catches is not
// "dim", it is INVISIBLE: a ground so close to the signal band that the set stops
// separating from the page at all, which is what the authored light ladder does
// on a page that is not white.
const signalFloor = 2.5

// ── THE ARITHMETIC ──────────────────────────────────────────────────────────

// luminanceOf is sRGB relative luminance, the sRGB transfer curve included — the
// linear-light average that contrast is defined on, rather than the raw
// channels.
//
// It lives here rather than in a test file now, and the move is the point: until
// the terminal could be asked, nothing at runtime knew what the background was,
// so nothing at runtime could compute a contrast and this was an AUTHOR'S check
// that belonged with the author's other checks. It is a runtime question now, and
// designlanguage_test.go reads it from here so the number a test asserts and the
// number the surface derives can never be two different numbers.
func luminanceOf(r, g, b uint8) float64 {
	channel := func(c uint8) float64 {
		v := float64(c) / 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(r) + 0.7152*channel(g) + 0.0722*channel(b)
}

// contrastRatio is the WCAG ratio between two relative luminances, lighter over
// darker, and it does not care which argument is which.
func contrastRatio(first, second float64) float64 {
	if first < second {
		first, second = second, first
	}
	return (first + 0.05) / (second + 0.05)
}

// contrastOn is one hue's contrast against a measured ground.
func contrastOn(h hue, ground float64) float64 {
	return contrastRatio(luminanceOf(h.r, h.g, h.b), ground)
}

// groundIsDark answers the two questions this file has: which ladder, and which
// way every value on it travels. IT IS ONE QUESTION BECAUSE IT HAS TO BE — a
// ladder chosen for a dark page and then derived downward is a ladder painting
// dark ink on a dark ground, and the terminals nearest the boundary are exactly
// where that would happen.
//
// The line is where a colour's contrast with white equals its contrast with
// black, which falls at a relative luminance of about 0.179. That is the
// standard pivot for "does this want light text or dark text", and it is also,
// read the other way, THE SIDE WITH MORE ROOM: below it there is more contrast to
// be had above the ground than below it, and above it the reverse. Because the
// direction is always the roomier side, the reach in that direction is never
// less than the square root of twenty-one — about 4.58:1 — which is more than
// twice the loudest step THE GROUND LADDER asks for. That is why the ground
// steps need no scaling and the reading tiers, which ask for twelve, do.
//
// It is NOT [tea.BackgroundColorMsg.IsDark], which is HSL lightness under a
// half, and the difference is not academic: a saturated yellow sits at HSL 0.47
// and would be called dark while carrying more light than most white-ish pages.
// A surface that put its pale ink on that ground would be unreadable, so this
// package asks the question in the space contrast is actually defined in and
// does not call the message's own method at all.
func groundIsDark(m measuredGround) bool {
	return luminanceOf(m.r, m.g, m.b) < groundPivot
}

// groundPivot is the luminance at which white and black are equally legible:
// the positive root of (v+0.05)² = 1.05 × 0.05. See [groundIsDark].
const groundPivot = 0.1791

// hslOf is the HSL of a colour: hue in degrees, saturation and lightness in
// zero-to-one. It is the space every palette in the world is quoted in, and it
// is the space this file moves values in for one reason — MOVING A COLOUR'S
// LIGHTNESS AND NOTHING ELSE IS HOW A COLOUR STAYS ITSELF. Re-aiming a tier by
// scaling its channels drags the hue toward whichever channel was largest, which
// is how a re-aimed palette drifts one wave at a time into a colour nobody chose.
func hslOf(r, g, b uint8) (float64, float64, float64) {
	rf, gf, bf := float64(r)/255, float64(g)/255, float64(b)/255
	hi := math.Max(math.Max(rf, gf), bf)
	lo := math.Min(math.Min(rf, gf), bf)
	l := (hi + lo) / 2
	if hi == lo {
		// A grey has no hue and no saturation, which is not a defect to be
		// worked around: a black or white terminal reports exactly this, and a
		// ladder derived off it comes out grey, which is what a ladder over a
		// grey ground should be.
		return 0, 0, l
	}
	span := hi - lo
	s := span / (2 - hi - lo)
	if l < 0.5 {
		s = span / (hi + lo)
	}
	var h float64
	switch hi {
	case rf:
		h = (gf - bf) / span
		if gf < bf {
			h += 6
		}
	case gf:
		h = (bf-rf)/span + 2
	default:
		h = (rf-gf)/span + 4
	}
	return h * 60, s, l
}

// rgbOfHSL is [hslOf] read backwards.
func rgbOfHSL(h, s, l float64) (uint8, uint8, uint8) {
	if s <= 0 {
		v := byteOf(l)
		return v, v, v
	}
	q := l * (1 + s)
	if l >= 0.5 {
		q = l + s - l*s
	}
	p := 2*l - q
	channel := func(t float64) float64 {
		for t < 0 {
			t++
		}
		for t > 1 {
			t--
		}
		switch {
		case t < 1.0/6:
			return p + (q-p)*6*t
		case t < 1.0/2:
			return q
		case t < 2.0/3:
			return p + (q-p)*(2.0/3-t)*6
		default:
			return p
		}
	}
	at := h / 360
	return byteOf(channel(at + 1.0/3)), byteOf(channel(at)), byteOf(channel(at - 1.0/3))
}

// byteOf rounds a zero-to-one channel into the byte a terminal is sent.
func byteOf(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(v*255 + 0.5)
}

// atLuminance holds a colour's HUE AND SATURATION and moves its LIGHTNESS until
// it carries the stated relative luminance.
//
// The search is a bisection rather than a formula because there is no closed form
// for it: luminance is the sRGB transfer curve applied per channel and then
// weighted, and HSL lightness reaches those channels through a piecewise
// function. What makes the bisection sound is that the composition is MONOTONE —
// every channel of a fixed hue and saturation rises with lightness, so luminance
// does too — and twenty-four halvings of a unit interval land far inside the
// rounding of a single byte.
func atLuminance(h, s, want float64) (uint8, uint8, uint8) {
	at := func(l float64) (uint8, uint8, uint8) { return rgbOfHSL(h, s, l) }
	lo, hi := 0.0, 1.0
	for i := 0; i < 24; i++ {
		mid := (lo + hi) / 2
		r, g, b := at(mid)
		if luminanceOf(r, g, b) < want {
			lo = mid
		} else {
			hi = mid
		}
	}
	return closerTo(want, at, lo, hi)
}

// closerTo settles a bisection onto the better of the two colours the search
// ended between.
//
// It matters because the answer is quantized to a byte per channel long before
// the search runs out of precision: at the dark end of the scale one value of a
// channel is a visible fraction of a contrast ratio, so the midpoint of the last
// interval can be a whole step further from the target than one of its own ends.
// Asking which of the two is nearer costs two evaluations and is the difference
// between landing on the ratio and landing beside it.
func closerTo(want float64, at func(float64) (uint8, uint8, uint8), lo, hi float64) (uint8, uint8, uint8) {
	lr, lg, lb := at(lo)
	hr, hg, hb := at(hi)
	if math.Abs(luminanceOf(lr, lg, lb)-want) <= math.Abs(luminanceOf(hr, hg, hb)-want) {
		return lr, lg, lb
	}
	return hr, hg, hb
}

// wantedLuminance is the luminance a colour must carry to sit at the stated
// contrast on the stated side of a ground: away from it, which on a dark
// terminal is up and on a light one is down.
//
// It is allowed to come back out of range, and the caller is expected to notice:
// a ratio a ground cannot reach is exactly the case [reachScale] exists for, and
// silently clamping it here would turn "this ground cannot carry this ladder"
// into "this ladder collapsed into one step" with nothing said.
func wantedLuminance(ground, ratio float64, up bool) float64 {
	if up {
		return (ground+0.05)*ratio - 0.05
	}
	return (ground+0.05)/ratio - 0.05
}

// reachOf is the loudest contrast this ground can carry in this direction: white
// above it, or black below it.
func reachOf(ground float64, up bool) float64 {
	if up {
		return contrastRatio(1, ground)
	}
	return contrastRatio(0, ground)
}

// reachScale is what a MID-TONE GROUND costs, expressed as one number.
//
// A terminal set to a mid grey can carry perhaps five to one in either direction,
// and the reading ladder wants twelve. Clamping each tier to what it can reach
// would answer that by collapsing three tiers onto one value — the body, the
// second voice and the murmur all pinned at the same wall, which is the one
// outcome a ladder may never have. So the whole ladder is SCALED instead: the
// excess above one to one is multiplied down by a single factor, every band
// shrinks by the same proportion, and three tiers that can no longer be far apart
// are at least still three tiers in the right order.
func reachScale(ground, top float64, up bool) float64 {
	reach := reachOf(ground, up)
	if reach >= top || top <= 1 {
		return 1
	}
	if reach <= 1 {
		return 0
	}
	return (reach - 1) / (top - 1)
}

// scaled is one band with [reachScale] applied. The excess above one to one is
// what shrinks, because one to one is invisibility and no scaling may move it.
func (b band) scaled(by float64) band {
	return band{low: 1 + (b.low-1)*by, high: 1 + (b.high-1)*by}
}

// holdInBand is the reading tiers' whole rule: A VALUE IN BAND IS NOT TOUCHED,
// and a value outside it is moved to the NEAREST EDGE and no further.
//
// The nearest edge rather than the middle is the restraint that makes this file
// safe to land. The authored tiers were chosen by people looking at them; where
// the measurement says they are fine, they stand exactly as authored, and where
// it says they are not, they move the least distance that fixes it. A derivation
// that re-aimed every tier at the centre of its band would repaint the surface
// for every person on a terminal that was never broken.
func holdInBand(base hue, ground float64, up bool, at band) hue {
	got := contrastOn(base, ground)
	want := got
	switch {
	case got < at.low:
		want = at.low
	case got > at.high:
		want = at.high
	default:
		return base
	}
	h, s, _ := hslOf(base.r, base.g, base.b)
	lum := wantedLuminance(ground, want, up)
	r, g, b := atLuminance(h, s, clamp01(lum))
	return hue{r: r, g: g, b: b, idx: nearest256(r, g, b), tier: base.tier}
}

// groundStep derives one step of THE GROUND LADDER from the measured ground.
//
// This is the compositor's move styles.go names and could not make: the step is
// the terminal's OWN background, moved away from itself until it sits at the
// stated ratio, so it inherits the theme's hue for free and self-inverts on a
// light terminal with no light branch at all. A blue-black terminal gets a
// blue-black cursor row; a warm cream page gets a warm cream one; a plain black
// screen gets grey, because that is what a plain black screen's own hue is.
//
// WHAT IS HELD IS THE ABSOLUTE TINT, NOT THE SATURATION, and that is the one
// place this function does not do what the reading tiers do. HSL saturation is a
// RATIO to the room a colour has left, so it explodes near black and near white:
// a cream page eight points off the top of the scale is HSL-saturated past 85%,
// and moving its lightness down at that saturation lands on gold. Holding each
// channel's distance from the colour's own mean instead moves a tint sideways
// without amplifying it, so a subtly tinted terminal gets subtly tinted steps and
// a strongly tinted one gets steps exactly as strong as it is. This matters here
// and nowhere else because a ground is the one colour on this surface that must
// never look like it means something.
//
// The index is [nearestGrey256] and not [nearest256], and that is the law the
// GROUND LADDER already states: a ground that rounded into the colour cube would
// be a tint that looked like it meant something, and no step on this ladder means
// anything by itself. Truecolor keeps the terminal's tint; the 256 rung gets the
// grey nearest to it.
func groundStep(m measuredGround, ratio float64, up bool) hue {
	ground := luminanceOf(m.r, m.g, m.b)
	r, g, b := groundAtLuminance(m, clamp01(wantedLuminance(ground, ratio, up)))
	return hue{r: r, g: g, b: b, idx: nearestGrey256(r, g, b), tier: flat}
}

// groundAtLuminance walks the measured ground's own MEAN LEVEL until the colour
// carries the stated relative luminance, holding every channel's distance from
// that mean. See [groundStep] for why the tint is held this way and [atLuminance]
// for why a bisection is the right instrument.
func groundAtLuminance(m measuredGround, want float64) (uint8, uint8, uint8) {
	mean := (float64(m.r) + float64(m.g) + float64(m.b)) / 3
	dr, dg, db := float64(m.r)-mean, float64(m.g)-mean, float64(m.b)-mean
	at := func(level float64) (uint8, uint8, uint8) {
		return byteLevel(level + dr), byteLevel(level + dg), byteLevel(level + db)
	}
	lo, hi := 0.0, 255.0
	for i := 0; i < 24; i++ {
		mid := (lo + hi) / 2
		r, g, b := at(mid)
		if luminanceOf(r, g, b) < want {
			lo = mid
		} else {
			hi = mid
		}
	}
	return closerTo(want, at, lo, hi)
}

// byteLevel rounds a nought-to-255 channel into the byte a terminal is sent,
// clamping rather than wrapping: a tint carried past the end of the scale is a
// tint that has run out of room, not a tint that starts again from black.
func byteLevel(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

// signalLift is the ONE lightness step every signal hue takes together, in HSL
// points, or zero where the set already clears [signalFloor].
//
// ONE STEP FOR THE WHOLE SET, AND THAT IS THE ISOLUMINANT LAW BEING KEPT RATHER
// THAN CHECKED. styles.go holds the signals inside a fifteen-point lightness band
// because a set at one lightness reads as a single quiet field and resolves into
// colours only when somebody looks — so the one move this file may make on them
// is a move that is the same for all seven. A shared step preserves the spread
// exactly, and where the step runs into black or white it can only COMPRESS it,
// never widen it. There is no arrangement of grounds under which deriving breaks
// the band — the only thing that moves it at all is the rounding back onto eight
// bits a channel, which is worth about four tenths of a point across a set of
// seven and is the whole of the slack adaptive_test allows.
//
// The walk is a point at a time rather than a bisection because the answer wanted
// is the SMALLEST step that clears the floor, the set is seven colours, and this
// runs once in the life of a process.
func signalLift(signals []hue, ground float64, up bool) float64 {
	clears := func(by float64) bool {
		for _, h := range signals {
			if contrastOn(liftedBy(h, by, up), ground) < signalFloor {
				return false
			}
		}
		return true
	}
	if clears(0) {
		return 0
	}
	for step := 1.0; step <= 100; step++ {
		if clears(step) {
			return step
		}
	}
	// Nothing clears the floor, which is a ground within a couple of points of
	// the whole signal band. The set goes as far as it can go and the surface's
	// text-side markers — the plus, the cross, the word "waiting" — carry what
	// the hues no longer can, exactly as they do on a sixteen-colour terminal.
	return 100
}

// liftedBy moves one hue's LIGHTNESS by the stated number of points, away from
// the ground: up on a dark terminal, down on a light one.
func liftedBy(base hue, points float64, up bool) hue {
	if points == 0 {
		return base
	}
	h, s, l := hslOf(base.r, base.g, base.b)
	if up {
		l += points / 100
	} else {
		l -= points / 100
	}
	r, g, b := rgbOfHSL(h, s, clamp01(l))
	return hue{r: r, g: g, b: b, idx: nearest256(r, g, b), tier: base.tier}
}

// fadeToward is [fadeOf] and [liftOf] with the anchor MEASURED rather than
// assumed.
//
// Those two composite the dim tier over black and over white respectively,
// because styles.go says in as many words that a terminal will not state its
// background so black is the honest anchor. It states it now. The thinking
// window's gradient therefore ends on the terminal's own background instead of
// near it, which is the difference between a line fading out and a line fading
// into a slightly wrong grey.
func fadeToward(h hue, m measuredGround, pct int) hue {
	mix := func(c, ground uint8) uint8 {
		return uint8((int(c)*pct + int(ground)*(100-pct) + 50) / 100)
	}
	r, g, b := mix(h.r, m.r), mix(h.g, m.g), mix(h.b, m.b)
	return hue{r: r, g: g, b: b, idx: nearest256(r, g, b), tier: h.tier}
}

// nearestGrey256 is the 24-step GREY RAMP entry a derived ground falls on, and
// it is the only index a ground may take. See [groundStep] for why, and THE
// GROUND LADDER in styles.go for the rule it is enforcing.
//
// THE METRIC IS LUMINANCE AND NOT RGB DISTANCE, which is the one place this file
// disagrees with [nearest256]. That function is choosing the nearest COLOUR to a
// pastel and squared distance is the honest answer; this one is choosing a grey
// to stand in for a colour that has already been discarded, and the only thing
// left to preserve is how light the step reads. Squared distance answers that
// question with a weighted average of three channels, so on a saturated ground
// the loudest channel drags the index around and two rungs of a ladder that is
// strictly ordered in lightness can come back out of order or on top of each
// other. Matching lightness cannot: the steps are ordered by luminance by
// construction, so their greys are ordered too.
func nearestGrey256(r, g, b uint8) uint8 {
	want := luminanceOf(r, g, b)
	best, bestOff := 232, math.Inf(1)
	for i := 0; i < 24; i++ {
		v := uint8(8 + 10*i)
		if off := math.Abs(luminanceOf(v, v, v) - want); off < bestOff {
			best, bestOff = 232+i, off
		}
	}
	return uint8(best)
}

// greyApart keeps one derived ground step off the step below it on the 256 rung.
//
// THE THREE STEPS THAT DRAW ARE THREE, and styles.go asserts it of the authored
// ladders for a reason that does not stop being true when the values are
// derived: a person has to be able to tell "the pointer is here" from "this is
// the chosen one" from "you have this marked" without being told which is which.
// The grey ramp is twenty-four entries ten values apart, and on a ground whose
// ladder is compressed two rungs can round onto one of them — so the upper rung
// is nudged one entry further along, in the direction the ladder is travelling.
//
// It moves the INDEX only. The truecolor value is the one that was solved for
// and it stays exactly where the ratio put it; this is the fallback rung
// correcting itself, which is where the collision lives.
func greyApart(step, below uint8, up bool) uint8 {
	if step != below {
		return step
	}
	if up {
		if below < 255 {
			return below + 1
		}
		return step
	}
	if below > 232 {
		return below - 1
	}
	return step
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// ── THE DERIVATION ──────────────────────────────────────────────────────────

// adaptRamp is the whole of what a background reply buys: a measured ground in,
// a finished ladder out.
//
// It is pure, and it is pure on purpose. Every judgement this wave makes about
// colour is in this function and the handful above it, none of them touches the
// app, and the table tests can therefore state a ground and assert a ladder
// rather than driving a surface to find out what it painted.
func adaptRamp(m measuredGround) ramp {
	if groundIsDark(m) {
		return adaptRampFrom(darkRamp, m)
	}
	return adaptRampFrom(lightRamp, m)
}

// adaptRampFrom is [adaptRamp] with the ladder NAMED, which is what a person who
// pinned a theme gets: the measurement still governs every value, but it does not
// get to overrule a person who said "light" out loud (styles.go's THEME SEAM).
func adaptRampFrom(base ramp, m measuredGround) ramp {
	ground := luminanceOf(m.r, m.g, m.b)
	// The direction of travel is the GROUND'S, not the ladder's. Every value
	// below moves AWAY from the background — up out of a dark one, down off a
	// light one — because that is what "readable" means and it is the one thing
	// that does not depend on which ladder somebody picked.
	up := groundIsDark(m)
	out := base

	// THE GROUND LADDER, composited from the terminal's own background, and then
	// held apart on the rung where colours are rounded.
	out.cursor = groundStep(m, groundCursorRatio, up)
	out.selected = groundStep(m, groundSelectedRatio, up)
	out.mark = groundStep(m, groundMarkRatio, up)
	out.selected.idx = greyApart(out.selected.idx, out.cursor.idx, up)
	out.mark.idx = greyApart(out.mark.idx, out.selected.idx, up)

	// THE READING TIERS, held in their bands. The scale is computed once off the
	// top of the ladder so that a mid-tone ground shrinks all three bands by one
	// factor and the ladder stays a ladder — see [reachScale].
	scale := reachScale(ground, inkBand.high, up)
	out.ink = holdInBand(base.ink, ground, up, inkBand.scaled(scale))
	out.muted = holdInBand(base.muted, ground, up, mutedBand.scaled(scale))
	out.dim = holdInBand(base.dim, ground, up, dimBand.scaled(scale))

	// THE SIGNAL HUES, verified and moved only as one. The order the set is
	// gathered in does not matter — the answer is a single step for all of them —
	// but the LIST does: every hue that answers "what KIND of thing is this" is
	// in it, and the reading tiers and the identity ring are not.
	signals := []hue{base.accent, base.add, base.del, base.bad, base.ask, base.warn, base.data}
	if lift := signalLift(signals, ground, up); lift != 0 {
		out.accent = liftedBy(base.accent, lift, up)
		out.add = liftedBy(base.add, lift, up)
		out.del = liftedBy(base.del, lift, up)
		out.bad = liftedBy(base.bad, lift, up)
		out.ask = liftedBy(base.ask, lift, up)
		out.warn = liftedBy(base.warn, lift, up)
		out.data = liftedBy(base.data, lift, up)
	}

	// THE THINKING WINDOW'S GRADIENT FOLLOWS THE DIM TIER, which is the law
	// styles.go states where the stops are derived rather than authored: change
	// the tier and the fade moves with it, or the gradient drifts off the tier it
	// belongs to. It now also ends on the real background rather than on an
	// assumed one.
	out.fade = [3]hue{
		fadeToward(out.dim, m, fadeOldest),
		fadeToward(out.dim, m, fadeMiddle),
		fadeToward(out.dim, m, fadeNewest),
	}

	// THE IDENTITY RING IS NOT DERIVED, and the omission is a decision rather
	// than an oversight. A ring hue means WHICH work a row belongs to and nothing
	// else, so what it owes is DISTINCTNESS around the wheel — and the one move
	// this file makes on a set of hues, a shared lightness step, is the move that
	// pushes six wheel angles toward the same white or the same black. The base
	// ladder's own light/dark choice already puts the ring on the right side of
	// the page, and below the rung where hues survive at all the ring's glyph
	// alphabet carries identity by itself, which is what it was chosen to be able
	// to do (styles.go's THE IDENTITY RING).
	return out
}

// ── THE HOOK ────────────────────────────────────────────────────────────────

// groundReply is the Update arm for [tea.BackgroundColorMsg] — the terminal
// answering the question [app.Init] asked, however many frames later it feels
// like answering, or never.
//
// NEVER IS A SUPPORTED ANSWER AND COSTS NOTHING. There is no timer here, no
// deadline and no fallback path, because the fallback is simply the palette the
// surface has been painting since it started: silence means the authored ladder
// stands, which is the same ladder every wave before this one shipped. That is
// the entire reason this can be an event rather than a query — a question nobody
// is waiting for cannot hang.
//
// A terminal that answers twice with the same colour is answered with nothing:
// rebuilding the ladder is cheap but dropping every cached row is not, and some
// terminals re-report on focus.
func (a *app) groundReply(msg tea.BackgroundColorMsg) tea.Cmd {
	if msg.Color == nil {
		return nil
	}
	// image/color reports sixteen bits a channel, alpha-premultiplied. A
	// background is opaque by construction, so the top byte of each channel is
	// the value a terminal was ever going to send.
	r, g, b, _ := msg.RGBA()
	m := measuredGround{r: uint8(r >> 8), g: uint8(g >> 8), b: uint8(b >> 8)}
	if a.pal.measured && a.pal.ground == m {
		return nil
	}
	a.pal.ground, a.pal.measured = m, true
	if a.pal.pin == themeAuto {
		a.pal.ramp = adaptRamp(m)
	} else {
		a.pal.ramp = adaptRampFrom(rampFor(a.pal.pin, nil), m)
	}
	a.repaintPalette()
	return nil
}

// repaintPalette drops every row this surface has painted and kept.
//
// It is the resize path's discipline aimed at a different fact. A cached row is
// a finished string with escape sequences already inside it, so a palette that
// changed under one is a row that will keep drawing yesterday's colours until
// something else happens to make it stale — and on a transcript that is scrolled
// back through, "something else" may be never. Three caches hold painted text and
// all three are named here rather than trusted to expire:
//
//   - every entry on every deck reachable right now (render.go's build/width/
//     stale key). The conversation, the open room's page, and a run's journal are
//     three separate lists and a person can be looking at any of them
//   - the highlighted source blocks (codeview.go), which are painted rows keyed
//     by text and width and nothing else
//   - the laid-out screen list itself, dropped by width so the next frame
//     rebuilds unconditionally
func (a *app) repaintPalette() {
	stale := func(entries []entry) {
		for i := range entries {
			entries[i].stale = true
		}
	}
	stale(a.entries)
	if a.room != nil {
		stale(a.room.entries)
		if a.room.orch != nil {
			stale(a.room.orch.journal)
		}
	}
	a.codeCache.drop()
	a.rows, a.rowsWidth = nil, 0
	a.hudStale = true
	a.touch()
}
