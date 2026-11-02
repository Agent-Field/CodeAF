package session

// tools_workspace.go hands the belt the four verbs that act on the WORKSPACE'S
// OWN HISTORY — the files, as they were — rather than on the conversation.
//
// THEY ARE furrow's, AND furrow IS SOMEBODY ELSE'S PROGRAM. It is a separate
// tool (Agent-Field/furrow, Apache-2.0) written in another language, carried
// inside this binary and written out on first need, and the only thing this
// tree does with it is run its command line. internal/furrow owns every fact
// about that; this file owns one decision, which is whether the verbs are on
// the belt at all.
//
// AND THAT DECISION IS THE ABSENCE LAW, IN ITS SHARPEST FORM. A model told it
// can restore a workspace will plan a whole reply around being able to — it
// will offer to try something risky, promise the person their .env is
// recoverable, and only discover at the call that nothing is there. So a
// folder furrow is not watching — or the rare machine where the carried binary
// could not be written out at all — is handed NO VERB: [furrow.Tools] answers
// nil and this appends nothing. There is no second check to remember and no
// tool that exists in order to fail.
//
// WHY THE NAMES ARE `workspace_` AND NOT `rewind_`. codeaf already has a
// rewind, and it is an edit of the CONVERSATION that touches no file on disk
// (rewind.go). These four move bytes. Two families that both meant "put it
// back how it was" would be one word doing two jobs, and the first person to
// confuse them would lose work — so the two share no vocabulary at all.

import (
	"context"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/furrow"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// workspaceTools is the family, or nothing, from what is ALREADY KNOWN.
//
// IT NEVER WAITS FOR FURROW. The first version of this asked furrow on the spot,
// and the belt is assembled while the engine answers a window's hello, so a
// cold launch held its first frame behind `furrow --version` and then
// `furrow status` — two to three seconds on a folder furrow was never pointed
// at, because even that answer opens furrow's global store. Nothing the first
// frame draws depends on these four verbs, so this reads the remembered answer
// ([furrow.PeekTools]) and nothing more.
//
// THE BELT IS BUILT ONCE, so a verb that is not on it now has to come later by
// the road every other late capability takes: [Agent.armFamily], which appends
// at the tail. [Agent.askFurrow] asks from the background when the session is
// built and arms the verbs when furrow says the folder is attached
// ([Agent.armFurrowVerbs]). An unattached folder — the common case — never
// changes at all, and the model is never told about a verb on a guess: the
// absence law holds with a cold cache exactly as it does with a missing binary.
func (a *Agent) workspaceTools() []bare.Tool {
	tools, _ := furrow.PeekTools(a.config.Workspace)
	return tools
}

// firstBeltWait is how long the first request of a session waits for a furrow
// question that is already running. It is a courtesy to the doors that send a
// message the instant the session exists (`--once`, a resumed conversation with a
// pending prompt, a spawned worker): without it their only request would go out
// before the answer and carry no workspace verbs, and the arming that follows
// would cost a second prompt-cache write. It is a cap and not a promise — the
// request goes out without the verbs when it passes — and it is paid only where a
// request is made, never while the session is built.
const firstBeltWait = 200 * time.Millisecond

// askFurrow starts the background question about this session's folder unless
// the remembered answer is already fresh.
func (a *Agent) askFurrow() {
	if _, fresh := furrow.PeekTools(a.config.Workspace); fresh {
		return
	}
	built := a.config
	a.furrow.ask(built.Workspace, func(root string) { a.armFurrowVerbs(root, built) })
}

// armFurrowVerbs puts the verbs on the belt once furrow has said the folder is
// attached. It runs on the refresher's goroutine, so it touches the session's
// configuration as little as a background reader can: the profile comes from the
// copy taken when the session was built (whose live-profile pointer still follows
// a change of model), and the one live field it reads, the folder, is read under
// the session lock. It arms nothing if the session has since been anchored to
// another folder (the answer was about the old one) or belongs to a model with no
// tools, whose belt was built empty.
func (a *Agent) armFurrowVerbs(root string, built Config) {
	// Nothing to arm is the common answer, and it is decided before the session's
	// configuration is touched at all.
	tools, _ := furrow.PeekTools(root)
	if len(tools) == 0 || built.promptProfile().chat() {
		return
	}
	a.mu.Lock()
	same := a.config.Workspace == root
	a.mu.Unlock()
	if same {
		_, _ = a.armFamily(tools)
	}
}

// settleFurrow is the first request's one look at the question: if furrow is
// being asked right now, wait for it for at most [firstBeltWait], and only the
// first time. Every later request reads the belt as it stands.
func (a *Agent) settleFurrow() { a.furrow.settle(firstBeltWait) }

// furrowRefresher owns the background questions a session asks furrow, on the
// terms of [Agent.startLaneBeat]: the goroutine is the session's, Close cancels
// it and joins it, and a session that has begun closing starts nothing.
//
// Its mutex guards the flags and the group together, and IS held across
// guard.Go and the group's Add — both are non-blocking — so that the join in
// [furrowRefresher.close] cannot miss a refresh that begins after it. It is the
// LAST lock in any order: nothing is taken while it is held, so it is safe to
// reach from the belt, the request path or an anchor under the session lock.
type furrowRefresher struct {
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	jobs    sync.WaitGroup
	asking  map[string]bool
	busy    chan struct{}
	stopped bool
	settled bool
}

// ask starts a question about root unless one about that folder is already in
// flight. A question about ANOTHER folder runs beside it: an owned conversation
// anchored to a project after it was built asks about the project while the
// scratch folder's question may still be running ([Agent.AnchorWorkspace]).
func (r *furrowRefresher) ask(root string, landed func(root string)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped || r.asking[root] {
		return
	}
	if r.ctx == nil {
		r.ctx, r.cancel = context.WithCancel(context.Background())
		r.asking = map[string]bool{}
	}
	if len(r.asking) == 0 {
		r.busy = make(chan struct{})
	}
	r.asking[root] = true
	busy := r.busy
	r.jobs.Add(1)
	ctx := r.ctx
	guard.Go("session/furrow-detect", func() {
		defer r.jobs.Done()
		defer func() {
			r.mu.Lock()
			delete(r.asking, root)
			if len(r.asking) == 0 {
				close(busy)
			}
			r.mu.Unlock()
		}()
		furrow.Detect(ctx, root)
		// The verbs are armed BEFORE the question is reported finished, so a
		// caller released by it sees them on the belt. A cancelled question
		// answered nothing and arms nothing.
		if ctx.Err() == nil {
			landed(root)
		}
	})
}

// idle is closed when no question is running: the channel of the batch in
// flight, or an already-closed one.
func (r *furrowRefresher) idle() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.asking) == 0 {
		done := make(chan struct{})
		close(done)
		return done
	}
	return r.busy
}

// settle waits up to max for the questions in flight, once per session.
func (r *furrowRefresher) settle(max time.Duration) {
	r.mu.Lock()
	if r.settled || len(r.asking) == 0 {
		r.mu.Unlock()
		return
	}
	r.settled = true
	busy := r.busy
	r.mu.Unlock()
	timer := time.NewTimer(max)
	defer timer.Stop()
	select {
	case <-busy:
	case <-timer.C:
	}
}

// close cancels the questions and waits for them.
func (r *furrowRefresher) close() {
	r.mu.Lock()
	r.stopped = true
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	r.jobs.Wait()
}
