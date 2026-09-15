package session

import (
	"context"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// A REQUEST MADE ON A NODE'S BEHALF LEAVES THE TRAIL ITS OWN WORKER'S REQUESTS
// LEAVE.
//
// ── THE MEASURED FAILURE ──
//
// Task 5 of conversation de9eabcb10cc1e45, 2026-09-11, a carry-on whose brief
// was already shaped. The reading that sizes the work went to a model that
// wrote its first token at 11.5 seconds and then thought for another 207 —
// 4,465 tokens of reasoning before a 1,187-token answer — and for all 219
// seconds of it the person saw `sizing the work` on the row, the model's id
// under it, and `nothing on this page yet` in the room. The pulse file said
// `"requests": 0`. Nothing on the screen moved, which is indistinguishable from
// a program that has stopped.
//
// A worker's request is never like that. loop.go takes the node's pulse either
// side of it, the stream draws the thinking and the count as they arrive, and
// the journal gets its line. An errand — the reading, and every other side call
// a node makes before or beside its worker — went through [Agent.callRole]
// without its stream (it is nobody's answer, so it must not type itself into the
// room) and so without any of that: one bill line at the end, and silence until
// then.
//
// ── WHAT THIS IS ──
//
// ONE WATCHER ON EVERY REQUEST MADE UNDER A CONTEXT, bound to the node the
// requests are for. internal/provider already counts each request's life at the
// one place its stream is read, and hands it to whoever asked through
// [provider.WithCallProgress]; this is the node's side of that seam, and it
// turns each moment into the three things a worker's request already leaves:
//
//   - THE PULSE. A request going out and coming back are the two edges
//     [taskBeat] is written at, so an outside reader sees a node inside a
//     three-minute request rather than a node that has made none.
//   - THE JOURNAL. Both ends of every request, on the node's own file
//     ([journalFlight]) — which model, which machine, when the first token came,
//     how much thought and how much answer, and how it ended.
//   - THE PHASE. The live request rides the phase notice the node is already
//     sending ([TaskPhaseNotice.Call]), beside the ladder's own sentence, so the
//     rail, the room and a hosted window all draw it from the one event they
//     already fold.
//
// ── WHO OWNS WHAT, AND WHERE IT JOINS ──
//
// The provider calls [callTrail.heard] SYNCHRONOUSLY FROM THE READ LOOP, between
// two deltas of somebody's answer, and it may do no work there: no file, no lock
// anybody else holds, nothing that can wait (internal/provider's callprogress.go
// states the law). So heard puts the moment on this trail's own queue — under a
// mutex nothing else in the process takes — and nudges ONE goroutine that the
// trail owns, which does the writing. The ladder's sentence goes on the same
// queue ([callTrail.say]) rather than being sent from the errand's goroutine,
// because the two are one row on the screen and a notice sent from each side
// could arrive in the wrong order and draw a request under a sentence about the
// rung after it.
//
// THE ONE WHO BUILDS IT ENDS IT. [callTrail.end] drains what is queued, closes
// the record of any request still out — a rescue that lost its race and has not
// said so yet, say — and joins the goroutine before it returns. A caller ends
// its trail before it moves the node out of the phase, so no notice about the
// phase can arrive after the one that leaves it, and nothing this trail started
// outlives the reading that started it. A moment the provider reports after the
// end is dropped at the door.
type callTrail struct {
	agent *Agent
	node  *TaskNode
	// base is the phase notice every announcement is built from — the node, the
	// life it is in and, where that life has them, its rounds — so a trail over a
	// repair round would keep the round on every notice it sends rather than
	// wiping it off the row.
	base   TaskPhaseNotice
	role   roles.Role
	teller *Agent

	mu    sync.Mutex
	queue []trailNews
	over  bool
	wake  chan struct{}
	done  chan struct{}

	// text and calls are the goroutine's own and nobody else reads them: the
	// sentence under the phase word, and every request of this errand that has
	// said anything and not yet ended, by which concurrent arm of the question it
	// is ([provider.CallProgress.Attempt]).
	text  string
	calls map[int]*trailCall
}

// trailNews is one thing to say: a moment of a request, or the sentence.
type trailNews struct {
	progress provider.CallProgress
	text     string
	said     bool
}

// trailCall is one request as the trail last heard it, and what the record owes
// about it: whether its going out has been written down — a request parked on a
// provider's pacing before it ever reached the wire has said something and has
// not yet begun — when the FIRST of its attempts went out, and how many times it
// has moved to another machine since.
//
// THE FIRST MOMENT IS KEPT BECAUSE THE LAST ONE MOVES. A request that is refused
// and walks reports a later [provider.CallProgress.Started], which is right for
// the row — the wait a person is sitting through began again — and wrong for the
// record, where the question's own length is what an autopsy asks for.
type trailCall struct {
	last  provider.CallProgress
	first time.Time
	hops  int
	begun bool
}

// trailCalls builds the trail for requests made on node's behalf while it is in
// the life base names, run under role. The agent is the one standing in for the
// node — its journal is the node's journal — and the notices go out the way
// every phase move does ([TaskNode.phaseTeller]).
func (a *Agent) trailCalls(node *TaskNode, base TaskPhaseNotice, role roles.Role) *callTrail {
	if node == nil {
		return nil
	}
	base.ID = node.id
	t := &callTrail{
		agent:  a,
		node:   node,
		base:   base,
		role:   role,
		teller: node.phaseTeller(a),
		wake:   make(chan struct{}, 1),
		done:   make(chan struct{}),
		calls:  map[int]*trailCall{},
	}
	go t.run()
	return t
}

// watching is ctx with this trail listening to every request made under it.
func (t *callTrail) watching(ctx context.Context) context.Context {
	if t == nil {
		return ctx
	}
	return provider.WithCallProgress(ctx, t.heard)
}

// heard is the provider's watcher. It queues and it nudges, and that is all it
// is allowed to do (see the type's own doc).
//
// A CLIMBING COUNT REPLACES THE ONE BEFORE IT, AND NOTHING ELSE DOES. The
// provider already speaks at most ten times a second, and if the goroutine is
// busy writing a line the counts that pile up behind it are worth only their
// newest value; but a request going out, a first token, a phase turning over
// and an ending each happen once and each is a line on the record, so none of
// them is ever folded into the next.
func (t *callTrail) heard(progress provider.CallProgress) {
	t.mu.Lock()
	if !t.over {
		if last := len(t.queue) - 1; last >= 0 && t.queue[last].countsOnly(progress) {
			t.queue[last].progress = progress
		} else {
			t.queue = append(t.queue, trailNews{progress: progress})
		}
	}
	t.mu.Unlock()
	t.nudge()
}

// countsOnly reports whether next differs from this news in its counts alone.
func (n trailNews) countsOnly(next provider.CallProgress) bool {
	was := n.progress
	return !n.said && was.Attempt == next.Attempt && was.Phase == next.Phase &&
		next.Phase != provider.CallEnded && was.Served == next.Served &&
		was.Started.Equal(next.Started) && was.FirstToken.Equal(next.FirstToken)
}

// say puts the sentence under the phase word, in its place among the moments of
// the requests it is about.
func (t *callTrail) say(text string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	if !t.over {
		t.queue = append(t.queue, trailNews{text: text, said: true})
	}
	t.mu.Unlock()
	t.nudge()
}

// end is the errand over: everything queued is said, every request still out is
// written down as having been left, and the goroutine is gone when it returns.
// It is safe to call twice and on a nil trail.
func (t *callTrail) end() {
	if t == nil {
		return
	}
	t.mu.Lock()
	already := t.over
	t.over = true
	t.mu.Unlock()
	if !already {
		t.nudge()
	}
	<-t.done
}

// nudge wakes the goroutine without ever waiting for it: a wake already pending
// is the same wake.
func (t *callTrail) nudge() {
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

// run is the trail's one goroutine: it takes what is queued, says it in order,
// and leaves once the errand is over and the queue is empty.
func (t *callTrail) run() {
	defer close(t.done)
	for range t.wake {
		t.mu.Lock()
		batch, over := t.queue, t.over
		t.queue = nil
		t.mu.Unlock()
		for _, news := range batch {
			if news.said {
				t.text = news.text
				t.announce()
				continue
			}
			t.hear(news.progress)
		}
		if over {
			t.left()
			return
		}
	}
}

// hear folds one moment of one request in, writes down its two ends, and puts
// what is now true on the phase.
//
// A QUESTION IS MORE THAN ONE REQUEST, AND ONLY THE QUESTION ENDS. The seam says
// [provider.CallEnded] ONCE, from the door the question returns through, naming
// the request that ended it (internal/provider's callprogress.go). Every other
// request the question had out — a rescue racing beside it, an arm the winner
// cut — is over at that same moment, and nothing else will ever say so about
// them: so the ending closes all of them, the named one as it really ended and
// the rest as left.
//
// AND A REQUEST THAT WALKS TO ANOTHER MACHINE IS STILL ONE REQUEST TO THE
// PERSON. A refusal that hops reports [provider.CallStarted] again under the
// same attempt, with a later moment and no ending in between; the row's clock
// restarts with it, which is the wait beginning again. The pulse therefore takes
// no second edge and the journal opens no second line — the worker's own pulse
// is one pair per question too (loop.go takes it either side of the retry
// ladder, not inside it) — and what the record keeps instead is how many
// machines the request took ([journalFlight.Hops]), which is what an autopsy of
// a long wait asks.
func (t *callTrail) hear(progress provider.CallProgress) {
	call := t.calls[progress.Attempt]
	if progress.Phase == provider.CallEnded {
		// THE ENDING'S OWN COUNTS ARE THE RECORD'S. They are the row as the seam
		// last held it and never less than the last report a reader was given:
		// the counts between two frames are held back for the beat, and the
		// ending is not (internal/provider's callprogress.go).
		if call != nil {
			call.last = progress
		}
		t.closeCalls(progress.Attempt, progress.End)
		t.announce()
		return
	}
	if call == nil {
		call = &trailCall{}
		t.calls[progress.Attempt] = call
	}
	if call.begun && progress.Started.After(call.last.Started) {
		call.hops++
	}
	call.last = progress
	if !call.begun && !progress.Started.IsZero() {
		call.begun, call.first = true, progress.Started
		t.node.beatWriter().began()
		t.agent.file.appendFlight(journalFlight{
			Role:    string(t.role),
			Model:   progress.Model,
			Attempt: progress.Attempt,
			Phase:   string(provider.CallStarted),
		})
	}
	t.announce()
}

// closeCalls writes the end of every request this trail still has open. ending
// names the one the seam reported an ending for and end is how it ended; every
// other open request is written down as left, and an ending that names none of
// them (-1, the errand being over) leaves them all that way.
func (t *callTrail) closeCalls(ending int, end provider.CallEnd) {
	for attempt, call := range t.calls {
		if call.begun {
			became := provider.CallEndCancelled
			if attempt == ending {
				became = end
			}
			t.landed(call, became)
		}
		delete(t.calls, attempt)
	}
}

// landed writes down the other end of one request.
//
// TWO CLOCKS, AND EACH IS MEASURED FROM THE MOMENT IT IS ABOUT. The length is
// the QUESTION'S, from the first attempt that went out — a request that walked
// to three machines waited for all three — and the first token is the LAST
// attempt's, because the counts reset when a request moves and a first token
// measured from a start that was abandoned is a figure about nothing.
//
// The end is read off the agent's own clock ([Agent.now]), which is [time.Now]
// in the product and the one clock a test can move: the seam reports the moments
// it knows and an ending is not one of them, so this is the only reading of the
// world this file makes and it goes through the one door.
func (t *callTrail) landed(call *trailCall, end provider.CallEnd) {
	p := call.last
	line := journalFlight{
		Role:       string(t.role),
		Model:      p.Model,
		Endpoint:   p.Served,
		Attempt:    p.Attempt,
		Phase:      string(provider.CallEnded),
		End:        string(end),
		Hops:       call.hops,
		DurationMS: t.agent.now().Sub(call.first).Milliseconds(),
		Output:     p.Tokens,
		Reasoning:  p.Reasoning,
	}
	if !p.FirstToken.IsZero() {
		line.FirstTokenMS = p.FirstToken.Sub(p.Started).Milliseconds()
	}
	t.agent.file.appendFlight(line)
	t.node.beatWriter().ended()
}

// left is the errand ending with requests still out. Each is written down as
// left rather than as failed — the caller walked away from it, which is
// [provider.CallEndCancelled]'s whole meaning — so no request this trail
// started stands open on the record, and the row stops drawing them.
func (t *callTrail) left() {
	if len(t.calls) == 0 {
		return
	}
	t.closeCalls(-1, provider.CallEndCancelled)
	t.announce()
}

// announce puts what is true right now on the phase, whole.
func (t *callTrail) announce() {
	notice := t.base
	notice.Text = t.text
	notice.Call = t.leading()
	t.teller.emitTaskPhase(notice)
}

// leading is the request a person is waiting on: of every arm of this question
// still out, the one nearest an answer — the one that has had the most back —
// and the caller's own where two have had the same.
//
// A RESCUE RACING BESIDE A SLOW REQUEST IS NOT A SECOND ROW. It is the same
// question asked twice, and the person is waiting on whichever answers; the arm
// that has written the most is the best reading there is of which that will be.
func (t *callTrail) leading() *TaskCall {
	var best provider.CallProgress
	bestAttempt, found := 0, false
	for attempt, call := range t.calls {
		p := call.last
		had, has := best.Tokens+best.Reasoning, p.Tokens+p.Reasoning
		if !found || has > had || (has == had && attempt < bestAttempt) {
			best, bestAttempt, found = p, attempt, true
		}
	}
	if !found {
		return nil
	}
	return &TaskCall{
		Model:      best.Model,
		Served:     best.Served,
		Started:    best.Started,
		FirstToken: best.FirstToken,
		Tokens:     best.Tokens,
		Reasoning:  best.Reasoning,
		Phase:      best.Phase,
	}
}
