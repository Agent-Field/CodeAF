package tui3

import (
	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

// An observer ends on a broken connection. Reconnect obtains another atomic
// history/current-turn boundary, rather than treating an old ownership cursor
// as permission to suppress a new live tail.
func (a *app) refreshHostedReplay() tea.Cmd {
	if a.hostReplayLoading {
		return nil
	}
	door, ok := a.agent.(attachReplayer)
	if !ok {
		return nil
	}
	if a.hostCalls > 0 {
		a.hostReplayWaiting = true
		return nil
	}
	a.hostReplayWaiting = false
	a.hostReplayLoading = true
	return a.offLoop(func() func(bool) tea.Cmd {
		entries, events, stop := door.AttachReplay()
		return func(here bool) tea.Cmd {
			if !here {
				if stop != nil {
					stop()
				}
				return nil
			}
			if a.streamStop != nil {
				a.streamStop()
				a.streamStop = nil
			}
			a.gen++
			a.stream = nil
			// The new observer starts at the current turn's beginning. Its
			// earlier visible prefix is replaced, not retained as "in the gap".
			a.entries = nil
			a.recordRows = 0
			a.replayList(entries)
			var joined tea.Cmd
			if events != nil {
				joined = a.adoptTurn(events, stop)
			} else {
				a.state = stateIdle
			}
			a.touch()
			return tea.Batch(joined, a.finishHostedReplay())
		}
	})
}

func (a *app) finishHostedReplay() tea.Cmd {
	a.hostReplayLoading = false
	pending := a.hostReplayPending
	a.hostReplayPending = nil
	var cmds []tea.Cmd
	for _, msg := range pending {
		if msg.gen == a.convGen {
			if a.hostReplayLoading {
				a.hostReplayPending = append(a.hostReplayPending, msg)
			} else if msg.turn.Replay {
				cmds = append(cmds, a.refreshHostedReplay())
			} else {
				cmds = append(cmds, a.admitFollowing(msg.turn))
			}
		}
	}
	if !a.hostReplayLoading {
		deferred := a.hostDeferred
		a.hostDeferred = nil
		for _, start := range deferred {
			cmds = append(cmds, start())
		}
		cmds = append(cmds, a.startFollow())
	}
	return tea.Batch(cmds...)
}

// A snapshot and a locally initiated turn cannot own the surface at the same
// time. Calls already sent settle before refresh begins; new calls wait until
// its atomic boundary has been folded. The composer itself remains responsive.
type hostCall struct{ generation int }

func (a *app) deferHosted(start func() tea.Cmd) bool {
	if !a.hostReplayLoading && !a.hostReplayWaiting {
		return false
	}
	a.hostDeferred = append(a.hostDeferred, start)
	return true
}

func (a *app) hostCallStarted() *hostCall {
	if _, ok := a.agent.(interface{ ReplayCovers(session.Event) bool }); !ok {
		return nil
	}
	a.hostCalls++
	return &hostCall{generation: a.convGen}
}

func (a *app) hostCallSettled(call *hostCall) tea.Cmd {
	if call == nil || call.generation != a.convGen {
		return nil
	}
	if a.hostCalls > 0 {
		a.hostCalls--
	}
	if a.hostCalls == 0 && a.hostReplayWaiting {
		return a.refreshHostedReplay()
	}
	return nil
}

func (a *app) hostStreamCovered(ch <-chan session.Event) func() bool {
	if agent, ok := a.agent.(interface {
		ReplayCoversStream(<-chan session.Event) func() bool
	}); ok {
		return agent.ReplayCoversStream(ch)
	}
	return nil
}
