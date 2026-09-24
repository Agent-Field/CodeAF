package run

// The chat's task door reaches this engine through an interface the session
// package owns ([session.RunEngine]), because this package is built ON the
// session package — its crew seats every task in [session.NewBeltWorker] and its
// landing is [session.LandRunTree] — so a door there that imported this one would
// be the cycle the compiler refuses. This file is the other end of that seam: it
// states [Start] and [Land] in the session's own words and installs the engine at
// load, so a binary that links this package gets the run road and one that does
// not gets the door it always had.

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

// engine is [session.RunEngine] over this package's doors: [Start] drives the
// store to an outcome, and [Land] commits the run's working copy and writes the
// landing on its root.
type engine struct{}

func (engine) Start(ctx context.Context, spec session.RunSpec) session.RunSummary {
	// THE REVIEW ROUND IS ON for every task the chat's door opens: a leaf
	// that lands done is checked against its acceptance, and a check that
	// does not hold becomes a fix task the run waits on.
	limits := Limits{CostUSD: spec.CostUSD, Elapsed: spec.Elapsed, StepsPerTask: spec.StepsPerTask, ReviewRound: true}
	// THE CREW IS THE PROFILE'S, read again at each launch, and the seat's
	// provider is the door's own completer through the one seam a test
	// scripts ([CrewFactory]).
	//
	// THE DOOR'S TWO SEATS RIDE WITH THE SPEC. The conversation resolved
	// them itself (the enginewire spec's WorkModel and PlanModel), so the
	// factory seats the work and plan roles on the door's answer rather than
	// asking the profile again for a row the door already moved.
	factory := CrewFactory(spec.Store, spec.Workspace, spec.ProfileDir, Seats{
		Work: spec.WorkModel,
		Plan: spec.PlanModel,
	}, spec.CompleterFor)
	if spec.Delegate != nil {
		// A DELEGATED RUN SEATS THE PROGRAM ON ITS ROOT and has no review
		// round: a check seat is a bash-belt worker, which the belt switch may
		// have left off, and the program's own verification is what its
		// terminal record reports ([DelegateWorker]).
		limits.ReviewRound = false
		// AND ITS MODEL API RIDES THE CONVERSATION'S OWN ROAD: the completer the
		// door handed the run, the services the conversation can reach, and the
		// work seat a leaf of this run would sit on — which is where a call on
		// a model nothing here can reach is answered instead.
		setup := DelegateSetup{
			CompleterFor: spec.CompleterFor,
			Serves:       spec.Serves,
			Seat:         WorkSeat(spec.ProfileDir, spec.WorkModel),
			PlainFolder:  spec.PlainFolder,
			Ground:       spec.Ground,
			Crew:         spec.Crew,
			// AND ITS MONEY IS THE CONVERSATION'S, CALL BY CALL: every ledger row
			// names the conversation and the task, and every call is folded
			// into the conversation's books whole as it is metered.
			Conversation: spec.Conversation,
			OnCharge:     spec.OnCharge,
		}
		factory = DelegateFactory(spec.Store, spec.Workspace, *spec.Delegate, setup, limits, factory)
	}
	outcome, summary := Start(ctx, Spec{
		Store:     spec.Store,
		Workspace: spec.Workspace,
		Title:     spec.Title,
		Brief:     spec.Brief,
		Slots:     spec.Slots,
		Limits:    limits,
		Factory:   factory,
		OnSpend:   spec.OnSpend,
	})
	return session.RunSummary{
		Outcome: string(outcome),
		Result:  summary.Result,
		// WHICH LIMIT FIRED IS A FACT AND NOT A WORD IN THE OUTCOME SENTENCE:
		// the run's own typed answer crosses the seam here, mapped one for one,
		// so the session draws the ending out of the fact and never parses the
		// sentence back apart.
		Limit: runLimitOf(summary.Limit),
		// AND A PROGRAM'S OWN ENDING CROSSES AS ITSELF, the same way: its
		// status word and its sentence, so the row names what the program said
		// and not the run's one word for every unfinished ending.
		Program: programEndingOf(summary.Program),
		// THE ROWS THE RUN'S OWN ENDING CUT CROSS AS THEMSELVES: the same
		// one-for-one carrying as the limit fact, so the session draws a row
		// the person's bound took down from the run's own record of it and
		// never from the sentence the store was left holding.
		Cut:   summary.Cut,
		Nodes: summary.Nodes,
		Steps: summary.Steps,
		USD:   summary.USD,
	}
}

// runLimitOf is the seam's one mapping of the limit fact: the run's words and
// the session's are spelled apart because neither package may reach the other,
// and a limit this build does not know reads as none rather than as a guess.
func runLimitOf(limit Limit) session.RunLimit {
	switch limit {
	case LimitTime:
		return session.RunLimitTime
	case LimitCost:
		return session.RunLimitCost
	}
	return ""
}

// programEndingOf is the program's ending in the session's words, nil where no
// program ended the run unfinished.
func programEndingOf(ended *ProgramEndedError) *session.ProgramEnding {
	if ended == nil {
		return nil
	}
	return &session.ProgramEnding{Status: ended.Status, Reason: ended.Reason, Result: ended.Result}
}

func (engine) Land(ctx context.Context, store *plandb.Store, workspace, rootID string) (session.RunLanding, error) {
	landing, err := Land(ctx, store, workspace, rootID)
	if err != nil {
		return session.RunLanding{}, err
	}
	return session.RunLanding{Branch: landing.Branch, Changed: landing.Changed, Refused: landing.Refused}, nil
}

// init installs the engine into the chat's task door. It runs whenever this
// package is linked, so the door's second road is live in a binary that carries
// the engine and absent in one that does not.
func init() { session.RegisterRunEngine(engine{}) }
