package session

// PROMOTING A RUNNING FOREGROUND COMMAND INTO A JOB.
//
// A foreground `bash` call used to be committed at the moment it was made. When
// the command turned out to be a nine-minute build, the 120-second law killed
// the process group, the call answered `Command timed out after 120 seconds`,
// and the only way forward was to run the whole thing AGAIN with
// background:true. Two minutes of work thrown away, every time, for a judgement
// nobody could make in advance: no model and no person knows which side of the
// line `make` falls on until it is already past it.
//
// So a call that hits its bound is now ADOPTED rather than killed. The process
// keeps running, the registry takes it over, and the call answers
//
//	still running as job 3; log at /path/to/.aforge-v3/jobs/3.log
//
// which is the sentence a background start already speaks (tools_jobs.go). From
// there it is a job in every way that matters — a row in `jobs list`, a tail in
// `jobs output`, a kill that reaches its whole process group, a death at Close,
// and an exit note on the steering lane at the next step boundary. Nothing new
// was invented; one existing capability grew one door.
//
// ── WHERE THE SEAM IS, AND WHY THERE ──
//
// Two things had to stay where they were.
//
// THE TIMEOUT LAW IS ONE READ, and it is [BashTimeoutSeconds]: the wrapper
// writes it into the wire arguments, bare arms one timer from those arguments,
// and internal/tui3 counts down against the same function. Moving the clock up
// here would have made a second authority on when a command dies, and the
// surface's countdown would have been counting against a number nothing
// enforced.
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
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// promotedSentence is the one line a promoted call answers with. It is
// deliberately the shape [Agent.backgroundBash] already speaks for a background
// start — an id and a path, and nothing else — because the model should not
// have to learn two ways of being told the same fact.
func promotedSentence(id int, logPath string) string {
	return fmt.Sprintf("still running as job %d; log at %s", id, logPath)
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
	agent  *Agent
	callID string
}

// Started registers the running call as promotable and hands back the
// forgetting. The registration is what makes gap B possible at all: a key
// pressed on a row has to find a process, and the process is only reachable
// while the call is in flight.
func (p bashPromotion) Started(call *bare.BashCall) func() {
	if p.callID == "" {
		// A call with no provider id — a test driving the belt directly — can
		// still be promoted by its own timeout; it simply cannot be addressed
		// by a keypress, because there is nothing to address it BY.
		return nil
	}
	p.agent.holdPromotable(p.callID, call)
	return func() { p.agent.releasePromotable(p.callID) }
}

// TimedOut is the timeout arriving with somebody there to take the process.
func (p bashPromotion) TimedOut(call *bare.BashCall) bool {
	_, promoted := p.agent.adoptRunningBash(call)
	return promoted
}

// promotable returns ctx carrying the door, for the FOREGROUND branch of bash
// and nowhere else.
func (a *Agent) promotable(ctx context.Context) context.Context {
	return bare.WithBashPromoter(ctx, bashPromotion{agent: a, callID: callIDFrom(ctx)})
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
	mu    sync.Mutex
	calls map[string]*bare.BashCall
}

func (p *promotableCalls) hold(id string, call *bare.BashCall) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.calls == nil {
		p.calls = map[string]*bare.BashCall{}
	}
	p.calls[id] = call
}

func (p *promotableCalls) release(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.calls, id)
}

func (p *promotableCalls) find(id string) *bare.BashCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[id]
}

func (a *Agent) holdPromotable(id string, call *bare.BashCall) {
	a.inFlightBash.hold(id, call)
}

func (a *Agent) releasePromotable(id string) { a.inFlightBash.release(id) }

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
func (a *Agent) adoptRunningBash(call *bare.BashCall) (string, bool) {
	var answer string
	adopted := call.Adopt(func() (string, bool, bool) {
		started, err := a.jobs.adopt(call)
		if err != nil {
			return "", false, false
		}
		answer = promotedSentence(started.id, started.logPath)
		return answer, false, true
	})
	if !adopted {
		return "", false
	}
	return answer, true
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
