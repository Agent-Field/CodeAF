package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE FOLLOW-UP: ctrl+q, "and after that, do this".
//
// There are two ways to say something to a working session and they mean
// different things, so they are two keys:
//
//	enter    STEERING. The message lands inside the running turn at its next
//	         step boundary — one more thing the person said mid-work.
//	ctrl+q   FOLLOW-UP. The message waits for the work to finish and then starts
//	         a turn of its own.
//
// Steering an "and then write the tests" is an interruption of the thing it is
// meant to follow; queueing a "no, the OTHER file" arrives too late to help.
// The session holds both queues (agent.go) and this file is the surface's half
// of the second one.
//
// The message is NOT drawn when it is queued. It is drawn when its turn starts,
// where it actually lands in the conversation — a user line painted above the
// rest of a turn it comes after would put the transcript in an order that never
// happened. Until then the count sits above the box, dim, so a person can see
// that something of theirs is waiting.

// queued is one follow-up: what was typed, and the stream the turn it starts
// will speak on. The channel exists from the moment the message is queued —
// session hands it back immediately — so there is nothing to wait for later.
type queued struct {
	text string
	ch   <-chan session.Event
}

// followMsg carries the session's answer back into the program loop. FollowUp
// takes the agent's lock, and the Update loop is not a place to wait.
type followMsg struct {
	text string
	ch   <-chan session.Event
	err  error
}

// followUp is ctrl+q. An empty draft does nothing at all: there is no message
// to queue, and a key that queued a blank one would be a key that spends a turn.
func (a *app) followUp() tea.Cmd {
	line := strings.TrimSpace(a.input.String())
	if line == "" {
		return nil
	}
	agent := a.agent
	a.input.reset()
	a.endRecall()
	a.closeLists()
	// A queued message is a submitted one for every purpose the person has: it
	// is remembered by ↑, and the draft file it came from is done with.
	a.remember(line)
	a.dropDraft()
	a.stick = true
	a.touch()
	return func() tea.Msg {
		ch, err := agent.FollowUp(line)
		return followMsg{text: line, ch: ch, err: err}
	}
}

// queueFollow takes the session's answer.
//
// A follow-up queued while NOTHING is running starts immediately — session says
// so, and it is the right answer: there is no turn end coming to drain it. So a
// stream we are not already pumping is adopted here rather than at the next
// close, which would never arrive.
func (a *app) queueFollow(msg followMsg) tea.Cmd {
	if msg.err != nil {
		a.note("follow-up failed: " + msg.err.Error())
		return nil
	}
	if msg.ch == nil {
		return nil
	}
	a.follows = append(a.follows, queued{text: msg.text, ch: msg.ch})
	a.touch()
	if a.stream != nil {
		return nil
	}
	return a.startFollow()
}

// startFollow begins the next queued message's turn: the person's line lands in
// the transcript, and the channel session already handed us becomes the stream.
//
// It is [app.submit] without the submit — the turn was started by the session
// when the last one ended, so there is nothing to ask for and nothing to wait on.
func (a *app) startFollow() tea.Cmd {
	if a.stream != nil || len(a.follows) == 0 {
		return nil
	}
	next := a.follows[0]
	a.follows = a.follows[1:]

	a.closeLive()
	a.turn++
	a.sel = -1
	a.entries = append(a.entries, entry{kind: entryUser, text: next.text, turn: a.turn})
	a.state = stateWorking
	a.lastDelta = time.Now()
	// The generation is bumped for the same reason [app.adopt] bumps it: the
	// stream that just closed may still have messages in flight, and they belong
	// to a turn that is over.
	a.gen++
	a.stream = next.ch
	a.follow()
	a.touch()
	return tea.Batch(waitEvent(next.ch, a.gen), a.wake())
}

// dropFollows forgets everything queued. The session drops its own queue on an
// interrupt — a stop followed by the session working again is not a stop — so
// the surface must not keep showing a count for turns that will never run, and
// must say that it dropped them: the person typed those words.
func (a *app) dropFollows() {
	n := len(a.follows)
	if n == 0 {
		return
	}
	a.follows = nil
	if n == 1 {
		a.note("1 queued message dropped")
	} else {
		a.note(itoa(n) + " queued messages dropped")
	}
}

// followHeight is the one row the count takes, or none.
func (a *app) followHeight() int {
	if len(a.follows) == 0 {
		return 0
	}
	return 1
}

// followRow is the count above the box: what is waiting, and for what. "after
// yield" rather than "queued" because it says the thing a person needs to
// predict — this goes when the current work stops, not now.
func (a *app) followRow(width int) string {
	if len(a.follows) == 0 {
		return ""
	}
	return a.pal.dim(fit("  after yield · "+itoa(len(a.follows)), width))
}
