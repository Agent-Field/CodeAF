package tokens

import "strings"

// Glyph tier detection (12.7 E.2).
//
// The honest headline first: **no terminal reliably reports its font.** There
// is no standard escape sequence that answers "are you patched"; iTerm2's OSC
// 1337 and kitty's remote-control protocol are proprietary, opt-in, and answer
// a different question. So detection here may only VETO, never confirm, and the
// default is on — which is a choice that has to be paid for, and is: by a real
// opt-out at three doors, and by the plain tier being a designed floor rather
// than a degradation (12.7 E.5).
//
// [DetectGlyphSet] follows [DetectProfile]'s exact shape — a pure function over
// an [Env] closure, so its decision table is a table test rather than a fixture
// — and returns the reason beside the tier, so the chat.log records WHY a
// terminal is drawing what it is drawing.

// Reason strings are values rather than prose built at the call site, so the
// log line and the settings sheet say the same sentence about the same cause.
const (
	glyphReasonNoEnv       = "no environment: the plain floor"
	glyphReasonNoTerm      = "TERM is unset or dumb: no capability claim at all"
	glyphReasonLinuxTTY    = "TERM=linux: the Linux console renders a 256-glyph bitmap font and cannot draw private use at all"
	glyphReasonAppleTerm   = "TERM_PROGRAM=Apple_Terminal: Terminal.app ships SF Mono and Menlo, neither of which carries private use"
	glyphReasonCJKLocale   = "an East-Asian locale is set: all private use is East_Asian_Width=Ambiguous, so ambiguous-wide would draw every icon at two cells and tier width parity would break"
	glyphReasonLegacyConIn = "a legacy Windows console: its font fallback for private use is unreliable"
	glyphReasonDefault     = "no veto signal: on by default"
)

// DetectGlyphSet returns the tier a terminal should start in and the reason,
// for the log line. It never confirms — a positive signal cannot exist — so the
// answer is [NerdFont] unless one of the vetoes below fires.
//
// The vetoes, and what each costs when it is wrong:
//
//  1. TERM unset or dumb. No capability claim at all; already the NoColor
//     floor. Costs nothing.
//  2. TERM=linux. The Linux console runs a 256/512-glyph bitmap font and
//     CANNOT render private use. This one is certain.
//  3. TERM_PROGRAM=Apple_Terminal. Terminal.app ships SF Mono and Menlo, and
//     its users are the population least likely to have patched a font.
//     [DetectProfile] already special-cases it for colour. The cost is a false
//     negative for the rare Terminal.app user who did patch one; they set the
//     flag once.
//  4. An East-Asian locale in LC_ALL, LC_CTYPE or LANG. All of private use is
//     East_Asian_Width=Ambiguous (12.7 B.3), so a terminal running
//     ambiguous-wide draws every icon at two cells while the plain tier draws
//     several of them at one: width parity holds under both rulers we ship
//     against and breaks there. The cost is a false negative for a CJK-locale
//     user whose terminal does not run ambiguous-wide; they set the flag once.
//  5. A legacy Windows console. Its font fallback for private use is
//     unreliable. Windows Terminal (WT_SESSION) and ConEmu say so themselves
//     and are not vetoed.
//
// Two things that are deliberately NOT vetoes, and one that is deliberately not
// a confirmation:
//
//   - tmux and screen pass the font straight through, because the font belongs
//     to the outer terminal. This differs from COLORTERM, which inside a
//     multiplexer is the multiplexer's claim about itself (10.1.2): the colour
//     ladder caps under a multiplexer and the glyph ladder must not.
//   - NO_COLOR is about colour. A user who wants no colour has said nothing
//     about their font, and reading it as a glyph veto would be this package
//     inventing a meaning for somebody else's convention.
//   - TERM_PROGRAM ∈ {WezTerm, ghostty, iTerm.app, WarpTerminal},
//     KITTY_WINDOW_ID, WEZTERM_EXECUTABLE, GHOSTTY_RESOURCES_DIR,
//     ALACRITTY_WINDOW_ID and LC_TERMINAL each say which TERMINAL is running
//     and nothing about which font it was configured with. A WezTerm user on
//     stock JetBrains Mono is a false positive, and false positives are
//     precisely the tofu case. Since the default is already on, a positive
//     signal buys nothing anyway — which is the tidy argument for never reading
//     them at all.
func DetectGlyphSet(env Env) (GlyphSet, string) {
	if env == nil {
		return Plain, glyphReasonNoEnv
	}

	term := strings.ToLower(strings.TrimSpace(env("TERM")))
	switch {
	case term == "" || term == "dumb":
		return Plain, glyphReasonNoTerm
	case term == "linux":
		return Plain, glyphReasonLinuxTTY
	}
	if strings.TrimSpace(env("TERM_PROGRAM")) == "Apple_Terminal" {
		return Plain, glyphReasonAppleTerm
	}
	if eastAsianLocale(env) {
		return Plain, glyphReasonCJKLocale
	}
	if legacyWindowsConsole(env) {
		return Plain, glyphReasonLegacyConIn
	}
	return NerdFont, glyphReasonDefault
}

// eastAsianLocale reads the locale the way POSIX resolves it — LC_ALL, then the
// specific category, then LANG, first non-empty wins — and asks only whether
// the language is one whose terminals commonly run ambiguous-wide.
//
// It matches on the language tag alone, so "ja_JP.UTF-8", "zh_CN.GB18030" and
// "ko_KR" all answer yes, while "C", "POSIX" and anything else answer no. It is
// deliberately not a list of every locale that might be configured wide: this
// is a heuristic that costs a false negative and buys never breaking a line.
func eastAsianLocale(env Env) bool {
	value := ""
	for _, name := range [3]string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := strings.TrimSpace(env(name)); v != "" {
			value = v
			break
		}
	}
	if value == "" {
		return false
	}
	lang := strings.ToLower(value)
	if i := strings.IndexAny(lang, "_.@-"); i >= 0 {
		lang = lang[:i]
	}
	switch lang {
	case "zh", "ja", "ko":
		return true
	}
	return false
}

// legacyWindowsConsole reports the old conhost, where private-use fallback is
// unreliable. Windows Terminal sets WT_SESSION and ConEmu sets ConEmuANSI; both
// draw private use fine, and both say so themselves. What is left is a POSIX
// layer (MSYSTEM: MSYS2, git-bash, Cygwin's MINGW shells) with neither, which
// is conhost.
func legacyWindowsConsole(env Env) bool {
	if strings.TrimSpace(env("WT_SESSION")) != "" || strings.TrimSpace(env("ConEmuANSI")) != "" {
		return false
	}
	return strings.TrimSpace(env("MSYSTEM")) != ""
}

// GlyphProbeLine is the sample the settings sheet's live preview renders in
// each tier, and the line the first-run probe (12.7 E.4) will ask its one
// question with. Four glyphs, one space apart: a flag, a chevron, a check and a
// boost — chosen because they are four DIFFERENT shapes, so a font that has
// patched some of the repertoire and not the rest shows it here rather than at
// the moment a card settles.
//
// It is a sample of the vocabulary and not a font test: rendered in a terminal
// without a patched font, the nerd-font line is exactly the tofu the user is
// being asked about, which is the point.
func GlyphProbeLine(g GlyphSet) string {
	if g >= glyphSetCount {
		g = Plain
	}
	return g.Glyph(GWaitsOn) + " " + g.Glyph(GCollapsed) + " " +
		g.Glyph(GSettled) + " " + g.Glyph(GBoosted)
}
