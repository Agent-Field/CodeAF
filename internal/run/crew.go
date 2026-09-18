package run

// WHICH MODEL RUNS A TASK IS THE CREW'S ANSWER, NOT THE RUN'S. Every store
// task carries a role — the seat its shape or its own declaration gives it, read
// off the store at the moment it is asked (internal/plandb's RoleOf) — and this
// file is the one place that turns that role into the model the task's worker is
// built on. The crew the profile holds ([config.TierSeatAt]) is what answers, so
// a preset, a row pinned by hand and a tuned row all decide which model sits in
// a seat, and the run itself names no model of its own.
//
// THE READ IS AT LAUNCH, NEVER CACHED. The factory closes over the profile
// directory and asks it again for every task it seats, so a crew change between
// two launches — /crew run in the conversation, a tuned row landing, a row
// pinned by hand — is seen by the next worker launched rather than frozen into
// the moment the run began.

import (
	"context"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

// SeatFor is the role-to-tier table: the crew row a task's role rides.
//
// The run's root and every coordinator are planning work and take the mastermind
// row; a leaf that does the work itself takes the worker row, which is also
// where a task born from add or split sits; the review round reads a finished
// leaf against its acceptance and takes the careful row; and a probe is a small
// disposable unknown and takes the small-work row. Any other word — a role this
// build has not learned, or a task the store could not name — does the work, so
// an unknown word falls to the seat every task is born in rather than failing a
// task on its metadata.
func SeatFor(role string) string {
	switch role {
	case plandb.RolePlan:
		return config.ModelTierMastermind
	case plandb.RoleCheck:
		return config.ModelTierHigh
	case plandb.RoleProbe:
		return config.ModelTierLow
	default:
		return config.ModelTierWorker
	}
}

// CrewFactory is the run's WorkerFactory: it seats each task in the model the
// crew gives the tier its role rides, and builds the bash-belt worker the run
// hosts it in already on that model.
//
// THE ROLE IS READ FROM THE STORE, NOT FROM THE TASK HANDED OVER. [SeatFor] is
// handed [plandb.Store.RoleOf]'s answer, so a leaf that split mid-work seats as a
// planner for its coordinating turns without a word on the task being rewritten,
// and the seat follows the shape as it stands at this launch.
//
// THE PROFILE ANSWERS THE MODEL, through [config.TierSeatAt], the same read a
// conversation and the settings sheet make; the seat it answers carries the rung
// it came from, but only the model travels here, because a worker is built at a
// path that has no surface to print the rung on. completerFor builds the seat's
// provider from the resolved model — a run hands the door's own, and a test
// records which model it was asked for.
//
// A TIER WITH NO MODEL FALLS TO THE WORKER ROW, and a worker row that is empty
// too is a task no model can run: the factory answers a worker that refuses
// rather than one built on an empty model, and its error names the tier, because
// the row a person has to go and fill is the one the message says. Empty here is
// not the same as never held: [config.TierSeatAt] answers this build's default
// for a tier key the profile has never held and only a row CLEARED on purpose
// reads empty, so a fallback means somebody emptied a row rather than that the
// profile is old.
func CrewFactory(store *plandb.Store, workspace, profileDir string, completerFor func(model string) session.Completer) WorkerFactory {
	return func(task plandb.Task) Worker {
		// A task the store cannot name — which the supervisor never hands over —
		// reads as the work seat, the same fallback SeatFor gives an unknown
		// role, so RoleOf's error needs no reader here.
		role, _ := store.RoleOf(task.ID)
		tier := SeatFor(role)
		model := config.TierSeatAt(profileDir, tier).Model
		if model == "" {
			model = config.TierSeatAt(profileDir, config.ModelTierWorker).Model
			if model == "" {
				return seatlessWorker{tier: tier}
			}
		}
		return NewBashWorker(store, workspace, model, completerFor(model))
	}
}

// seatlessWorker is the seat a task gets when the crew holds no model for its
// tier and none on the worker row either. It runs nothing and reports an error,
// because a task that cannot be seated must fail with the row that has to be
// filled rather than be built on an empty model and reach a provider with
// nothing to call.
type seatlessWorker struct {
	tier string
}

// Run refuses the task. The error names the tier the task rides and the worker
// row it fell through to, so a person reading the failure knows the two crew
// rows that have to hold a model before the task can run.
func (w seatlessWorker) Run(context.Context, plandb.Task) (Report, error) {
	return Report{}, fmt.Errorf("no model for the %s seat: its crew row is empty and the worker row is empty too", w.tier)
}
