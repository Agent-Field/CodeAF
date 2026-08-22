package tui3

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// answerStrip is the one-line bridge between the card being read and home's
// keys. It reads the preview once so hover and cursor cannot make the strip
// describe different rows in the same frame.
func (a *app) answerStrip(width int, now time.Time) []string {
	line, ok := a.home.previewLine()
	if !ok || width < 1 {
		return nil
	}
	lead := "› "
	if line.kind != homeSession {
		return oneAnswerStripRow(a.answerStripPreview(line, lead, width))
	}
	row := a.homeTrue(line.row)
	question, offered := answerable(row, now)
	if !offered || (a.leaveAnswer == nil && !a.answeringHere(row)) {
		return oneAnswerStripRow(a.answerStripPreview(line, lead, width))
	}
	if _, sent := a.answerSent(row, question); sent {
		return oneAnswerStripRow(a.answerStripPreview(line, lead, width))
	}
	chips := answerChips(question)
	if len(chips) == 0 {
		return oneAnswerStripRow(a.answerStripPreview(line, lead, width))
	}
	answers := make([]string, 0, len(chips))
	for _, chip := range chips {
		answers = append(answers, chip.text)
	}
	tail := strings.Join(answers, answerChipGap)
	tailWidth := ansi.StringWidth(tail)
	if tailWidth >= width {
		return []string{a.pal.ask(fit(tail, width))}
	}
	room := width - tailWidth - 1
	left := fit(lead+strings.TrimSpace(question.Text), room)
	gap := width - ansi.StringWidth(left) - tailWidth
	return []string{a.pal.ink(left) + strings.Repeat(" ", gap) + a.pal.ask(tail)}
}

func oneAnswerStripRow(row string) []string {
	if row == "" {
		return nil
	}
	return []string{row}
}

// answerStripPreview keeps the existing hover-or-cursor choice visible when
// there is no answer to take. Rows without a one-line card title draw nothing,
// preserving THE EMPTINESS LAW instead of inventing a label for them.
func (a *app) answerStripPreview(line homeLine, lead string, width int) string {
	var text string
	switch line.kind {
	case homeSession:
		text = homeName(a.homeTrue(line.row))
	case homeItem:
		text = line.item.Title()
	case homeProject:
		text = line.project
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return a.pal.dim(fit(lead+text, width))
}
