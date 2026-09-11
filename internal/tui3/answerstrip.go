package tui3

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// answerStrip is the one-line bridge between the card being read and home's
// keys, AND IT IS AN ANSWER, NOT A MIRROR: it draws only when the cursor's row
// carries a question the person can answer from here. An earlier draft fell
// back to previewing whatever the cursor was on, and that permanent row moved
// home's geometry on every frame — the pad law and the straight gutter both
// broke for a line that repeated what the list already said. A row that only
// appears when something can be answered is the only row whose cost the foot's
// budget can always explain.
func (a *app) answerStrip(width int, now time.Time) []string {
	line, ok := a.home.previewLine()
	if !ok || width < 1 || line.kind != homeSession {
		return nil
	}
	lead := "› "
	row := a.homeTrue(line.row)
	question, offered := answerable(row, now)
	if !offered || (a.leaveAnswer == nil && !a.answeringHere(row)) {
		return nil
	}
	if _, sent := a.answerSent(row, question); sent {
		return nil
	}
	if a.answersStepAside(row) {
		return nil
	}
	chips := answerChips(question)
	if len(chips) == 0 {
		return nil
	}
	answers := make([]string, 0, len(chips))
	for _, chip := range chips {
		answers = append(answers, chip.text)
	}
	tail := strings.Join(answers, answerChipGap)
	tailWidth := ansi.StringWidth(tail)
	// THE ANSWERS ARE KEYS AND WORDS, not a stripe of the question hue (owner
	// ruling 2026-09-11, colour pick C): the key steps up to the payload hue and
	// its word is ink, exactly as on the panel above a conversation's box.
	paint := func(text string) string { return a.pal.ink(text) }
	if tailWidth >= width {
		return []string{paint(fit(tail, width))}
	}
	room := width - tailWidth - 1
	left := fit(lead+strings.TrimSpace(question.Text), room)
	gap := width - ansi.StringWidth(left) - tailWidth
	return []string{a.pal.ink(left) + strings.Repeat(" ", gap) + paint(tail)}
}
