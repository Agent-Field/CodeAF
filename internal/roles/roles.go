// Package roles is the model-role registry: one name for every auxiliary LLM
// call the surface makes, and one rule for which model answers it.
//
// An auxiliary call is any call the person did not type — the title a session
// names itself, the summary a compaction writes, the advisor's aside, a commit
// message. Each is a ROLE. Roles group under TIERS, and a tier is what the
// person actually configures: "low model = X, high model = Y", set once. New
// auxiliary calls register a role, inherit their tier's model, and need no
// settings of their own; the person never learns a new knob per feature.
//
// The package is resolution and registry only. It reads settings through a
// [Source] seam and imports nothing of the surface — no session, no config —
// so the rule can be tested without a profile directory on disk and the
// wiring wave can attach whichever store it likes.
package roles

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Role is one named auxiliary LLM call.
//
// The constants below are the two calls that exist today, but the type is a
// string and the registry is OPEN: a package that adds an auxiliary call
// declares its own role in its own file and calls [Register] from an init.
// A closed enum would mean every new advisor, namer or commit-message writer
// had to edit this file to exist, which puts an unrelated package's vocabulary
// in the registry's source and makes the registry a merge point for work that
// has nothing to do with routing.
type Role string

const (
	// RoleTitle names a session from its opening exchange.
	RoleTitle Role = "title"
	// RoleCompaction summarizes a transcript that outgrew its window.
	RoleCompaction Role = "compaction"
	// RoleConsolidate is the dreaming pass over what a session has remembered:
	// one call over fifty short lines whose whole instruction is "merge the
	// duplicates and drop what is superseded" (internal/session's
	// memory_consolidate.go, which owns the call and registers it). Low — the
	// archetypal cheap call, made while nobody is waiting, over a file the next
	// idle minute passes over again.
	RoleConsolidate Role = "consolidate"
	// RoleGuardian answers "is this specific tool call safe to run without
	// asking the person?" — a cheap binary classification, so it sits low.
	RoleGuardian Role = "guardian"
	// RoleVision reads images for a chat model that cannot see them natively;
	// it wants to be genuinely good at seeing, not cheap.
	RoleVision Role = "vision"
	// RoleImageGen generates images — a different modality entirely; the chat
	// model never substitutes.
	RoleImageGen Role = "imagegen"
	// RolePlanner is the ADAPTIVE RUN'S BRAIN (internal/orchestrate): the call
	// that reads the goal, the digests of what has finished and the fuel left,
	// and answers with an amendment to the plan. It is the ONLY call in a run
	// that sees the run whole, it is made once at the start and once per node
	// completion, and every node the rest of the run pays for is a node it cut.
	// A planner that cuts badly spends a whole tank on work nobody wanted, so
	// it sits high.
	RolePlanner Role = "planner"
	// RoleDesigner writes a harness page and reviews it before it is offered
	// (internal/session's harness_build.go). What it writes is SAVED and run
	// again by everyone who picks it afterwards, so a bad page is not one wrong
	// answer, it is a wrong answer with a name on the menu; high.
	RoleDesigner Role = "designer"
	// RoleWorker is a node: one small question, a handful of turns, a digest at
	// the end. Low, because the shape of an adaptive run is many small workers
	// under one careful planner — width is where the work happens, and it is
	// where a run's money actually goes.
	RoleWorker Role = "worker"
	// RoleSpeech turns words into sound, and RoleVideo turns them into film.
	// They complete the set of media pins the v3 use-time resolver reads as its
	// second rung (docs/MULTIMODAL.md Decision 5), beside RoleImageGen and
	// RoleVision, so an operator can pin every modality by the same mechanism
	// rather than three of four.
	//
	// THEY ARE PIN NAMES AND ARE DELIBERATELY NOT REGISTERED. Registration
	// binds a role to a TIER, and a tier is a class of text model: resolving
	// "speech" through the low tier would hand a text model to an endpoint that
	// returns audio, which is the wrong answer delivered confidently. A media
	// role's ladder is the resolver's — slot, pin, catalog, curated name — and
	// every rung of it is capability-checked, which the tier ladder cannot be.
	// [Pinned] reads a key and needs no registration, which is exactly the
	// slice of this package a media resolver wants.
	RoleSpeech Role = "speech"
	RoleVideo  Role = "video"
	// RoleAuditor is the VERIFIED FRONTIER: the read-only judge that decides
	// whether a piece of finished-looking work is actually finished, against
	// hard evidence it gathered itself. It sits HIGH and it is the one role
	// where the tier is not an economy question at all — a wrong verdict
	// either lands broken work as done or throws good work away, and both are
	// mistakes nobody downstream can see to correct. Registered from
	// internal/session/task_audit.go, which owns the call.
	RoleAuditor Role = "auditor"
	// RoleReflex is the per-turn pair: the pre-turn router that reads the
	// message you just typed and answers which remembered lines belong in this
	// turn, and the post-turn extractor that reads the exchange and answers
	// whether anything in it is worth keeping. Both answer in a few words of
	// JSON, both run every single turn, and neither is allowed to think — which
	// is why they are their own tier rather than the low one. Registered here
	// with the built-ins because the tier IS the role's whole design, and a
	// reflex role assigned anywhere else would be the same mistake as putting
	// the reflex model on the high tier.
	RoleReflex Role = "reflex"
	// RoleRouter is the sidecar judge that reviews a tool-less answer and asks
	// whether it should have been work. It sits LOW because it SCREENS: it is
	// asked after every substantial turn that answered in words alone, which is
	// volume, and volume belongs on the cheap model.
	//
	// ITS OLD ECONOMICS ARE GONE AND THE LINE THAT STATED THEM WAS STALE. "A
	// wrong no costs an offer card that was never shown, never money" was true
	// while a yes raised a card somebody had to press; a yes now STARTS the work
	// (internal/session/route_judge.go), so a wrong yes spends a whole task's
	// money. What holds that shut is NOT a dearer model here — moving the screen
	// off the cheap tier would pay for thinking on every turn to correct the rare
	// one — it is [RoleRouterConfirm], asked once on the yes. A wrong no still
	// costs nothing but work that was never started.
	RoleRouter Role = "router"
	// RoleRouterConfirm is the SECOND question, asked only where RoleRouter has
	// already answered yes: the same brief, the same one-object contract, a
	// fresh context, on the tier that thinks. Only both-yes starts anything, and
	// the confirm's no is silence — no note, no retry.
	//
	// IT SITS ON THE MASTERMIND TIER AND ITS EXPECTED COST IS STILL NEARLY ZERO,
	// which is the whole shape of a cascade. It is never asked about the turns
	// the cheap screen already threw out — the trivial ones, the ones that called
	// tools, the plain nos — so what it costs is one mastermind call per yes, and
	// a yes is rare. What it prevents each time is a task's worth of spend, in a
	// worktree, on work nobody asked for. Registered from
	// internal/session/route_judge.go, which owns the call.
	RoleRouterConfirm Role = "routerconfirm"
	// RoleMarkReader reads ONE MARK of an answer that is still running: the
	// transcript the turn has built so far, and one question asking it to sketch
	// what is left as parts and arrows (internal/session/checkpoint.go, which owns
	// the call). The harness parses the shape it draws and hands the turn over
	// where the shape has independent parts in it.
	//
	// IT IS ITS OWN ROLE BECAUSE IT ASKS ITS OWN QUESTION. RoleRouterConfirm is
	// the second reader of a work-or-words judgement about a REQUEST nobody has
	// worked on yet; this reads a turn's own findings and answers what remains.
	// Sharing a name would mean one pin could only ever point both calls at one
	// model, and the two are billed on completely different rhythms.
	//
	// IT SITS ON THE MASTERMIND TIER BECAUSE THE CHEAP ANSWER WAS MEASURED AND IT
	// WAS NOT AN ANSWER. Asked mid-turn, the running chat model emitted a tool call
	// instead of answering between 17% and 53% of the time, depending on the
	// phrasing; the mastermind left 0% to 7% unanswered on the same transcripts
	// (bench/oneroad/replay/RESULTS.md). A reader that goes silent under tool
	// momentum is silent on exactly the turns this is for. What it costs is bounded
	// hard: at most three calls, and only on a turn that has already run ten rounds
	// of tools, which most turns never do.
	RoleMarkReader Role = "markreader"
	// RoleHandoff WRITES THE BRIEF a handed-over turn gives the worker that takes
	// it: the person's ask, the state card, an account of what the turn did and
	// what came back, and the draft the running model wrote — in, and one
	// instruction somebody who saw none of it can work from, out
	// (internal/session/checkpoint.go, which owns the call).
	//
	// IT IS ITS OWN ROLE BECAUSE THE DRAFT AND THE DOCUMENT ARE DIFFERENT JOBS.
	// The model that spent the turn is the only one holding the findings, so it
	// still drafts; it is also, by then, a tired model at the end of forty rounds,
	// and asking it to be its own editor was measured producing 5,882 characters
	// in which 39 of 85 clauses were distinct and 62% was one six-sentence loop —
	// which a cold worker was then started on as its whole world.
	//
	// IT SITS ON THE MASTERMIND TIER FOR THE MARK READER'S REASON, arrived at from
	// the other side: this document is the ENTIRE context of everything that
	// happens after the handover — the worker's instruction, what the division
	// reviewer reads, what the checker is eventually held against — so a cheap
	// answer here is not a cheap answer, it is a whole task's spend on the wrong
	// work. What it costs is bounded hard: at most two calls, and only on a turn
	// that is being handed over at all, which most turns never are.
	RoleHandoff Role = "handoff"
	// RoleTaskName is the two or three words a piece of work is CALLED on the
	// rail, the home card and the task list — made from the node's own gloss and
	// brief when whoever started it left a raw sentence there instead of a name.
	// Low, for the title's reason: a wrong name costs a glance at a column and
	// nothing downstream is decided from it. Registered from
	// internal/session/taskname.go, which owns the call.
	RoleTaskName Role = "taskname"
	// RoleJobName is the three or four words a background command is CALLED on
	// its row — made from the command itself when the registry left a raw
	// invocation there instead of a label. Low, for the title's reason: a wrong
	// name costs a glance at a column and nothing downstream is decided from it.
	// Registered from internal/session/jobname.go, which owns the call.
	RoleJobName Role = "jobname"
	// RoleCaption names the discrete step over a live tool batch — the checklist
	// item a person reads, never the model's thinking. LOW, for the title's
	// reason — a wrong caption costs a glance, the rows beneath it are the truth,
	// and nothing downstream is decided from it. Registered from
	// internal/session/caption.go, which owns the call.
	RoleCaption Role = "caption"
	// RoleShaper turns the words somebody typed after /task into the brief the
	// worker is actually handed: it reads one request and writes the paragraphs
	// and the done-condition around it. It sits HIGH for the auditor's reason
	// rather than the title's — the brief is the worker's whole world, and a
	// vague one is not a glance wasted but a whole task's spend on work nobody
	// wanted. Registered from internal/session/task_shape.go, which owns the
	// call.
	RoleShaper Role = "shaper"

	// RoleIntake fills a subharness's input form from what the conversation has
	// already said (docs/SUBHARNESS-PRD.md §4: infer, then confirm — never
	// interrogate). It sits LOW for the title's reason rather than the shaper's:
	// its answer lands on a card the person is looking at and about to confirm,
	// so a wrong guess costs one correction rather than a whole run's spend, and
	// the alternative to a cheap call here is not a better call but an empty form
	// somebody fills in by hand. Registered from
	// internal/session/subharness_intake.go, which owns the call.
	RoleIntake Role = "intake"

	// RoleDivision reviews a DIVISION as a plan. A worker halfway through its
	// own work has named the parts it wants to hand out, and this is the one
	// call that reads them TOGETHER — the evidence, the parent's own brief, and
	// every part's title, brief and done-condition — and answers with those
	// parts approved, amended, merged, or refused as not a division at all.
	//
	// IT IS ON THE MASTERMIND TIER FOR THAT TIER'S OWN STATED REASON: one answer
	// shapes all the other calls. A part's brief is that part's WHOLE WORLD — it
	// never sees the conversation and cannot ask anybody anything — and the
	// briefs arrive written by whichever model the parent task happens to be
	// running on, which on a cheap crew is the cheap one. Every turn every part
	// ever takes is downstream of those paragraphs. Registered from
	// internal/session/task_divide.go, which owns the call.
	RoleDivision Role = "division"
	// RoleCareful is the model a part graded `careful` runs on. It is not an
	// auxiliary call at all — it is a whole worker's model — and it is a role so
	// that the answer comes out of this registry rather than out of a model id
	// written into a source file somewhere.
	//
	// IT SITS HIGH AND NOT ON THE MASTERMIND TIER, and the difference is the
	// call rhythm the tiers are cut along. A mastermind call is one answer that
	// decides what the others do; a careful part is MANY turns of ordinary work,
	// done by a model that can be trusted with something subtle. It is
	// [RoleWorker]'s twin: the cheap tier for parts whose failure is "not done
	// yet", this one for parts whose failure is quiet wrongness. Registered from
	// internal/session/task_divide.go, which owns the field that reaches it.
	RoleCareful Role = "careful"
)

// Tier is a class of model the person configures once. Roles are open; tiers
// are deliberately not. Five settings is a decision someone can hold in their
// head — a tier per feature is the per-feature knob this package exists to
// avoid — and each one past the first two was added only because a CALL RHYTHM
// differs, never because a feature wanted a knob: the reflex tier because a call
// made twice every turn is a different bill from a call made once a session, the
// mastermind tier because a call whose answer decides what every other call does
// is a different bill again, the worker tier because the seat that does the
// work is the seat that pays the bill, and a cost dial that cannot reach it is
// not a cost dial.
type Tier string

const (
	// TierReflex is the cheapest of all: a model small enough to read EVERY
	// TURN. The two calls on it — the router that decides which memories a turn
	// needs, the extractor that decides whether the turn is worth remembering —
	// run whether or not anybody asked, twice per exchange, for the whole life
	// of a conversation. That rhythm is the tier: a model here is chosen for
	// costing near nothing per call rather than for being good at anything, and
	// no role that has to REASON belongs on it.
	TierReflex Tier = "reflex"
	// TierLow is the cheap, fast model for the SMALL calls: a name, a digest,
	// the guardian's yes-or-no, the intake form.
	TierLow Tier = "low"
	// TierWorker is the model that DOES THE WORK: the worker of every task a
	// conversation hands off, the ordinary parts that worker divides its work
	// into, and every node of an adaptive run. It is the seat that pays most of
	// a task's bill — twenty or thirty tool turns against a handful of one-shot
	// calls around them — and until it existed no tier governed it: a chat task
	// rode whatever model the person happened to be talking to, and the crew
	// dial moved everything about a task except its cost. Its rhythm is the
	// mastermind's opposite — many turns of ordinary work — so the model here is
	// chosen for holding a thread through a long agentic run at a price that can
	// be paid thirty times over.
	TierWorker Tier = "worker"
	// TierHigh is the capable, expensive one.
	TierHigh Tier = "high"
	// TierMastermind is the one tier that is not an economy at all. What every
	// role on it has in common is that ONE ANSWER SHAPES ALL THE OTHER CALLS:
	// the planner that decides what an adaptive run does next, the designer that
	// writes a harness page everybody afterwards runs, the confirm standing
	// between a cheap judge's yes and a task that starts itself, and the review
	// that reads a division's parts before a single one of them is minted. A
	// planner that cuts badly spends a whole tank on work nobody wanted; a
	// designer that writes badly puts a wrong answer on the menu with a name on
	// it; a division reviewed badly hands four workers four briefs that nobody
	// inside them can correct. The first two were on the high tier, beside the
	// compaction summary and the
	// auditor, which made a person choosing "the capable model" choose one
	// figure for two very different bills: the careful calls are many and short,
	// the mastermind's are few and worth thinking about. Separating them is what
	// lets the shipped crew spend on thinking exactly where thinking pays.
	TierMastermind Tier = "mastermind"
)

// Tiers lists every tier, cheapest first, for a settings surface to render.
var Tiers = []Tier{TierReflex, TierLow, TierWorker, TierHigh, TierMastermind}

// known reports whether a tier is one this package has. It reads [Tiers] rather
// than a switch, so a fifth tier is one line in that list and not a second list
// somebody has to remember to widen — the mistake that would otherwise show up
// as a register-time panic on a tier the settings surface is already drawing.
func known(tier Tier) bool {
	for _, candidate := range Tiers {
		if candidate == tier {
			return true
		}
	}
	return false
}

// DefaultAssignment is the tier each built-in role starts on.
//
// Titles are disposable prose: a wrong one costs a glance and is rewritten by
// the next session, so it goes to the cheap model. A compaction summary is the
// session's memory — everything before the cut is gone and only the summary
// survives it — so it goes to the capable one. The asymmetry is about what a
// bad answer destroys, not about how hard the task reads.
//
// THE THREE RUN ROLES ARE ASSIGNED HERE rather than from the files that make
// their calls, which is the arrangement guardian, vision and the auditor keep.
// The reason is that ONE RUN IS ALL THREE — a planner, the workers it cuts, and
// the designer of a page a node may run — and the decision is not any one of
// their tiers but the BALANCE between them: one careful call that decides what
// happens, many cheap ones that do it. Split across three files, that balance
// is three unrelated lines nobody reads together.
var DefaultAssignment = map[Role]Tier{
	RoleTitle:      TierLow,
	RoleCaption:    TierLow,
	RoleCompaction: TierHigh,
	RolePlanner:    TierMastermind,
	RoleDesigner:   TierMastermind,
	RoleWorker:     TierWorker,
	RoleRouter:     TierLow,
	RoleReflex:     TierReflex,
}

// ErrUnknownRole is returned by [Resolve] for a role that was never
// registered. It is a programming error rather than a misconfiguration: a
// caller asking for a role it did not declare has a typo or a missing init.
var ErrUnknownRole = errors.New("roles: unknown role")

// ErrNoModel is returned by [Resolve] when the ladder runs out — no pin, no
// tier model, and an empty session default. There is no model to call and a
// caller must not invent one, so this is an error rather than a blank string.
var ErrNoModel = errors.New("roles: no model")

// Source reads one settings key. It reports false for a key that is unset,
// which is how the resolution ladder knows to fall through to the next rung.
//
// It is a function rather than an interface so the config registry can be
// mapped onto it in the wiring wave with a closure and no adapter type, and so
// a test can be a map literal. A nil Source is legal and reads as "nothing is
// set" — a fresh install before any settings file exists.
type Source func(key string) (string, bool)

// Key prefixes for the two settings a person can write. They live here, beside
// the reader, so the later settings writer cannot drift from the reader: both
// go through [PinKey] and [TierKey].
const (
	pinPrefix  = "roles."
	tierPrefix = "tiers."
)

// PinKey is the settings key that pins one role to one model outright.
func PinKey(role Role) string { return pinPrefix + string(role) }

// TierKey is the settings key holding a tier's model.
func TierKey(tier Tier) string { return tierPrefix + string(tier) }

// roleDescriptions is the plain line under each built-in role's name.
//
// IT COVERS ROLES THIS PACKAGE DOES NOT REGISTER — guardian, auditor, vision
// are declared from internal/session's own inits, which say a role and a tier
// and nothing else. A description is what a PERSON reads on a settings row, and
// the settings surface is nowhere near those files; asking each owner to pass a
// sentence it never had would have left the three most-asked-about roles blank.
// A package that does pass one to [Register] overwrites its entry here, which is
// the same last-one-wins rule the tier assignment keeps.
var roleDescriptions = map[Role]string{
	RolePlanner:    "the plan that steers an adaptive run",
	RoleDesigner:   "writes and reviews a harness page",
	RoleAuditor:    "whether finished-looking work is actually finished",
	RoleCompaction: "the summary that survives a compaction",
	RoleWorker:     "one node of an adaptive run",
	RoleTitle:      "the name a session gives itself",
	RoleIntake:     "filling in a program's form from what was already said",
	RoleGuardian:   "is this one tool call plainly safe",
	RoleRouter:     "whether a turn should have been work",
	// The cascade's second half, and the two calls a division makes.
	RoleRouterConfirm: "a second look before work starts itself",
	RoleMarkReader:    "what is left of a long answer, and whether it has parts",
	RoleHandoff:       "the instruction a handed-over turn gives whoever finishes it",
	RoleDivision:      "the parts a worker hands its own work out in",
	RoleCareful:       "a part of a task that needs judgement",
	RoleReflex:        "reads every turn for memory — routing and keeping",
	RoleVision:        "reads images for a model that cannot see them",
	RoleShaper:        "the brief a task you started yourself is given",
	RoleTaskName:      "the two or three words a task is called",
	RoleJobName:       "the three or four words a background job is called",
	RoleCaption:       "the discrete step title over a live tool batch",
}

var (
	registryMu sync.RWMutex
	registry   = map[Role]Tier{}
	// descriptions is seeded from [roleDescriptions] and then written by
	// [Register]. It is separate from the registry map because a description
	// belongs to a role whether or not this build registered it: the guardian's
	// line is written here and its tier is written in internal/session.
	descriptions = map[Role]string{}
)

func init() {
	for role, description := range roleDescriptions {
		descriptions[role] = description
	}
	for role, tier := range DefaultAssignment {
		Register(role, tier)
	}
}

// Register declares a role and the tier it resolves under by default. It is
// meant to be called from a package's init, which is why it panics rather than
// returning an error: a role that failed to register would not fail loudly at
// registration but silently at the first auxiliary call, in whichever model
// answered it by accident.
//
// A repeat registration overwrites, last one wins. That is what lets a surface
// retune a built-in — moving titles to the high tier for a run, say — without
// editing this file, which is the same reason the role registry is open.
//
// The optional description is the role IN A PERSON'S WORDS, printed under its
// name in the settings list. It is variadic rather than a second function or a
// wider signature because every existing call site is an init in another package
// that says only "this role, this tier", and a role whose owner has not written
// a line yet is better than a build that will not compile. The built-ins fill
// theirs from [roleDescriptions] below; a package registering its own role
// passes one here.
func Register(role Role, tier Tier, description ...string) {
	if strings.TrimSpace(string(role)) == "" {
		panic("roles: register with empty role")
	}
	if !known(tier) {
		panic(fmt.Sprintf("roles: register %q with unknown tier %q", role, tier))
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[role] = tier
	if len(description) > 0 && strings.TrimSpace(description[0]) != "" {
		descriptions[role] = strings.TrimSpace(description[0])
	}
}

// Describe is the role in a sentence a person reads — "the plan that steers an
// adaptive run", not "planner". A role nobody wrote a line for answers the
// empty string, which a surface renders as nothing (THE EMPTINESS LAW) rather
// than as the role's own name said twice.
func Describe(role Role) string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return descriptions[role]
}

// Registered lists every role, sorted. Sorted because the registry is a map
// filled from init functions whose order is not defined, and a settings screen
// that lists roles in a different order on every launch is a screen nobody
// trusts.
func Registered() []Role {
	registryMu.RLock()
	roles := make([]Role, 0, len(registry))
	for role := range registry {
		roles = append(roles, role)
	}
	registryMu.RUnlock()
	sort.Slice(roles, func(i, j int) bool { return roles[i] < roles[j] })
	return roles
}

// TierOf reports the tier a role resolves under, and false if the role was
// never registered.
func TierOf(role Role) (Tier, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	tier, ok := registry[role]
	return tier, ok
}

// Resolve answers which model a role's call should use.
//
// The ladder is exact, most specific first:
//
//  1. the role's own pin, settings key "roles.<role>" — one call, one model,
//     set deliberately;
//  2. the model on the role's tier, key "tiers.<tier>" — the setting a person
//     actually maintains;
//  3. sessionDefault — the model the session is already talking to.
//
// The session model is the FLOOR, not a last resort that fails. A fresh
// install has no settings file, no tier models and no pins, and every
// auxiliary call still routes to the one model the person already has
// configured and already pays for. Tiers are an optimisation someone opts
// into once they care about the cost of titles, not a prerequisite for the
// surface to work.
//
// An unset key and a key set to blank are the same thing here: clearing a
// setting in a UI usually writes an empty string, and falling through to the
// next rung is what "cleared" plainly means.
func Resolve(src Source, role Role, sessionDefault string) (string, error) {
	call, err := ResolveCall(src, role, sessionDefault)
	return call.Model, err
}

// ResolveCall is [Resolve] with the effort kept: the same ladder, answered as
// the two halves a request actually needs.
//
// It exists because a tier value may carry a level — `moonshotai/kimi-k3:low` —
// and the two halves travel to different places. The id goes in the request's
// model field; the level is a separate request option, and a caller that pasted
// the whole string into the model field would be asking the provider for a model
// whose name has a colon in it. So the split happens ONCE, here, on the way out
// of the ladder, and [Resolve] is this function with the level dropped — which
// is the correct behaviour for every caller that has no way to send one.
func ResolveCall(src Source, role Role, sessionDefault string) (Call, error) {
	rungs, err := Ladder(src, role, sessionDefault)
	if err != nil {
		return Call{}, err
	}
	return rungs[0], nil
}

// Ladder is the WHOLE of [ResolveCall]'s ladder — every rung that resolves,
// most specific first — rather than only the rung that wins.
//
// It exists because the ladder answers two questions, not one. "Which model
// answers this call" is the first rung and is what almost every caller wants.
// "AND WHAT IF THAT MODEL CANNOT ANSWER AT ALL" is the rest of the list, and it
// is a real question: a role pinned to a small model that is down, or a tier
// pointing at a slug an account has lost access to, used to fail the errand
// outright while the model the person is talking to sat there able to do it.
//
// Repeats are removed, so a rung naming the model a higher rung already named
// is not a rung — falling through to the same id is one more identical request
// and a second identical failure. That is also why this is the fall-through and
// not a list of every configured model: what follows a rung is what the person
// configured to follow it, not a guess.
//
// The first rung is exactly what [ResolveCall] used to compute, so a caller that
// only wants that is byte-for-byte where it was. A non-empty slice or an error:
// never both, never neither.
func Ladder(src Source, role Role, sessionDefault string) ([]Call, error) {
	tier, ok := TierOf(role)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownRole, role)
	}
	var rungs []Call
	seen := map[string]bool{}
	add := func(candidate Call) {
		model := strings.TrimSpace(candidate.Model)
		if model == "" || seen[model] {
			return
		}
		seen[model] = true
		rungs = append(rungs, candidate)
	}
	if value, ok := read(src, PinKey(role)); ok {
		add(call(value))
	}
	if value, ok := read(src, TierKey(tier)); ok {
		add(call(value))
	}
	// THE SESSION MODEL IS NOT SPLIT. It is the id a running conversation is on,
	// and whatever effort that conversation was dialled to belongs to the person
	// who typed into it, not to an errand this package is routing.
	add(Call{Model: strings.TrimSpace(sessionDefault)})
	if len(rungs) == 0 {
		return nil, fmt.Errorf("%w for role %q", ErrNoModel, role)
	}
	return rungs, nil
}

// ── how long one call on a tier is worth waiting for ────────────────────────

// Patience is the OUTER BOUND on one call of a role's tier, and it exists
// because the adapter's own bound cannot be one: a completion is bounded by a
// figure scaled off its output cap, between five and fifteen minutes
// (internal/provider's adaptiveCompletionTimeout), which is the right answer for
// a person's turn and an absurd one for the eight words a session names itself.
// A title that has not arrived in two minutes is not a title that is coming.
//
// IT IS DERIVED FROM THE TIER AND NEVER FROM THE ROLE. A tier is already the
// statement of what a class of call is worth — cheap and constant, or few and
// worth thinking about — and a table of per-role seconds would be a second
// place saying the same thing, drifting the first time somebody adds a role.
//
// It is an OUTER bound, not a budget. A caller that knows its own call is
// tighter still says so and wins, because two deadlines on one context leave the
// nearer one in force: the guardian's ten seconds and the task namer's twenty
// are facts about those calls that no tier can know.
func Patience(tier Tier) time.Duration {
	switch tier {
	case TierReflex:
		// Twice per exchange, forever, in front of a person who is waiting for
		// their own turn behind it. A reflex call that takes a minute has
		// already cost more than the answer is worth.
		return 45 * time.Second
	case TierHigh:
		// Long enough for a compaction summary over a full window — the one
		// call on this tier that legitimately reads a great deal before it
		// writes anything.
		return 5 * time.Minute
	case TierMastermind:
		// A planner reading a whole run, or a designer writing a page everybody
		// afterwards runs. These are the calls worth waiting for, and this is
		// still a bound.
		return 10 * time.Minute
	default:
		return 2 * time.Minute
	}
}

// PatienceFor is [Patience] for a role, and the ordinary way to ask. An
// unregistered role gets the cheap tier's answer rather than no bound at all:
// the caller is already about to fail on [Ladder]'s ErrUnknownRole, and a
// missing bound is the one outcome worse than a short one.
func PatienceFor(role Role) time.Duration {
	tier, ok := TierOf(role)
	if !ok {
		return Patience(TierLow)
	}
	return Patience(tier)
}

// Pinned reports a role's explicit pin — rung 1 of the ladder on its own, for
// a settings screen that wants to show whether a role is pinned rather than
// which model it ends up on.
func Pinned(src Source, role Role) (string, bool) {
	return read(src, PinKey(role))
}

// TierModel reports the model configured for a tier — rung 2 alone.
func TierModel(src Source, tier Tier) (string, bool) {
	return read(src, TierKey(tier))
}

// There are no writers here on purpose. Pinning a role and assigning a tier a
// model are settings WRITES, and this package holds a read seam; giving it a
// half-built writer would mean two places that know how a pin is stored. The
// settings writer wave owns them, and writes through [PinKey] and [TierKey] so
// the two halves cannot name a key differently.

// ── effort, carried on a tier value ────────────────────────────────────────
//
// A tier value may name a level as well as a model: `moonshotai/kimi-k3:low` is
// "the mastermind is kimi-k3, and ask it to think a little". It is one string
// rather than a fifth settings row per tier because the level is not a separate
// decision — nobody sets an effort for a tier without setting the model, and a
// row that could hold a level for a model nobody chose would be a knob wired to
// a blank. The notation is the SURFACE'S OWN, already read by the model picker's
// ctrl+t and printed after an id on the picker row and the /status model line.

// Efforts lists the levels a tier value may carry, cheapest first.
//
// They are STRINGS HERE and not internal/provider's Effort, for the reason the
// settings keys are strings: this package imports nothing of the surface, and a
// level is a word somebody typed into a config file. The caller that puts the
// word on a request is the one that owns the adapter's type.
//
// "off" is deliberately not one of them. It is a different request — it asks a
// provider to suppress the thinking pass outright, which some endpoints refuse
// — and a tier value is a thing a person writes once and forgets, which is the
// wrong place for a knob that can fail on the wire.
var Efforts = []string{"low", "medium", "high"}

// Call is one resolved auxiliary call: which model answers it, and how hard it
// was asked to think. The effort is empty for a value that named none, which is
// every value that has ever been written until somebody writes a suffix — and
// empty means SEND NOTHING, leaving the request byte-for-byte what it was.
type Call struct {
	Model  string
	Effort string
}

// String is the call as the surface spells it — the id, and the level after a
// colon when there is one. It is the notation the picker row and /status already
// use, so a settings list can print a resolved call without inventing a second
// way to say the same thing.
func (c Call) String() string {
	if c.Model == "" || c.Effort == "" {
		return c.Model
	}
	return c.Model + ":" + c.Effort
}

// SplitEffort separates a tier value into the model id and the level it carries.
//
// A value with no colon, and a value whose text after the last colon is not one
// of [Efforts], is A MODEL ID AND NOTHING ELSE. That is not leniency: model ids
// carry their own suffixes (`…/model:free`, `…:nitro`, `…:thinking`), and a
// function that treated every colon as an effort would quietly break every one
// of them. Only the three words this package knows are levels are read as levels.
//
// So this function never refuses anything, and it is not the validator. A person
// who wrote `:of` for `:off` has written a model id no provider serves, and the
// only place that can say so in a sentence is the settings row they wrote it in
// (internal/config's ValidateTierValue). This is the reader; that is the gate.
func SplitEffort(value string) (string, string) {
	value = strings.TrimSpace(value)
	at := strings.LastIndex(value, ":")
	if at <= 0 {
		return value, ""
	}
	suffix := strings.ToLower(strings.TrimSpace(value[at+1:]))
	if !ValidEffort(suffix) {
		return value, ""
	}
	return strings.TrimSpace(value[:at]), suffix
}

// ValidEffort reports whether a word is one of [Efforts].
func ValidEffort(word string) bool {
	word = strings.ToLower(strings.TrimSpace(word))
	for _, effort := range Efforts {
		if effort == word {
			return true
		}
	}
	return false
}

// call is [SplitEffort] as a [Call].
func call(value string) Call {
	model, effort := SplitEffort(value)
	return Call{Model: model, Effort: effort}
}

// read is one rung of the ladder: a lookup that treats a nil source, a missing
// key and a blank value alike.
func read(src Source, key string) (string, bool) {
	if src == nil {
		return "", false
	}
	value, ok := src(key)
	if !ok {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}
