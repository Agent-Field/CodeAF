package tui3

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

const conversationCloseWithin = time.Second
const closeSaveFailedWord = "this chat could not be saved; its unsaved turns stay on this computer for the next start"
const closeStillFinishingWord = "this chat is still finishing in the background"

type conversationClosedMsg struct {
	err       error
	finishing bool
}

func quitAfterClosing(work func()) tea.Cmd {
	return func() tea.Msg {
		done := make(chan struct{})
		go func() { defer close(done); work() }()
		timer := time.NewTimer(conversationCloseWithin)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
		}
		return tea.QuitMsg{}
	}
}

// Closing releases the keyboard after a bounded wait. The close method has no
// cancellation door, so it can finish later without holding the update loop.
func closeConversationLater(agent Agent, door session.StopDoor, report func(error)) tea.Cmd {
	return func() tea.Msg {
		done := make(chan error, 1)
		go func() {
			agent.InterruptFor(door)
			err := agent.Close()
			if err != nil && report != nil {
				report(err)
			}
			done <- err
		}()
		timer := time.NewTimer(conversationCloseWithin)
		defer timer.Stop()
		select {
		case err := <-done:
			return conversationClosedMsg{err: err}
		case <-timer.C:
			return conversationClosedMsg{finishing: true}
		}
	}
}
