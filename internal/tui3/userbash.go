package tui3

import (
	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

func bashRefusal(line string, attachments, busy bool) string {
	command, _ := session.BashCommand(line)
	switch {
	case command == "":
		return session.BashEmptyWord
	case attachments:
		return "remove attachments before running a ! command"
	case busy:
		return session.BashBusyWord
	}
	return ""
}

// enterBash spends a command only after it can run. Shell syntax must bypass
// slash tags, mentions, model setup and the alternate send doors.
func (a *app) enterBash(line string) tea.Cmd {
	if tag := a.missingPaste(line); tag != "" {
		a.note(draftOrphanSendWord + " · " + tag)
		return nil
	}
	if refusal := bashRefusal(a.pastesUnfolded(line), len(a.chips) > 0, a.stream != nil || a.state == stateWorking); refusal != "" {
		a.note(refusal)
		return nil
	}
	a.input.reset()
	a.endRecall()
	a.closeLists()
	a.stick = true
	a.spendWelcome()
	a.remember(line)
	a.dropDraft()
	return a.submit(a.expandPastes(line))
}
