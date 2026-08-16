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
	// woken says nobody typed this one: it is a turn THE SESSION STARTED ON ITS
	// OWN (see the wake lane below). It rides the same queue because the queue
	// is about streams waiting for the one being pumped, and that is exactly
	// what it is — but it is not a message, so it is not counted above the box
	// and it writes no user line when it starts.
	woken bool
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
	// NO USER LINE FOR A TURN NOBODY ASKED FOR. A woken turn is the session
	// speaking because work landed while the room was idle, and a "›" row above
	// it would be this surface putting words in a person's mouth. Everything
	// else about the turn is identical: it is the next thing that happens in the
	// conversation, and it is drawn as one.
	if !next.woken {
		a.entries = append(a.entries, entry{kind: entryUser, text: next.text, turn: a.turn})
	}
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
	// The COUNT is of messages, and a woken stream is not one: an interrupt
	// drops it with the rest — the session it belonged to has been stopped — but
	// a surface that counted it would report a queued message the person never
	// typed.
	n := a.followWaiting()
	if len(a.follows) == 0 {
		return
	}
	a.follows = nil
	if n == 0 {
		return
	}
	if n == 1 {
		a.note("1 queued message dropped")
	} else {
		a.note(itoa(n) + " queued messages dropped")
	}
}

// followWaiting is how many QUEUED MESSAGES are waiting — the person's words,
// and not the woken streams that share the queue with them.
func (a *app) followWaiting() int {
	n := 0
	for _, q := range a.follows {
		if !q.woken {
			n++
		}
	}
	return n
}

// followHeight is the one row the count takes, or none.
func (a *app) followHeight() int {
	if a.followWaiting() == 0 {
		return 0
	}
	return 1
}

// followRow is the count above the box: what is waiting, and for what. "after
// yield" rather than "queued" because it says the thing a person needs to
// predict — this goes when the current work stops, not now.
func (a *app) followRow(width int) string {
	n := a.followWaiting()
	if n == 0 {
		return ""
	}
	return a.pal.dim(fit("  after yield · "+itoa(n), width))
}

// ── THE WAKE LANE ───────────────────────────────────────────────────────────
//
// A TURN NOBODY ASKED FOR IS STILL A TURN, AND IT IS DRAWN.
//
// Work handed to a task lands minutes later, usually into an idle room: the
// session takes the news off its steering queue and STARTS A TURN OF ITS OWN to
// say what it makes of it (session's agent.go). That turn has no caller — that
// is what makes it a wake — so its events go to the journal and, without this,
// nowhere else: the person would see the completion card this surface draws and
// never the sentence the model wrote about it.
//
// [session.Agent.Wakes] hands one channel per woken turn, before its first
// event, and the adoption is the follow-up's own ([app.startFollow]) MINUS the
// user line: same generation bump, same pump, same close. The wake NOTE itself
// is not drawn here — it is a line the model was told, and it stays where it
// is; what lands in the conversation is the reply.

// wakeAgent is the standing subscription to woken turns, asserted rather than
// added to [Agent] for the reason [taskAgent] is (task.go): a surface driven by
// a scripted agent that never wakes must stay representable.
type wakeAgent interface {
	Wakes() <-chan (<-chan session.Event)
}

// watchWakes opens the lane and starts pumping it. It runs where [app.watchTasks]
// runs and for the same reason — once at boot, again wherever the agent is
// REPLACED — because the channel belongs to the agent that handed it over.
func (a *app) watchWakes() tea.Cmd {
	agent, ok := a.agent.(wakeAgent)
	if !ok {
		return nil
	}
	a.wakeGen++
	a.wakeLane = agent.Wakes()
	return waitWake(a.wakeLane, a.wakeGen)
}

// waitWake takes one woken turn's stream off the lane and asks for the next.
func waitWake(lane <-chan (<-chan session.Event), gen int) tea.Cmd {
	return func() tea.Msg {
		ch, ok := <-lane
		if !ok {
			return wakeLaneClosedMsg{gen: gen}
		}
		return wokenMsg{gen: gen, ch: ch}
	}
}

// adoptWake takes one woken turn.
//
// IT GOES THROUGH THE FOLLOW-UP QUEUE, and that is the whole of the ordering
// rule: a wake is handed over BEFORE its first event, and a surface that is
// still pumping the tail of another stream would otherwise have two turns
// speaking into one transcript. Queued, it is adopted at the next close by the
// same [app.startFollow] that starts a queued message — and on the ordinary
// path, with nothing being pumped, that is right now.
//
// The lane is re-armed FIRST: a second landing while this one is drawn is a
// second turn, and the pump is what hears about it.
func (a *app) adoptWake(msg wokenMsg) tea.Cmd {
	if msg.gen != a.wakeGen {
		return nil
	}
	next := waitWake(a.wakeLane, a.wakeGen)
	if msg.ch == nil {
		return next
	}
	a.follows = append(a.follows, queued{ch: msg.ch, woken: true})
	a.touch()
	if a.stream != nil {
		return next
	}
	return tea.Batch(next, a.startFollow())
}
