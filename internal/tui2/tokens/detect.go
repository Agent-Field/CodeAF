package tokens

import "strings"

// Glyph tier detection is conservative: UNKNOWN FONT SUPPORT MEANS PLAIN.
// Terminal identity, colour depth and locale cannot establish that the active
// font contains the private-use characters in our vocabulary. Cursor-position
// probes cannot establish it either: a missing-glyph box can occupy the same
// one cell as the icon it replaced. There is no portable terminal query for
// glyph coverage, so auto never enables Nerd Font icons on those signals.
// A person who has selected a patched font can opt in through Display's rich
// setting; the surface folds that explicit choice over this default.
//
// Detection stays a pure function over Env, with a reason for the log. Known
// terminal limitations remain distinguishable from unknown font coverage.

// Reason strings are values rather than prose built at the call site, so the
// log line and the settings sheet say the same sentence about the same cause.
const (
	glyphReasonNoEnv       = "no environment: the plain floor"
	glyphReasonNoTerm      = "TERM is unset or dumb: no capability claim at all"
	glyphReasonLinuxTTY    = "TERM=linux: the Linux console renders a 256-glyph bitmap font and cannot draw private use at all"
	glyphReasonAppleTerm   = "TERM_PROGRAM=Apple_Terminal: Terminal.app ships SF Mono and Menlo, neither of which carries private use"
	glyphReasonCJKLocale   = "an East-Asian locale is set: all private use is East_Asian_Width=Ambiguous, so ambiguous-wide would draw every icon at two cells and tier width parity would break"
	glyphReasonLegacyConIn = "a legacy Windows console: its font fallback for private use is unreliable"
	glyphReasonDefault     = "font coverage is unknown: plain symbols until rich is explicitly selected"
)

// DetectGlyphSet returns the safe starting tier and its reason. A terminal
// name is never evidence of a patched font, including over tmux or SSH, where
// the font belongs to the terminal at the other end of the connection.
// NO_COLOR remains a colour preference and has no bearing on this decision.
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
	return Plain, glyphReasonDefault
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
