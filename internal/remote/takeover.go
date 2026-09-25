package remote

// takeover.go is an engine honouring the move-it-here request a window on this
// machine left beside a journal the engine holds (internal/session's
// takeover.go is the request).
//
// A window with no engine behind it (`--no-host`, `--debug`) cannot open a
// conversation an engine holds; it can only ask for it, by writing
// `takeover.json` beside the journal. The agent inside the engine sees that
// request on its own heartbeat and remembers it ([session.Agent.TakeoverAsked]),
// but the announcement it makes rides a surface's task lane — and an engine
// holding a conversation nobody is looking at has no surface to hear it. So the
// request sat there for its whole ten-minute life and the asking window was told
// the other window "did not answer", about a process that has no window.
//
// THE HOST ASKS INSTEAD. It looks at each conversation's agent on a short beat
// (internal/enginehost's doorstep) and closes the one that was asked for, the
// way every other holder lets go: its turn stops where it is and keeps its
// partial reply, its tasks land paused, and the journal lock is released for
// the window waiting on it.

import "github.com/Agent-Field/codeaf/internal/session"

// takeoverAsker is the one agent door this file reads. It is asserted rather
// than added to [WrappedAgent] for the reason the task lane's door is: a
// scripted engine has never heard of a takeover and must stay representable.
type takeoverAsker interface {
	TakeoverAsked() bool
}

// TakeoverAsked reports that another window on this machine has asked for this
// conversation and it has not been let go of yet. A closed conversation has
// nothing left to let go of and answers false.
func (sess *Session) TakeoverAsked() bool {
	if sess == nil {
		return false
	}
	sess.mu.Lock()
	agent, closed := sess.agent, sess.closed
	sess.mu.Unlock()
	if closed {
		return false
	}
	door, ok := agent.(takeoverAsker)
	return ok && door.TakeoverAsked()
}

// ReleaseForTakeover closes this conversation because another window asked for
// it. It is the ordinary close under the takeover's own door, so the stop is
// recorded as a move rather than as the engine going away.
func (sess *Session) ReleaseForTakeover() error {
	return sess.closeFor(session.StopByTakeover, "")
}
