package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// THE FOLLOW-UP: ctrl+enter, "and after that, do this".
//
// There are two ways to say something to a working session and they mean
// different things, so they are two keys:
//
//	enter        PARK. The message is held HERE, on the surface, between the
//	             answer and the box, and it starts a turn of its own when the
//	             answer it was typed over is finished (park.go).
//	ctrl+enter   FOLLOW-UP. The message is handed to the SESSION the moment it
//	             is typed, and it starts a turn of its own when the work is done.
//
// THIS HEADER ONCE SAID SOMETHING ELSE, AND IT WAS WRONG BY THE TIME ANYBODY
// READ IT. `enter` used to STEER: the message went straight to the session and
// reached the model at the running turn's next step boundary. park.go ended
// that — a steer typed at a turn whose last request had already gone out landed
// with nothing left to answer it, and the sentence was drawn into the middle of
// the reply besides — and nothing in this file had to change when it did, which
// is exactly why the description of the pair went on describing the old one.
// `ctrl+q` held the follow-up key until 2026-09-30, when the queue moved onto
// ctrl+enter (the standing-order mark that owned it keeps its capability
// through the sentence, standmark.go); on a terminal that cannot tell
// ctrl+enter from a plain enter the key arrives as `ctrl+j`, a newline, and
// stays one — the chord is offered only where a terminal says it can send it.
//
// So the two keys no longer differ in WHEN the message is heard. Both wait for
// the turn to end. They differ in WHO IS HOLDING IT WHILE IT WAITS, and
// everything a person can do about it follows from that: a parked message is
// still theirs — it can be edited, taken back with ↑, or sent at once by
// stopping the turn it was typed over (`ctrl+shift+enter` as one gesture,
// bargein.go) — while a follow-up is in the session's hands … though not
// without a way back: a queued row taken by a click is UNQUEUED from the
// session before it can run (session's [session.Agent.UnqueueFollowUp]) and
// comes back to the box whole — its pasted documents with it — so taking it
// back is taking it out of the queue,
// not recalling a sent thing. What has no take-back is a follow-up the turn
// already started, and a queued message is DROPPED when the turn it was queued
// behind is interrupted, because a drain never restarts a turn the person
// stopped ([app.dropFollows]).
//
// Both queues drain at the stream's close and the SESSION'S goes first (app.go's
// streamClosedMsg): a follow-up was handed over before the parked message was
// typed in the ordinary case, and a surface that let the newer sentence jump it
// would be reordering the person's own words.
//
// THE QUEUE IS DRAWN MESSAGE BY MESSAGE, and the drawing is the difference
// between waiting and sent: each row sits above the box in the queued glyph and
// dim ink, a register nothing in the transcript wears, and the rows only leave
// when their turn starts — where the message lands as an ordinary sent line. A
// count alone ("after yield · 2", the row this block replaced) made a person
// guess which of their sentences were still queued and which were being read.

// queued is one follow-up: what was typed, and the stream the turn it starts
// will speak on. The channel exists from the moment the message is queued —
// session hands it back immediately — so there is nothing to wait for later.
//
// THE DRAFT TRAVELS WITH IT so a take-back can put the box back exactly as it
// was. The pasted documents are the half that has to: queueing unfolds them
// into the words the model reads and spends the chips ([app.composed]), so
// without them a click would restore the tokens as dead text and the next send
// would carry `[paste 1 · 4 lines]` to the model.
type queued struct {
	covered func() bool
	text    string
	pastes  []pasteChip
	ch      <-chan session.Event
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
	call   *hostCall
	text   string
	pastes []pasteChip
	ch     <-chan session.Event
	err    error
}

// followUp is the queue key (input.go's `ctrl+enter` case). An empty draft does
// nothing at all: there is no message
// to queue, and a key that queued a blank one would be a key that spends a turn.
//
// THE QUEUE CARRIES WORDS ALONE, so a tray holding anything else is refused
// rather than split. [Agent.FollowUp] takes text: queueing over a picture or a
// picked harness would send the words later and leave the rest on the tray to
// ride out with whatever was typed next — one message quietly becoming two.
// Nothing is queued and the draft is untouched, the refusal the standing chord
// this key replaced made on the same ground.
func (a *app) followUp() tea.Cmd {
	line := strings.TrimSpace(a.input.String())
	if line == "" {
		return nil
	}
	if !a.trayEmptyForQueue() {
		a.note(queueWordsOnly)
		return nil
	}
	a.noticeEvent(eventQueued)
	// The model reads the paste and the queue's row keeps the tag (pastechip.go);
	// the chips are kept BESIDE the tag so a take-back can put the draft back
	// whole ([app.recallQueuedAt]).
	pastes := append([]pasteChip(nil), a.pastes...)
	spoken, line := a.composed(line)
	a.input.reset()
	a.endRecall()
	a.closeLists()
	// A queued message is a submitted one for every purpose the person has: it
	// is remembered by ↑, and the draft file it came from is done with.
	a.remember(line)
	a.dropDraft()
	a.stick = true
	a.touch()
	return a.sendFollow(spoken, line, pastes)
}

func (a *app) sendFollow(spoken, line string, pastes []pasteChip) tea.Cmd {
	return a.sendFollowFrom(a.agent, spoken, line, pastes)
}

func (a *app) sendFollowFrom(agent Agent, spoken, line string, pastes []pasteChip) tea.Cmd {
	if a.deferHosted(func() tea.Cmd { return a.sendFollowFrom(agent, spoken, line, pastes) }) {
		return nil
	}
	call := a.hostCallStarted()
	return func() tea.Msg {
		ch, err := agent.FollowUp(spoken)
		return followMsg{text: line, pastes: pastes, ch: ch, err: err, call: call}
	}
}

// queueFollow takes the session's answer.
//
// A follow-up queued while NOTHING is running starts immediately — session says
// so, and it is the right answer: there is no turn end coming to drain it. So a
// stream we are not already pumping is adopted here rather than at the next
// close, which would never arrive.
func (a *app) queueFollow(msg followMsg) (cmd tea.Cmd) {
	defer func() { cmd = tea.Batch(cmd, a.hostCallSettled(msg.call)) }()
	if msg.err != nil {
		a.note("follow-up failed: " + msg.err.Error())
		return nil
	}
	if msg.ch == nil {
		return nil
	}
	a.follows = append(a.follows, queued{text: msg.text, pastes: msg.pastes, ch: msg.ch, covered: a.hostStreamCovered(msg.ch)})
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
	if a.hostReplayLoading || a.hostReplayWaiting || a.stream != nil || len(a.follows) == 0 {
		return nil
	}
	for len(a.follows) > 0 && a.follows[0].covered != nil && a.follows[0].covered() {
		a.follows = a.follows[1:]
	}
	if len(a.follows) == 0 {
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
		// The context the turn runs in rides with it, for [app.submitting]'s reason
		// (turncontext.go): a follow-up is the person's own sentence arriving one
		// turn late, and where it goes is the same fact about it either way.
		a.entries = append(a.entries, entry{
			kind: entryUser, text: next.text, turn: a.turn, context: a.turnContext(),
			plainTags: restingDoorWords([]rune(next.text)),
		})
	}
	a.state = stateWorking
	a.lastDelta = time.Now()
	// The turn is open and the first request is out with nothing back from it.
	a.awaited = time.Now()
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

// followHeight is the block's height: one row per row a queued message wraps
// over, or none.
func (a *app) followHeight() int {
	width, _ := a.size()
	return len(a.followRows(width))
}

// queueWordsOnly is the refusal when the tray holds something the queue cannot
// carry ([app.followUp]).
const queueWordsOnly = "ctrl+enter queues words alone — take the pictures or the shape of work off first"

// trayEmptyForQueue reports whether the tray holds nothing the queue would have
// to leave behind: no picture and no picked harness.
func (a *app) trayEmptyForQueue() bool {
	return len(a.chips) == 0 && a.harnChip == ""
}

// queueFootWord is what the running foot calls the queue key: the short
// key-then-noun form every clause on that row keeps, not the queued block's
// sentence — the foot names the key, the block says what happened.
const queueFootWord = "ctrl+enter queue"

// queueFootOffered reports whether the running foot may name the queue key. It
// asks what [app.followUp] and the key's own case in [app.key] ask: the
// terminal can tell ctrl+enter from a plain enter, and there are words in the
// box to queue, and nothing on the tray the queue would refuse to carry
// ([app.trayEmptyForQueue]) — a hint for a key that would only refuse is the
// lie every hint here is written not to tell. The rest
// (a turn running, the box the conversation's own) is [app.runSendOffered],
// which the caller has already asked.
func (a *app) queueFootOffered() bool {
	return a.keysDisambiguated && strings.TrimSpace(a.input.String()) != "" && a.trayEmptyForQueue()
}

// followRows draws the queued block: the messages the SESSION is holding, each
// led by the return arrow, and nothing else.
//
// THE REGISTER IS THE POINT. A sent message is the person's accent hue with
// their own glyph, and a parked one is the same hue held between the answer and
// the box; these rows are dim and led by the return key's arrow
// (tokens.GFollowUp — the key, held with ctrl, that put them there), so a
// person scanning the foot can see at a glance which of their sentences the
// session is holding and which of them are already being worked. They leave
// the block when their turn starts — where the words land in the transcript as
// the ordinary sent line they become.
//
// THERE IS NO LINE UNDER THE BLOCK, by the owner's call (2026-09-30). It said
// `queued for after this turn · click takes one back · ctrl+enter queues the
// draft`; the arrow says the first, the hover says the second, and the running
// foot says the third, so the sentence was three things already on the frame.
func (a *app) followRows(width int) []string {
	if a.followWaiting() == 0 || width < 4 {
		return nil
	}
	out := make([]string, 0, a.followWaiting())
	// A ROW LIGHTS ONLY WHERE A PRESS WOULD TAKE IT. With no line under the
	// block to say so, the hover is the whole of the take-back's advertisement,
	// and an agent that cannot unqueue must not be shown offering to.
	takes := a.queuedTakesBack()
	for i, q := range a.follows {
		if q.woken {
			continue
		}
		// THE WHOLE MESSAGE LIGHTS, NOT THE ROW THE POINTER IS ON — park.go's
		// rule, because a sentence that wrapped over three rows with one of them
		// banded would read as three things.
		hot := takes && a.hoveringQueued(i)
		for j, line := range wrap(q.text, width-2) {
			lead := "   "
			if j == 0 {
				lead = "  " + a.pal.dim(a.icon(tokens.GFollowUp)) + " "
			}
			text := lead + a.pal.dim(line)
			if hot {
				text = a.hoverRow(text, width)
			}
			out = append(out, text)
		}
	}
	return out
}

// followMark is the pointer's answer for one row of the block: which queued
// message that row belongs to, so a click can take that one back.
func (a *app) followMark(row, width int) chromeRow {
	at := 0
	for i, q := range a.follows {
		if q.woken {
			continue
		}
		height := len(wrap(q.text, width-2))
		if height < 1 {
			height = 1
		}
		if row >= at && row < at+height {
			return chromeRow{kind: chromeQueued, index: i}
		}
		at += height
	}
	return chromeRow{}
}

// followPress is a click on the queued block: that message comes out of the
// session's queue and back into the box whole, draft and all.
//
// THE POINTER IS THE ONLY WAY TO THIS QUEUE. ↑ belongs to the parked block and
// to history and never reaches a queued row (input.go), so the click is the one
// gesture that takes an individual message back.
//
// THE CLICK IS ANSWERED AT ONCE and the words arrive with the fold
// (offloop.go): the press is claimed here — the pointer named a message — and
// what the session said about it lands on the next pass. A false from the
// agent leaves the row exactly where it was, which is the answer the row was
// promised (recallQueuedAt).
func (a *app) followPress(y int) (tea.Cmd, bool) {
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeQueued {
		return nil, false
	}
	if cmd, asked := a.recallQueuedAt(mark.index); asked {
		return cmd, true
	}
	return nil, false
}

// followUnqueuer is the agent's half of the take-back (session's
// [session.Agent.UnqueueFollowUp]). It is asserted rather than added to [Agent]
// for the reason [wakeAgent] is: a surface driven by a scripted agent must stay
// representable, and an agent that cannot unqueue is one whose queued rows this
// surface does not offer to take.
type followUnqueuer interface {
	UnqueueFollowUp(ch <-chan session.Event) bool
}

// queuedTakesBack reports whether the take-back is real here: the agent can
// hand a queued message back, and there is at least one message of the
// person's to hand back. A woken stream is not one, and a queue of nothing
// offers nothing.
func (a *app) queuedTakesBack() bool {
	if _, ok := a.agent.(followUnqueuer); !ok {
		return false
	}
	return a.followWaiting() > 0
}

// recallQueuedAt asks for ONE queued message back by its position in the
// queue, and says whether the ask went out. It is reached by a CLICK alone
// ([app.followPress]): ↑ is the parked block's key and nothing more, because
// the session's queue is what a person names with the pointer (input.go).
//
// THE SESSION IS ASKED FIRST, and it is asked OFF THE LOOP (offloop.go) — the
// queue is the session's, so the words come back into the box only when it says
// the message came out, and the removal happens in the fold, matched by the
// STREAM and not by the position: a turn that ends during the round trip moves
// the queue under the index, and an index that lied would take down a message
// the person did not name ([app.removeQueuedByStream]).
//
// A false from the agent means the turn drained the queue between the frame and
// the press; the row stays and the message runs. What comes back is the draft
// EXACTLY as it was queued: the words, and the pasted documents that were
// unfolded into them, put back on the tray so the tokens are chips again and
// the next send carries the paste rather than its tag (pastechip.go). There are
// no pictures to put back: the queue never takes a message with pictures on the
// tray ([app.followUp]). The transcript line the turn will draw is still ahead of this message, so it
// was never drawn and nothing is unwound.
func (a *app) recallQueuedAt(i int) (tea.Cmd, bool) {
	if i < 0 || i >= len(a.follows) {
		return nil, false
	}
	one := a.follows[i]
	if one.woken {
		return nil, false
	}
	unqueuer, ok := a.agent.(followUnqueuer)
	if !ok {
		return nil, false
	}
	ch, text, pastes := one.ch, one.text, append([]pasteChip(nil), one.pastes...)
	cmd := a.offLoop(func() func(here bool) tea.Cmd {
		out := unqueuer.UnqueueFollowUp(ch)
		return func(here bool) tea.Cmd {
			if !here || !out {
				// FALSE IS THE RACE, SAID HONESTLY: the turn drained the queue
				// between the frame and the press; the row stays and the
				// message runs. A fold for a window that moved on puts nothing
				// back either.
				return nil
			}
			if !a.removeQueuedByStream(ch) {
				return nil
			}
			a.input.setText(text)
			a.pastes = pastes
			a.stick = true
			a.touch()
			return a.edited()
		}
	})
	return cmd, true
}

// removeQueuedByStream takes the queued message whose stream is ch off the
// surface's queue, and reports whether it was there. It is the fold's half of
// the take-back: the session answered by the stream, so the surface answers by
// the stream too, and the message that goes is the one the person named.
func (a *app) removeQueuedByStream(ch <-chan session.Event) bool {
	for i, q := range a.follows {
		if q.ch == ch && !q.woken {
			a.follows = append(a.follows[:i], a.follows[i+1:]...)
			return true
		}
	}
	return false
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
	if leavable, ok := agent.(leavableWaker); ok {
		a.wakeLane, a.stops.wakes = leavable.WatchWakes()
	} else {
		a.wakeLane, a.stops.wakes = agent.Wakes(), nil
	}
	return waitWake(a.wakeLane, a.wakeGen)
}

// leavableWaker is the wake lane WITH A WAY OUT OF IT (session's agent.go). It
// is asserted separately from [wakeAgent] for that interface's own reason, and
// a nil stop is an agent that can only be abandoned (switcher.go's [laneStops]).
type leavableWaker interface {
	WatchWakes() (<-chan (<-chan session.Event), func())
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
