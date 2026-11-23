package tui3

import tea "charm.land/bubbletea/v2"

// nextErrand gives wakes and parked messages the same ordering as the chat's
// follow-up queue: finish the current stream before adopting another turn.
func (a *app) nextErrand(ex *homeExchange) tea.Cmd {
	if ex.stream != nil || ex.sending {
		return nil
	}
	if len(ex.streams) > 0 {
		ex.stream, ex.streams = ex.streams[0], ex.streams[1:]
		ex.startTurn(a.now())
		return tea.Batch(waitErrand(ex, ex.stream), a.wake())
	}
	if len(ex.parks) > 0 {
		next := ex.parks[0]
		ex.parks = ex.parks[1:]
		ex.startTurn(a.now())
		return tea.Batch(errandSendChips(ex, next.text, next.chips), a.wake())
	}
	ex.working = false
	return nil
}
