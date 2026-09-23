package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// runAskCommand uses the existing home exchange door. The command stays in the
// box until there are words to ask, and a refusal leaves the draft editable.
func (a *app) runAskCommand(text string) tea.Cmd {
	var shown tea.Cmd
	if !a.at(pageHome) {
		shown = a.showPage(pageHome)
	}
	text = strings.TrimSpace(text)
	a.home.box.setText("/ask " + text)
	a.home.picked = false
	a.home.build()
	return tea.Batch(shown, a.askHere(text))
}
