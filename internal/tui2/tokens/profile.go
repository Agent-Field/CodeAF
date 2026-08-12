package tokens

import "strings"

// Terminal capability and degradation (10.1.2).
//
// A token has one value; a terminal has one of four vocabularies for saying it.
// Resolution is parameterized by capability rather than detected inside the
// render, so the golden harness can render every width at every profile and a
// screenshot test can prove the 256-color degradation looks like a palette
// rather than like mud.

// Profile is the color vocabulary a terminal actually has, ordered by
// capability so `p >= ANSI256` is a meaningful question.
type Profile uint8

const (
	// NoColor emits no SGR color at all: NO_COLOR, TERM=dumb, or a pipe. The
	// surface must remain fully legible here — every state that color carries
	// also has a glyph (5.17), which is why this profile is a degradation and
	// not a failure.
	NoColor Profile = iota
	// ANSI16 is the classic sixteen. Our pastels land in the bright family and
	// the terminal's own theme supplies the actual shade, so identity hues
	// collapse (see [Profile.IdentityDistinct]).
	ANSI16
	// ANSI256 is the xterm cube. Every token maps to a distinct index, and the
	// mapping deliberately avoids indices 0-15 because those are whatever the
	// user's theme says they are.
	ANSI256
	// TrueColor is 24-bit direct color: the palette exactly as authored.
	TrueColor
	profileCount
)

// String names the profile. These are also the spellings [ParseProfile]
// accepts, so a settings row and a log line use one vocabulary.
func (p Profile) String() string {
	switch p {
	case NoColor:
		return "none"
	case ANSI16:
		return "16"
	case ANSI256:
		return "256"
	case TrueColor:
		return "truecolor"
	}
	return "invalid"
}

// IdentityDistinct reports whether the identity wheel survives this profile.
// At 16 colors eight identity hues collapse onto six chromatic slots, so the
// 5.16 promise — no two adjacent rail cards share an identity color — cannot be
// kept. A shell that asks this and gets false must stop drawing identity
// accents entirely rather than draw two neighbours the same color: a lying
// identity is worse than no identity (5.20, the affordance never lies).
func (p Profile) IdentityDistinct() bool { return p >= ANSI256 }

// BandTintDistinct reports whether the identity TINT on the selection band
// survives this profile. It is a separate question from [IdentityDistinct] and
// the answer is different: the tint is 8% of a hue mixed into a near-black
// ground (5.16 wants it "answered peripherally, never noticed"), and no
// 256-palette entry is that close to another. All eight tinted bands resolve to
// the same grey below truecolor.
//
// A shell that asks and gets false should draw the plain [Band] rather than
// [BandFor]: the two produce identical bytes there, so the difference is only
// wasted work — but a shell that believed the tint was showing would also
// believe the room was announced, and it would not be.
func (p Profile) BandTintDistinct() bool { return p >= TrueColor }

// SelectionStyle says how selection must be drawn under this profile, and it is
// the ONE place that question is answered — a renderer asks here and never
// decides for itself, so the three answers cannot drift apart.
//
// 5.16 wants a background band. At 16 colors the only available "raised
// background" is bright black, which is a different shade in every theme and can
// land on top of the text tier; reverse video is the honest fallback there,
// because it is defined relative to whatever the terminal's own foreground and
// background are. At [NoColor] there is no SGR to spend at all — the profile is
// NO_COLOR, TERM=dumb, or a pipe — and a selection drawn in bytes that must not
// be written is a selection nobody can see.
//
// So the degradation ladder ends where the ladder for every other state ends:
// "every state that color carries also has a glyph (5.17), which is why this
// profile is a degradation and not a failure" (see [NoColor]). Under
// [SelectionMarker] the cursor is carried by a CHARACTER — the ▎ accent rail
// (5.21: "structure without boxes"), in the gutter the map renderings already
// reserve. That is not a new idiom either: it is exactly what an unfocused pane
// already draws, because [Legal] forbids a dimmed foreground on a raised band,
// so the marker path was built and tested before this profile needed it.
type SelectionStyle uint8

const (
	SelectionBand    SelectionStyle = iota // draw [Band] / [BandFor] as a background
	SelectionReverse                       // SGR 7; no band token is used
	// SelectionMarker draws no ground at all: the row is marked with
	// [GlyphAccentRail] in the gutter. It is the only answer that costs zero
	// escape bytes, which is what makes it the right one for a profile defined
	// by having none to spend.
	SelectionMarker
)

// SheetGround reports whether a floating dialog may paint [Sheet] as its own
// ground under this profile. It is the same question [SelectionStyle] answers
// about the band, and it has the same answer for the same reason: below 256
// colours there is no raised background this palette owns. At [ANSI16] the only
// candidate is bright black — a different shade in every theme, and the one the
// chrome tier already lives in — so a sheet painted there would either vanish
// into the text or into the terminal's own background, depending on a setting
// we do not control. At [NoColor] there are no bytes to spend at all.
//
// The dialog does not lose its boundary when this is false: 12.11's boundary is
// a one-cell margin ruled top and bottom, and the rule is a CHARACTER. The
// ground is the enhancement; the hairline is the floor.
func (p Profile) SheetGround() bool { return p >= ANSI256 }

// SelectionStyle returns the selection idiom available under this profile.
func (p Profile) SelectionStyle() SelectionStyle {
	switch {
	case p >= ANSI256:
		return SelectionBand
	case p == NoColor:
		return SelectionMarker
	default:
		return SelectionReverse
	}
}

type layer uint8

const (
	layerFg layer = iota
	layerBg
	// layerUl is the UNDERLINE colour (SGR 58), which is a third layer and not
	// a variant of the foreground: a word may carry a coloured rule without
	// changing tier. See [Token.UnderlineColor].
	layerUl
)

// Fg returns the precomputed SGR sequence that sets this token as the
// foreground under a profile and focus. It is a constant string built once at
// package initialization: a render appends it, never formats it. NoColor
// returns "".
func (t Token) Fg(p Profile, f Focus) string {
	return table[t.check()].sgrFg[p.check()][f.check()]
}

// Bg is [Token.Fg] for the background. Only surface tokens are normally drawn
// this way, but the table is complete so a preview screen can show any token
// as a swatch.
func (t Token) Bg(p Profile, f Focus) string {
	return table[t.check()].sgrBg[p.check()][f.check()]
}

// Index returns the palette index this token resolves to under a profile: an
// ANSI-16 index for [ANSI16], an xterm-256 index for [ANSI256]. It is
// meaningless for [TrueColor] and [NoColor], which return 0 — callers wanting
// exact color ask [Token.Color].
func (t Token) Index(p Profile, f Focus) uint8 {
	e := &table[t.check()]
	switch p {
	case ANSI16:
		return e.ansi16[f.check()]
	case ANSI256:
		return e.idx256[f.check()]
	}
	return 0
}

func (p Profile) check() Profile {
	if p >= profileCount {
		panic("tokens: invalid Profile")
	}
	return p
}

// ResetFg, ResetBg, and Reset are the counterparts to [Token.Fg] and
// [Token.Bg]. Reset clears every attribute; the narrower two exist because a
// row that only set a foreground should not also clear a caller's bold.
func ResetFg(p Profile) string {
	if p == NoColor {
		return ""
	}
	return "\x1b[39m"
}

// ResetBg clears the background only.
func ResetBg(p Profile) string {
	if p == NoColor {
		return ""
	}
	return "\x1b[49m"
}

// Reset clears all SGR attributes.
func Reset(p Profile) string {
	if p == NoColor {
		return ""
	}
	return "\x1b[0m"
}

// GroundResets is every sequence this package writes that also clears the
// BACKGROUND, and therefore every place a caller who asserted a ground at the
// head of a row has to assert it again.
//
// It exists because a surface that paints its own floor — the composer hug's two
// grounds, a card's ground, any fixed chrome a scroll passes under — composes
// rows out of spans painted by OTHER packages through this package's ordinary
// doors, and one of those doors ([Styler.PaintOn], the selection band and the
// caret cell) ends its span by clearing both layers. A floor asserted once at
// column 0 stops at the first such span and the rest of the row falls back to
// the terminal's own background, which reads as the strip ending halfway across.
//
// The list belongs HERE rather than restated at each caller for the reason every
// other spelling in this package does: it is a fact about what this package
// emits, and a restatement is a fact that goes stale on a commit that never
// touched the file it lives in. [Styler.PaintToken] is deliberately absent — it
// resets the foreground alone, so a run painted with it keeps whatever ground it
// was drawn over, which is what makes the ordinary row free.
//
// A profile with no colour writes nothing at all, so there is nothing to repair.
func GroundResets(p Profile) []string {
	if p == NoColor {
		return nil
	}
	return []string{sgrResetAll, Reset(p)}
}

// Reverse is SGR 7, the selection idiom for [SelectionReverse].
func Reverse(p Profile) string {
	if p == NoColor {
		return ""
	}
	return "\x1b[7m"
}

// Underline is SGR 4, and [UnderlineOff] is SGR 24.
//
// It is the quietest mark the vocabulary has for "this one, of several" — a rule
// UNDER a word rather than a ground behind it — and it exists here for the same
// reason [Reverse] does: an attribute a surface would otherwise spell as a
// literal escape is an attribute no profile can degrade.
//
// [NoColor] emits NOTHING, including the underline. That profile's promise is
// not "no colour", it is NO ESCAPES: its consumers are a dumb pipe and a golden
// file, and both read a stray SGR as corruption. A surface that needs the
// distinction there says it in words.
func Underline(p Profile) string {
	if p == NoColor {
		return ""
	}
	return "\x1b[4m"
}

// UnderlineOff is SGR 24. It clears the underline WITHOUT clearing colour, so a
// span that underlined one word does not have to repaint the tier of the next.
func UnderlineOff(p Profile) string {
	if p == NoColor {
		return ""
	}
	return "\x1b[24m"
}

// UnderlineColor is SGR 58: the underline's own colour, so a bright word can
// carry a coloured rule without the word itself changing tier.
//
// It returns "" where the profile has no honest form for it — [NoColor], and
// [ANSI16], where the only 58 form is the 256-colour one and a 16-colour
// terminal's palette is the user's theme to define. Both fall back to a plain
// [Underline], which is the widely-supported half of the pair; a caller writes
// the two together and gets whatever the terminal can carry.
func (t Token) UnderlineColor(p Profile, f Focus) string {
	return table[t.check()].sgrUl[p.check()][f.check()]
}

// UnderlineColorOff is SGR 59, which returns the underline to the foreground
// colour. It is written beside [UnderlineOff] rather than instead of it: a
// terminal that ignored 58 also ignores 59, and one that honoured 58 would carry
// the colour into the next underlined run if nobody cleared it.
func UnderlineColorOff(p Profile) string {
	if p == NoColor {
		return ""
	}
	return "\x1b[59m"
}

func sgrString(p Profile, e *entry, f Focus, l layer) string {
	switch p {
	case NoColor:
		return ""
	case ANSI16:
		if l == layerUl {
			// See [Token.UnderlineColor]: no honest 16-colour form.
			return ""
		}
		idx := e.ansi16[f]
		var base int
		switch {
		case l == layerFg && idx < 8:
			base = 30 + int(idx)
		case l == layerFg:
			base = 90 + int(idx) - 8
		case idx < 8:
			base = 40 + int(idx)
		default:
			base = 100 + int(idx) - 8
		}
		return "\x1b[" + itoa(base) + "m"
	case ANSI256:
		lead := "\x1b[38;5;"
		switch l {
		case layerBg:
			lead = "\x1b[48;5;"
		case layerUl:
			lead = "\x1b[58;5;"
		}
		return lead + itoa(int(e.idx256[f])) + "m"
	default:
		c := e.color[f]
		lead := "\x1b[38;2;"
		switch l {
		case layerBg:
			lead = "\x1b[48;2;"
		case layerUl:
			lead = "\x1b[58;2;"
		}
		return lead + itoa(int(c.R)) + ";" + itoa(int(c.G)) + ";" + itoa(int(c.B)) + "m"
	}
}

func itoa(n int) string {
	var buf [4]byte
	return string(appendInt(buf[:0], int64(n)))
}

// cube256 are the six channel levels of the xterm 6×6×6 color cube.
var cube256 = [6]uint8{0, 95, 135, 175, 215, 255}

// color256 returns the sRGB value xterm renders for a 256-palette index in the
// portion of the palette that is fixed by the standard: the 6×6×6 cube
// (16-231) and the 24-step grey ramp (232-255). Indices 0-15 are excluded on
// purpose — they are whatever the user's theme sets, so mapping a token onto
// one would hand our palette to a stranger.
func color256(i int) Color {
	switch {
	case i >= 232:
		v := uint8(8 + (i-232)*10)
		return Color{v, v, v}
	case i >= 16:
		i -= 16
		return Color{cube256[i/36], cube256[(i/6)%6], cube256[i%6]}
	}
	return Color{}
}

func nearest256(c Color) uint8 {
	best, bestDist := 16, 1<<30
	for i := 16; i < 256; i++ {
		if d := distance(c, color256(i)); d < bestDist {
			best, bestDist = i, d
		}
	}
	return uint8(best)
}

// minSeparation256 is how far apart two 256-palette entries must sit before the
// eye will call them different colours. The cube's coarsest step is 40 in one
// channel (175 → 215 → 255), which [distance] scores at 3200 on red, 6400 on
// green and 4800 on blue — so a threshold of 3000 means "at least one full cube
// step in at least one channel". Below that, two entries are the same colour
// wearing different numbers.
const minSeparation256 = 3000

// nearestDistinct256 is [nearest256] constrained to entries that are visibly
// different from every already-claimed index. It is how the identity wheel
// survives a 256-color terminal (see buildTable for why it must).
//
// If no candidate clears the separation — which cannot happen for this palette,
// and is asserted by profile_test.go — it falls back to the plain nearest
// entry rather than returning nothing. A slightly-wrong colour is a
// degradation; no colour is a bug.
func nearestDistinct256(c Color, claimed []uint8) uint8 {
	floor := chroma(c) / 2
	best, bestDist := -1, 1<<30
	fallback, fallbackDist := 16, 1<<30
	for i := 16; i < 256; i++ {
		cand := color256(i)
		d := distance(c, cand)
		if d < fallbackDist {
			fallback, fallbackDist = i, d
		}
		if chroma(cand) < floor || crowded256(cand, claimed) {
			continue
		}
		if d < bestDist {
			best, bestDist = i, d
		}
	}
	if best < 0 {
		return uint8(fallback)
	}
	return uint8(best)
}

// chroma is how colourful a value is: the spread between its brightest and
// dimmest channel. [nearestDistinct256] uses it as a FLOOR, and the reason is
// specific to what an identity accent is for.
//
// Plain RGB distance is happy to answer a desaturated blue with a grey of the
// same lightness — the grey is genuinely nearer in the cube. For a semantic hue
// that would be an acceptable approximation of brightness; for an identity
// accent it is a total loss, because the accent's entire job is to be a HUE the
// eye recognizes across a rail. So a candidate must keep at least half the
// target's colourfulness, and the walk gives up lightness accuracy to hold it.
func chroma(c Color) int {
	hi, lo := int(c.R), int(c.R)
	for _, v := range [2]int{int(c.G), int(c.B)} {
		if v > hi {
			hi = v
		}
		if v < lo {
			lo = v
		}
	}
	return hi - lo
}

func crowded256(c Color, claimed []uint8) bool {
	for _, i := range claimed {
		if distance(c, color256(int(i))) < minSeparation256 {
			return true
		}
	}
	return false
}

// ParseProfile parses a profile by name ("none", "16", "256", "truecolor",
// plus the obvious synonyms), reporting whether the spelling was recognized.
//
// It exists so an explicit override has a door WITHOUT this package minting an
// environment pin of its own. Colour capability is a decided value that the
// shell passes down — from a settings row when Wave 4 adds one, from a flag, or
// from [DetectProfile] — and an unrecognized spelling reports false rather than
// guessing, because an override that silently did something else would be the
// affordance lying about what it accepted (5.20).
func ParseProfile(s string) (Profile, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "none", "no", "off", "0":
		return NoColor, true
	case "16", "ansi":
		return ANSI16, true
	case "256":
		return ANSI256, true
	case "truecolor", "24bit", "rgb":
		return TrueColor, true
	}
	return NoColor, false
}

// Env reads one environment variable. Detection takes it as a parameter rather
// than calling os.Getenv so the decision table is a pure function and the tests
// below it are a table, not a fixture.
type Env func(name string) string

// DetectProfile decides a terminal's color vocabulary from the STANDARD
// ecosystem variables only — NO_COLOR, TERM, COLORTERM, TMUX, TERM_PROGRAM.
// This package deliberately mints no environment pin of its own: an explicit
// override arrives as a [Profile] value through [ParseProfile], so there is one
// door for capability and it is the shell's to open.
//
// The governing rule from 10.1.2 is that COLORTERM is never trusted on its own:
// inside a multiplexer it is the multiplexer's claim about itself, not the
// outer terminal's capability, and tmux's own documentation is explicit that it
// forwards what it is told.
//
// The ladder, highest precedence first:
//
//  1. NO_COLOR (any non-empty value) — the cross-tool convention; honored
//     unconditionally.
//  2. TERM unset or "dumb" — no color.
//  3. TERM naming a direct-color terminfo entry ("*-direct", "*-truecolor").
//     This is the one truecolor claim we accept without corroboration, because
//     it is a claim about a terminfo database entry that exists on this
//     machine, not a string a program can wish into the environment.
//  4. Inside tmux or screen (TMUX set, or TERM prefixed "tmux"/"screen"):
//     capped at 256 regardless of COLORTERM. A multiplexer that really does
//     pass RGB through is expected to advertise it via a *-direct TERM, which
//     rule 3 already caught.
//  5. TERM_PROGRAM=Apple_Terminal — capped at 256. Terminal.app is the standard
//     example of an emulator whose environment can end up carrying a truecolor
//     claim it cannot honor.
//  6. COLORTERM in {truecolor, 24bit} — truecolor.
//  7. TERM containing "256" — 256 colors.
//  8. Anything else with a TERM — 16 colors.
func DetectProfile(env Env) Profile {
	if env == nil {
		return NoColor
	}
	if env("NO_COLOR") != "" {
		return NoColor
	}
	term := strings.ToLower(env("TERM"))
	if term == "" || term == "dumb" {
		return NoColor
	}
	if strings.HasSuffix(term, "-direct") || strings.Contains(term, "truecolor") {
		return TrueColor
	}

	multiplexed := env("TMUX") != "" ||
		strings.HasPrefix(term, "tmux") || strings.HasPrefix(term, "screen")
	capped := multiplexed || env("TERM_PROGRAM") == "Apple_Terminal"

	colorterm := strings.ToLower(strings.TrimSpace(env("COLORTERM")))
	if !capped && (colorterm == "truecolor" || colorterm == "24bit") {
		return TrueColor
	}
	if strings.Contains(term, "256") || colorterm == "truecolor" || colorterm == "24bit" {
		return ANSI256
	}
	return ANSI16
}
