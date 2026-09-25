package run

// WHICH MODEL RUNS A TASK IS THE CREW'S ANSWER, NOT THE RUN'S. Every store
// task carries a role — the seat its shape or its own declaration gives it, read
// off the store at the moment it is asked (internal/plandb's RoleOf) — and this
// file is the one place that turns that role into the model the task's worker is
// built on. The crew the profile holds ([config.TierSeatAt]) is what answers, so
// a preset, a row pinned by hand and a tuned row all decide which model sits in
// a seat, and the run itself names no model of its own.
//
// THE SEAT A PERSON NAMED IS THE SEAT EVERY LAUNCH TAKES, WAKES INCLUDED. The
// two seats a door resolves for a run — the work seat and the plan seat
// ([config.ResolveSeats]) — ride here as [Seats], because a run does not launch
// once: the root is woken again to fold a child it split, a planner adds a leaf
// mid-run, and every one of those launches seats a task the door never saw. So
// the factory is HANDED the seats the door already climbed and reads them for
// the two tiers they name, rather than asking the profile again for a row the
// person overrode with a flag. THE CHECK RIDES THE CAREFUL WORK TIER ([SeatFor]),
// so a check the review round adds after the launch still takes the profile seat.
// The probe row has no door flag, nothing names it, so it still
// comes from the profile's tiers, and an empty seat falls exactly where an empty
// tier always fell.
//
// UNDER `--one-model` THE PROFILE DOES NOT ANSWER. A conversation started with
// the flag names its own model as every seat ([Seats.One]), and the factory
// seats every role on it without reading a crew row or the check seat's
// environment rung: the flag promises every text call rides the model the
// person is talking to, and a crew row answering for an empty seat is how a
// run under it once billed two models nobody named.
//
// THE READ IS AT LAUNCH, NEVER CACHED. The factory asks the profile again for
// every task it seats — for the check and probe rows, and for any seat the door
// left empty — so a crew change between two launches (a /crew run in the
// conversation, a tuned row landing, a row pinned by hand) is seen by the next
// worker launched rather than frozen into the moment the run began.

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
// leaf against its acceptance and takes the careful work tier; and a probe is
// a small disposable unknown and takes the small-work row. Any other word, a role this build has not
// learned, or a task the store could not name — does the work, so an unknown
// word falls to the seat every task is born in rather than failing a task on its
// metadata.
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

// Seats are the run's two resolved seats, as the door that opened the run named
// them: the work seat every leaf rides and the plan seat every planning task
// rides. They are the door's own answer — the flag, the environment and the crew
// it climbed ([config.ResolveSeats]) — carried so a task launched after the door
// sits in the seat the person named rather than one the profile happens to hold.
//
// AN EMPTY SEAT IS THE DOOR HAVING NAMED NOTHING, and it falls the way an empty
// tier always fell: the profile's row for the task's tier, and the worker row
// beneath that.
//
// Only the work and plan seats climb the ladder on their own; the check seat
// is the door's own three way answer ([config.CheckSeat]), carried here as
// Check: the check flag the person typed, else the plan flag they typed, else
// empty. A check rides the careful work tier ([SeatFor]), and an EMPTY Check
// is the door having named no check model, which falls the way an empty tier
// always fell: the profile's careful row, the crew's checker. The small row a
// probe rides has no flag on any door and is the profile's, read below.
type Seats struct {
	Work  string
	Plan  string
	Check string
	// One is the conversation's model under `--one-model`, and when it is set
	// it is EVERY seat: the three above, the probe no door names, and any role
	// this build has not learned. Nothing here asks the profile or the check
	// seat's environment rung while it is set, because the flag promises that
	// every text call rides the model the person is talking to — and an empty
	// seat falling to the crew row is exactly how a run under it billed models
	// nobody named.
	One string
}

// CrewFactory is the run's WorkerFactory: it seats each task in the model its
// role's seat names — the door's resolved seat where the door named one, the
// profile's tier row otherwise — and builds the bash-belt worker the run hosts
// it in already on that model.
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
func CrewFactory(store *plandb.Store, workspace, profileDir string, seats Seats, completerFor func(model string) session.Completer) WorkerFactory {
	return func(task plandb.Task) Worker {
		// UNDER `--one-model` THERE IS NO TIER TO READ. The door named one model
		// for every seat ([Seats.One]), so the role does not matter and neither
		// the profile nor the environment is asked.
		if seats.One != "" {
			return NewBashWorker(store, workspace, seats.One, completerFor(seats.One))
		}
		// A task the store cannot name — which the supervisor never hands over —
		// reads as the work seat, the same fallback SeatFor gives an unknown
		// role, so RoleOf's error needs no reader here.
		role, _ := store.RoleOf(task.ID)
		tier := SeatFor(role)
		// THE DOOR'S SEAT WINS WHERE IT NAMED ONE. A planner (the run's root or
		// a task that has children) rides the plan seat. A check rides the careful
		// work seat. A leaf and every task an unknown role falls to the work seat. The probe tier is named by nobody, so it
		// keeps the profile's row below.
		var model string
		switch tier {
		case config.ModelTierMastermind:
			model = seats.Plan
		case config.ModelTierWorker:
			model = seats.Work
		case config.ModelTierHigh:
			model = seats.Check
		}
		if model == "" {
			model = config.TierSeatAt(profileDir, tier).Model
		}
		if model == "" {
			model = config.TierSeatAt(profileDir, config.ModelTierWorker).Model
			if model == "" {
				return seatlessWorker{tier: tier}
			}
		}
		// EVERY CALL IS MARKED WITH THE SEAT THE TASK SITS, because this is the
		// one place that knows it: the spend guard holds the checker to its own
		// ceiling by seat, and a crew whose seats share one model would give it
		// nothing else to tell a check's call from a worker's.
		seat, _ := config.CrewTierSeat(tier)
		return NewBashWorker(store, workspace, model, session.SeatCompleter(seat, completerFor(model)))
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
