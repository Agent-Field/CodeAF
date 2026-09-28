package tui3

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"errors"
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
	return a.submitBash(a.expandPastes(line))
}

// bashAgent is deliberately separate from Submit: only an explicit shell
// gesture can execute a command, including across a host connection.
type bashAgent interface {
	SubmitBash(context.Context, string) (<-chan session.Event, error)
}

func (a *app) submitBash(line string) tea.Cmd {
	agent, ctx := a.agent, a.ctx
	return a.submitting(line, func() (<-chan session.Event, error) {
		door, ok := agent.(bashAgent)
		if !ok {
			return nil, errors.New("this session cannot run ! commands")
		}
		return door.SubmitBash(ctx, line)
	})
}
