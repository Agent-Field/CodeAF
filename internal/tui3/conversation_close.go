package tui3

import (
	"errors"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncwords"
)

// conversationCloseWithin bounds how long the surface waits for engine cleanup.
const conversationCloseWithin = time.Second
const closeSaveFailedWord = "this chat could not be saved; its unsaved turns stay on this computer for the next start"
const closeStillFinishingWord = "this chat is still finishing in the background"

// conversationClosedMsg returns a closing result without blocking the update loop.
type conversationClosedMsg struct {
	err       error
	notices   []string
	finishing bool
	address   string
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
func closeConversationLater(agent Agent, door session.StopDoor, report func(error), started func(<-chan struct{}), lateFailure func(error), address string, lateNotices ...func([]string)) tea.Cmd {
	done, finished := make(chan conversationClosedMsg, 1), make(chan struct{})
	var mu sync.Mutex
	late := false
	if started != nil {
		started(finished)
	}
	go func() {
		defer close(finished)
		err := leaveAgentFor(agent, door)
		if err != nil && report != nil {
			report(err)
		}
		lines := takeSavingNotices(agent)
		notifyLate := func() bool {
			mu.Lock()
			defer mu.Unlock()
			done <- conversationClosedMsg{err: err, address: address, notices: lines}
			return late
		}()
		notifyLines := notifyLate && len(lines) > 0
		notify := notifyLate && err != nil && !errors.Is(err, session.ErrSavingUnanswered) && lateFailure != nil
		if notify {
			lateFailure(err)
		}
		if notifyLines && len(lateNotices) > 0 && lateNotices[0] != nil {
			lateNotices[0](lines)
		}
	}()
	return func() tea.Msg {
		timer := time.NewTimer(conversationCloseWithin)
		defer timer.Stop()
		select {
		case result := <-done:
			return result
		case <-timer.C:
			mu.Lock()
			defer mu.Unlock()
			select {
			case result := <-done:
				return result
			default:
				late = true
			}
			return conversationClosedMsg{finishing: true, address: address}
		}
	}
}

// closeSaveWords uses the same local receipt as app quit before claiming turns are saved here.
func closeSaveWords(err error, finishing bool, address string) string {
	if line := syncwords.SavedHereWords(err, address); line != "" {
		return line
	}
	switch {
	case errors.Is(err, session.ErrSavingUnanswered), err == nil && finishing:
		return closeStillFinishingWord
	case err != nil:
		return closeSaveFailedWord
	default:
		return ""
	}
}

// finishAgentSaving gives in-process chats the same final release as an engine close.
func finishAgentSaving(agent Agent, err error) error {
	if local, ok := agent.(interface{ FinishSaving() error }); ok {
		return errors.Join(err, local.FinishSaving())
	}
	return err
}

func takeSavingNotices(agent Agent) []string {
	if source, ok := agent.(interface{ TakeSavingNotices() []string }); ok {
		return source.TakeSavingNotices()
	}
	return nil
}
