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

// SelectionStyle says how selection must be drawn under this profile. 5.16
// wants a background band; at 16 colors the only available "raised background"
// is bright black, which is a different shade in every theme and can land on
// top of the text tier. Reverse video is the honest fallback: it is defined
// relative to whatever the terminal's own foreground and background are.
type SelectionStyle uint8

const (
	SelectionBand    SelectionStyle = iota // draw [Band] / [BandFor] as a background
	SelectionReverse                       // SGR 7; no band token is used
)

// SelectionStyle returns the selection idiom available under this profile.
func (p Profile) SelectionStyle() SelectionStyle {
	if p >= ANSI256 {
		return SelectionBand
	}
	return SelectionReverse
}

type layer uint8

const (
	layerFg layer = iota
	layerBg
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

// Reverse is SGR 7, the selection idiom for [SelectionReverse].
func Reverse(p Profile) string {
	if p == NoColor {
		return ""
	}
	return "\x1b[7m"
}

func sgrString(p Profile, e *entry, f Focus, l layer) string {
	switch p {
	case NoColor:
		return ""
	case ANSI16:
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
		if l == layerBg {
			lead = "\x1b[48;5;"
		}
		return lead + itoa(int(e.idx256[f])) + "m"
	default:
		c := e.color[f]
		lead := "\x1b[38;2;"
		if l == layerBg {
			lead = "\x1b[48;2;"
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
