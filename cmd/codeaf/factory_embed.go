package main

import (
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// ── THE ITEM PAGE'S CENTER: A RUNNING STEP'S CHAT, LIVE ─────────────────────
//
// The item page hosts the selected step's chat in its center through the
// window's own Open door (internal/tui3's factory_host.go). A step that is
// RUNNING is a conversation this process already holds: the runner's stage
// agent ([stageMaker.Open]), with the flock on its journal. Opening that
// journal a second time is refused by the lock, and on the owner's run
// (2026-10-09, a PR's `read` step) the center said `opening the chat…` for
// good while the log row went on showing the step's bash and fetches.
//
// SO THE DOOR HANDS THE LIVE AGENT ITSELF, as a view ([liveStepView]): the
// window attaches to its turn in flight (the record and the stream as one
// reading) and to every turn it wakes into, exactly as it attaches to a
// conversation it keeps behind, and the center streams the step as a normal
// chat streams. Letting go of the view is a detach that ends nothing: the
// step is the runner's, and only the runner closes it. When it does (the
// round ended), the view says so ([session.Agent.Closed]) and the page opens
// the journal again, a conversation of the window's own that can be carried
// on from its box.
//
// THE OTHER DIRECTION HOLDS TOO ([stageTakeJournal]): a step's chat the
// window opened from its journal while the step stood paused is let go of
// when the runner resumes that step in the same chat, so the run never meets
// a lock held by a view.

// liveStepView is a conversation another owner in this process holds, lent to
// a window: everything is the agent's but the ending, which is not the
// window's to do.
type liveStepView struct {
	*session.Agent
}

// Close is a view going: the agent is the runner's and goes on.
func (v liveStepView) Close() error { return nil }

// Detach is the same, said the way a window leaving a conversation says it.
func (v liveStepView) Detach() error { return nil }

// WorkOutlivesExit is true: the step keeps working when the view goes.
func (v liveStepView) WorkOutlivesExit() bool { return true }

// liveConversation is the conversation open in this process on transcript, as
// a view a window can hold, and false when none is open.
func liveConversation(workspace, transcript string) (tui3.Conversation, bool) {
	agent, ok := session.LiveAgentFor(transcript)
	if !ok {
		return tui3.Conversation{}, false
	}
	return tui3.Conversation{
		Agent:       liveStepView{agent},
		SessionFile: transcript,
		Workspace:   strings.TrimSpace(workspace),
		Resumed:     true,
	}, true
}

// stageTakeJournal makes a step's chat free for the runner to reopen: a
// conversation this process holds on it with no turn in flight (a window's
// view of the paused step) is closed, and one mid-turn is a refusal that says
// why. Nothing held is nothing done.
func stageTakeJournal(transcript string) error {
	agent, ok := session.LiveAgentFor(transcript)
	if !ok {
		return nil
	}
	if agent.TurnRunning() {
		return errors.New("this step's chat is answering in a window; run it again when that turn ends")
	}
	return agent.Close()
}
