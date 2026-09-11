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
	//
	// IT IS NOT A COUNT OF TRIES. A call that walks to a second machine keeps its
	// number and says so with a fresh Started; what changes is where it is, never
	// how many goes it has had.
	Attempt int
	// Started is when THIS ATTEMPT really went out, and it MOVES: a call that is
	// refused and walks to another machine reports [CallStarted] again with a
	// later Started, because the wait a person is sitting through began again.
	// FirstToken is when the first delta of anything — answer or thought — came
	// back, and is ZERO UNTIL IT DOES, which is the state a surface most needs to
	// draw: the gap between the two is the whole of what a person is waiting
	// through.
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
	// [CallEnd]. It is said ONCE, when the request has really come back — never
	// for an attempt that is about to be made again on another machine, which is
	// what a person sees as one call still running (see
	// [callProgress.landed] for why an attempt's ending is latched and not
	// spoken).
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

// callProgressBeat is the fastest this seam will speak, and IT IS THE FRAME THE
// SURFACE DRAWS ON, not a number chosen here.
//
// ── THE DERIVATION ──────────────────────────────────────────────────────────
//
// A report that arrives between two paints is drawn identically to one that
// never arrived: the reader holds the newest [CallProgress] and paints it, so
// two reports inside one frame differ only in which of them is thrown away. The
// quantity to match is therefore the surface's own frame, and this build has
// exactly one — `internal/tui3`'s `frameInterval`, 33 ms, the LOCAL cadence
// every animation on the chat surface is counted in (app.go). A surface read
// over a connection paints on `remoteFrameInterval`, three of those, and steps
// the same distance through each frame (link.go's `app.frameEvery` and
// `app.frameStride`) — so the link's stride is COARSER and holding a local
// surface to it would stutter a token count on one frame in three.
//
// So the beat is the local frame and not the link's, and 33 ms is the figure it
// carries. This package cannot import `internal/tui3` — a surface may depend on
// a transport and never the other way — so the two constants are named here
// instead, and a change to `frameInterval` is a change to this (PERF.md carries
// the pair).
//
// THE MOMENTS THAT ARE NEWS ARE EXEMPT FROM IT — the first token, the phase turning
// over, the machine naming itself, the ending — for the same reason they are
// there at all: each happens once, and each changes what the row says rather
// than what a number in it reads.
const callProgressBeat = 33 * time.Millisecond

// CallWatcher receives one call's progress synchronously and in order. It is a
// function and not a one-method interface for the reason [StreamObserver] is:
// every watcher in this build is a closure over a surface's own row, and an
// interface would be a named type each of them had to declare to say the same
// thing.
type CallWatcher func(CallProgress)

type callProgressKey struct{}

// WithCallProgress attaches the one callback this package makes about a call
// while the call is still running: going out, parked on a provider's pacing,
// thinking, writing, and how it ended.
//
// WHAT REPORTS IS EVERY STREAMED CALL THAT RUNS UNDER A WAITING CONTROLLER, and
// that is the honest bound rather than "every call". The report is opened at
// [Client.completeWithMessagesStreaming], so anything that never reaches that
// door says nothing; and what fills it comes through a [streamWatch], which is
// installed from one site — hedge.go's startArm, under [Client.raceFor]'s gate —
// so a build with no controller installed (`internal/lane`'s seam, which a
// shipped binary always fills) reports nothing either. A watcher attached to a
// call that cannot report is SILENT rather than wrong; if that empty state ever
// becomes a real door rather than a test's, the fix is to give the bare stream
// loop a watch, not to feed this from somewhere else.
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

type callProgressReporterKey struct{}

// beginCallProgress opens the reporter for ONE QUESTION and puts it where every
// request made for that question will find it.
//
// IT ANSWERS A REPORTER ONLY TO THE CALL THAT OPENED IT, and that is what makes
// the ending single: the door this is called from is re-entered by every arm of
// a race on a child context, and an arm that found one already there is not the
// question returning — it is one of its requests. So the outermost call, and
// only it, gets something to defer [callProgress.finished] on.
//
// A question nobody is watching opens nothing and costs one context lookup.
func beginCallProgress(ctx context.Context, model string) (context.Context, *callProgress) {
	if ctx == nil || ctx.Value(callProgressReporterKey{}) != nil {
		return ctx, nil
	}
	progress := newCallProgress(callProgressFrom(ctx), model)
	if progress == nil {
		return ctx, nil
	}
	return context.WithValue(ctx, callProgressReporterKey{}, progress), progress
}

// callProgressOn is the reporter this question opened, nil when nobody is
// watching it.
func callProgressOn(ctx context.Context) *callProgress {
	if ctx == nil {
		return nil
	}
	progress, _ := ctx.Value(callProgressReporterKey{}).(*callProgress)
	return progress
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

// callProgress is ONE QUESTION'S REPORT: what the caller asked for, the state of
// each request in flight for it, and the coalescing that keeps a sixty-hertz
// stream from becoming sixty redraws a second.
//
// ── IT IS PER QUESTION AND NOT PER REQUEST, AND THAT IS THE WHOLE DESIGN ────
//
// One question is more than one request more often than a reader would guess. A
// hedge puts two on the wire at once; a refusal starts a rescue arm; a retry
// inside the dispatcher, a repaired 400 and a rung of the endpoint ladder each
// close the model-call log's row and open another. Every one of those is a
// request going out, and NONE of them is the question being over.
//
// So the state is kept per attempt and the ENDING is kept once. A reader is told
// which request each report is about ([CallProgress.Attempt]) and gets exactly
// one [CallEnded] — from the door the question itself returns through — so a row
// drawn from this seam cannot settle on the first 429 of a call that went on to
// answer.
//
// Every method is nil-safe, so an unwatched call pays one nil check at each seam
// and nothing else.
type callProgress struct {
	watcher CallWatcher
	model   string

	mu sync.Mutex
	// attempts is what each request in flight has done, kept apart because two
	// arms of a race are two requests: one set of counters would interleave two
	// token counts and a reader would draw their sum.
	attempts map[int]*callAttempt
	// spoke is when this question last reported. It is SHARED by the attempts
	// because the beat is a fact about the reader and not about any one stream:
	// two arms writing at sixty hertz each are still one row being redrawn.
	spoke time.Time
	// end, err and ending are the question's own ending and the attempt it came
	// from, latched as the attempts land and spent once by
	// [callProgress.finished].
	end    CallEnd
	err    error
	ending int
	// open is set by the first request going out and cleared by the ending, so
	// that nothing speaks after the last word.
	open bool
}

// callAttempt is one request's state.
type callAttempt struct {
	started    time.Time
	firstToken time.Time
	served     string
	tokens     int
	reasoning  int
	phase      CallPhase
}

// newCallProgress builds the reporter for one question, or nil when nobody is
// watching — which is every call in an ordinary run.
func newCallProgress(watcher CallWatcher, model string) *callProgress {
	if watcher == nil {
		return nil
	}
	return &callProgress{watcher: watcher, model: model, attempts: map[int]*callAttempt{}}
}

// state is one attempt's row, made if this is the first anybody has heard of it.
// It runs with the lock held.
func (p *callProgress) state(attempt int) *callAttempt {
	row, known := p.attempts[attempt]
	if !known {
		row = &callAttempt{}
		p.attempts[attempt] = row
	}
	return row
}

// opened is one request going out. The moment is taken from the row the
// model-call log writes at that instant (calllog.go) rather than from a clock
// read of its own, because the two facts are the same fact and a second reading
// of the world's clock could only disagree with the first.
//
// THE COUNTS ARE RESET AND [CallProgress.Started] MOVES. A request that is
// refused and sent again is a new wait for the person sitting in front of it,
// and a row still counting up from the first one would be a clock that does not
// mean anything.
func (p *callProgress) opened(attempt int, at time.Time) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.attempts[attempt] = &callAttempt{started: at, phase: CallStarted}
	p.open = true
	p.say(attempt, at)
}

// serving is the machine the stream named, reported at once rather than held for
// the next delta: it happens once per request, and on a lane that then thinks
// for a minute it is the only thing there is to say.
func (p *callProgress) serving(attempt int, lane string) {
	if p == nil || lane == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	row := p.state(attempt)
	if row.served == lane {
		return
	}
	row.served = lane
	p.say(attempt, row.started)
}

// paced is a request parking on a provider's "not yet", and leaving that park.
//
// LEAVING IT RESTORES THE PHASE THE REQUEST WAS IN, which is [CallStarted] on
// every real park: the bytes never reached a machine, so nothing had been
// thought or written.
func (p *callProgress) paced(attempt int, parked bool, at time.Time) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	// A PARK ONLY EXISTS WHILE THE QUESTION DOES. The dispatcher takes the park
	// back from a deferred call on every way out of its loop, including the way
	// out of a call that has just been given up on — so speaking here would put a
	// request going out AFTER the question's own ending, which is the one order a
	// surface cannot draw.
	if !p.open {
		return
	}
	row := p.state(attempt)
	phase := CallStarted
	if parked {
		phase = CallPaced
	}
	if row.phase == phase {
		return
	}
	row.phase = phase
	p.say(attempt, at)
}

// note is one moment of one stream, already folded by the watch that owns the
// counts — visible is progress a person could read and hidden is the run of
// thought, exactly as [control.Reading] separates them.
//
// The moment comes from the reading rather than from a clock here, so a scenario
// written in seconds is judged in the seconds it wrote.
func (p *callProgress) note(attempt int, at time.Time, visible, hidden int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	row := p.state(attempt)
	first := row.firstToken.IsZero()
	if first {
		row.firstToken = at
	}
	row.tokens, row.reasoning = visible, hidden
	phase := CallThinking
	if visible > 0 {
		phase = CallWriting
	}
	turned := row.phase != phase
	row.phase = phase
	// NEWS IS NEVER HELD, AND EVERYTHING ELSE IS. The first token and the phase
	// turning over each happen once and are the two things a person is watching
	// for; a climbing count between them is worth one report a frame and no more
	// (callProgressBeat).
	if first || turned || at.Sub(p.spoke) >= callProgressBeat {
		p.say(attempt, at)
	}
}

// landed is ONE REQUEST coming back, and it says nothing out loud.
//
// ── WHY AN ATTEMPT ENDING IS NOT AN ENDING ──────────────────────────────────
//
// The model-call log writes a closing row for every attempt, and a question
// makes more of them than a reader would guess. Reporting each as [CallEnded]
// would have a surface settle a row on the first 429 of a call that went on to
// answer — the one mistake a reader of this seam would make, because the word
// would have told them to.
//
// So an attempt's ending is LATCHED here and spent by [callProgress.finished].
// What a reader sees in the meantime is the next [callProgress.opened] — the
// request going out again, with its own moment — which is the useful fact.
//
// A CANCELLED ARM NEVER BECOMES THE QUESTION'S ENDING WHILE ANYTHING ELSE
// COULD. A race cuts its loser off the instant the winner commits, and that
// arm's row says `context canceled` — exhaust, not failure (armwatch.go's
// [streamWatch.lost] says what reading it as failure has already cost one
// census). So a cancel is latched only when nothing has landed at all, and any
// real ending after it takes its place.
func (p *callProgress) landed(attempt int, end CallEnd, err error) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.open {
		return
	}
	if end == CallEndCancelled && p.end != "" {
		return
	}
	p.end, p.err, p.ending = end, err, attempt
}

// finished is the QUESTION being over, said once.
//
// IT IS DRIVEN FROM THE ONE PLACE THAT KNOWS — the door the question itself
// returns through (client.go's completeWithMessagesStreaming, on the call that
// built this reporter) — because no row in the log and no arm of a race can say
// whether another request is still coming. A question that ended with nothing
// latched is one nothing wrote a closing row for, which the log's own law says
// cannot happen; [CallEndCut] is the honest reading if it ever does, because the
// request is over and no answer came out of it.
func (p *callProgress) finished() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.open {
		return
	}
	p.open = false
	if p.end == "" {
		p.end = CallEndCut
	}
	p.state(p.ending).phase = CallEnded
	p.say(p.ending, p.state(p.ending).started)
}

// say hands the report over. It runs with the lock held, which is what keeps two
// goroutines' events in the order they happened — the read loop's deltas and a
// race's cancel are genuinely concurrent, and a reader shown the ending before
// the last token would draw a call that finished before it wrote.
//
// It is also why the watcher may not do work: this lock is on the read loop's
// own path. See [WithCallProgress].
func (p *callProgress) say(attempt int, at time.Time) {
	// The beat is only ever moved FORWARD. Two of the seams have no moment of
	// their own and hand over the one they know — the request going out — and a
	// clock that walked backwards on them would spend the next delta's coalescing
	// budget on nothing.
	if at.After(p.spoke) {
		p.spoke = at
	}
	row := p.state(attempt)
	report := CallProgress{
		Model:      p.model,
		Served:     row.served,
		Attempt:    attempt,
		Started:    row.started,
		FirstToken: row.firstToken,
		Tokens:     row.tokens,
		Reasoning:  row.reasoning,
		Phase:      row.phase,
	}
	// A LATCHED ENDING IS NOT AN ENDING YET, and no report but the last one may
	// carry one: a report that went out in between carrying it — the dispatcher
	// taking a pacing park back, say — would tell a surface that a question still
	// running had already failed.
	if report.Phase == CallEnded {
		report.End, report.Err = p.end, p.err
	}
	p.watcher(report)
}
