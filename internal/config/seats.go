package config

import (
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/env"
)

// THE THREE SEATS EVERY DOOR SITS SOMEBODY IN, RESOLVED ONE WAY.
//
// A run needs its crew named before it can start: the worker that does the
// work, the planner that structures it, the checker that reads the result.
// Every door — `codeaf do`, `plan run`, `plan new`, `exec`, a saved program —
// resolves them here, through one ladder, so a pin said in the product's own
// vocabulary holds on every surface (#166 was the day it did not).
//
// THE LADDER, ONCE PER SEAT:
//
//  1. the flag, which is what this invocation said — a ONE-TASK PIN;
//  2. the environment, which is what this campaign said — a pin too;
//  3. a one-task `--pin`, then the profile's pin (/crew pin);
//  4. THE ROUTER, for every seat nothing above named: the task is classified
//     and the seat picked for it (internal/crewroute, [RouteCrew]).
//
// THERE IS NO FIFTH RUNG. A seat nothing pinned is routed; it never falls
// back to a model this build chose for everybody, and the check seat never
// quietly inherits the planner's model because only the planner was named —
// a person who pinned the planner said something about planning.
//
// IT ALSO REPORTS WHICH RUNG ANSWERED, because the defect this file was
// written for was invisible from outside, and a run whose receipt names the
// rung can be checked by the script that ran it.

// SeatRole is which of the three seats one answer fills, by the name a person
// reads. It is on the [Seat] so a seat can name its own flag and variable.
type SeatRole string

const (
	// SeatWork is the worker: the model that does the work.
	SeatWork SeatRole = "worker"
	// SeatPlan is the planner: plans, replans, contracts, the delivery gate.
	SeatPlan SeatRole = "planner"
	// SeatCheck is the checker: the model that reads finished work.
	SeatCheck SeatRole = "checker"
)

// crewSeatOf is the router's seat for a role.
func crewSeatOf(role SeatRole) crewroute.Seat {
	switch role {
	case SeatPlan:
		return crewroute.Planner
	case SeatCheck:
		return crewroute.Checker
	}
	return crewroute.Worker
}

// SeatSource is the rung that answered. It is an enum rather than a sentence
// because a caller putting it in JSON is making a promise a script parses.
type SeatSource string

const (
	// SeatFlag is `--model`, `--plan-model` or `--check-model`: the most recent
	// thing the person said, and it always wins.
	SeatFlag SeatSource = "flag"
	// SeatEnv is the environment: automation's override, set once for a
	// campaign instead of threaded onto every invocation.
	SeatEnv SeatSource = "env"
	// SeatPinned is a pin: the profile's own (/crew pin) or a one-task `--pin`.
	SeatPinned SeatSource = "pinned"
	// SeatRouted is the router's pick for this task.
	SeatRouted SeatSource = "routed"
	// SeatDefault is this build's own model for a tier that is not a crew
	// seat — reflex and small work — on a profile that never wrote the row.
	SeatDefault SeatSource = "default"
)

// ModelEnv, PlanModelEnv, and CheckModelEnv are the variables the seats read.
// They are spelled here once because the ladder, [Load], and receipts name them.
const (
	ModelEnv      = "CODEAF_MODEL"
	PlanModelEnv  = "CODEAF_PLAN_MODEL"
	CheckModelEnv = "CODEAF_CHECK_MODEL"
)

// Seat is one seat's answer: the model, where it came from, and the provider
// it runs on when the router or a pin named one.
type Seat struct {
	// envSpelling is the variable that answered when Source is [SeatEnv] —
	// the current spelling or the former one — recorded at resolution so the
	// receipt says the row the person wrote.
	envSpelling string
	Role        SeatRole
	Model       string
	Source      SeatSource
	Provider    string
}

// Flag is the flag that fills this seat.
func (s Seat) Flag() string {
	switch s.Role {
	case SeatPlan:
		return "--plan-model"
	case SeatCheck:
		return "--check-model"
	}
	return "--model"
}

// Env is the variable that fills this seat.
func (s Seat) Env() string {
	switch s.Role {
	case SeatPlan:
		return PlanModelEnv
	case SeatCheck:
		return CheckModelEnv
	}
	return ModelEnv
}

// Rung is the answering rung as a receipt says it: the flag or the variable by
// name, and otherwise the rung's own word. Naming the flag and the variable
// rather than saying "flag" and "env" costs nothing and tells a reader which
// of the two they would have to change.
func (s Seat) Rung() string {
	switch s.Source {
	case SeatFlag:
		return s.Flag()
	case SeatEnv:
		if s.envSpelling != "" {
			return s.envSpelling
		}
		return s.Env()
	case "":
		return string(SeatDefault)
	}
	return string(s.Source)
}

// Describe is one seat in a receipt's voice:
//
//	worker z-ai/glm-5.3-flash (routed)
//	checker moonshotai/kimi-k3 (pinned)
//
// THE EMPTINESS LAW: a seat with no model says so in words rather than
// leaving a gap somebody has to interpret.
func (s Seat) Describe() string {
	model := strings.TrimSpace(s.Model)
	if model == "" {
		model = "no model"
	}
	return string(s.Role) + " " + model + " (" + s.Rung() + ")"
}

// modelsLabel is the word a door puts in front of the sentence. It is spelled
// once so the doors cannot label the same fact differently.
const modelsLabel = "models: "

// Line is one seat labelled, for the door that seats only one — `exec`, which
// executes and never plans.
func (s Seat) Line() string { return modelsLabel + s.Describe() }

// Report is what a door prints for one seat.
func (s Seat) Report() string { return s.Line() }

// Seats is one run's crew as the door resolved it, and the router's decision
// beside it when a seat was routed.
type Seats struct {
	Work  Seat
	Plan  Seat
	Check Seat
	// Crew is the router's decision for this task — the class it read, every
	// seat's pick and the estimate — nil only when no router was asked.
	Crew *crewroute.Decision
}

// Sentence is the crew, unlabelled:
//
//	worker z-ai/glm-5.3-flash (routed) · planner z-ai/glm-5.3-flash (routed) · checker moonshotai/kimi-k3 (pinned)
//
// ONE SHAPE AND NOT TWO: every run says every seat and names the rung beside
// each, so a script parsing this has one grammar to know.
func (s Seats) Sentence() string {
	return s.Work.Describe() + " · " + s.Plan.Describe() + " · " + s.Check.Describe()
}

// Line is the one line a headless run opens with.
func (s Seats) Line() string { return modelsLabel + s.Sentence() }

// Report is what a door prints: the models line, and under it the crew's own
// line — the class the task was read as and the estimate — when it was routed.
func (s Seats) Report() string {
	if s.Crew == nil {
		return s.Line()
	}
	return s.Line() + "\ncrew: " + s.Crew.Line(PinMark, -1)
}

// PinMark is the mark a pinned seat wears on a headless line. The chat
// surface draws its own, from its icon vocabulary.
const PinMark = "📌"

// SeatFlags are the three seat flags a door took.
type SeatFlags struct {
	Model      string
	PlanModel  string
	CheckModel string
}

// ResolveSeats climbs the ladder for all three seats and routes every seat
// nothing named, for the task in ask.
//
// A flag and a variable are handed to the router AS PINS FOR THIS TASK, so the
// decision it answers — the crew line, the estimate, the logged row — is the
// crew that actually runs, and a seat the flag filled still says `--model`
// on the receipt. A value is handed on WHOLE: a flag may read
// `moonshotai/kimi-k3:low`, and the level is split off where every level is,
// at the point of the call ([roles.SplitEffort]).
//
// The error is the router's: a seat nothing allowed can sit, or
// [ErrCrewAtCap] — beside which the seats still come back, because what a
// door does at the cap (ask, refuse, or be told to spend) is the door's.
func ResolveSeats(profileDir string, flags SeatFlags, ask CrewAsk) (Seats, error) {
	seats := Seats{Work: Seat{Role: SeatWork}, Plan: Seat{Role: SeatPlan}, Check: Seat{Role: SeatCheck}}
	if ask.Sends == nil {
		ask.Sends = map[crewroute.Seat]string{}
	}
	for _, slot := range []struct {
		seat *Seat
		flag string
	}{{&seats.Work, flags.Model}, {&seats.Plan, flags.PlanModel}, {&seats.Check, flags.CheckModel}} {
		seat := slot.seat
		if value := strings.TrimSpace(slot.flag); value != "" {
			seat.Model, seat.Source = value, SeatFlag
		} else if value := strings.TrimSpace(env.Value(seat.Env())); value != "" {
			seat.Model, seat.Source = value, SeatEnv
			seat.envSpelling, _ = env.Spelling(seat.Env())
		} else {
			continue
		}
		ask.Sends[crewSeatOf(seat.Role)] = seat.Model
	}
	decision, err := RouteCrew(profileDir, ask)
	if err != nil && !errors.Is(err, ErrCrewAtCap) {
		return seats, err
	}
	seats.Crew = &decision
	for _, seat := range []*Seat{&seats.Work, &seats.Plan, &seats.Check} {
		pick := decision.Seat(crewSeatOf(seat.Role))
		seat.Provider = pick.Provider
		if seat.Source != "" {
			continue
		}
		seat.Model = pick.Send
		if pick.Pinned {
			seat.Source = SeatPinned
		} else {
			seat.Source = SeatRouted
		}
	}
	return seats, err
}

// CheckEnvModel is the check seat's environment rung alone, for the chat's
// runs: a conversation has no flag, and CODEAF_CHECK_MODEL reaches a `/task`
// run the way the manual says it reaches a headless one. Empty when unset.
func CheckEnvModel() string {
	return strings.TrimSpace(env.Get(CheckModelEnv))
}

// TierSeatAt is ONE TIER ROW READ AS A SEAT, for the surfaces that seat roles
// rather than run a door: the conversation's role map (cmd/codeaf's v3Crew),
// the settings sheet's rows, and a run's crew factory for a seat its door did
// not name.
//
// A CREW SEAT'S ROW — worker, planner (mastermind), checker (high) — is its
// pin when one is written, and otherwise the router's pick for work of no
// particular class ([standingCrewSeat]): the calls that ride those tiers
// without a task in front of them are routed too, never handed a model this
// build chose for everybody.
//
// THE OTHER TWO ROWS — reflex and small work — are not crew seats and read the
// way they always have: a row somebody WROTE is theirs, a row they CLEARED
// reads empty and follows the conversation ([TierModelAt] argues why UNSET
// and CLEARED are different answers), and a key never held reads this build's
// own near-free model — or, on an account known to be low, a free one
// ([freeTierModelAt]).
func TierSeatAt(profileDir, tier string) Seat {
	if seat, ok := CrewTierSeat(tier); ok {
		role := SeatRole(seat)
		if pin, ok := CrewPinAt(profileDir, seat); ok {
			return Seat{Role: role, Model: pin.Model, Source: SeatPinned, Provider: pin.Provider}
		}
		return Seat{Role: role, Model: standingCrewSeat(profileDir, seat), Source: SeatRouted}
	}
	role := SeatRole(tierWords(tier))
	if written, held := persistedString(profileDir, tierKeyFor(tier)); held {
		return Seat{Role: role, Model: strings.TrimSpace(written), Source: SeatPinned}
	}
	return Seat{Role: role, Model: freeTierModelAt(profileDir, tier), Source: SeatDefault}
}

// tierWords is a non-crew row's name on the settings sheet.
func tierWords(tier string) string {
	switch tier {
	case ModelTierReflex:
		return "reflex"
	case ModelTierLow:
		return "small work"
	}
	return tier
}
