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
	outcome, summary := Start(ctx, Spec{
		Store:     spec.Store,
		Workspace: spec.Workspace,
		Title:     spec.Title,
		Brief:     spec.Brief,
		Slots:     spec.Slots,
		// THE REVIEW ROUND IS ON for every task the chat's door opens: a leaf
		// that lands done is checked against its acceptance, and a check that
		// does not hold becomes a fix task the run waits on.
		Limits: Limits{CostUSD: spec.CostUSD, Elapsed: spec.Elapsed, StepsPerTask: spec.StepsPerTask, ReviewRound: true},
		// THE CREW IS THE PROFILE'S, read again at each launch, and the seat's
		// provider is the door's own completer through the one seam a test
		// scripts ([CrewFactory]).
		//
		// THE DOOR'S TWO SEATS RIDE WITH THE SPEC. The conversation resolved
		// them itself (the enginewire spec's WorkModel and PlanModel), so the
		// factory seats the work and plan roles on the door's answer rather than
		// asking the profile again for a row the door already moved.
		Factory: CrewFactory(spec.Store, spec.Workspace, spec.ProfileDir, Seats{
			Work: spec.WorkModel,
			Plan: spec.PlanModel,
		}, spec.CompleterFor),
	})
	return session.RunSummary{
		Outcome: string(outcome),
		Result:  summary.Result,
		Nodes:   summary.Nodes,
		Steps:   summary.Steps,
		USD:     summary.USD,
	}
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
