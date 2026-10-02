package tui3

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
)

// Destructive confirmations share two choices and visible keyboard hints. A
// terminal must fit the complete card before an affirmative choice can act.
func (a *app) simpleConfirmCard(question string, cursor int, message string) (wallCard, bool) {
	width, height := a.size()
	w := min(width-2, 64)
	inner := w - 4
	hint := "enter choose · esc cancel"
	if inner < ansi.StringWidth(hint) {
		return wallCard{}, false
	}
	var lines []wallCardLine
	for _, line := range wrap(question, inner) {
		lines = append(lines, wallCardLine{s: a.pal.ink(line)})
	}
	lines = append(lines, wallCardLine{})
	for i, word := range []string{"cancel", "yes"} {
		lines = append(lines, wallCardLine{s: wallPopRowPaint(a.pal, word, inner, i == cursor), hits: []wallHit{{x1: inner, y1: 1, arg: i}}})
	}
	if message != "" {
		for _, line := range wrap(message, inner) {
			lines = append(lines, wallCardLine{s: a.pal.warn(line)})
		}
	}
	lines = append(lines, wallCardLine{}, wallCardLine{s: strings.Repeat(" ", inner-ansi.StringWidth(hint)) + a.pal.muted(hint)})
	h := len(lines) + 2
	if h > height-2 {
		return wallCard{}, false
	}
	x, y := (width-w)/2, max((height-h)/3, 1)
	return wallCardBuild(a.pal, "", lines, x, y, w, 1, 0), true
}

func (a *app) confirmCardOver(frame string, card wallCard) string {
	width, height := a.size()
	rows := strings.Split(frame, "\n")
	for len(rows) < height {
		rows = append(rows, "")
	}
	for i, row := range card.rows {
		if card.y+i < height {
			rows[card.y+i] = wallSplice(rows[card.y+i], row, card.x, width)
		}
	}
	return strings.Join(rows[:height], "\n")
}
