package tui3

import (
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

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
//	ask     #C08FE8   THE QUESTION HUE, and nothing else (see below)
//	hover   #2E3440   a background, not an ink: the row the pointer is over
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
// ── THE FIFTH COLOUR, AND WHY IT IS THE ONLY ONE ──
//
// Violet is the identity wheel's question hue, and D11 reserves it: it is spent
// on the moment the agent is WAITING FOR A PERSON and on nothing else — the
// consent question, its glyph, its choices, and the word in the status line.
// Nothing decorative may take it, because its whole value is that seeing it
// anywhere means exactly one thing.
//
// #C08FE8 rather than nord's own #B48EAD, which is the hue this table would
// otherwise have borrowed: at that saturation nord's purple sits within a few
// values of the body ink on a dark terminal, and the defect being fixed here is
// a person who could not tell the surface was waiting for them. A question hue
// that has to be looked for is not a question hue.
//
// It is also not the softer #C3A6E6 this wave first authored, and the reason is
// the second rung of the ladder: #C3A6E6's nearest xterm-256 neighbour is 146,
// WHICH IS THE ACCENT'S. On every 256-colour terminal the question would have
// been painted the same colour as the person's own › glyph — the exact failure
// this hue exists to prevent, arriving through the fallback nobody looked at.
// #C08FE8 resolves to 140, which is a violet and is nothing else on this
// surface. Any future change here owes the same check.
//
// Its sixteen-colour degradation is `heavy` (bold), which is the same answer
// this table gives every hue that LEADS. The question is never carried by
// colour alone: the glyph is a "?", the status line says "waiting · your call"
// in words, and the choices name their keys.
//
// The hover background is the other addition, and it is a BACKGROUND — the
// first this file has ever drawn. #2E3440 is one step up from a dark
// terminal's own black: enough to say "the pointer is here", short of a band.
// A sixteen-colour or NO_COLOR terminal gets no hover at all, which is honest:
// there is no weight that means "under the pointer", and a bold row that moved
// with the mouse would be noise.

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
	hueAsk    = mustHue("#C08FE8", heavy)
	// hueWarn is the SIXTH colour, and it exists for one shape: a bound that is
	// about to be reached. A deadline thirty seconds out is not a failure and
	// must not wear the failure hue — the call may still land — but it is no
	// longer a fact you can leave in the dim tier either, because it is the one
	// thing on the row that is about to change what happens. Nord's yellow, one
	// clear step from the orange-red of [hueBad] on the 256 rung so the two
	// tiers of the same warning never collapse into one colour.
	hueWarn  = mustHue("#EBCB8B", heavy)
	hueHover = mustHue("#2E3440", flat)
	// hueBand is the SELECTED row's background, and it is the hover background's
	// louder sibling: one more step off black, so the two read as two states of
	// the same row rather than as one. The pointer is a guess about what you
	// might do; the cursor is where you are, and it may say so more loudly.
	hueBand = mustHue("#3B4252", flat)
	// hueViolet is the SHELL OPERATOR's hue (shellx.go), and it is deliberately
	// NOT the question hue above.
	//
	// The fifth colour's law is that seeing #C08FE8 means one thing — a person
	// is being waited on — so a pipe in a command line may not wear it. This is
	// a dimmer, greyer violet a whole tier below it: 97 rather than 140 in the
	// 256 fallback, so the two never collapse into each other on the rung where
	// hues get rounded. It is the one hue both ladders share, because it is
	// mid-tone by construction and reads on a dark terminal and a white page
	// alike.
	hueViolet = mustHue("#8F6FA8", quiet)
)

// ── THE IDENTITY RING ───────────────────────────────────────────────────────
//
// Six hues that mean NOTHING, and that is the whole of their design.
//
// Every other colour on this surface is a ROLE: violet is a question, amber is
// a bound about to be reached, orange-red is a failure, and the law that makes
// them readable is that seeing one tells you what kind of thing you are looking
// at. The ring is the opposite kind of fact. A person running four tasks at
// once needs to know WHICH ONE a row belongs to — the rail row, the note in the
// transcript, the card that lands ten minutes later — and "which one" is not a
// state, has no ordering, and must never be mistaken for one.
//
// So the ring is spent on EXACTLY ONE CELL: the task's own glyph, at the head
// of a task row (taskident.go). No role ever paints that column, so a ring hue
// cannot be read as a role — the confusion the role law exists to prevent is
// impossible by construction rather than by choosing distant colours. The title
// beside it keeps the ordinary ink, the clock keeps the dim, and a failure
// keeps [hueBad], because those are facts about the work and the ring is a fact
// about which work.
//
// The hues are mid-tone by construction, six steps around the wheel, and they
// carry NO sixteen-colour tier: below the 256 rung the glyph alphabet carries
// identity by itself, which is what it was chosen to be able to do.
var taskRing = []hue{
	mustHue("#8FBCBB", flat), // teal
	mustHue("#81A1C1", flat), // steel
	mustHue("#B48EAD", flat), // mauve
	mustHue("#9CC49B", flat), // sage
	mustHue("#E0A96D", flat), // amber
	mustHue("#D08C9B", flat), // rose
}

// lightTaskRing is the same ring for a page: the same six angles, saturated and
// darkened, by the move the whole light ladder makes.
var lightTaskRing = []hue{
	mustHue("#3E7C7B", flat),
	mustHue("#4C6E92", flat),
	mustHue("#7E5A79", flat),
	mustHue("#4F7A4E", flat),
	mustHue("#A06A2C", flat),
	mustHue("#97505F", flat),
}

// ── THE LIGHT LADDER ────────────────────────────────────────────────────────
//
// The table above is dark-terminal first and was, for four waves, the only
// table there was. A person on a white terminal got soft pastels authored
// against black: #D8DEE9 body ink on #FFFFFF is very nearly invisible, and the
// dim tier below it is invisible outright.
//
// So there is a second ladder, authored the same way and against the same law —
// nothing bright — but for a page rather than for a void. The moves are the
// obvious ones and they are all the same move: what carried by being LIGHTER
// than the background now carries by being DARKER than it.
//
//	role    dark      light     what changed
//	ink     #D8DEE9   #3B4252   the body inverts: near-black on the page
//	accent  #9DC3E6   #5E81AC   the pastel blue saturates; a pastel on white
//	                            is a smudge
//	muted   #7FA6C9   #8098B8   accent, one step back, on both ladders
//	dim     #6B7280   #9AA3B2   the meta tier goes LIGHTER, not darker: it
//	                            recedes toward the page
//	add     #A3BE8C   #7BA23F   nord's green has no contrast on white
//	del     #BF616A   #B55B64   already dark enough; barely moves
//	bad     #D08770   #C57A3C   soft orange-red, one step down
//	ask     #C08FE8   #6F3FA8   THE QUESTION HUE, inverted rather than dimmed:
//	                            it has to lead on a page too
//	warn    #EBCB8B   #A6791F   a pale yellow is nothing on white; the page
//	                            wants the same warning as dark amber
//	hover   #2E3440   #E5E9F0   one step off the #ECEFF4 page, the way the dark
//	                            hover is one step off black
//	violet  #8F6FA8   #8F6FA8   the shared one (above)
//
// Every light index was checked against its neighbours the way #C08FE8 was:
// no two roles in this ladder resolve to the same xterm-256 index, because the
// 256 rung is where an unchecked pair silently becomes one colour. bundle_test
// asserts it, and any future change here owes the same check.
var (
	lightInk    = mustHue("#3B4252", flat)
	lightAccent = mustHue("#5E81AC", heavy)
	lightMuted  = mustHue("#8098B8", flat)
	lightDim    = mustHue("#9AA3B2", quiet)
	lightAdd    = mustHue("#7BA23F", heavy)
	lightDel    = mustHue("#B55B64", quiet)
	lightBad    = mustHue("#C57A3C", heavy)
	lightAsk    = mustHue("#6F3FA8", heavy)
	lightWarn   = mustHue("#A6791F", heavy)
	lightHover  = mustHue("#E5E9F0", flat)
	// The band is one step further off the page than the hover is, which is the
	// same move the dark ladder makes in the other direction.
	lightBand = mustHue("#D8DEE9", flat)
)

// ramp is one whole ladder: every role this surface paints, resolved once.
//
// The palette holds a ramp rather than reading the package vars directly, which
// is the entire mechanism of the light theme — every p.ink(), p.dim() and
// p.hover() call site in the package was already going through the palette, so
// the second ladder cost the call sites nothing.
type ramp struct {
	ink, accent, muted, dim hue
	add, del, bad, ask      hue
	warn                    hue
	hover, band, violet     hue
	fade                    [3]hue
	// mark is the identity ring (above): not a role, and the only thing on the
	// ladder that is a list rather than a colour.
	mark []hue
}

var darkRamp = ramp{
	ink: hueInk, accent: hueAccent, muted: hueMuted, dim: hueDim,
	add: hueAdd, del: hueDel, bad: hueBad, ask: hueAsk, warn: hueWarn,
	hover: hueHover, band: hueBand, violet: hueViolet, fade: thoughtFade,
	mark: taskRing,
}

var lightRamp = ramp{
	ink: lightInk, accent: lightAccent, muted: lightMuted, dim: lightDim,
	add: lightAdd, del: lightDel, bad: lightBad, ask: lightAsk, warn: lightWarn,
	hover: lightHover, band: lightBand, violet: hueViolet, fade: lightFade,
	mark: lightTaskRing,
}

// lightFade is the thinking window's gradient on a page. It fades toward WHITE
// rather than toward black — the gradient's whole job is "this line is on its
// way out", and on a light terminal the way out is up, not down.
var lightFade = [3]hue{
	liftOf(lightDim, fadeOldest),
	liftOf(lightDim, fadeMiddle),
	liftOf(lightDim, fadeNewest),
}

// liftOf is [fadeOf]'s mirror: one hue at pct opacity over WHITE.
func liftOf(h hue, pct int) hue {
	mix := func(c uint8) uint8 { return uint8((int(c)*pct + 255*(100-pct) + 50) / 100) }
	r, g, b := mix(h.r), mix(h.g), mix(h.b)
	return hue{r: r, g: g, b: b, idx: nearest256(r, g, b), tier: h.tier}
}

// ── THE THEME SEAM ──────────────────────────────────────────────────────────
//
// theme is which ladder a surface paints from.
type theme uint8

const (
	// themeAuto asks the terminal, and falls back to dark. See [detectTheme].
	themeAuto theme = iota
	themeDark
	themeLight
)

// themeFromRow turns a settings row's value into a theme. It is THE SEAM, and
// it is a seam rather than a wire because the registry row does not exist yet:
// internal/config owns the rows, this package owns the ladders, and the day the
// row lands (a Display tab entry beside the nerd-font tier) it is one call —
// `newThemedPalette(profile, ascii, themeFromRow(settings.Get("display.theme")))`
// — and nothing else in this package moves.
//
// Anything unrecognized is auto, which is the honest answer to a row somebody
// spelled wrong: ask the terminal rather than pin the wrong ladder.
func themeFromRow(row string) theme {
	switch strings.ToLower(strings.TrimSpace(row)) {
	case "dark":
		return themeDark
	case "light":
		return themeLight
	default:
		return themeAuto
	}
}

// detectTheme is the auto answer: COLORFGBG, and nothing else.
//
// There is exactly one thing a terminal will tell you about its background
// without being interrogated, and it is this variable — "15;0" is light-on-dark,
// "0;15" is dark-on-light. The field that matters is the LAST one (some
// terminals send three, with the cursor colour in the middle), read as an ANSI
// index: 0-6 and 8 are the dark half of the sixteen, everything else is light.
//
// The other way to ask — OSC 11, a query and a reply parsed off the input
// stream — is deliberately not done here. It is a round trip on a terminal that
// may never answer, in a constructor that must not block, to decide a colour
// that a person who cares can pin outright. Unset means dark, which is what
// this surface has always assumed and what most terminals are.
func detectTheme(env func(string) string) theme {
	if env == nil {
		return themeDark
	}
	value := strings.TrimSpace(env("COLORFGBG"))
	if value == "" {
		return themeDark
	}
	fields := strings.Split(value, ";")
	background, err := strconv.Atoi(strings.TrimSpace(fields[len(fields)-1]))
	if err != nil {
		return themeDark
	}
	if background >= 0 && background <= 6 || background == 8 {
		return themeDark
	}
	return themeLight
}

func rampFor(t theme, env func(string) string) ramp {
	if t == themeAuto {
		t = detectTheme(env)
	}
	if t == themeLight {
		return lightRamp
	}
	return darkRamp
}

// ── THE THINKING WINDOW'S FADE ──────────────────────────────────────────────
//
// While a model reasons, the last three lines of its working are on screen and
// nothing else (thinking.go). Three lines of identical dim text is a paragraph
// that has to be READ to learn which end of it is new, so the window is painted
// as an OPACITY GRADIENT instead: the oldest visible line furthest toward the
// background, the newest at the dim tier it will keep when it settles.
//
// The stops are the dim ink at three opacities over black — a terminal will not
// say what its background is, and every terminal this palette was authored for
// is dark, so black is the honest anchor. Naming the opacities rather than the
// colours is the point: change hueDim and the fade follows it, which is what
// stops the gradient drifting off the tier it belongs to.
//
//	35%  #25282D  the oldest line — read already, on its way out
//	60%  #40444D  the middle
//	85%  #5B616D  the newest, one step under the settled block's own dim
//
// Below ANSI256 there is no hue to fade — the sixteen are the user's theme —
// and the window falls back to the dim tier's weight, which is what the block
// has always worn. NO_COLOR gets three plain lines: the newest is still last,
// which is the fact the gradient was drawing.
const (
	fadeOldest = 35
	fadeMiddle = 60
	fadeNewest = 85
)

// thoughtFade is the ramp, oldest first. It is derived at init rather than
// authored so that the hexes in the table above can be ASSERTED (thinking_test)
// instead of maintained by hand.
var thoughtFade = [3]hue{
	fadeOf(hueDim, fadeOldest),
	fadeOf(hueDim, fadeMiddle),
	fadeOf(hueDim, fadeNewest),
}

// fadeOf is one hue at pct opacity over black, rounded rather than truncated:
// truncation loses a whole value on two of the three stops and the ramp's job
// is that its steps are even.
func fadeOf(h hue, pct int) hue {
	mix := func(c uint8) uint8 { return uint8((int(c)*pct + 50) / 100) }
	r, g, b := mix(h.r), mix(h.g), mix(h.b)
	return hue{r: r, g: g, b: b, idx: nearest256(r, g, b), tier: h.tier}
}

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
	// ramp is the ladder this palette paints from — dark, or light.
	ramp ramp
	// linear is the screen-reader tier (Options.Linear): no motion, no pointer.
	// It gates the two paints that mean neither of those things to a reader —
	// the thinking window's gradient and the hover background — because a
	// gradient is an animation frozen in space and a hover is a pointer's
	// shadow, and a surface being read aloud has neither.
	linear bool
}

func newPalette(p tokens.Profile, ascii bool) palette {
	return palette{profile: p, ascii: ascii, ramp: darkRamp}
}

// newThemedPalette is [newPalette] with the ladder said out loud. It is what
// the settings row will call through [themeFromRow]; detection is the default
// and pins are the exception, which is the same shape every other display
// question on this surface has.
func newThemedPalette(p tokens.Profile, ascii bool, t theme, env func(string) string) palette {
	pal := newPalette(p, ascii)
	pal.ramp = rampFor(t, env)
	return pal
}

// detectPalette reads the terminal the way the rest of the tree does.
func detectPalette() palette {
	return newThemedPalette(
		tokens.DetectProfile(os.Getenv), detectASCII(os.Getenv), themeAuto, os.Getenv)
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

func (p palette) ink(s string) string    { return p.paint(s, p.ramp.ink) }
func (p palette) accent(s string) string { return p.paint(s, p.ramp.accent) }
func (p palette) muted(s string) string  { return p.paint(s, p.ramp.muted) }
func (p palette) dim(s string) string    { return p.paint(s, p.ramp.dim) }
func (p palette) add(s string) string    { return p.paint(s, p.ramp.add) }
func (p palette) del(s string) string    { return p.paint(s, p.ramp.del) }
func (p palette) bad(s string) string    { return p.paint(s, p.ramp.bad) }

// warn is the tier below bad: something is about to go wrong rather than has
// (styles.go's [hueWarn]). The only thing that wears it today is a timeout with
// seconds left on it (toolview.go).
func (p palette) warn(s string) string { return p.paint(s, p.ramp.warn) }

// violet is the shell operator's tier and nothing else on this surface — see
// [hueViolet] for why it is not the question hue.
func (p palette) violet(s string) string { return p.paint(s, p.ramp.violet) }

// markPaint paints one task's glyph in that task's own hue ([taskRing]). The
// tint is an index off the id's hash and is wrapped here rather than at the
// call sites, so a ring that grows or shrinks is one line in this file.
func (p palette) markPaint(tint int, s string) string {
	ring := p.ramp.mark
	if len(ring) == 0 || s == "" {
		return s
	}
	at := tint % len(ring)
	if at < 0 {
		at += len(ring)
	}
	return p.paint(s, ring[at])
}

// underline is the third bare attribute, and it has one job: a PATH inside a
// highlighted command (shellx.go). A path is the one token in a command line
// that names a thing you could go and open, and underline is how every terminal
// on earth has said "this is a location" since before there were hyperlinks.
//
// "When the terminal allows" is the profile question and not the glyph one: a
// terminal told to draw no SGR at all (NO_COLOR) is not underlined either, and
// everything above that rung can do SGR 4 — it is in the original ECMA-48 set.
func (p palette) underline(s string) string {
	if p.profile == tokens.NoColor || s == "" {
		return s
	}
	return "\x1b[4m" + s + "\x1b[24m"
}

// fade paints one line of the streaming thinking window: stop 0 is the oldest
// and faintest, the last stop the newest. See [thoughtFade] for the ramp.
//
// The gradient is a COLOUR question and so it asks the profile and not the
// glyph tier: a truecolor terminal in a C locale is still a truecolor terminal,
// and the ascii flag has exactly one job in this file (box drawing). Where
// there is no hue — the sixteen, and NO_COLOR — the whole window comes back at
// the dim tier, unfaded, which is what it wore before this ramp existed.
func (p palette) fade(s string, stop int) string {
	switch p.profile {
	case tokens.TrueColor, tokens.ANSI256:
	default:
		return p.dim(s)
	}
	// The linear tier takes the same answer the sixteen do: a gradient is an
	// animation held still, and it says nothing to a reader.
	if p.linear {
		return p.dim(s)
	}
	if stop < 0 {
		stop = 0
	}
	if stop >= len(p.ramp.fade) {
		stop = len(p.ramp.fade) - 1
	}
	return p.paint(s, p.ramp.fade[stop])
}

// ask is the question hue: the consent block, and nothing else on this surface.
func (p palette) ask(s string) string { return p.paint(s, p.ramp.ask) }

// askBold is what the question's own marker takes — the hue and the weight
// together, so the row a person has to answer leads on a truecolor terminal and
// on a sixteen-colour one alike.
func (p palette) askBold(s string) string { return p.bold(p.ask(s)) }

// hover paints one row's background: the pointer is on this row.
//
// The text arrives already painted, and that is fine — every foreground
// sequence in this file closes with SGR 39, which resets the ink and leaves the
// background alone. The row is padded to width first, because a highlight that
// stops where the text stops reads as a smudge rather than as a row.
//
// A terminal below ANSI256 gets the row back untouched: see the note at the top
// of this file for why there is no weight-tier fallback here.
func (p palette) hover(s string, width int) string {
	if p.linear {
		return s
	}
	return p.background(s, width, p.ramp.hover)
}

// band paints the SELECTED row's background: the same mechanism as the hover
// one step louder ([hueBand]).
//
// It is NOT gated on the linear tier the way the hover is, and the difference
// is the whole reason those two are separate methods: a hover is a pointer's
// shadow and there is no pointer to have one, while the cursor is a position in
// a list that exists whoever is reading it. What linear mode drops is motion
// and pointers, not the answer to "which row am I on".
func (p palette) band(s string, width int) string {
	return p.background(s, width, p.ramp.band)
}

// background is the one place this file draws a background: the row padded to
// the full width, wrapped in the colour, closed with SGR 49. A terminal below
// ANSI256 gets the row back untouched — there is no weight that means "this
// row", and the callers each carry a text-side marker anyway (the lead glyph,
// the bold label).
func (p palette) background(s string, width int, h hue) string {
	if s == "" {
		return s
	}
	if pad := width - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	switch p.profile {
	case tokens.TrueColor:
		return "\x1b[48;2;" + itoa(int(h.r)) + ";" + itoa(int(h.g)) + ";" +
			itoa(int(h.b)) + "m" + s + "\x1b[49m"
	case tokens.ANSI256:
		return "\x1b[48;5;" + itoa(int(h.idx)) + "m" + s + "\x1b[49m"
	default:
		return s
	}
}

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

// railWidth is what any of those three measure, which is the point of them all
// being four cells: a tool row's arithmetic starts from a constant instead of
// measuring one on every frame, and it was measuring one per row per frame.
// TestRailFormsAreOneWidth holds the three to it.
const railWidth = 4

// railCont is the stem an expanded call's detail rows hang from.
func (p palette) railCont() string {
	if p.ascii {
		return railContASCII
	}
	return railCont
}

// product is what this surface calls itself, everywhere it speaks: the status
// line, the welcome box's wordmark, /help. It is written down ONCE because a
// product name spelled out at four call sites is a product name that gets
// renamed at three of them.
const product = "openaf"

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
	// glyphQueued marks a call the model has asked for and nothing has started:
	// an EMPTY circle, dim, deliberately not a spinner. A spinner is a claim
	// that something is turning, and the whole point of this state is that
	// nothing is.
	glyphQueued = "◌"
	// glyphAsk marks the call a person is being asked about. It is the only
	// glyph on this surface that takes the question hue.
	glyphAsk = "?"
	// glyphAdd and glyphDel spell the diffstat. The minus is U+2212, which is
	// the width of the plus; ASCII '-' is not, and a stat is a pair of numbers
	// read side by side. The diff BODY keeps ASCII +/- — a diff is a diff, and
	// its first column is quoted, copied and pasted.
	glyphAdd = "+"
	glyphDel = "−"
)

// ── THE LINEAR TIER (Options.Linear) ────────────────────────────────────────
//
// Linear mode is the SCREEN-READER tier, and it is one question: what does this
// surface look like to somebody who is not looking at it? Three answers, and
// all three are subtractions:
//
//	no animation   a spinner read aloud is a word repeated forever
//	no hover       a pointer's shadow is nothing to a reader
//	no glyphs      "╰─▶" is announced as three characters nobody named
//
// So every marker that carries meaning by SHAPE gets an ASCII stand-in that
// carries it by NAME, and every marker that carries it by motion stops moving.
// The colours stay: a screen reader ignores SGR, and a person using linear mode
// on a terminal that has hues loses nothing by keeping them.
//
// The stand-ins are the obvious ones. `*` is running because it is what every
// installer that ever printed a progress line used, and `o` is queued because
// it is the empty circle spelled in one byte.
const (
	glyphYouASCII    = "> "
	glyphToolASCII   = "-> "
	glyphBadASCII    = "x"
	glyphIdleASCII   = "."
	glyphQueuedASCII = "o"
	glyphRunASCII    = "*"
)

// youGlyph and toolGlyph are the two markers the transcript opens rows with.
// Everything else on this surface is either inside a tool line (toolview.go
// asks the palette for its own marks) or is a word.
func (p palette) youGlyph() string {
	if p.linear {
		return glyphYouASCII
	}
	return glyphYou
}

func (p palette) toolGlyph() string {
	if p.linear {
		return glyphToolASCII
	}
	return glyphTool
}

// badGlyph is the one glyph a failure is allowed to spend.
func (p palette) badGlyph() string {
	if p.linear {
		return glyphBadASCII
	}
	return glyphBad
}

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

// glyphHarness marks a sub-harness — a saved SHAPE of work rather than a piece
// of it (harnesspanel.go). It is the roster's own identity diamond deliberately:
// what a harness and a task node have in common is that both are things this
// session is carrying, and the difference between them is said by the word
// beside the mark rather than by a second alphabet of symbols nobody was taught.
const (
	glyphHarness      = "◆"
	glyphHarnessASCII = "#"
)
