package tui3

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/standing"
)

// standActivityUpdateWord formats the activity/update tail of a standing card row,
// switching on the action kind so task executions display "task:" instead of "said:".
func standActivityUpdateWord(card *standingCard) string {
	if card == nil {
		return ""
	}
	if card.update == "fired" {
		return standFiredWord(card.item.Does.Kind, card.text)
	}
	return standUpdateWord(card.update, card.text)
}

// standFiredWord returns the action-specific label for a firing event.
// Delegated tasks display "task: " (or "task" if empty text);
// say actions display "said: " (or "ran" if empty text).
func standFiredWord(kind standing.ActionKind, text string) string {
	text = strings.TrimSpace(text)
	switch kind {
	case standing.ActionTask:
		if text == "" {
			return "task"
		}
		return "task: " + text
	default:
		if text == "" {
			return "ran"
		}
		return "said: " + text
	}
}
