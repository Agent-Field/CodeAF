package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Interruption is the key people reach for and the one this surface used to
// punish: escape, while a reply was arriving, quit the whole session. Nothing
// below it could be stopped either — the head answered on the process's own
// context, so a turn the reader had given up on ran to completion and landed in
// a conversation that had moved on.
//
// The rung this file adds sits above the draft and the quit in the back-out
// ladder, and it holds the same law the rest of the product holds: a stopped
// turn still ends in words. The head posts them; this side only freezes what is
// already drawn so the two halves meet on the same sentence.

// quitConfirmWindow is how long the second ctrl+c means it. Long enough to read
// the hint, short enough that a press a minute later is a fresh intention.
const quitConfirmWindow = 4 * time.Second

// updateQuitKey is ctrl+c. On an idle surface it still ends the session on the
// first press, because that is what the chord is for and nothing is lost. With
// a turn in flight it is the interrupt instead — one accidental press must not
// take a session down with work in it — and it says so, so the second press
// inside the window is an answer rather than a surprise.
func (m *Model) updateQuitKey() (tea.Cmd, bool) {
	now := m.standingTime()
	if !m.quitArmedAt.IsZero() && now.Sub(m.quitArmedAt) <= quitConfirmWindow {
		return tea.Quit, true
	}
	if !m.interruptTurn() {
		return tea.Quit, true
	}
	m.quitArmedAt = now
	return m.showStatus("stopped · ctrl+c again to quit"), true
}

// turnInFlight is a head turn this window is still owed: a reply arriving, or
// one that has not begun to. A turn already stopped is not in flight — that is
// what lets a second escape carry on down the ladder rather than stopping the
// same reply forever.
func (m *Model) turnInFlight() bool {
	if m.streamInterrupted {
		return false
	}
	return m.awaitingSeq != 0 || m.streamMode == streamReal
}

// interruptTurn stops the turn and keeps every word of it that reached the
// screen. It reports false only when there was nothing to stop, which is what
// keeps the escape ladder's lower rungs reachable.
func (m *Model) interruptTurn() bool {
	if !m.turnInFlight() {
		return false
	}
	// Whitespace is not an answer: a stream holding only blanks is frozen into
	// nothing the reader can see, and the head would mark nothing in return.
	// The partial is trimmed to exactly what the head will keep, so the durable
	// line it posts still begins with the words already on screen.
	partial := strings.TrimSpace(m.streamTarget)
	visible := m.streamVisible() && partial != ""
	stopped := false
	if m.commander != nil {
		stopped = m.commander.Interrupt(partial)
	}
	if stopped && visible {
		// The reply stops moving where it is. The head's durable line carries
		// these words plus the mark, so it lands on top of them and only the
		// tail types itself in.
		m.streamProviderDone = true
		m.streamTarget = partial
		m.streamShown = partial
		m.streamInterrupted = true
	} else {
		// Nothing of the reply was on screen, or the head had already let the
		// turn go. Either way this window holds no piece of it: the state is
		// reset in full and whatever lands next draws normally.
		m.finishStream()
	}
	m.clearAwaitingReply()
	if stopped && !visible {
		m.restoreSentDraft()
	}
	m.refreshChat()
	m.setSize(m.width, m.height)
	return true
}

// restoreSentDraft hands back the turn the reader just stopped. It is the
// correction flow and only that: a turn interrupted before a single word of its
// answer arrived is one nobody has been answered on yet, so the words go back
// where they can be edited. Once any of the reply is on screen the exchange has
// happened, and re-filling the composer would be undoing the wrong thing. A
// draft already being typed always wins over both.
func (m *Model) restoreSentDraft() {
	if len(m.inputHistory) == 0 || strings.TrimSpace(m.input.Value()) != "" {
		return
	}
	m.setDraft(m.inputHistory[len(m.inputHistory)-1])
}
