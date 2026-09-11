package tui3

import (
	tea "charm.land/bubbletea/v2"
)

// ── EVERY DOOR IS ASKED OFF THE LOOP ────────────────────────────────────────
//
// THE LAW. A door on the agent — answering a question, writing a rule, taking up
// a conversation's record — is a CALL TO ANOTHER PROCESS. Bare `aforge` runs its
// engine in a session host and talks to it over a socket, so this is true of
// every window and not only of `--host` (cmd/aforge's chatv3_local.go). A call
// made from Update is a call the window cannot draw during, cannot take a key
// during, and cannot even read the news of its own answer during — which is how
// one keystroke came to cost ten seconds (doorbell.go tells that story from the
// other end). So no door is asked from Update. It is asked from the command
// Update hands back, and what it said is folded in on the next pass.
//
// WHAT THE PERSON SEES IS DECIDED IMMEDIATELY, WITHOUT WAITING TO BE TOLD. The
// question closes and writes its receipt on the keystroke ([app.closeQuestion]),
// because this window knows what it just sent and a row that sat unchanged for a
// round trip is a row somebody presses twice. If the door then refuses, the fold
// puts the question back with the door's own sentence
// ([app.reopenQuestion]) — the rare road, drawn honestly, rather than the
// common one paid for in frozen frames.
//
// AND A FOLD KNOWS WHETHER IT IS STILL LOOKING AT THE SAME CONVERSATION. A
// person can switch away between the keystroke and the answer; putting the old
// conversation's question back on the new conversation's screen would be this
// surface inventing a question. So the fold is told `here`, which is false when
// the window has moved on, and a fold with something to release — a subscription
// taken by a door that was asked for a conversation nobody is looking at any
// more — releases it there.
//
// [TestAnAnswerOverAConnectionAsksTheFarMachineNothingFromUpdate] is the runtime
// law, and offlooplaw_test.go is the structural one: a door named here may not
// be called outside an [app.offLoop] literal.

// doorMsg is what one door said, on its way back to the update loop.
//
// IT CARRIES THE FOLD AND NOT THE ANSWER. Every door answers something different
// — an error, a record and a stream, nothing at all — and a message per door
// would be a message type per door plus a case in [app.Update] for each. What
// they have in common is the only thing this loop needs: a piece of work to do
// with what came back, on the loop, for the conversation it was asked of.
type doorMsg struct {
	// front is [app.frontGen] when the door was asked: which conversation this
	// window was looking at.
	front int
	fold  func(here bool) tea.Cmd
}

// offLoop asks one door off the update loop and folds what it said back in.
//
// `ask` runs on the command's own goroutine and may take as long as the engine
// takes; it hands back the fold, which runs on the loop and may touch the
// surface. Nothing in `ask` may touch the surface, and nothing in the fold may
// call a door — the two halves are exactly that split.
func (a *app) offLoop(ask func() func(here bool) tea.Cmd) tea.Cmd {
	if ask == nil {
		return nil
	}
	front := a.frontGen
	return func() tea.Msg {
		return doorMsg{front: front, fold: ask()}
	}
}

// doorSaid folds one door's answer in. See [doorMsg] for why the fold is told
// whether the conversation it belongs to is still the one in front.
func (a *app) doorSaid(msg doorMsg) tea.Cmd {
	if msg.fold == nil {
		return nil
	}
	return msg.fold(msg.front == a.frontGen)
}
