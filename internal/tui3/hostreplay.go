package tui3

import tea "charm.land/bubbletea/v2"

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
			a.follows = nil
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
			cmds = append(cmds, a.admitFollowing(msg.turn))
		}
	}
	return tea.Batch(cmds...)
}
