package provider

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

// CallProgress is ONE OUTBOUND CALL WATCHED WHILE IT IS STILL RUNNING, and it
// is the whole of what this package will say about a call before the call is
// over.
//
// ── WHY IT EXISTS ───────────────────────────────────────────────────────────
//
// A task room can draw a worker's call — the model thinking, the token count
// climbing, the seconds since it went out — because a worker's turn streams
// through the session's own observer. Nothing else does. A division, a sizing
// pass, a mark being read are each ONE call through internal/session's
// callRole, and every one of them draws nothing at all while it runs: measured
// on 2026-09-10 those calls ran between 12 and 219 seconds with a blank line
// over them, which is indistinguishable from a process that has stopped.
//
// The events were never missing. Every streamed call in this process already
// reports each moment of its stream to a hazard controller ([streamWatch.note]
// in armwatch.go), which is where the tokens, the first token, the heartbeats
// and the silences are counted for the waiting policy. This seam forwards what
// that one place already knows, so there is no second decoder and no second
// count of anything.
//
// ── WHY IT RIDES THE CONTEXT ────────────────────────────────────────────────
//
// The same reason [WithPatientRateLimits] does, said in patience.go and true
// here: the adapter is SHARED. A task node's agent talks through the very same
// *Client the person's conversation does, so a field on the client would have
// a division's progress drawn over a conversation's. The call is the only thing
// that knows whose call it is, and the context is what the call already
// carries — which is also what lets a caller attach this without any file it
// does not own already being changed.
//
// ── IT IS CALLED FROM THE READ LOOP, SO IT MUST DO NO WORK ──────────────────
//
// The one rule this seam has, and it is the same rule [WithPacingNotice] has:
// the function is called SYNCHRONOUSLY from the goroutine reading the stream,
// between two deltas of the person's answer. It may set a field and announce.
// It may not take a lock somebody else holds, write a file, or call back into
// this package. Anything it does is time the answer is not being read in.
type CallProgress struct {
	// Model is what was asked, and Served the machine that is answering it —
	// empty until the stream names one, which on a cold path is never.
	Model  string
	Served string
	// Attempt names WHICH CONCURRENT REQUEST of this question this is: 0 for the
	// one the caller made, 1 and up for a rescue racing beside it (hedge.go). A
	// reader drawing one line per call keys on it, so that the rescue does not
	// overwrite the request it was sent to save — and so that the loser's
	// [CallEndCancelled] does not read as the question having been abandoned.
	Attempt int
	// Started is when this request really went out, and FirstToken when the
	// first delta of anything — answer or thought — came back. FirstToken is
	// ZERO UNTIL IT DOES, which is the state a surface most needs to draw: the
	// gap between the two is the whole of what a person is waiting through.
	Started    time.Time
	FirstToken time.Time
	// Tokens is progress a person could read and Reasoning is the run of thought
	// underneath it, counted apart for the reason [control.Reading] counts them
	// apart: hidden work keeps a stream alive and shows nothing. Both are the
	// stream's own running estimate and neither is the bill — the provider's
	// usage receipt is what money is counted from, always (calllog.go).
	Tokens    int
	Reasoning int
	// Phase is where the call is now, and End is filled in only on the last one.
	Phase CallPhase
	End   CallEnd
	// Err is what ended it, on the endings that carry a reason.
	Err error
}

// CallPhase is where one call is, in the few words a surface can draw.
//
// THE PACING PARK IS A PHASE AND NOT A SECOND CHANNEL. A call that is waiting
// out a provider's "not yet" is in a state exactly as a call that is thinking
// is, and it is the state a person waits longest in; [WithPacingNotice] says
// the same thing as a bare bool, from the same one site in dispatch.go, and is
// the older spelling of this phase rather than a second account of it.
type CallPhase string

const (
	// CallStarted is the request on the wire with nothing back yet.
	CallStarted CallPhase = "started"
	// CallPaced is the request parked before the wire because every machine it
	// may go to is being held (patience.go).
	CallPaced CallPhase = "paced"
	// CallThinking is the endpoint writing where nobody can read.
	CallThinking CallPhase = "thinking"
	// CallWriting is the answer arriving.
	CallWriting CallPhase = "writing"
	// CallEnded is the last report this call makes, and the only one carrying an
	// [CallEnd].
	CallEnded CallPhase = "ended"
)

// CallEnd is HOW a call ended, in the four outcomes that are different things
// to draw. They are deliberately not the taxonomy's causes: a surface drawing a
// line needs to know whether to leave the answer up, replace it, or say nothing
// at all, and four words is the whole of that question.
type CallEnd string

const (
	// CallEndAnswered is the model having finished.
	CallEndAnswered CallEnd = "answered"
	// CallEndCut is the request ending before its answer did — a bound of ours,
	// a torn connection, or this call moving on to another machine.
	CallEndCut CallEnd = "cut"
	// CallEndRefused is the machine saying no.
	CallEndRefused CallEnd = "refused"
	// CallEndCancelled is nobody's fault: the caller left, or this arm lost a
	// race another arm had already won. IT IS NOT A FAILURE and a surface that
	// drew it as one would be drawing this build's own hedging policy as
	// provider weather (armwatch.go's [streamWatch.lost] says what that cost).
	CallEndCancelled CallEnd = "cancelled"
)

// callProgressBeat is the fastest this seam will speak, and IT IS NOT A NUMBER
// THIS FILE CHOSE. It is the rule this build already states, one layer up, in
// the same words: internal/session's `formingInterval` (toolhint.go) holds a
// forming tool call to ten reports a second because "ten frames a second is
// already faster than a person reads a growing byte count, and the two things
// that are NOT time-based bypass it entirely, because those are the moments the
// row actually changes what it says".
//
// The quantity both approximate is the same one, and it is a fact about the
// READER rather than about the stream: a count climbing faster than a surface
// paints is drawn identically whether it was reported once or sixty times, so
// every report past the paint is a struct copy and a redraw nobody sees. The
// moments that are NEWS — the first token, the phase turning over, the machine
// naming itself, the ending — are exempt for the same reason they are there:
// each happens once, and each changes what the row says.
//
// SO THE NUMBER BELONGS DOWN HERE AND THE COPY UP THERE IS THE ONE TO DELETE.
// This package is beneath internal/session, so the throttle a forming call
// applies to its own fragments can read this; the reverse cannot. That fold is
// a one-line change in a file this wave does not own, and it is written down in
// the lane report rather than reached for here.
const callProgressBeat = 100 * time.Millisecond

// CallWatcher receives one call's progress synchronously and in order. It is a
// function and not a one-method interface for the reason [StreamObserver] is:
// every watcher in this build is a closure over a surface's own row, and an
// interface would be a named type each of them had to declare to say the same
// thing.
type CallWatcher func(CallProgress)

type callProgressKey struct{}

// WithCallProgress attaches the one callback this package makes about a call
// while the call is still running. Every call made under ctx reports to it —
// going out, parked on a provider's pacing, thinking, writing, and how it
// ended.
//
// IT IS CALLED SYNCHRONOUSLY FROM THE READ LOOP AND MUST DO NO WORK. See the
// type's own doc; the same law [WithPacingNotice] states in patience.go.
//
// A nil watcher is nobody listening and the context comes back unchanged, so a
// caller with a conditional surface may pass what it has.
func WithCallProgress(ctx context.Context, watcher CallWatcher) context.Context {
	if watcher == nil {
		return ctx
	}
	return context.WithValue(ctx, callProgressKey{}, watcher)
}

// callProgressFrom is the attached watcher, nil when nobody is listening.
func callProgressFrom(ctx context.Context) CallWatcher {
	if ctx == nil {
		return nil
	}
	watcher, _ := ctx.Value(callProgressKey{}).(CallWatcher)
	return watcher
}

// endingWords is the log's own closing vocabulary (calllog.go) read into this
// seam's, and it is a TABLE rather than a chain of cases because that is what it
// is: four words the log already spells, each with one answer to "what does a
// surface do about this". A reader adding a fifth closing word adds a row here
// and nothing else.
//
// A hop and an abandonment are both CUT because they are the same thing to
// draw: this request is over and its answer is not coming, while the question it
// belonged to is still alive and will report again under a new [CallProgress.Started].
var endingWords = map[string]CallEnd{
	endedCancelled: CallEndCancelled,
	endedDeadline:  CallEndCancelled,
	endedHopped:    CallEndCut,
	endedAbandoned: CallEndCut,
}

// callEndOf reads the ending off the row the model-call log is about to write,
// because that row is where every path in this package has already said what
// happened (calllog.go).
//
// THE WORD THE LOG CLOSED THE ROW WITH OUTRANKS THE ERROR, on the rows that
// carry one. A cancelled attempt and a hop both come back as `context
// canceled`, and only the closing word tells them apart — which is the whole
// reason [calllog.Record.Ended] exists.
func callEndOf(facts recordFacts) CallEnd {
	if end, closed := endingWords[facts.ended]; closed {
		return end
	}
	if facts.err == nil {
		return CallEndAnswered
	}
	if errors.Is(facts.err, context.Canceled) || errors.Is(facts.err, context.DeadlineExceeded) {
		return CallEndCancelled
	}
	// A BOUND OF OURS AND A TORN CONNECTION ARE THE SAME THING TO DRAW. Both are
	// an answer that had started and stopped; what a surface does about either is
	// to leave what arrived up and say the call did not finish. Why it stopped is
	// the taxonomy's question and is on the row already.
	var cut *StreamCut
	if errors.As(facts.err, &cut) || errors.Is(facts.err, io.ErrUnexpectedEOF) {
		return CallEndCut
	}
	return CallEndRefused
}

// callProgress is one request's report: the identity that never changes, the
// counts as they stand, and the coalescing that keeps a sixty-hertz stream from
// becoming sixty redraws a second.
//
// IT IS PER REQUEST AND NOT PER CALLER. A race's arms each build their own from
// the same watcher, which is what makes [CallProgress.Attempt] mean anything:
// two arms writing into one of these would interleave two token counts and the
// reader would draw their sum.
//
// Every method is nil-safe, so an unwatched call pays one nil check at each of
// the four seams and nothing else.
type callProgress struct {
	watcher CallWatcher
	model   string
	attempt int

	mu sync.Mutex
	// state is the report as it stands, carried between events so that each one
	// is whole: a surface never has to remember what the last one said.
	state CallProgress
	// spoke is when this request last reported, and open whether it has started
	// and not yet ended. A retry inside the dispatcher opens a second time
	// (dispatch.go), and that is honest — the bytes really did go out again.
	spoke time.Time
	open  bool
}

// newCallProgress builds the reporter for one request, or nil when nobody is
// watching — which is every call in an ordinary run.
func newCallProgress(watcher CallWatcher, model string, attempt int) *callProgress {
	if watcher == nil {
		return nil
	}
	return &callProgress{watcher: watcher, model: model, attempt: attempt}
}

// opened is the request going out. It is taken from the row the model-call log
// writes at that moment (calllog.go) rather than from a clock read of its own,
// because the two facts are the same fact and a second reading of the world's
// clock could only disagree with the first.
//
// THE COUNTS ARE NOT RESET. A retry inside the dispatcher re-opens this
// reporter, and everything the stream had delivered by then it really had
// delivered; a count that went backwards would be a surface told the answer was
// being unwritten.
func (p *callProgress) opened(at time.Time) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.state.Model = p.model
	p.state.Attempt = p.attempt
	p.state.Started = at
	p.state.Phase = CallStarted
	p.state.End = ""
	p.state.Err = nil
	p.open = true
	p.say(at)
	p.mu.Unlock()
}

// serving is the machine the stream named, reported at once rather than held
// for the next delta: it happens once per request, and on a lane that then
// thinks for a minute it is the only thing there is to say.
func (p *callProgress) serving(lane string) {
	if p == nil || lane == "" {
		return
	}
	p.mu.Lock()
	if p.state.Served != lane {
		p.state.Served = lane
		p.say(p.state.Started)
	}
	p.mu.Unlock()
}

// paced is the call parking on a provider's "not yet", and leaving that park.
//
// LEAVING IT RESTORES THE PHASE THE CALL WAS IN, which is [CallStarted] on
// every real park: the request never reached a machine, so nothing had been
// thought or written and the call is once again one that has gone out and is
// waiting. Inventing a phase here — or leaving the park's own word standing —
// would be a surface told a call was still queued while its answer arrived.
func (p *callProgress) paced(parked bool, at time.Time) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	phase := CallStarted
	if parked {
		phase = CallPaced
	}
	if p.state.Phase == phase {
		return
	}
	p.state.Phase = phase
	p.say(at)
}

// note is one moment of the stream, already folded by the watch that owns the
// counts — visible is progress a person could read and hidden is the run of
// thought, exactly as [control.Reading] separates them.
//
// The moment comes from the reading rather than from a clock here, so a
// scenario written in seconds is judged in the seconds it wrote.
func (p *callProgress) note(at time.Time, visible, hidden int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	first := p.state.FirstToken.IsZero()
	if first {
		p.state.FirstToken = at
	}
	p.state.Tokens = visible
	p.state.Reasoning = hidden
	phase := CallThinking
	if visible > 0 {
		phase = CallWriting
	}
	turned := p.state.Phase != phase
	p.state.Phase = phase
	// NEWS IS NEVER HELD, AND EVERYTHING ELSE IS. The first token and the phase
	// turning over each happen once and are the two things a person is watching
	// for; a climbing count between them is worth ten reports a second and no
	// more (callProgressBeat).
	if first || turned || at.Sub(p.spoke) >= callProgressBeat {
		p.say(at)
	}
}

// closed is the end, and it is said ONCE for each time the request was opened.
// A call that reached several endings — the guard cutting a stream the race had
// already abandoned, say — is one request that ended once, and the first
// ending is the one that ended it.
func (p *callProgress) closed(end CallEnd, err error) {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.open {
		p.open = false
		p.state.Phase = CallEnded
		p.state.End = end
		p.state.Err = err
		p.say(p.state.Started)
	}
	p.mu.Unlock()
}

// say hands the report over. It runs with the lock held, which is what keeps
// two goroutines' events in the order they happened — the read loop's deltas
// and the race's cancel are genuinely concurrent, and a reader shown the ending
// before the last token would draw a call that finished before it wrote.
//
// It is also why the watcher may not do work: this lock is on the read loop's
// own path. See [WithCallProgress].
func (p *callProgress) say(at time.Time) {
	// The beat is only ever moved FORWARD. Two of the four seams have no moment
	// of their own and hand over the one they know — the request going out — and
	// a clock that walked backwards on them would spend the next delta's
	// coalescing budget on nothing.
	if at.After(p.spoke) {
		p.spoke = at
	}
	p.watcher(p.state)
}
