package tui3

import (
	"strings"
)

func init() {
	registerHomeBand(homeBand{name: "leftoff", order: bandOrderLeftOff, draw: drawLeftOffBand})
}

func drawLeftOffBand(a *app, ctx bandContext) []string {
	// THE JOURNAL WAS PEEKED AT WHEN THIS CARD ARRIVED (homecardread.go's
	// [app.readHomeLeftOff]). A body does not open files.
	summary := a.home.last[ctx.subject.row.Transcript]
	ask, answer := strings.TrimSpace(summary.LastUser), lastSentence(summary.LastAssistant)
	if ask == "" && answer == "" {
		return nil
	}
	var rows []string
	if ask != "" {
		rows = append(rows, ctx.pal.muted(fit("› "+ask, ctx.width)))
	}
	if answer != "" {
		wrapped := wrap(answer, ctx.width)
		if len(wrapped) > 2 {
			wrapped = wrapped[:2]
		}
		for _, line := range wrapped {
			rows = append(rows, ctx.pal.dim(fit(line, ctx.width)))
		}
	}
	return rows
}

// lastSentence keeps the closing sentence of a reply, which is the part that
// says where the exchange came to. Punctuation followed by whitespace is the
// boundary; prose without one remains whole.
func lastSentence(text string) string {
	text = strings.TrimSpace(text)
	for i := len(text) - 2; i >= 0; i-- {
		if (text[i] == '.' || text[i] == '!' || text[i] == '?') && text[i+1] == ' ' {
			return strings.TrimSpace(text[i+1:])
		}
	}
	return text
}
