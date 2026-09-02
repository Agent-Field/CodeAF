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

// SeatSource is the rung that answered, and the five constants are the ladder in
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
	// SeatInherited is the profile's crew answering through an OLDER row than
	// this seat's own, because the profile was written before this seat existed
	// ([tierLineage]). It is still the crew answering — the model came from what
	// the person chose — and it is a separate rung because a person is owed the
	// difference between "the row you wrote" and "the row your row was split out
	// of".
	SeatInherited SeatSource = "inherited"
	// SeatDefault is this build's choice, for a profile that has never said
	// anything about models at all.
	SeatDefault SeatSource = "default"
)

// tierLineage is THE TABLE THAT MAKES A NEW SEAT SAFE TO ADD, and every tier
// word has a row in it whether or not it has an ancestor.
//
// A seat added to the ladder after people have profiles is a seat their profiles
// have no key for, and the ladder would fall past the crew they chose to this
// build's default — which is what happened when the worker row landed (#302):
// four pinned rows, and all the work quietly running on a model nobody picked.
// So the rule is written once, here, rather than discovered again per seat: A
// TIER WITH NO KEY OF ITS OWN INHERITS FROM THE ROW IT WAS SPLIT OUT OF, AND
// SAYS THAT IT DID. A tier that is nobody's descendant declares that by naming
// no ancestor, explicitly, so the next tier cannot join the table by forgetting
// to.
//
// Words is what the settings sheet calls the row, because the one line a person
// reads about the substitution has to name a row they can go and find.
//
// AND THE READ NEVER REPAIRS THE PROFILE. Writing the inherited key back would
// end the substitution for good after one run, and it would also mean every
// `aforge do` — several at once on the same profile, on a machine where the disk
// may be full — rewrites the file a person's crew lives in, to record a decision
// this build made rather than one they did. [ApplyCrew] already writes every
// row, so a person who answers the line ends it themselves, once, deliberately.
var tierLineage = map[string]struct {
	// Inherits is the tier this one falls back to, or empty for a tier that has
	// always existed and inherits from nothing.
	Inherits string
	// Words is this row's name on the settings sheet.
	Words string
}{
	ModelTierReflex: {Words: "reflex"},
	ModelTierLow:    {Words: "small work"},
	// The worker seat was split out of the small-work row in #278: before it,
	// the work seat of every headless run and every node of an adaptive run
	// resolved through `models.tiers.low`. That row is therefore the honest
	// ancestor — a profile pinned before #278 meant its small-work model when it
	// said what the work should run on.
	ModelTierWorker:     {Inherits: ModelTierLow, Words: "worker"},
	ModelTierHigh:       {Words: "careful work"},
	ModelTierMastermind: {Words: "thinking"},
}

// tierWords is a row's name on the settings sheet, for the one line that has to
// send somebody to it. An unknown tier answers with its own word, which is what
// a table this file owns can promise about a word it does not.
func tierWords(tier string) string {
	if row, ok := tierLineage[tier]; ok && row.Words != "" {
		return row.Words
	}
	return tier
}

// inheritedTier is the crew's answer for a tier the profile has no key for: the
// nearest ancestor row that was actually WRITTEN, and which row that was.
//
// It walks the lineage rather than taking one step, so a seat split out of a
// seat still reaches the row a profile older than both of them holds. The walk
// is bounded by the number of tiers, which makes a table with a cycle in it slow
// nothing down — [TestEveryTierDeclaresItsAncestry] is what refuses the cycle.
//
// AN UNWRITTEN ANCESTOR IS NOT AN ANSWER. Reading the ancestor through
// [TierModelAt] would hand back this build's default for that row and report it
// as the crew, which is the same lie one row over. A row cleared on purpose
// reads empty — "follow the conversation", which a headless run cannot — and
// falls through too.
func inheritedTier(profileDir, tier string) (model, from string, ok bool) {
	for hops := 0; hops < len(tierLineage); hops++ {
		row, known := tierLineage[tier]
		if !known || row.Inherits == "" {
			return "", "", false
		}
		tier = row.Inherits
		if written, held := persistedString(profileDir, tierKeyFor(tier)); held {
			if value := strings.TrimSpace(written); value != "" {
				return value, tier, true
			}
			return "", "", false
		}
	}
	return "", "", false
}

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
	// From is the tier word this seat's model was INHERITED from, and is empty
	// unless Source is [SeatInherited]. It is carried rather than re-derived
	// because the line that tells a person what happened has to name the row it
	// read, and a second walk of the lineage at print time could name a
	// different one.
	From string
	// Crew is the preset word the profile's five tier rows make — `frugal`,
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
		return s.crewRung()
	case SeatInherited:
		// The crew answered, through an older row. Both halves are said,
		// because a receipt that said only `crew custom` would hide exactly the
		// substitution this rung exists to report.
		return s.crewRung() + ", inherited"
	}
	return "default"
}

// crewRung is the crew half of a rung: the preset the profile's rows make, or
// the bare word for a profile whose preset cannot be read.
func (s Seat) crewRung() string {
	if strings.TrimSpace(s.Crew) == "" {
		return "crew"
	}
	return "crew " + s.Crew
}

// Notice is the ONE line a person reads when this seat was not filled by a row
// they wrote, and it is empty for every other seat.
//
// IT IS SAID ONCE, WHERE THE SEATS ARE REPORTED, and never at the point of a
// call: this seat answers every request a run makes, and a line that arrived
// with all of them would be noise a person learns to read past — which leaves
// them exactly where the silence did. [Report] is how a door prints it, so the
// door does not have to remember.
//
// It is the register the surface already speaks in — an observation, a middle
// dot, a promise, lowercase, no full stop, nothing about machinery
// (internal/session's taskEscalationNote and checkpointCeilingNote). The
// observation is the true thing: the profile is older than the seat. The promise
// is what is running instead and what ends it.
func (s Seat) Notice() string {
	if s.Source != SeatInherited {
		return ""
	}
	return "your crew was set before the " + string(s.Role) + " seat existed · " +
		"it is running on your " + tierWords(s.From) + " model until you pick a crew again"
}

// Report is what a door prints: the seat, and the line that says a row was
// inherited when one was.
func (s Seat) Report() string { return withNotice(s.Line(), s.Notice()) }

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

// Notice is the inheritance line for whichever seat was filled by an older row,
// and empty when neither was. Both seats are asked and their lines joined,
// because the table decides which tiers have ancestors and this must not have to
// be edited when a second one does.
func (s Seats) Notice() string {
	lines := make([]string, 0, 2)
	for _, seat := range []Seat{s.Work, s.Plan} {
		if notice := seat.Notice(); notice != "" {
			lines = append(lines, notice)
		}
	}
	return strings.Join(lines, "\n")
}

// Report is what a door prints instead of [Line]: the models line, and beneath
// it the one line that says a seat was inherited. ONE PLACE, so a door added
// tomorrow cannot print the models and swallow the reason.
func (s Seats) Report() string { return withNotice(s.Line(), s.Notice()) }

// withNotice puts a notice under a line, and is the whole of the composition so
// the two Report methods cannot disagree about it.
func withNotice(line, notice string) string {
	if notice == "" {
		return line
	}
	return line + "\n" + notice
}

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
//  4. the crew again, through an OLDER ROW, for a profile written before this
//     seat existed — the worker row's ancestor is the small-work row it was
//     split out of ([tierLineage]), and the seat says it was inherited rather
//     than reporting the model as though the person had pinned it;
//  5. this build's default, which a profile that has said nothing about models
//     at all is the only thing left for.
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
// five shipped tier defaults are the balanced row, so TierModelAt would answer
// for a person who has never said anything, the bottom rung would become
// unreachable, and this build's headless default model would quietly change for
// everybody. `crew` here means somebody chose a crew. A row cleared on purpose
// reads empty, which is "follow the conversation" — a sentence a headless run
// has no conversation to answer with — so it falls through too.
//
// AND A KEY THAT WAS NEVER HELD IS NOT THE SAME AS A PROFILE THAT SAID NOTHING.
// The two were one case until the worker row landed (#302) and a profile that
// had pinned every row it knew about fell all the way to the default for the
// seat that does the work. So an unheld key asks the lineage before the bottom
// rung: the crew answers through the ancestor row when the person wrote one, on
// its own rung, and the run says so in one line ([Seat.Notice]).
//
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
		seat.Model, seat.Source = fallback, SeatDefault
		return seat
	}
	// The key was never held, which on a profile older than this seat means the
	// crew was chosen before the seat existed. It still answers, through the row
	// this row was split out of ([tierLineage]) — and the seat carries the fact
	// so the run can say it out loud.
	if value, from, ok := inheritedTier(profileDir, tier); ok {
		seat.Model, seat.Source, seat.From, seat.Crew = value, SeatInherited, from, CrewAt(profileDir)
		return seat
	}
	seat.Model, seat.Source = fallback, SeatDefault
	return seat
}
