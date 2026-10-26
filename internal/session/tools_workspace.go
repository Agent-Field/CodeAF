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
// ([furrow.PeekTools]) and, when there is none or it has gone stale, asks
// furrow from a goroutine this session owns ([Agent.askFurrow]) and hands over
// whatever it already knew.
//
// THE COST OF THAT IS ONE BELT, AND IT IS BOUNDED. A session that is asked for
// its belt before the first answer lands has no workspace verbs on it; the
// belt is rebuilt per turn, so the next one has them if the folder is
// attached. The refresh starts when the session is built (see [newAgent]), a
// person has to read the screen and type before the first turn, and an
// unattached folder — the common case — never changes at all. The model is
// never told about a verb on a guess: the absence law holds with a cold cache
// exactly as it does with a missing binary.
func (a *Agent) workspaceTools() []bare.Tool {
	tools, fresh, _ := furrow.PeekTools(a.config.Workspace)
	if !fresh {
		a.askFurrow()
	}
	return tools
}

// askFurrow refreshes the remembered answer about this session's folder from a
// goroutine, once at a time, and returns at once.
//
// THE GOROUTINE IS THE SESSION'S AND CLOSE JOINS IT ([Agent.stopFurrow]).
// Cancelling the context kills a `furrow status` that is still running, and the
// join is what keeps a half-finished exec from outliving the state directory a
// caller is about to remove. A session already closing starts nothing: the
// flag and the group are touched under one lock, so the join cannot miss a
// refresh that begins after it.
func (a *Agent) askFurrow() {
	a.furrowMu.Lock()
	defer a.furrowMu.Unlock()
	if a.furrowStopped || a.furrowAsking {
		return
	}
	if a.furrowCtx == nil {
		a.furrowCtx, a.furrowStop = context.WithCancel(context.Background())
	}
	a.furrowAsking = true
	a.furrowJobs.Add(1)
	ctx, root := a.furrowCtx, a.config.Workspace
	guard.Go("session/furrow-detect", func() {
		defer a.furrowJobs.Done()
		defer func() {
			a.furrowMu.Lock()
			a.furrowAsking = false
			a.furrowMu.Unlock()
		}()
		furrow.Detect(ctx, root)
	})
}

// stopFurrow ends the refresh and waits for it, on [Agent.stopLaneBeat]'s terms.
func (a *Agent) stopFurrow() {
	a.furrowMu.Lock()
	a.furrowStopped = true
	stop := a.furrowStop
	a.furrowMu.Unlock()
	if stop != nil {
		stop()
	}
	a.furrowJobs.Wait()
}
