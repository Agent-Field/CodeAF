package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE TIP ROW, AND HOME'S KEYS ROW ─────────────────────────────────────────
//
// The foot of either box is three rows: the tip, the rule, the keys.
//
//	                                  💡 /ask answers right here without opening a conversation ✕
//	─ glm-5.3-flash:auto · ◇ asks ───────────────────────────────────────────────────────────────
//	 › type to search or start something new
//	 → options · alt+p project · alt+e effort · alt+a approvals · / commands     project: ~/codeaf
//
// THE TIP IS RIGHT-ALIGNED OVER THE RULE, one cell in from the edge, directly
// above where the rule used to say the project (the owner's placing,
// 2026-09-22). It is led by a bulb and closed by a cross a pointer can press:
// the cross puts the tip away until the row next changes hands
// ([app.noticeDismiss]). The same bulb, tip and cross, laid out by the same
// function, stand over a conversation's project at the right end of its keys
// row (footswap.go's [app.hintRow]) once the person has been quiet there for
// [chatTipQuiet] (notice.go's THE CONVERSATION'S TIP).
//
// THE PROJECT IS ON THE KEYS ROW NOW, right-justified, and it is the keys that
// keep their room: the path gives up its right end, one ellipsis, where the
// keys leave it no room for the whole, and goes entirely where they leave it
// less than a word. On home it is still the door onto the folder chooser it
// was on the rule (placemouse.go's [app.placeTargetPress]), so its columns are
// recorded where they are drawn, on [app.homeDoor]'s bargain; a conversation's
// keys row does the same for its own workspace (footswap.go's [app.hintRow]).

// homeTipLead is the bulb before a tip on the row.
//
// IT IS AN EMOJI, AND THAT IS THE OWNER'S RULING (2026-09-22) against the
// vocabulary's own no-emoji-in-chrome law (internal/tui2/tokens's glyph.go):
// one bulb, on one row, asked for by name. It is not a slot in the vocabulary
// because the vocabulary refuses the emoji planes on purpose and its width
// gate would refuse this one; and it is not the icon law's to own, because the
// law owns the vocabulary's runes and no other. It measures two cells
// everywhere the renderer measures, and the row is laid out from that
// measurement rather than from a guess.
const homeTipLead = "💡"

// homeTipGap is the cell between the bulb and the tip, and between the tip and
// its cross.
const homeTipGap = " "

// homeTipFloor is the fewest cells of tip worth drawing beside the bulb and
// the cross: under it the row says nothing, because a bulb beside three
// letters and an ellipsis is a row that teaches nothing.
const homeTipFloor = 8

// homeFootPathFloor is the fewest cells of path worth drawing after
// `project: ` on a keys row — the root and an ellipsis, or nothing.
const homeFootPathFloor = 4

// tipLine lays the tip row out: the bulb, the tip, the cross, right-aligned
// to end one cell in from the right edge. It reports the cross's columns, for
// the press, and an empty line where the frame is too narrow for the row to
// say anything.
func (a *app) tipLine(tip string, width int, pal palette) (string, hudSpan) {
	cross := pal.glyph(tokens.GFailed)
	lead := homeTipLead + homeTipGap
	tail := homeTipGap + cross
	room := width - 1 - ansi.StringWidth(lead) - ansi.StringWidth(tail)
	if room < homeTipFloor {
		return "", hudSpan{}
	}
	tip = fit(tip, room)
	pad := width - 1 - ansi.StringWidth(lead) - ansi.StringWidth(tip) - ansi.StringWidth(tail)
	from := pad + ansi.StringWidth(lead) + ansi.StringWidth(tip)
	span := hudSpan{from: from, to: from + ansi.StringWidth(tail)}
	line := strings.Repeat(" ", pad) + lead + paintHint(tip, pal, pal.dim) + homeTipGap + pal.dim(cross)
	return line, span
}

// homeFootLine is home's keys row: the keys one cell in, fitted first, and the
// project right-justified in whatever they leave. It records the path's
// columns in [app.targetFolderSpan] — the same span the rule used to write —
// and clears them where the path does not fit.
func (a *app) homeFootLine(width int, pal palette) string {
	a.targetFolderSpan = hudSpan{}
	// THE LOW-CREDIT LINE, when it applies, stands just left of the project and
	// is fitted before either the keys or the path (credits.go, #1439).
	warning := a.homeCreditWarningFor(width)
	room := width - 2
	if warning != "" {
		room -= ansi.StringWidth(warning) + 1
	}
	hint := hintFit(a.placeHint(), room)
	line := " " + paintHint(hint, pal, pal.dim)
	used := 1 + ansi.StringWidth(hint)
	before := used
	if warning != "" {
		before += 1 + ansi.StringWidth(warning)
	}
	project := a.targetProject()
	if project == "" {
		return withCreditWarning(line, used, width, warning, pal)
	}
	text, span, ok := projectAtRight(project, before, width)
	if !ok {
		return withCreditWarning(line, used, width, warning, pal)
	}
	a.targetFolderSpan = span
	painted := a.paintSeamProject(text, hudSpan{from: ansi.StringWidth(targetProjectLead), to: ansi.StringWidth(text)},
		a.targetHover == hoverSeamProject)
	if warning != "" {
		pad := width - 1 - used - ansi.StringWidth(warning) - hudGap - ansi.StringWidth(text)
		return line + strings.Repeat(" ", max(1, pad)) + pal.warn(warning) + strings.Repeat(" ", hudGap) + painted
	}
	pad := width - 1 - used - ansi.StringWidth(text)
	return line + strings.Repeat(" ", pad) + painted
}

// projectAtRight is the arithmetic both keys rows share: the project after
// `project: `, fitted to what the keys leave and ending one cell in from the
// right edge, with the path's columns on the row. It answers false where the
// keys leave less than a word of path.
func projectAtRight(project string, used, width int) (text string, span hudSpan, ok bool) {
	lead := targetProjectLead
	room := width - 1 - used - hudGap
	if room < ansi.StringWidth(lead)+homeFootPathFloor {
		return "", hudSpan{}, false
	}
	path := fit(project, room-ansi.StringWidth(lead))
	text = lead + path
	from := width - 1 - ansi.StringWidth(path)
	return text, hudSpan{from: from, to: from + ansi.StringWidth(path)}, true
}
