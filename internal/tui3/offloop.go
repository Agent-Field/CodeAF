package tui3

import (
	tea "charm.land/bubbletea/v2"

	"sync"
)

// ── EVERY DOOR IS ASKED OFF THE LOOP ────────────────────────────────────────
//
// THE LAW. A door on the agent — answering a question, writing a rule, taking up
// a conversation's record — is a CALL TO ANOTHER PROCESS. Bare `codeaf` runs its
// engine in a session host and talks to it over a socket, so this is true of
// every window and not only of `--host` (cmd/codeaf's chatv3_local.go). A call
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
// `ask` runs on the door line's goroutine and may take as long as the engine
// takes; it hands back the fold, which runs on the loop and may touch the
// surface. Nothing in `ask` may touch the surface, and nothing in the fold may
// call a door — the two halves are exactly that split.
//
// THE ASK IS PUT IN THE LINE HERE, ON THE LOOP, AND THAT IS WHAT ORDERS IT.
// Commands are started on goroutines of their own in whatever order the runtime
// feels like, so a door asked from a command was a door that could overtake the
// one asked a keystroke earlier: `D` batches the rule and the answer, and the
// answer resuming the turn could re-ask the question before the rule it was
// supposed to be written under existed. Queuing on the loop means the wire sees
// what a person did in the order they did it ([doorLine]).
func (a *app) offLoop(ask func() func(here bool) tea.Cmd) tea.Cmd {
	if ask == nil {
		return nil
	}
	front := a.frontGen
	said := a.doorLine.add(ask)
	return func() tea.Msg {
		return doorMsg{front: front, fold: <-said}
	}
}

// ── THE DOOR LINE ───────────────────────────────────────────────────────────
//
// ONE QUEUE, ONE GOROUTINE, IN THE ORDER THE KEYS WERE PRESSED. Every door this
// surface asks is a call to another process, and two of them in flight at once
// arrive in whichever order two goroutines happen to be scheduled. That is not
// a race about speed — it is a race about MEANING: the rule a person wrote with
// `D` and the answer they gave in the same keystroke are one gesture, and the
// engine reading them backwards re-asks a question the rule had just settled.
//
// So the asks go into a line as they are made — which is on the update loop,
// under the keystroke that made them — and one goroutine walks it. A door is
// still never called from the loop: what the loop does is put a job on a slice
// and ring a one-slot channel, neither of which can block on the engine.
type doorLine struct {
	mu    sync.Mutex
	jobs  []doorJob
	wake  chan struct{}
	stop  chan struct{}
	begun sync.Once
	ended sync.Once
}

// doorJob is one ask waiting its turn, and where to put what it said.
type doorJob struct {
	ask  func() func(here bool) tea.Cmd
	said chan func(here bool) tea.Cmd
}

func newDoorLine() *doorLine {
	return &doorLine{wake: make(chan struct{}, 1), stop: make(chan struct{})}
}

// add puts one ask at the back of the line and hands back where its answer will
// appear. It is called ON THE LOOP and never blocks there.
func (l *doorLine) add(ask func() func(here bool) tea.Cmd) chan func(here bool) tea.Cmd {
	said := make(chan func(here bool) tea.Cmd, 1)
	if l == nil {
		// A surface with no line — a fixture that built an app by hand — asks
		// the door on the command's own goroutine, which is what this did
		// before the line existed. It loses the ORDER and nothing else.
		go func() { said <- ask() }()
		return said
	}
	l.begun.Do(func() { go l.walk() })
	l.mu.Lock()
	l.jobs = append(l.jobs, doorJob{ask: ask, said: said})
	l.mu.Unlock()
	select {
	case l.wake <- struct{}{}:
	default:
	}
	return said
}

// walk asks the line's doors, one at a time, in order.
func (l *doorLine) walk() {
	for {
		select {
		case <-l.stop:
			// EVERYTHING ALREADY IN THE LINE IS STILL ASKED. A person's last
			// keystroke before a window closes is an answer somebody gave, and
			// dropping it would lose a decision on the way out.
			l.drain()
			return
		case <-l.wake:
			l.drain()
		}
	}
}

func (l *doorLine) drain() {
	for {
		l.mu.Lock()
		if len(l.jobs) == 0 {
			l.mu.Unlock()
			return
		}
		job := l.jobs[0]
		l.jobs = l.jobs[1:]
		l.mu.Unlock()
		job.said <- job.ask()
	}
}

// close ends the line after what is in it has been asked.
func (l *doorLine) close() {
	if l == nil {
		return
	}
	l.ended.Do(func() { close(l.stop) })
}

// doorSaid folds one door's answer in. See [doorMsg] for why the fold is told
// whether the conversation it belongs to is still the one in front.
func (a *app) doorSaid(msg doorMsg) tea.Cmd {
	if msg.fold == nil {
		return nil
	}
	return msg.fold(msg.front == a.frontGen)
}
