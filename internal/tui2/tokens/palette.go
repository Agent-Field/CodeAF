package tokens

// The palette: pastel semantics on a dark ground (5.16), the three-tier grey
// ramp (5.13), and the selection band (5.16). Every value below is a literal so
// the whole palette can be read at once; every DERIVED value (dimmed variants,
// identity-tinted bands) is additionally re-derived from its base by
// palette_test.go through [Mix], so a hand-edited literal that stops obeying
// the derivation rule fails the build.
//
// Base ground and the derivation constants
// ----------------------------------------
// The dark ground is near-black with a trace of blue so the pastels read as
// warm against it; the band is the same ground raised one step.
const (
	// DimTowardGround is how far a token's dimmed variant travels toward the
	// ground. 0.45 is the point where the whole pane visibly recedes while
	// every body-class token still clears 3.0 contrast (contrast_test.go).
	DimTowardGround = 0.45

	// BandIdentityTint is how much identity hue the selection band takes when
	// the user is inside that task's scope (5.16: "the selection band tint
	// inside that task's scope"). 0.08 is the largest tint at which the chrome
	// tier still clears 3.0 on the tinted band — the tint is meant to be
	// answered peripherally, never noticed.
	BandIdentityTint = 0.08

	// SheetTowardBand is how far the floating dialog's own ground travels from
	// the [Ground] toward the [Band] — the one derivation that makes the
	// elevation ladder ground → sheet → band monotone by construction rather
	// than by three hand-picked literals.
	//
	// 0.45 is chosen by the two ends it has to satisfy at once, and both are
	// tight: below about 0.35 the sheet resolves to the SAME xterm-256
	// greyscale entry as the ground (the cube is coarse in this corner — see
	// [Sheet]) and the elevation disappears on the majority profile; above 0.5
	// the band it carries stops reading as raised against it. At 0.45 the sheet
	// is 1.09 over the ground and the band is 1.14 over the sheet, and the
	// three rungs land on three different 256 entries in the right order.
	SheetTowardBand = 0.45
)

// Base values. These are the only hand-authored colors in the package.
var (
	groundBase = MustHex("#12121A") // the default dark ground
	bandBase   = MustHex("#262633") // selection band: the ground raised one step
	// sheetBase is DERIVED, not authored: see [SheetTowardBand]. It is written
	// here beside its neighbours so the whole ladder can be read at once, and
	// palette_test.go re-derives it, so the literal and the law cannot drift.
	sheetBase = Mix(groundBase, bandBase, SheetTowardBand) // #1B1B25

	// Three-tier grey ramp (5.13): primary speech and titles, secondary status
	// lines and receipts, tertiary telemetry.
	textPrimaryBase   = MustHex("#E6E6F0")
	textSecondaryBase = MustHex("#A0A6BB")
	textTertiaryBase  = MustHex("#7C8296")

	// The four semantic hues (5.16). Hue angles: coral 8°, amber 38°,
	// green 145°, cyan 196°. They are more saturated than the identity wheel
	// on purpose — meaning is louder than identity (palette_test.go asserts it).
	amberBase = MustHex("#EECE96") // needs a human
	cyanBase  = MustHex("#A4D7EA") // alive
	greenBase = MustHex("#A2E2BC") // money + success
	coralBase = MustHex("#EFA99F") // broken

	// The 8-hue identity wheel (5.16). Hue angles 70, 108, 170, 225, 258, 288,
	// 318, 345 — chosen to stay at least 20° off every semantic hue and at
	// least 25° from each other, so no identity color can be misread as a
	// state color (palette_test.go asserts both distances).
	identityBase = [IdentityCount]Color{
		MustHex("#DDE6B3"), // 0 · lime
		MustHex("#BDE6B3"), // 1 · leaf
		MustHex("#B3E6DD"), // 2 · teal
		MustHex("#BDC7E5"), // 3 · blue
		MustHex("#C9BDE5"), // 4 · indigo
		MustHex("#DBB3E6"), // 5 · violet
		MustHex("#E6B3D6"), // 6 · orchid
		MustHex("#E6B3BF"), // 7 · rose
	}
)

// IdentityCount is the size of the identity wheel. Eight is enough that no two
// adjacent rail cards ever need to share (the assignment in identity.go proves
// it) and few enough that each hue stays distinguishable.
const IdentityCount = 8

// Token names every color the surface may draw. It is a dense small integer so
// tables can be indexed by it and a Token can live in a struct field for free.
type Token uint8

// The token inventory. Order is load-bearing only for Identity0..Identity7 and
// BandIdentity0..BandIdentity7, which are contiguous so a wheel index can be
// added to the first member.
const (
	TextPrimary   Token = iota // tier 1: speech, titles
	TextSecondary              // tier 2: status lines, receipts
	TextTertiary               // tier 3: telemetry (and interactive chips at rest)

	Amber // needs a human: question badges, waiting states, ctx meter near limit
	Cyan  // alive: working glyphs, stream caret, thinking pulse
	Green // money + success: cost figures, settled ✓
	Coral // broken: failures, cancels

	Identity0
	Identity1
	Identity2
	Identity3
	Identity4
	Identity5
	Identity6
	Identity7

	Ground // the default dark ground
	Band   // selection band background (5.16: selection is a band, not a color)
	// Sheet is a floating dialog's OWN ground — the palette, the `?` capability
	// surface, the settings sheet, the consent dialog, and the one-cell margin
	// 12.11 rules top and bottom.
	//
	// It exists because 12.11's boundary was drawn and never painted, and an
	// unpainted panel has no ground: it inherits whatever the terminal's
	// default background is, which is the same nothing the transcript behind it
	// inherits. Two rooms with the same floor read as one room, which is 12.13's
	// wall finding arriving on the other axis — there the fix was a column of
	// ground, here it is a PLANE of it.
	//
	// It is deliberately the smallest step that survives every profile rather
	// than the largest step that looks impressive: 5.13 spends the structure
	// budget on whitespace and hairlines, and a dialog that announced itself
	// with a loud slab would be the box 5.21 refuses wearing a background.
	// [Profile.SheetGround] is the one place that says where it may be drawn at
	// all — at 16 colours the only raised background is bright black, which is
	// the user's theme's to define, so the sheet keeps its hairline boundary
	// and paints no ground there.
	Sheet

	BandIdentity0 // selection band tinted with identity 0..7, used inside that
	BandIdentity1 // task's scope so "which room am I in" is answered
	BandIdentity2 // peripherally
	BandIdentity3
	BandIdentity4
	BandIdentity5
	BandIdentity6
	BandIdentity7

	tokenCount
)

// Class is the contrast contract a token signs. The gate in
// [Class.MinContrast] is what contrast_test.go enforces over [Pairings].
type Class uint8

const (
	// ClassBody is text and glyphs that carry meaning: the primary tier and
	// every semantic and identity hue. WCAG AA for body text.
	ClassBody Class = iota
	// ClassSupport is the secondary tier — status lines and receipts. Same
	// gate as body: it is still prose a person reads.
	ClassSupport
	// ClassChrome is the tertiary tier: telemetry, separators, hints. Gated at
	// the AA large-text / non-text-component ratio, because it is scanned, not
	// read, and 5.13 requires it to recede. Chrome that is also a button
	// brightens one tier on focus (5.22) — see [Promote] — so no interactive
	// control ever lives permanently at this gate.
	ClassChrome
	// ClassSurface is a background. Surfaces are gated on separation from the
	// ground instead of on contrast (see [BandSeparationMin]).
	ClassSurface
)

// MinContrast is the shipping gate for a class in a focus state.
//
// Base values must clear AA (4.5) as body text; chrome clears the 3.0 gate
// WCAG uses for large text and non-text UI components. Dimmed values mark a
// pane the user is deliberately not reading, so they are gated one rung lower —
// with the standing rule that a dimmed token is never the only carrier of a
// piece of information (the row still has its shape, its glyph, and its place).
func (c Class) MinContrast(f Focus) float64 {
	switch c {
	case ClassBody, ClassSupport:
		if f == FocusDimmed {
			return 3.0
		}
		return 4.5
	case ClassChrome:
		if f == FocusDimmed {
			return 2.0
		}
		return 3.0
	}
	return 0 // ClassSurface is never a foreground
}

// BandSeparationMin and BandSeparationMax bound how far the selection band may
// sit from the ground. Below the minimum the band is invisible and selection
// stops reading; above the maximum it becomes a box, which 5.13 forbids
// ("cards separated by whitespace not boxes").
const (
	BandSeparationMin = 1.15
	BandSeparationMax = 1.70
)

// SheetSeparationMin is the smallest step at which one plane reads as another
// plane on this ground, and it is the gate BOTH rungs of the dialog's ladder
// sign: the [Sheet] over the [Ground], and the [Band] over the Sheet.
//
// It is lower than [BandSeparationMin] on purpose. A band is a mark the eye
// must FIND — it says which of twenty rows the keyboard is on, unaided. A plane
// is a mark the eye only has to BELIEVE: it is bounded by 12.11's hairline, it
// is a rectangle of hundreds of cells rather than one row, and an edge between
// two large fields is visible far below the ratio a small mark needs. Holding a
// sheet to the band's floor would spend the whole ground→band range on the
// first rung and leave the selection nothing to be raised above.
//
// The band on a sheet keeps its own second carrier regardless: every list on
// these surfaces draws [GlyphAccentRail] on the selected row as well, which is
// 12.11.2's ruling that a state colour carries applied one plane up.
const SheetSeparationMin = 1.08

// Focus is whether the pane owning a row currently has the user's attention.
// Dimming is a property of the pane (8.3: "dim/tint global chrome while scoped
// so you always know which room you're in"), never of the datum.
type Focus uint8

const (
	FocusNormal Focus = iota // the pane the user is in
	FocusDimmed              // an unfocused pane
	focusCount
)

// entry is one row of the palette. The derived fields (idx256, sgrFg, sgrBg)
// are filled once by buildTable; nothing here mutates afterwards.
type entry struct {
	name  string
	class Class
	color [focusCount]Color
	// ansi16 is a CURATED mapping, not a nearest-color computation: at 16
	// colors the only honest move is to keep the hue family and let the
	// terminal's own theme supply the shade (see remap.go for the same
	// argument applied to tool output). Dimming at 16 colors is the drop from
	// the bright family to the standard family of the same hue.
	ansi16 [focusCount]uint8
	idx256 [focusCount]uint8
	sgrFg  [profileCount][focusCount]string
	sgrBg  [profileCount][focusCount]string
}

// table is built exactly once, by one function, from the literals above. No
// init function and no cross-file initialization order to reason about: Go
// initializes this var by evaluating buildTable, and buildTable reads only
// package-level literals declared in this file.
var table = buildTable()

func buildTable() [tokenCount]entry {
	var t [tokenCount]entry

	// dimmed derives every dimmed variant by the one rule (5.16: the palette
	// ships as named tokens with tested pairs).
	dimmed := func(c Color) Color { return Mix(c, groundBase, DimTowardGround) }

	set := func(tok Token, name string, class Class, base Color, dim Color, bright, standard uint8) {
		t[tok] = entry{
			name:   name,
			class:  class,
			color:  [focusCount]Color{base, dim},
			ansi16: [focusCount]uint8{bright, standard},
		}
	}

	set(TextPrimary, "text.primary", ClassBody, textPrimaryBase, dimmed(textPrimaryBase), 15, 7)
	set(TextSecondary, "text.secondary", ClassSupport, textSecondaryBase, dimmed(textSecondaryBase), 7, 8)
	set(TextTertiary, "text.tertiary", ClassChrome, textTertiaryBase, dimmed(textTertiaryBase), 8, 8)

	set(Amber, "amber", ClassBody, amberBase, dimmed(amberBase), 11, 3)
	set(Cyan, "cyan", ClassBody, cyanBase, dimmed(cyanBase), 14, 6)
	set(Green, "green", ClassBody, greenBase, dimmed(greenBase), 10, 2)
	set(Coral, "coral", ClassBody, coralBase, dimmed(coralBase), 9, 1)

	// Identity hues map to the nearest of the six chromatic bright slots by hue
	// angle. Eight into six means identity collapses at 16 colors — see
	// [Profile.IdentityDistinct], which tells the shell to stop drawing
	// identity accents rather than draw two neighbours the same.
	identity16 := [IdentityCount][2]uint8{
		{11, 3}, // 0 lime   → yellow
		{10, 2}, // 1 leaf   → green
		{14, 6}, // 2 teal   → cyan
		{12, 4}, // 3 blue   → blue
		{12, 4}, // 4 indigo → blue
		{13, 5}, // 5 violet → magenta
		{13, 5}, // 6 orchid → magenta
		{9, 1},  // 7 rose   → red
	}
	for i := range IdentityCount {
		base := identityBase[i]
		set(Identity0+Token(i), "identity."+string(rune('0'+i)), ClassBody,
			base, dimmed(base), identity16[i][0], identity16[i][1])
	}

	// The ground does not dim: it is the thing everything else is dimmed
	// toward. Giving it a dimmed variant would mean the floor moved, and every
	// contrast number in this package is stated against a floor that holds.
	set(Ground, "ground", ClassSurface, groundBase, groundBase, 0, 0)
	set(Band, "band", ClassSurface, bandBase, dimmed(bandBase), 8, 0)
	// The sheet's 16-colour value is black for completeness and is never drawn:
	// [Profile.SheetGround] refuses the profile before a renderer can ask for
	// it, for the reason [Profile.SelectionStyle] refuses the band there.
	set(Sheet, "sheet", ClassSurface, sheetBase, dimmed(sheetBase), 0, 0)
	for i := range IdentityCount {
		tint := Mix(bandBase, identityBase[i], BandIdentityTint)
		set(BandIdentity0+Token(i), "band.identity."+string(rune('0'+i)), ClassSurface,
			tint, dimmed(tint), 8, 0)
	}

	// Derived resolutions: the 256-color index and the precomputed SGR strings
	// for every profile and focus. Doing it here means a render never formats
	// an escape sequence — it appends a constant string.
	//
	// The 256-color resolution runs in TWO passes, and the second one exists
	// because a naive nearest-neighbour walk measurably breaks 5.16. The xterm
	// cube is coarse in exactly the pastel corner this palette lives in: taken
	// independently, identity.1 lands on the same cube entry as GREEN and
	// identity.2 on the same entry as CYAN, so a 256-color terminal would draw
	// a task's identity accent in the colour that means "success" or "alive".
	// That is not a degradation, it is the vocabulary collapsing — the exact
	// failure the hue-separation test forbids at the source values.
	//
	// So: every other token resolves to its nearest entry first and CLAIMS it,
	// and the identity wheel then resolves to the nearest entry that is not
	// already claimed and is a visible step away from every claim. Meaning is
	// served first and identity absorbs the approximation, which is the right
	// way round — a slightly-off pastel still says "this task", while a
	// perfectly-accurate one that reads as green says something false.
	var claimed [focusCount][]uint8
	for i := range t {
		if tok := Token(i); tok >= Identity0 && tok <= Identity7 {
			continue
		}
		e := &t[i]
		for f := Focus(0); f < focusCount; f++ {
			e.idx256[f] = nearest256(e.color[f])
			claimed[f] = append(claimed[f], e.idx256[f])
		}
	}
	for i := range IdentityCount {
		e := &t[Identity0+Token(i)]
		for f := Focus(0); f < focusCount; f++ {
			e.idx256[f] = nearestDistinct256(e.color[f], claimed[f])
			claimed[f] = append(claimed[f], e.idx256[f])
		}
	}
	for i := range t {
		e := &t[i]
		for f := Focus(0); f < focusCount; f++ {
			for p := Profile(0); p < profileCount; p++ {
				e.sgrFg[p][f] = sgrString(p, e, f, layerFg)
				e.sgrBg[p][f] = sgrString(p, e, f, layerBg)
			}
		}
	}
	return t
}

// Color returns the token's value in a focus state.
func (t Token) Color(f Focus) Color { return table[t.check()].color[f.check()] }

// Hex returns the token's value as "#RRGGBB" — the form lipgloss and the
// golden-test harness read.
func (t Token) Hex(f Focus) string { return t.Color(f).Hex() }

// Class returns the contrast contract this token signs.
func (t Token) Class() Class { return table[t.check()].class }

// String is the token's stable name ("amber", "text.tertiary",
// "band.identity.3"). Golden tests key on these, so they are part of the API.
func (t Token) String() string { return table[t.check()].name }

// IsSurface reports whether the token is a background rather than a
// foreground. Surfaces have no contrast gate of their own.
func (t Token) IsSurface() bool { return t.Class() == ClassSurface }

func (t Token) check() Token {
	if t >= tokenCount {
		panic("tokens: invalid Token")
	}
	return t
}

func (f Focus) check() Focus {
	if f >= focusCount {
		panic("tokens: invalid Focus")
	}
	return f
}

// All returns every token in inventory order. Tests, the golden harness, and a
// palette-preview screen all want to walk the whole set.
func All() []Token {
	out := make([]Token, 0, tokenCount)
	for t := Token(0); t < tokenCount; t++ {
		out = append(out, t)
	}
	return out
}

// Identity returns the identity token for a wheel index, wrapping. Callers
// normally get one from [IdentityFor] rather than by index.
func Identity(i int) Token {
	return Identity0 + Token(((i%IdentityCount)+IdentityCount)%IdentityCount)
}

// BandFor returns the identity-tinted selection band for an identity token.
// Passing anything that is not an identity token returns the plain [Band] —
// the home scope has no identity, and its selection is untinted.
func BandFor(identity Token) Token {
	if identity < Identity0 || identity > Identity7 {
		return Band
	}
	return BandIdentity0 + (identity - Identity0)
}

// IdentityIndex returns the wheel index of an identity or identity-band token,
// and false for anything else.
func IdentityIndex(t Token) (int, bool) {
	switch {
	case t >= Identity0 && t <= Identity7:
		return int(t - Identity0), true
	case t >= BandIdentity0 && t <= BandIdentity7:
		return int(t - BandIdentity0), true
	}
	return 0, false
}

// Pairing is one legal (foreground, ground) combination — a combination the
// surface is allowed to draw, and therefore one the contrast gate must cover.
type Pairing struct {
	Fg          Token
	FgFocus     Focus
	Ground      Token
	GroundFocus Focus
}

// Legal states the composition law. There are exactly three rules, and each
// one is a design decision, not an accident of what happened to pass:
//
//  1. A surface token is never a foreground, and a non-surface token is never a
//     ground. Backgrounds and text are different vocabularies.
//  2. A focused foreground may sit on the ground, on the [Sheet], on the plain
//     band, or on any identity-tinted band. Every combination is drawn in
//     practice — any tier of text can land on a selected rail row, and the
//     whole grey ramp lands on a dialog's own ground — and all are gated.
//  3. A DIMMED foreground may sit only on the ground. This is the design law
//     that keeps the dim state honest: an unfocused pane draws no selection
//     band at all — it marks its selection with the dim identity accent rail
//     ▎ (5.21) — because a band is a focus artifact, and a dimmed pastel on a
//     raised background is the one combination in this palette that cannot
//     clear its gate. Forbidding it is cheaper and truer than brightening
//     every dim value until an invisible pane stops being invisible.
func Legal(fg Token, ff Focus, ground Token, gf Focus) bool {
	if fg.IsSurface() || !ground.IsSurface() {
		return false
	}
	if gf != FocusNormal {
		return false
	}
	if ff == FocusDimmed {
		return ground == Ground
	}
	return true
}

// Pairings enumerates every legal combination, in a stable order. The contrast
// test walks this list; a pairing missing from it is a pairing the surface may
// not draw, not a pairing that escaped review.
func Pairings() []Pairing {
	out := make([]Pairing, 0, 64)
	for fg := Token(0); fg < tokenCount; fg++ {
		for ff := Focus(0); ff < focusCount; ff++ {
			for g := Token(0); g < tokenCount; g++ {
				for gf := Focus(0); gf < focusCount; gf++ {
					if Legal(fg, ff, g, gf) {
						out = append(out, Pairing{fg, ff, g, gf})
					}
				}
			}
		}
	}
	return out
}

// Contrast returns the contrast ratio a pairing actually achieves.
func (p Pairing) Contrast() float64 {
	return Contrast(p.Fg.Color(p.FgFocus), p.Ground.Color(p.GroundFocus))
}

// Min returns the ratio this pairing must achieve to ship.
func (p Pairing) Min() float64 { return p.Fg.Class().MinContrast(p.FgFocus) }
