package tui3

import (
	"os"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The whole surface's palette lives here and nowhere else.
//
// This file is the CURATED PASTEL AUTHORITY of docs/CHAT-V3.md Decision 11.
// The colours are authored as hex, dark-terminal first, and there is one rule
// above every choice: if a colour could be described as "bright", it is wrong.
//
//	role    hex       what it paints
//	ink     #D8DEE9   the body — what was said, and every tool's TARGET
//	accent  #9DC3E6   the person's › glyph, the rail, this surface's own headings
//	muted   #7FA6C9   the same hue one step back: tool names, the spinner
//	dim     #6B7280   everything the surface says about itself — stats, notes,
//	                  hunk markers, the status line
//	add     #A3BE8C   a diff's + lines, and a write's line count
//	del     #BF616A   a diff's − lines
//	bad     #D08770   the ✗ of a call that failed — soft orange-red, not fire
//
// Why hex rather than internal/tui2/tokens (which this file used to delegate
// to): tokens is the v2 identity ramp, tuned for a rail of coloured cards, and
// D11 asks this surface for a quieter one. Detection is still tokens' — the
// profile question ("what can this terminal say") has one answer in this tree
// and it is [tokens.DetectProfile]; only the answer to "which colour" moved
// here. The spinner frames stay tokens' too: they are a glyph set, not a hue.
//
// The fallback ladder, and what each rung costs:
//
//	TrueColor  the palette exactly as authored — 38;2;r;g;b
//	ANSI256    the nearest member of the xterm cube+grey ramp, computed once at
//	           init by [nearest256]. Pastels land on soft indices; nothing in
//	           the table resolves into 0-15, which are whatever the user's theme
//	           says they are
//	ANSI16     NO HUE AT ALL. The sixteen are the terminal's own theme and its
//	           reds and greens are loud by definition, so this rung answers with
//	           weight instead: bold for what leads, faint for what recedes,
//	           plain for the body. Every distinction D11 draws in colour is also
//	           drawn in text (+/−, ✗, "exit 2"), so nothing is lost but the tint
//	NoColor    no SGR at all, weight included: a terminal told not to style is
//	           not styled halfway
//
// Violet is absent on purpose. The identity wheel's violet is reserved for
// question UX; a chat surface that spent it on decoration would leave the one
// thing that needs a human indistinguishable from the thing that does not.

// tier16 is what a sixteen-colour terminal draws instead of a hue.
type tier16 uint8

const (
	flat  tier16 = iota // no attribute: the body
	heavy               // SGR 1: what leads
	quiet               // SGR 2: what recedes
)

// hue is one authored colour and its two degradations.
type hue struct {
	r, g, b uint8
	// idx is the nearest xterm-256 index, computed once at init.
	idx uint8
	// tier is the weight a sixteen-colour terminal gets instead of the hue.
	tier tier16
}

// The table. Changing a colour is changing one line here, and nothing else in
// the package holds an escape sequence.
var (
	hueInk    = mustHue("#D8DEE9", flat)
	hueAccent = mustHue("#9DC3E6", heavy)
	hueMuted  = mustHue("#7FA6C9", flat)
	hueDim    = mustHue("#6B7280", quiet)
	hueAdd    = mustHue("#A3BE8C", heavy)
	hueDel    = mustHue("#BF616A", quiet)
	hueBad    = mustHue("#D08770", heavy)
)

// mustHue parses an authored "#RRGGBB" and resolves its 256-colour neighbour.
// It panics on a malformed literal, which is a compile-time mistake caught at
// init rather than a colour that silently renders as black.
func mustHue(hex string, tier tier16) hue {
	r, g, b, ok := parseHex(hex)
	if !ok {
		panic("tui3: malformed palette colour " + hex)
	}
	return hue{r: r, g: g, b: b, idx: nearest256(r, g, b), tier: tier}
}

func parseHex(hex string) (r, g, b uint8, ok bool) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0, 0, 0, false
	}
	value, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return uint8(value >> 16), uint8(value >> 8), uint8(value), true
}

// cubeLevels are the six values of the xterm 6×6×6 colour cube.
var cubeLevels = [6]int{0, 95, 135, 175, 215, 255}

// nearest256 is the closest xterm-256 index to an authored colour, searched
// across the cube (16-231) and the 24-step grey ramp (232-255) and never the
// first sixteen — those are the user's theme, not a colour we chose.
//
// The metric is plain squared RGB distance. A perceptual one (CIE76 and up)
// would be defensible, but the table is seven low-saturation colours that are
// each far from their runners-up, and a colour-science dependency for a
// distance that does not change the answer is a dependency for nothing.
func nearest256(r, g, b uint8) uint8 {
	best, bestDist := 0, 1<<30
	consider := func(index, cr, cg, cb int) {
		dr, dg, db := int(r)-cr, int(g)-cg, int(b)-cb
		if d := dr*dr + dg*dg + db*db; d < bestDist {
			best, bestDist = index, d
		}
	}
	for ri, rv := range cubeLevels {
		for gi, gv := range cubeLevels {
			for bi, bv := range cubeLevels {
				consider(16+36*ri+6*gi+bi, rv, gv, bv)
			}
		}
	}
	for i := 0; i < 24; i++ {
		v := 8 + 10*i
		consider(232+i, v, v, v)
	}
	return uint8(best)
}

// palette paints one terminal's worth of this table.
type palette struct {
	profile tokens.Profile
	// ascii is the glyph floor: a terminal that cannot be trusted with box
	// drawing gets "+-> " where the rail would be. It gates GLYPHS only —
	// colour is the profile's business, and the two questions are independent
	// (a truecolor terminal in a C locale is a real terminal).
	ascii bool
}

func newPalette(p tokens.Profile, ascii bool) palette {
	return palette{profile: p, ascii: ascii}
}

// detectPalette reads the terminal the way the rest of the tree does.
func detectPalette() palette {
	return newPalette(tokens.DetectProfile(os.Getenv), detectASCII(os.Getenv))
}

// detectASCII decides whether this surface may draw box-drawing characters.
//
// Like tokens' glyph detection it may only VETO: there is no escape sequence
// that answers "can you draw U+251C", so the answer is yes unless something
// says otherwise. The two vetoes are the ones that are actually knowable — a
// terminal that made no capability claim at all, and a locale that is not
// UTF-8, where a multi-byte rune arrives as mojibake rather than as a rail.
//
// There is deliberately no environment pin here. A human override of a terminal
// veto is a Display setting (internal/config's registry already fronts the
// nerd-font tier that way, and the repo's completeness gate says any new pin
// arrives as a row); inventing an AFORGE_* variable for this one surface would
// be a second door onto the same question.
func detectASCII(env func(string) string) bool {
	if env == nil {
		return true
	}
	term := strings.ToLower(strings.TrimSpace(env("TERM")))
	if term == "" || term == "dumb" {
		return true
	}
	for _, key := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := strings.TrimSpace(env(key)); v != "" {
			return !strings.Contains(strings.ToUpper(v), "UTF-8") &&
				!strings.Contains(strings.ToUpper(v), "UTF8")
		}
	}
	// No locale set at all is the POSIX C locale in every shell that matters,
	// and the C locale is not UTF-8.
	return true
}

// paint wraps text in the escape sequence one hue asks for on this terminal.
func (p palette) paint(s string, h hue) string {
	if s == "" || p.profile == tokens.NoColor {
		return s
	}
	switch p.profile {
	case tokens.TrueColor:
		return "\x1b[38;2;" + itoa(int(h.r)) + ";" + itoa(int(h.g)) + ";" + itoa(int(h.b)) + "m" +
			s + "\x1b[39m"
	case tokens.ANSI256:
		return "\x1b[38;5;" + itoa(int(h.idx)) + "m" + s + "\x1b[39m"
	default:
		switch h.tier {
		case heavy:
			return "\x1b[1m" + s + "\x1b[22m"
		case quiet:
			return "\x1b[2m" + s + "\x1b[22m"
		default:
			return s
		}
	}
}

func (p palette) ink(s string) string    { return p.paint(s, hueInk) }
func (p palette) accent(s string) string { return p.paint(s, hueAccent) }
func (p palette) muted(s string) string  { return p.paint(s, hueMuted) }
func (p palette) dim(s string) string    { return p.paint(s, hueDim) }
func (p palette) add(s string) string    { return p.paint(s, hueAdd) }
func (p palette) del(s string) string    { return p.paint(s, hueDel) }
func (p palette) bad(s string) string    { return p.paint(s, hueBad) }

// bold is the one attribute this file draws without a hue behind it: weight is
// what carries the user/assistant distinction on a monochrome terminal.
func (p palette) bold(s string) string {
	if p.profile == tokens.NoColor || s == "" {
		return s
	}
	return "\x1b[1m" + s + "\x1b[22m"
}

// italic is the second attribute, and it has exactly one job: the model's own
// reasoning (thinking.go), which is text that has to read as a tier below the
// answer even where the dim hue lands close to it. A terminal that ignores SGR 3
// loses nothing — the block is dim and behind its own marker either way.
func (p palette) italic(s string) string {
	if p.profile == tokens.NoColor || s == "" {
		return s
	}
	return "\x1b[3m" + s + "\x1b[23m"
}

// rail is the marker that opens a tool line: the elbow for the last call of a
// cluster, the tee for every call above it, and one ASCII arrow for a terminal
// that cannot draw either. All three are four cells wide, so a cluster's names
// start in one column whatever the terminal can say.
func (p palette) rail(last bool) string {
	if p.ascii {
		return railASCII
	}
	if last {
		return railLast
	}
	return railMid
}

// railCont is the stem an expanded call's detail rows hang from.
func (p palette) railCont() string {
	if p.ascii {
		return railContASCII
	}
	return railCont
}

// The glyph vocabulary of this surface.
//
// There is NO success glyph, deliberately and permanently (D11): a quiet line
// is a success, and a column of ✓ is a column that has to be read to learn
// nothing. Only failure speaks.
const (
	glyphYou      = "› "
	glyphTool     = "↳ " // the fold line's marker, and only the fold line's
	glyphBad      = "✗"
	glyphMore     = "…"
	railMid       = "├─▶ "
	railLast      = "╰─▶ "
	railCont      = "│ "
	railASCII     = "+-> "
	railContASCII = "| "
	// glyphThought opens the reasoning block (thinking.go). It is the spinner's
	// own alphabet at rest — the full braille cell — because a thought is the
	// same machine the spinner is drawing, stopped.
	glyphThought = "⠿"
	// glyphIdle marks a call that was still running when its turn ended. A
	// frozen spinner would claim the call is alive; a dot claims nothing.
	glyphIdle = "·"
	// glyphAdd and glyphDel spell the diffstat. The minus is U+2212, which is
	// the width of the plus; ASCII '-' is not, and a stat is a pair of numbers
	// read side by side. The diff BODY keeps ASCII +/- — a diff is a diff, and
	// its first column is quoted, copied and pasted.
	glyphAdd = "+"
	glyphDel = "−"
)

// spinnerStep is how many frame ticks one braille frame lasts. The frame clock
// runs at [frameInterval] (33ms) because that is the repaint ceiling, but the
// spinner turns on the house grid — 4 × 33ms ≈ tokens.MotionInterval — because
// below about 100ms a braille cycle stops reading as rotation and starts
// reading as shimmer.
const spinnerStep = 4

// pulseStep is the same idea for the quiet ellipsis: nine ticks ≈ 300ms per
// step, a breath rather than a spin.
const pulseStep = 9

// ellipsisFrames is the sign of life while a turn is silent. No spinner here —
// a session that is thinking is not a progress bar; the spinners belong to the
// tool lines, which are the things actually running.
var ellipsisFrames = [3]string{"·", "··", "···"}

func itoa(n int) string { return strconv.Itoa(n) }
