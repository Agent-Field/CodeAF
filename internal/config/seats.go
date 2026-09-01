package config

import (
	"os"
	"strings"
)

// THE TWO SEATS EVERY DOOR SITS SOMEBODY IN, RESOLVED ONE WAY.
//
// A run needs two models named before it can start: the one that plans and the
// one that works. The chat surface has always asked the profile for them — a
// role rides a tier, a tier is a crew row, and /crew writes all four
// ([ApplyCrew], internal/roles) — while every headless door resolved its own
// pair from a flag and an environment variable and never opened the profile at
// all. So "my crew is frugal", said in the product's own vocabulary, held in the
// conversation and was silently lost the moment the same brain ran under `do`
// (#166). The benchmark that found it had to hand-translate a preset into two
// slugs, which is the one-source-of-truth violation the crew exists to prevent.
//
// [ResolveSeats] is the whole ladder, in one place, for every door: the flag, the
// environment, the crew, the shipped default. It lives beside [CrewAt] rather
// than in the commands because a fifth door must inherit it by calling it, not
// by remembering to re-derive it — a rung copied into four files is a rung that
// will differ in one of them.
//
// IT ALSO REPORTS WHICH RUNG ANSWERED. That is not decoration: the whole defect
// was invisible from outside, and a run whose receipt names the rung can be
// checked by the script that ran it. [Config.Load] cannot answer that question —
// it folds the environment and the default together into one string — which is
// why the ladder reads the environment itself rather than reading it back off a
// loaded config.

// SeatRole is which of the two seats one answer fills. It is on the [Seat] so a
// seat can name its own flag and its own variable rather than being handed them.
type SeatRole string

const (
	// SeatWork is the model that does the work.
	SeatWork SeatRole = "work"
	// SeatPlan is the model that structures it — plans, replans, contracts, the
	// delivery gate. Empty is legal here and means "the work model plans too";
	// see [Config.PlanModelResolved].
	SeatPlan SeatRole = "plan"
)

// SeatSource is the rung that answered, and the four constants are the ladder in
// order. It is an enum rather than a sentence because a caller putting it in
// JSON is making a promise a script parses.
type SeatSource string

const (
	// SeatFlag is `--model` or `--plan-model`: the most recent thing the person
	// said, and it always wins.
	SeatFlag SeatSource = "flag"
	// SeatEnv is the environment: automation's override, set once for a campaign
	// instead of threaded onto every invocation.
	SeatEnv SeatSource = "env"
	// SeatCrew is the profile's own tier row — what /crew wrote.
	SeatCrew SeatSource = "crew"
	// SeatDefault is this build's choice, for a profile that has never said
	// anything about models at all.
	SeatDefault SeatSource = "default"
)

// ModelEnv and PlanModelEnv are the two variables the seats read. They are
// spelled here once because three places need them by name: the ladder, [Load],
// and the receipt that says one of them answered.
const (
	ModelEnv     = "AFORGE_MODEL"
	PlanModelEnv = "AFORGE_PLAN_MODEL"
)

// Seat is one seat's answer: the model, and where it came from.
type Seat struct {
	Role   SeatRole
	Model  string
	Source SeatSource
	// Crew is the preset word the profile's four tier rows make — `frugal`,
	// `balanced`, `max` or `custom` ([CrewAt]) — and is empty unless Source is
	// [SeatCrew]. It is read rather than stored, exactly as the settings sheet
	// reads it, so a receipt and the sheet cannot disagree about which crew ran.
	Crew string
}

// Flag is the flag that fills this seat.
func (s Seat) Flag() string {
	if s.Role == SeatPlan {
		return "--plan-model"
	}
	return "--model"
}

// Env is the variable that fills this seat.
func (s Seat) Env() string {
	if s.Role == SeatPlan {
		return PlanModelEnv
	}
	return ModelEnv
}

// Rung is the answering rung as a receipt says it: the flag or the variable by
// name, the crew by preset, and otherwise the one word `default`. Naming the
// flag and the variable rather than saying "flag" and "env" costs nothing and
// tells a reader which of the two they would have to change.
func (s Seat) Rung() string {
	switch s.Source {
	case SeatFlag:
		return s.Flag()
	case SeatEnv:
		return s.Env()
	case SeatCrew:
		if strings.TrimSpace(s.Crew) == "" {
			return "crew"
		}
		return "crew " + s.Crew
	}
	return "default"
}

// Describe is one seat in a receipt's voice:
//
//	work qwen/qwen3.8-27b (crew frugal)
//	plan follows the work model (default)
//
// THE EMPTINESS LAW, as the crew row already keeps it: an unfilled plan seat is
// said in words rather than left as a gap somebody has to interpret.
func (s Seat) Describe() string {
	model := strings.TrimSpace(s.Model)
	if model == "" {
		model = "follows the work model"
	}
	return string(s.Role) + " " + model + " (" + s.Rung() + ")"
}

// modelsLabel is the word a door puts in front of the sentence. It is spelled
// once so the four doors cannot label the same fact differently.
const modelsLabel = "models: "

// Line is one seat labelled, for the door that seats only one — `exec`, which
// executes and never plans.
func (s Seat) Line() string { return modelsLabel + s.Describe() }

// Seats is both seats of one run.
type Seats struct {
	Work Seat
	Plan Seat
}

// Sentence is both seats, unlabelled, for a door whose opening lines have a
// label column of their own:
//
//	work qwen/qwen3.8-27b (crew frugal) · plan qwen/qwen3.8-27b (crew frugal)
//
// ONE SHAPE AND NOT TWO. It would read a little better to collapse a run whose
// seats came from the same crew into one clause, and it would mean a script
// parsing this has two grammars to know and a person comparing two runs has two
// shapes to compare. Every run says both seats and names the rung beside each.
func (s Seats) Sentence() string {
	return s.Work.Describe() + " · " + s.Plan.Describe()
}

// Line is the one line a headless run opens with:
//
//	models: work qwen/qwen3.8-27b (crew frugal) · plan qwen/qwen3.8-27b (crew frugal)
func (s Seats) Line() string { return modelsLabel + s.Sentence() }

// ResolveSeats climbs the ladder once per seat.
//
//  1. the flag, which is what this invocation said;
//  2. the environment, which is what this campaign said;
//  3. the profile's crew — the planning seat takes the MASTERMIND tier and the
//     work seat takes the WORKER tier, because that is where the two roles ride
//     in chat: roles.DefaultAssignment puts RolePlanner on TierMastermind and
//     RoleWorker on TierWorker, and a chat task's own worker resolves through
//     the same row (internal/session's defaultTaskModel), so a headless run, an
//     adaptive run and a task handed off in conversation all call the same
//     model on the same profile;
//  4. this build's default.
//
// profileDir is the profile to ask, [ProfileDir] for an ordinary process. The
// environment is read HERE, at call time, and not carried in from [Load]: Load
// resolves AFORGE_MODEL and DefaultModel into one field and the difference
// between them is exactly what the receipt has to report.
//
// A tier value may carry a thinking level (`moonshotai/kimi-k3:low`) and is
// handed on WHOLE, exactly as a flag or a variable carrying one is. The level is
// applied where every other level is applied — the role ladder splits it into a
// model and an effort at the point of the call ([roles.SplitEffort],
// internal/session's roleRequest) — and the client seam takes the level off the
// slug it sends ([Config.providerConfig]). Neither of those is this function's
// business, and a rung that quietly shortened the value would be the second
// model policy this whole file exists to remove.
func ResolveSeats(profileDir, flagModel, flagPlanModel string) Seats {
	return Seats{
		Work: resolveSeat(SeatWork, profileDir, flagModel, ModelTierWorker, DefaultModel),
		Plan: resolveSeat(SeatPlan, profileDir, flagPlanModel, ModelTierMastermind, ""),
	}
}

// resolveSeat is the ladder itself, once, for either seat. The two seats differ
// in three values — which flag, which tier, what the bottom rung is — and in
// nothing else, which is why there is one function and not two.
//
// UNTOUCHED IS NOT THE CREW ANSWERING. A tier is read through [persistedString]
// rather than [TierModelAt] so that a profile which has never held the key falls
// THROUGH to the default rather than being reported as `crew balanced` — the
// four shipped tier defaults are the balanced row, so TierModelAt would answer
// for a person who has never said anything, the bottom rung would become
// unreachable, and this build's headless default model would quietly change for
// everybody. `crew` here means somebody chose a crew. A row cleared on purpose
// reads empty, which is "follow the conversation" — a sentence a headless run
// has no conversation to answer with — so it falls through too.
// EVERY RUNG HANDS ITS VALUE ON WHOLE. A tier value may read
// `moonshotai/kimi-k3:low` and a flag may say the same thing; both travel from
// here unchanged, are seeded into the plan role's binding unchanged, and are
// split into a model and a thinking effort by the role ladder at the point of
// the call. A crew rung that shortened the value would leave a person planning
// at `low` in the conversation and at nothing headless — the same divergence
// #166 is about, one knob over.
func resolveSeat(role SeatRole, profileDir, flag, tier, fallback string) Seat {
	seat := Seat{Role: role}
	if value := strings.TrimSpace(flag); value != "" {
		seat.Model, seat.Source = value, SeatFlag
		return seat
	}
	if value := strings.TrimSpace(os.Getenv(seat.Env())); value != "" {
		seat.Model, seat.Source = value, SeatEnv
		return seat
	}
	if written, ok := persistedString(profileDir, tierKeyFor(tier)); ok {
		if value := strings.TrimSpace(written); value != "" {
			seat.Model, seat.Source, seat.Crew = value, SeatCrew, CrewAt(profileDir)
			return seat
		}
	}
	seat.Model, seat.Source = fallback, SeatDefault
	return seat
}
