package tui3

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

// conversationCloseWithin bounds how long the surface waits for engine cleanup.
const conversationCloseWithin = time.Second
const closeSaveFailedWord = "this chat could not be saved; its unsaved turns stay on this computer for the next start"
const closeStillFinishingWord = "this chat is still finishing in the background"

// conversationClosedMsg returns a closing result without blocking the update loop.
type conversationClosedMsg struct {
	err       error
	finishing bool
}

// quitAfterClosing restores the terminal even when cleanup is still running.
func quitAfterClosing(work func(), started func(<-chan struct{})) tea.Cmd {
	done := make(chan struct{})
	if started != nil {
		started(done)
	}
	go func() { defer close(done); work() }()
	return func() tea.Msg {
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
func closeConversationLater(agent Agent, door session.StopDoor, report func(error), started func(<-chan struct{})) tea.Cmd {
	done, finished := make(chan error, 1), make(chan struct{})
	if started != nil {
		started(finished)
	}
	go func() {
		defer close(finished)
		agent.InterruptFor(door)
		err := agent.Close()
		if err != nil && report != nil {
			report(err)
		}
		done <- err
	}()
	return func() tea.Msg {
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
