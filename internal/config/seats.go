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

// tierSeatRole is what one tier's seat is CALLED in the line a person reads
// about it. The two seats a run sits somebody in have names of their own
// ([SeatWork], [SeatPlan]) and both surfaces have to spell them the same way: a
// conversation whose work seat was inherited must read the sentence `aforge do`
// prints, not a second wording for one fact. Every other row answers with its
// own name on the settings sheet, so a tier that joins the lineage tomorrow has
// a sentence before anybody writes one.
func tierSeatRole(tier string) SeatRole {
	switch tier {
	case ModelTierWorker:
		return SeatWork
	case ModelTierMastermind:
		return SeatPlan
	}
	return SeatRole(tierWords(tier))
}

// crewRow is THE PROFILE HALF OF THE LADDER, and the one place that tells a row
// somebody WROTE from a row they CLEARED from a key that was NEVER HELD.
//
// The distinction is the whole mechanism and it is decided here so that the two
// ladders above it cannot decide it differently: [resolveSeat], which climbs a
// flag and a variable first for a run, and [TierSeatAt], which is what a
// conversation and the settings sheet read. A second copy of these three cases
// is how a person's crew comes to mean one thing headless and another in chat,
// which is the defect this file was written for one surface at a time (#166,
// #302, #312).
//
// source is empty when the profile has nothing this ladder can use, and
// `cleared` tells the two ways of having nothing apart: a row emptied ON PURPOSE
// says "follow the conversation", which a conversation can do and a headless run
// cannot, so each caller's own bottom rung answers for it.
func crewRow(profileDir, tier string) (model, from string, source SeatSource, cleared bool) {
	if written, held := persistedString(profileDir, tierKeyFor(tier)); held {
		if value := strings.TrimSpace(written); value != "" {
			return value, "", SeatCrew, false
		}
		return "", "", "", true
	}
	// The key was never held, which on a profile older than this seat means the
	// crew was chosen before the seat existed. It still answers, through the row
	// this row was split out of ([tierLineage]).
	if value, ancestor, ok := inheritedTier(profileDir, tier); ok {
		return value, ancestor, SeatInherited, false
	}
	return "", "", "", false
}

// TierSeatAt is ONE TIER ROW READ AS A SEAT, for the surfaces that seat roles
// rather than run a door: the conversation's role map (cmd/aforge's v3Crew), the
// five rows of the settings sheet, and the crew word derived from them.
//
// IT IS [resolveSeat] WITHOUT THE TWO RUNGS THAT BELONG TO AN INVOCATION. A flag
// is something a command line said and a conversation has no command line for
// its crew; AFORGE_MODEL names the model a person TALKS TO ([Load] folds it into
// Config.Model), and a variable that also filled the work seat of every task
// handed off in that conversation would be one word quietly moving two dials.
// What is left is the profile — which is where a conversation's seats have
// always come from.
//
// Where it differs from a run is the BOTTOM, and only there:
//
//   - a row the person WROTE is the crew answering;
//   - a row they CLEARED reads empty, and stays empty, because on this surface
//     that is an answer — "follow the conversation" — and refusing it would make
//     a default into a rule ([TierModelAt] argues it at length);
//   - a key NEVER HELD asks the lineage before the bottom rung, so a profile
//     older than the worker seat hands a task the same model `aforge do` hands
//     it (#302 headless, #312 in the conversation), and the seat carries the fact
//     so a surface can say it once ([Seat.Notice]);
//   - anything else is this build's own choice for that class of work.
//
// THE PRESET WORD IS DELIBERATELY NOT READ HERE. [Seat.Crew] stays empty:
// [CrewAt] derives the preset from all five rows THROUGH this function, so a
// seat that filled it in would be the ladder asking the summary that is computed
// from the ladder — five extra file reads per row, and a cycle. A surface that
// wants both facts asks for both.
func TierSeatAt(profileDir, tier string) Seat {
	model, from, source, cleared := crewRow(profileDir, tier)
	if source == "" {
		source = SeatDefault
		if !cleared {
			model = defaultTierModel(tier)
		}
	}
	return Seat{Role: tierSeatRole(tier), Model: model, Source: source, From: from}
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
//
// AND THE PROMISE NAMES THE DOOR, because it used to end in one nobody could
// find. `until you pick a crew again` is a remedy with no address on it, and
// this line is printed on FOUR HEADLESS DOORS — `aforge do`, `aforge plan run`,
// `aforge exec` and `aforge run` — where there is no way at all to pick a crew:
// [CrewPreset] is written by `/crew` and by the settings sheet's Providers row,
// both of which are the conversation, and no flag and no terminal verb sets one.
// So a person reading this in a terminal was told to do something, given no way
// to do it, and left to discover on their own that the answer was a different
// surface. Cause plus what to do is the law on both surfaces; a cause plus a
// dead end is the defect.
//
// ONE FORM, TRUE FROM BOTH PLACES IT IS PRINTED. Naming `/crew` reads as the
// next keystroke in the conversation and as a destination from the terminal, and
// it is the same sentence in both — which is what keeps somebody who has seen
// one surface recognising the other. `seat` STAYS: it is this product's own
// noun for a row of the crew, taught under that name on the manual's own page
// and spoken by the settings sheet and the model picker, and the vocabulary law
// is about MACHINERY — the program's words for its own process — not about a
// domain noun the product teaches.
//
// AND THE PROMISE ATTRIBUTES THE MODEL RATHER THAN DESCRIBING IT. This read
// `it is running on your small work model`, printed directly under a models
// line naming `deepseek/deepseek-v4-pro` — so two consecutive lines called one
// model by its name and then called it small, and the developer who met them
// could not tell which model the lane was actually on. The two lines were never
// in disagreement about the FACT: whenever the source is [SeatInherited] the
// model on the line above IS the inherited one, always, because that is what
// inheriting means. What differed was the grammar. `small work` is the NAME OF
// A SEAT on the settings sheet, one of the five this product seats, and putting
// it in front of `model` turns a seat's name into an adjective about the model
// it holds.
//
// So the notice never characterises the model a second time — it says which
// SEAT lent it. `your small work seat's model` cannot be read as a claim about
// deepseek-v4-pro, and it answers the question the person actually has: this
// seat has no row of its own, so it is borrowing that one's. A description was
// always going to contradict the line above it, since the two are one model.
func (s Seat) Notice() string {
	if s.Source != SeatInherited {
		return ""
	}
	return "your crew was set before the " + string(s.Role) + " seat existed · " +
		"it is running on your " + tierWords(s.From) + " seat's model until you pick a crew with " +
		CrewCommand + " in the conversation"
}

// CrewCommand is the one door that ends the inheritance [Notice] describes. It
// is a constant so the sentence and the surface that answers to it cannot drift
// apart — the notice is printed by four commands that cannot reach it, and a
// remedy naming a door that has been renamed is worse than one naming none.
const CrewCommand = "/crew"

// FromWords is the row this seat's model was inherited from, in the words the
// settings sheet calls that row by — empty unless the source is
// [SeatInherited]. It is exported for the surface that has its own sentence to
// build about the same fact ([Notice] is the sentence; this is the noun), so two
// surfaces cannot invent two names for one row.
func (s Seat) FromWords() string {
	if s.Source != SeatInherited {
		return ""
	}
	return tierWords(s.From)
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
// UNTOUCHED IS NOT THE CREW ANSWERING. A tier is read through [crewRow] rather
// than [TierModelAt] so that a profile which has never held the key falls
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
	// AND THE PROFILE IS READ THROUGH THE ROW A CONVERSATION READS IT THROUGH
	// ([crewRow]), so the three cases a tier key can be in are decided once for
	// both surfaces. Nothing the profile can say fills this seat with the
	// fallback except silence: a row cleared on purpose says "follow the
	// conversation", which a headless run has no conversation to answer with,
	// and it lands on the same bottom rung a profile that said nothing does.
	model, from, source, _ := crewRow(profileDir, tier)
	if source == "" {
		seat.Model, seat.Source = fallback, SeatDefault
		return seat
	}
	seat.Model, seat.Source, seat.From, seat.Crew = model, source, from, CrewAt(profileDir)
	return seat
}
