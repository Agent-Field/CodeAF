package session

// PROMOTING A RUNNING FOREGROUND COMMAND INTO A JOB.
//
// A foreground `bash` call used to be committed at the moment it was made. When
// the command turned out to be a fifteen-minute build, the timeout killed the
// process group, the call answered `Command timed out after N seconds`, and the
// only way forward was to run the whole thing AGAIN with background:true. The
// elapsed work thrown away, every time, for a judgement nobody could make in
// advance: no model and no person knows which side of the line `make` falls on
// until it is already past it.
//
// So a call that hits its bound is now ADOPTED rather than killed. The process
// keeps running, the registry takes it over, and the call answers
//
//	still running as job 3; log at /path/to/.aforge-v3/jobs/3.log
//
// followed by everything the command has printed so far ([promotedSentence]).
// The first line is the sentence a background start already speaks
// (tools_jobs.go). From there it is a job in every way that matters — a row in
// `jobs list`, a tail in `jobs output`, a kill that reaches its whole process
// group, a death at Close, and an exit note carrying its output on the steering
// lane at the next step boundary. Nothing new was invented; one existing
// capability grew one door.
//
// THE FIRST ARMED BOUND WINS. The session's background-after clock normally
// makes the handoff; the command's own timeout may make it sooner. With the
// session clock off, the timeout-only posture remains exactly as it was.
//
// ── WHERE THE SEAM IS, AND WHY THERE ──
//
// Two things had to stay where they were.
//
// THE TIMEOUT LAW IS ONE READ, and it is [BashTimeoutSeconds]: the wrapper
// writes it into the wire arguments and bare arms one timer from those
// arguments. The session clock is armed here from the one Config value, and
// [BashBoundSeconds] lets the surface count down against whichever comes first.
//
// PROCESS OWNERSHIP IS ONE REGISTRY, and it is [jobRegistry]. bare must not
// grow a second reaper: everything that decides a process is over — the settle,
// the note, the kill, the shutdown — lives in jobs.go, and a second place that
// knew how to end a process would be a second set of rules about which deaths
// are reported.
//
// So bare keeps the timer and the exec.Cmd frame, and offers the RUNNING call
// through a door in the context (internal/exec/bare's promote.go). This file
// implements that door. bare never learns what a job is; the registry never
// learns what a timeout is.
//
// The one thing that could not be moved is the wait: Go permits exactly one
// cmd.Wait per command and bash's is already in flight when a promotion
// happens. So the registry does not wait on an adopted process — it reads the
// exit code off the channel bare's existing waiter feeds, and settles it the way
// it settles everything else ([jobRegistry.settleExit]).
//
// ── THE DECISIONS AT THE EDGES ──
//
// AN INTERRUPT IS STILL AN INTERRUPT. esc cancels the turn's context, and a call
// whose context is cancelled cannot be adopted — bare refuses it inside the
// claim. A person who asked for the work to stop does not get a job that
// outlives the turn they just ended.
//
// A BACKGROUND CALL IS NEVER PROMOTED, because it never reaches here: the door
// is installed only on the foreground branch of [Agent.backgroundBash], so a
// call that asked for background:true went to [jobRegistry.start] and was a job
// from the first instant. The guard is the branch, and it is stated at the
// branch.
//
// CLOSE KILLS A PROMOTED JOB LIKE ANY OTHER. Between the promotion and the exit
// it is an ordinary row in the registry, so [jobRegistry.shutdown] claims it,
// SIGTERMs it, and kills it after the shared grace (jobs.go). Its death is
// requested, so no note lands on a queue whose journal is about to close.
//
// A TASK NODE PROMOTES THE SAME WAY. `watch` comes off a node's belt because a
// watch's whole delivery mechanism is a note arriving in a conversation and a
// node has none — but `jobs` does not, and never has: a node has a steering lane
// of its own, drains it at its own step boundaries, and a node whose `make` runs
// long is in exactly the bind this file exists to end. So the behaviour is the
// same in a node, deliberately, and the only difference is who reads the note.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// promotedSentence is what a promoted call answers with: the line
// [Agent.backgroundBash] already speaks for a background start — an id and a
// path — and then THE OUTPUT THE COMMAND HAS ALREADY PRODUCED.
//
// ── WHY THE OUTPUT IS HERE AND NOT LEFT IN THE LOG ──
//
// The id and the path used to be the whole answer, on the reasoning that the
// model could go and read the rest. What that cost was measured: a model handed
// a bare id has learned nothing about the work, so its next move is to look at
// the log — and a command that is still running has usually printed the part
// that matters (the plan, the first failures, the progress) long before it
// exits. Handing that back with the id turns a promotion from a question into an
// answer, and the commonest next call from a model that reads a promotion — go
// and tail this — stops being worth making.
//
// It is bounded by [bare.TailForResult], which is the SAME truncation this
// command's own result would have been cut by had it finished: last whole lines
// inside pi's line and byte caps. A promoted call and a finished one are the
// same command, so the amount of it the model may read is the same number.
//
// THE ID LEADS. Everything downstream reads this sentence from the front — the
// surface, the tests, a person's eye — and a tail of build output above it would
// bury the one fact that says what happened.
//
// BashPromotedLead is exported because the surface has to recognize this one
// result without inventing a second spelling of it. The composer and every
// reader share the same lead; the job id and the rest of the sentence remain
// the engine's facts.
const BashPromotedLead = "still running as job "

func promotedSentence(id int, logPath, sofar string) string {
	line := fmt.Sprintf("%s%d; log at %s", BashPromotedLead, id, logPath)
	if strings.TrimSpace(sofar) == "" {
		return line
	}
	return line + "\n\n" + bare.TailForResult(sofar)
}

// ── the call id, carried to the tool ────────────────────────────────────────

// callIDKey is how one tool call's provider id reaches the tool that is running
// it. It is set once, at the pre-action chokepoint (loop.go's
// [Agent.executeTool]), so every path that can run a tool carries it — the
// batch and the early warm start alike.
type callIDKey struct{}

func withCallID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, callIDKey{}, id)
}

func callIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(callIDKey{}).(string)
	return id
}

// ── the door ────────────────────────────────────────────────────────────────

// bashPromotion is this session's answer to bare's handoff seam: one per
// foreground bash call, carrying the call's id so the SURFACE can address it
// while it is still running (internal/tui3's ctrl+g).
type bashPromotion struct {
	agent   *Agent
	callID  string
	request uint64
}

// Started arms the session clock, registers the running call as promotable, and
// hands back the forgetting for both. The registration is what makes gap B
// possible at all: a key pressed on a row has to find a process, and the process
// is only reachable while the call is in flight.
func (p bashPromotion) Started(call *bare.BashCall) func() {
	// The origin is registered before any clock or steering can adopt the call.
	p.agent.holdPromotable(p.callID, call, p.request)
	var timer *time.Timer
	if seconds := p.agent.config.BashBackgroundAfterSeconds; seconds > 0 {
		wait := time.Duration(seconds)*time.Second - call.RunningFor()
		if wait < 0 {
			wait = 0
		}
		timer = time.AfterFunc(wait, func() {
			// THE CLAIM IS QUIET. The process and job id become one fact under
			// bare's adoption lock; the person-visible row is announced only
			// after that lock is released, exactly as the steer door does.
			//
			// AND IT IS OWED. The clock moved a command the work was WAITING for;
			// nobody asked for it to be let go of, so the work is not asked for
			// its next step until this command's ending is in front of it
			// (task_job_park.go).
			started, adopted := p.agent.adoptRunningBashAs(call, func(started *job) string {
				return promotedSentence(started.id, started.logPath, started.sink.text())
			}, adoption{quiet: true, owed: true})
			if adopted {
				p.agent.jobs.announceRow(started)
			}
		})
	}
	return func() {
		if timer != nil {
			timer.Stop()
		}
		p.agent.releasePromotable(p.callID, call)
	}
}

// TimedOut is the timeout arriving with somebody there to take the process. It
// is owed for [bashPromotion.Started]'s reason, and it goes through
// [Agent.adoptRunningBash], which is where that is said.
func (p bashPromotion) TimedOut(call *bare.BashCall) bool {
	_, promoted := p.agent.adoptRunningBash(call)
	return promoted
}

// promotable returns ctx carrying the door, for the FOREGROUND branch of bash
// and nowhere else.
func (a *Agent) promotable(ctx context.Context) context.Context {
	return bare.WithBashPromoter(ctx, bashPromotion{agent: a, callID: callIDFrom(ctx), request: a.requestForWork()})
}

// ── the in-flight calls a keypress can reach ────────────────────────────────

// promotableCalls is every foreground bash call running right now, keyed by the
// provider's id for it.
//
// Its lock is its own and is NEVER [Agent.mu]. The surface calls in from the
// input goroutine while a turn holds mu, and the whole point of this map is to
// be reachable at the moment the session is busiest — the same argument
// jobs.go makes for keeping the registry's locks off the turn's.
type promotableCalls struct {
	mu       sync.Mutex
	calls    map[string]*bare.BashCall
	requests map[*bare.BashCall]uint64
}

func (p *promotableCalls) hold(id string, call *bare.BashCall, request uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.calls == nil {
		p.calls = map[string]*bare.BashCall{}
	}
	if id != "" {
		p.calls[id] = call
	}
	if p.requests == nil {
		p.requests = make(map[*bare.BashCall]uint64)
	}
	p.requests[call] = request
}

func (p *promotableCalls) release(id string, call *bare.BashCall) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.calls, id)
	delete(p.requests, call)
}

func (p *promotableCalls) find(id string) *bare.BashCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[id]
}

func (p *promotableCalls) snapshot() []*bare.BashCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	calls := make([]*bare.BashCall, 0, len(p.calls))
	for _, call := range p.calls {
		calls = append(calls, call)
	}
	return calls
}

// requestOf reads immutable producer metadata without taking the agent lock.
// Steering already holds that lock when it promotes a foreground command.
func (p *promotableCalls) requestOf(call *bare.BashCall) uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requests[call]
}

func (a *Agent) holdPromotable(id string, call *bare.BashCall, request uint64) {
	a.inFlightBash.hold(id, call, request)
}

func (a *Agent) releasePromotable(id string, call *bare.BashCall) {
	a.inFlightBash.release(id, call)
}

// ── the adoption itself ─────────────────────────────────────────────────────

// adoptRunningBash takes a running foreground bash process into the job
// registry and answers with the sentence the tool call returns.
//
// Everything happens INSIDE bare's claim (see [bare.BashCall.Adopt]): between
// deciding to promote and having a job id to name, the process could exit on its
// own or the person could interrupt, and a job row for a call that already
// answered would be a second account of one command. A registry that cannot open
// a log file simply declines — the claim is given back and the call ends the way
// it would have ended with no promoter at all, which is the honest failure for a
// capability whose whole promise is "and the work is not lost".
// BOTH ROADS THROUGH HERE ARE OWED. A timeout and the surface's key are the two
// ways a command the work is still WAITING for becomes a job without the work
// ever having asked to be free of it, so neither is a step the model may be
// asked to follow until the ending arrives (task_job_park.go).
func (a *Agent) adoptRunningBash(call *bare.BashCall) (string, bool) {
	var answer string
	_, adopted := a.adoptRunningBashAs(call, func(started *job) string {
		answer = promotedSentence(started.id, started.logPath, started.sink.text())
		return answer
	}, adoption{owed: true})
	if !adopted {
		return "", false
	}
	return answer, true
}

// adoptRunningBashAs is the one adoption claim with the tool-result sentence
// left to the caller. Timeout promotion, a person's steer and a person's stop
// all take the same process into the same registry; only the immediate account
// returned to the interrupted tool call differs — and [adoption], which is what
// the caller knows about the road it came down and this claim does not.
func (a *Agent) adoptRunningBashAs(call *bare.BashCall, answerFor func(*job) string, how adoption) (*job, bool) {
	var started *job
	how.request = a.inFlightBash.requestOf(call)
	adopted := call.Adopt(func() (string, bool, bool) {
		var err error
		started, err = a.jobs.adopt(call, how)
		if err != nil {
			return "", false, false
		}
		answer := answerFor(started)
		return answer, false, true
	})
	if !adopted {
		return nil, false
	}
	return started, true
}

// PromoteCall sends a running foreground bash call to the background and
// answers with the line that names the job it became.
//
// It is the SURFACE's door onto exactly the machinery the timeout uses — one
// seam, one adoption, one kind of job — because a key that killed and restarted
// the command would be the throw-away this whole file exists to remove. It
// answers false when there is no such call running, when the call has already
// finished, and when the turn has been interrupted; internal/tui3 draws no key
// in the first case, which is a capability that cannot work being absent rather
// than broken.
func (a *Agent) PromoteCall(callID string) (string, bool) {
	if callID == "" {
		return "", false
	}
	call := a.inFlightBash.find(callID)
	if call == nil {
		return "", false
	}
	return a.adoptRunningBash(call)
}
