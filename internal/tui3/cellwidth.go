package tui3

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// THE SURFACE MEASURES THE WAY THE TERMINAL WILL DRAW, AND NOT THE OTHER WAY.
//
// THE DEFECT THIS CLOSES, measured rather than argued: the sidebar rail sat at
// column 91 on every row of a conversation except the one carrying two `❤️` and
// the one carrying two `🇯🇵`, where it sat at 89. A rail is a straight line down
// the frame, so two cells of bend is the whole surface looking broken.
//
// The cause is two answers to one question. This package lays out with
// [ansi.StringWidth], which is `ansi.GraphemeWidth` — the modern reading, where
// an emoji with a variation selector is two cells wide. The renderer underneath
// composes its cell grid with `ansi.WcWidth`, the older reading, where the same
// sequence is one. They agree about everything else, including the ZWJ family
// sequences an earlier note blamed for this; that note was read off a
// `capture-pane` dump, which is one entry per grid CELL and so cannot be indexed
// by rune to find a bend.
//
// WHICH ONE IS RIGHT DEPENDS ON THE TERMINAL, which is why neither can simply be
// hardcoded. bubbletea asks the terminal about mode 2027 at startup and switches
// its buffer to grapheme widths only if the answer comes back; tmux says no, and
// a terminal that says yes would be measured wrongly by the opposite hardcoding.
// The answer arrives as a [tea.ModeReportMsg], which bubbletea acts on AND passes
// through to this model — so the surface can hold the same fact the renderer
// acted on rather than guessing at it.
//
// [widthMethod] starts at `ansi.WcWidth` because that is what the renderer starts
// at: a frame drawn before the terminal has answered is drawn by the older
// reading, and measuring it by the newer one would bend the rail for exactly as
// long as the handshake takes.
type cellRuler struct {
	method ansi.Method
}

// noteModeReport takes the terminal's answer about mode 2027, which is the same
// message and the same condition bubbletea uses to switch its own renderer
// (its tea.go, `case ansi.ModeUnicodeCore`). Reading the answer in one place
// keeps the two from disagreeing about a frame.
func (r *cellRuler) noteModeReport(msg tea.ModeReportMsg) {
	if msg.Mode != ansi.ModeUnicodeCore {
		return
	}
	switch msg.Value {
	case ansi.ModeReset, ansi.ModeSet, ansi.ModePermanentlySet:
		r.method = ansi.GraphemeWidth
	}
}

// cells is how wide this string will be drawn, in the reading the renderer is
// currently using. Every LAYOUT decision that has to line up with a drawn column
// belongs on this rather than on [ansi.StringWidth]; the rail is the first of
// them, because it is the one where being wrong is a bent line down the frame.
func (r *cellRuler) cells(text string) int {
	if r.method == ansi.GraphemeWidth {
		return ansi.StringWidth(text)
	}
	return ansi.WcWidth.StringWidth(text)
}
