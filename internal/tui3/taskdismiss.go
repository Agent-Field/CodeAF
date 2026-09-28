package tui3

import "github.com/Agent-Field/codeaf/internal/session"

// Dismissal is presentation state. It never stops, deletes or accepts work.
func (a *app) dismissDone(i int) bool {
	card := a.doneCardAt(i)
	if card == nil || card.dismissed || card.status.Tier == session.TaskTierYourCall {
		return false
	}
	card.dismissed = true
	a.sel = -1
	a.touch()
	return true
}

func (a *app) dismissNotifications(arg string) {
	if a.roomOpen() {
		a.note("return to the conversation to dismiss notifications")
		return
	}
	if arg != "" && arg != "undo" {
		a.note("use /dismiss or /dismiss undo")
		return
	}
	for i := range a.entries {
		if arg == "undo" {
			if card := a.doneCardAt(i); card != nil {
				card.dismissed = false
			}
		} else {
			a.dismissDone(i)
		}
	}
	a.touch()
}
