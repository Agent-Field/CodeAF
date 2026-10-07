package tui3

import (
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

// Destructive confirmations share two choices and visible keyboard hints. A
// terminal must fit the complete card before an affirmative choice can act.
func (a *app) simpleConfirmCard(question string, cursor int, message string) (wallCard, bool) {
	return a.choiceConfirmCard("", question, []string{"cancel", "yes"}, cursor, message, 64)
}

// Titles stay on the border. Options wrap without a second details pane, and
// the selected option must fit fully before its destructive action can run.
func (a *app) choiceConfirmCard(title, question string, words []string, cursor int, message string, limit int) (wallCard, bool) {
	return a.choiceConfirmCardAt(title, question, words, cursor, message, limit, nil)
}

// A refusal can name many teams. Its text scrolls independently while the
// choices and keyboard hints stay visible, even in a short terminal.
func (a *app) choiceConfirmCardAt(title, question string, words []string, cursor int, message string, limit int, messageTop *int) (wallCard, bool) {
	width, height := a.size()
	w := min(width-2, limit)
	inner := w - 4
	hint := "enter choose · esc cancel"
	if inner < ansi.StringWidth(hint) || title != "" && ansi.StringWidth(title)+5 > w {
		return wallCard{}, false
	}
	var lines []wallCardLine
	if question != "" {
		for _, line := range wrap(question, inner) {
			lines = append(lines, wallCardLine{s: a.pal.ink(line)})
		}
	}
	lines = append(lines, wallCardLine{})
	room := height - len(lines) - 6
	messageRows := wrap(message, inner)
	if message == "" {
		messageRows = nil
	}
	messageRange := ""
	if messageTop != nil && len(messageRows) > 0 {
		options := 0
		for _, word := range words {
			options += len(wrap(word, inner))
		}
		capacity := room - options
		if capacity < 1 {
			return wallCard{}, false
		}
		if len(messageRows) > capacity {
			capacity--
			if capacity < 1 {
				return wallCard{}, false
			}
			*messageTop = min(max(*messageTop, 0), len(messageRows)-capacity)
			end := *messageTop + capacity
			messageRange = fmt.Sprintf("%d-%d of %d · scroll", *messageTop+1, end, len(messageRows))
			messageRows = messageRows[*messageTop:end]
		} else {
			*messageTop = 0
		}
	}
	room -= len(messageRows)
	if messageRange != "" {
		room--
	}
	start, count := min(cursor, len(words)-1), 0
	if start < 0 {
		return wallCard{}, false
	}
	count = len(wrap(words[start], inner))
	if count > room {
		return wallCard{}, false
	}
	for start > 0 && count+len(wrap(words[start-1], inner)) <= room {
		start--
		count += len(wrap(words[start], inner))
	}
	count = 0
	for i := start; i < len(words); i++ {
		wrapped := wrap(words[i], inner)
		if count+len(wrapped) > room {
			break
		}
		for _, line := range wrapped {
			lines = append(lines, wallCardLine{s: wallPopRowPaint(a.pal, line, inner, i == cursor), hits: []wallHit{{x1: inner, y1: 1, arg: i}}})
		}
		count += len(wrapped)
	}
	for _, line := range messageRows {
		lines = append(lines, wallCardLine{s: a.pal.warn(line)})
	}
	if messageRange != "" {
		lines = append(lines, wallCardLine{s: a.pal.muted(messageRange)})
	}
	lines = append(lines, wallCardLine{}, wallCardLine{s: strings.Repeat(" ", inner-ansi.StringWidth(hint)) + a.pal.muted(hint)})
	h := len(lines) + 2
	if h > height-2 {
		return wallCard{}, false
	}
	x, y := (width-w)/2, max((height-h)/3, 1)
	return wallCardBuild(a.pal, title, lines, x, y, w, 1, 0), true
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
