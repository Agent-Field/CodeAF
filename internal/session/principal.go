package session

// THE PRINCIPAL: WHO THIS SESSION IS WORKING FOR.
//
// Every road out of a running turn eventually reaches the same sentence — "ask
// them what to do next" — and until this file there was no `them`. The
// harness said the words into whatever was there: a terminal with a person in
// front of it, or, on a run started with `--yolo` and left overnight, nobody at
// all. The two look identical from inside the engine, and that is exactly the
// bug. A note that reads "offer them a follow-up in their own words before
// anything else is spent" (task_run.go's [taskNote]) is a correct sentence to a
// person and a dead end to an empty room: the model answers it in words, the
// turn ends, and the session idles with the budget unspent.
//
// So the addressee is named, once, and every such road goes through it.
//
// ── THE EVIDENCE THAT WROTE THIS FILE ───────────────────────────────────────
//
// Three autonomous runs, same build, same shape of ending. One quit after two
// and a half hours holding a measured partial result with three quarters of its
// budget left. One quit at the same hour on a tree that did not compile. One
// quit after eighteen minutes with nothing built at all. None of the three hit
// a wall, ran out of money or was stopped: each reached a moment where the
// engine's answer was "ask the person", there was no person, and the run ended
// there. Two more were zeroed at the end by scratch files the session had left
// lying beside the deliverable — nothing in the engine ever looked back over
// what it had created.
//
// ── THE TWO PRINCIPALS ──────────────────────────────────────────────────────
//
// [Person] is the interactive one and it is DELIBERATELY EMPTY. It answers
// every question the way the engine answered it before this file existed: it
// holds no acceptance (a person holds their own), it has no budget, it turns no
// landing into a brief, and it decides exactly what [Agent.readRemains]'s empty
// string already decided. That emptiness is the contract — an interactive
// session must not be able to tell that this interface arrived — and
// principal_person_test.go is the part a build fails on.
//
// [Steward] is the autonomous one. It is handed the goal, an acceptance written
// for the WHOLE ask rather than for one unit of work, and a budget; and it
// answers the questions a person would have answered, out of the evidence the
// session already has.
//
// ── WHY A BUDGET IS WHAT ARMS IT ────────────────────────────────────────────
//
// `--yolo` today says one thing: run tools without asking. It says nothing
// about how long, how much, or whether anybody is coming back — and a flag that
// silently started carrying a session on for hours because it also happened to
// mean "unattended" would be the harness deciding to spend somebody's money on
// a sentence they did not write. A BUDGET IS THE SENTENCE. It is a ceiling the
// person states in advance, it is the thing a Steward stops at, and without one
// `--yolo` is exactly what it has always been.

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// Principal is the addressee of every "ask the person" path in this package.
//
// FIVE METHODS, AND EACH ONE IS A QUESTION THE ENGINE USED TO ANSWER FOR
// ITSELF. Ask and Acceptance are what the work is measured against; Budget is
// what may be spent measuring it; Report is one unit of work coming home, and
// what — if anything — to open next on the strength of it; Decide is the end of
// a turn that stopped.
//
// IT IS DELIBERATELY NOT A STRUCT OF CALLBACKS. Two implementations exist and
// the whole point of the interface is that a third — a person on another
// machine, a queue, a scheduled owner — can be written without any road in this
// package learning a new name.
type Principal interface {
	// Ask is the goal in the principal's own words, verbatim and never
	// interpreted. Empty before anything has been asked.
	Ask() string

	// Acceptance is what the WHOLE ask has to satisfy before the work is
	// finished, as one observable sentence. Empty means this principal holds
	// no acceptance of its own — which is a person, who is looking at the
	// work and does not need one written down.
	Acceptance() string

	// Budget is what may still be spent. The zero Budget is no ceiling at all,
	// which is what an attended session has always had.
	Budget() Budget

	// Report is told how one unit of work landed, and answers WHAT TO OPEN
	// NEXT — a brief, in the words the next attempt should start on. An empty
	// answer means nothing more is started on the strength of this landing,
	// which is every landing for a person: they read the news and decide.
	Report(Landing) string

	// Decide answers the end of a turn that stopped: carry on with a brief, or
	// the ask is finished, or stop and say why.
	Decide(Remains) Decision
}

// Budget is a ceiling and what has been spent against it.
//
// TWO NUMBERS BECAUSE THERE ARE TWO WAYS TO RUN OUT, and a run bounded by one
// of them is bounded. Wall is how long the session may go on; USD is what it
// may spend. ZERO IS NO CEILING for each of them independently — an hours-only
// budget is a real thing to want — and a Budget with neither is not set at all
// ([Budget.Set]), which is the whole of how `--yolo` keeps its old behaviour.
//
// THE SPENT FIGURES ARE READ, NEVER ACCUMULATED HERE. Wall comes off the clock
// and USD off the session's own journaled usage (rail.go reads the same
// figure), so this is a reading and not a second ledger to drift.
type Budget struct {
	Wall      time.Duration
	USD       float64
	SpentWall time.Duration
	SpentUSD  float64
}

// Set reports that somebody stated a ceiling. It is the arming question and
// nothing else reads the two fields to ask it.
func (b Budget) Set() bool { return b.Wall > 0 || b.USD > 0 }

// Exhausted reports that the run is over on the budget, and says which ceiling
// in words a person reads. A budget nobody set is never exhausted.
//
// THE WORDS ARE THE PERSON'S AND NOT THE MACHINERY'S: "the four hours are up",
// not "wall limit exceeded".
func (b Budget) Exhausted() (bool, string) {
	if b.Wall > 0 && b.SpentWall >= b.Wall {
		return true, fmt.Sprintf("the %s this was given are up", spellDuration(b.Wall))
	}
	if b.USD > 0 && b.SpentUSD >= b.USD {
		return true, fmt.Sprintf("the $%.2f this was given is spent", b.USD)
	}
	return false, ""
}

// Left is what is still there to spend, floored at zero on each ceiling. A
// ceiling nobody set answers zero, which callers read together with
// [Budget.Set] rather than as "nothing left".
func (b Budget) Left() (time.Duration, float64) {
	var wall time.Duration
	if b.Wall > b.SpentWall {
		wall = b.Wall - b.SpentWall
	}
	var money float64
	if b.USD > b.SpentUSD {
		money = b.USD - b.SpentUSD
	}
	return wall, money
}

// spellDuration writes a budget in the units a person stated it in. It is here
// rather than in spellout.go because it answers about a CEILING — a round
// figure somebody typed — and not about an elapsed time, which is what every
// other duration in this package is and which rounds differently.
func spellDuration(d time.Duration) string {
	switch {
	case d >= time.Hour && d%time.Hour == 0:
		if hours := int(d / time.Hour); hours == 1 {
			return "hour"
		} else {
			return fmt.Sprintf("%d hours", hours)
		}
	case d >= time.Minute:
		return fmt.Sprintf("%d minutes", int(d/time.Minute))
	default:
		return d.String()
	}
}

// Landing is ONE UNIT OF WORK COMING HOME, in the fields a principal decides
// on. It is a reading of a task node and never the node itself: the interface
// must be answerable by something that has never seen this package's graph.
//
// Signature IS WHAT MAKES TWO FAILURES THE SAME FAILURE. It is what the loop
// guard counts ([Steward.Report]), and it is supplied by the caller rather than
// derived here because what "the same failure" means belongs to whoever
// classified the landing — the sibling taxonomy lane owns that word, and a
// Landing carrying one it wrote is exactly the hand-off this field is for. An
// empty Signature is a landing the guard cannot count, and it is then never
// counted rather than lumped in with every other unsigned one.
type Landing struct {
	ID        uint64
	Title     string
	State     TaskState
	Report    string
	Signature string

	// Ending is WHY a failed landing stopped where it did, in the harness's own
	// typed word ([TaskEnding]). It is here for one question: whether this
	// failure is a finding about the WORK at all ([Landing.aboutTheWork]).
	Ending TaskEnding

	// Files is what this unit of work changed, worktree-relative and in the
	// words its own ledger uses. It answers whether a failed unit's work exists
	// on the tree anyway, having been done by somebody else
	// ([Remains.absorbedBy]).
	Files []string

	// Merged says the work came home — not merely that the node said done, but
	// that what it made is on the person's own branch. Only a merged landing may
	// absorb a failed one, because a landing that finished and could not come
	// home is not the tree holding anything.
	Merged bool

	// Checked says this unit's OWN check read the work and accepted it. It is
	// narrower than done on purpose — a unit taken as it stands, one landed with
	// the check switched off and one a person accepted are all done and none of
	// them was judged — and [Remains.absorbedBy] will not let a landing nobody
	// judged speak for somebody else's work.
	Checked bool
}

// aboutTheWork reports whether this landing's failure says anything about the
// job at all.
//
// A NODE THAT DIED ON THE WIRE OR WAS REFUSED BY THE PROVIDER FOUND NOTHING OUT.
// The connection dropped, or an upstream would not serve the request: neither is
// evidence that the ask is unfinished, and reading one as a gap is the harness
// mistaking its own bad afternoon for a fact about the work. Measured (#513):
// the reef cell's third task died on an API 404, its parent had already written
// the very file it was for and merged it home with its own check green, and the
// run carried on for the rest of its wall over a finished tree because a dead
// sibling was still being counted as work that did not finish.
//
// EVERY OTHER ENDING IS THE WORK'S and is left exactly as it was — a check that
// found gaps, a loop guard, a threshold, a person stopping it, a working copy
// that could not be made.
func (l Landing) aboutTheWork() bool {
	return l.Ending != TaskEndingWire && l.Ending != TaskEndingUpstream
}

// unsatisfied reports a landing that did not finish the work it was given.
//
// FAILED AND UNVERIFIED ARE BOTH UNSATISFIED HERE, and that is not the same
// claim task_contract.go makes about them. There, keeping the two apart is what
// stops a broken judge from cascading; here the question is only "is this unit
// finished", and "nobody could say" is not "yes".
func (l Landing) unsatisfied() bool {
	return l.State == TaskFailed || l.State == TaskUnverified
}

// CheckRun is one of the session's declared checks, run and read.
//
// Tail is the END of what it printed, for [checkpointResultTail]'s reason: what
// a check concluded is in its last lines.
type CheckRun struct {
	Command string
	Passed  bool
	Tail    string

	// Ran says the command STARTED AND FINISHED — it was found, it executed, and
	// the shell gave an exit status. A command that would not start and one the
	// window cut off both answer false, and both are DIFFERENT NEWS from a check
	// that ran and failed: a check nobody could run taught nobody anything about
	// the tree, so a baseline may not record it as already-red (recording it
	// there would silence a real failure on it later).
	Ran bool
}

// Remains is the end of a turn as a principal is shown it.
//
// Reader is the mark reader's one line about what is left, and EMPTY MEANS IT
// READ THE ASK AS MET — the contract [Agent.readRemains] has always had. The
// three fields under it are what a person would have looked at before agreeing:
// the acceptance for the whole ask, how the units of work landed, and what the
// session's own declared checks say about the tree right now.
type Remains struct {
	Said       string
	Reader     string
	Acceptance string
	Landings   []Landing
	Checks     []CheckRun

	// Landed says whether this session has finished ANY unit of work. A session
	// that has landed nothing has not finished an ask, whatever a reader of its
	// transcript makes of it, and [Steward.Decide] refuses to call that done.
	Landed bool

	// Running names the units of work that are IN FLIGHT — started, or queued
	// behind something that is — in the words a person reads them by.
	//
	// WORK THAT IS STILL RUNNING IS NEITHER DONE NOR A STANDSTILL, and this is
	// the field that makes both halves of that sayable. An ask with something
	// still moving is not finished, however tidy everything that already landed
	// looks; and a run whose unmet set has not changed BECAUSE it is waiting on
	// something is not going round in a circle, it is waiting, so the floor under
	// carrying on ([Steward.standstill]) must not fire on it.
	//
	// RUNNING MEANS IN FLIGHT AND NOTHING ELSE. Work that is merely UNSETTLED is
	// not the same thing: a unit queued behind a prerequisite that settled short
	// will never start, and putting it here would hold that floor open for the
	// rest of a run that had already stopped getting anywhere — which is the
	// whole reason the field below exists beside this one.
	Running []string

	// Blocked is the work that will not start, said whole: what each unit is
	// waiting on and what became of that. It is PART OF WHAT IS LEFT — a unit
	// waiting on something that is not coming is a gap in the ask exactly as a
	// failed one is — and it is never a reason to keep carrying on.
	Blocked []string

	// BaselineRead says the before-reading has LANDED. It runs in the background
	// at the start of an unattended run, so a reading assembled in the first
	// minutes has no baseline yet — and with none, NO CHECK IS COUNTED AS THIS
	// RUN'S OWN RED. Naming a check before anybody knows whether it was already
	// failing is the mistake this whole field exists to stop, made in a hurry.
	BaselineRead bool

	// WasFailing is the checks that were ALREADY RED before this session did any
	// work, by the same command names Checks carries. It is meaningless unless
	// BaselineRead.
	//
	// A CHECK IS OURS ONLY IF WE TURNED IT RED. An acceptance that says "the
	// existing test suite passes" is written over whatever the project's suite
	// does today, and where one test was red before anybody touched anything
	// that sentence can never be true: the run reads its own failure in the
	// project's, carries on into it, and spends its whole ceiling on somebody
	// else's bug. Measured (#513): the attrs cell's acceptance was `tox -e py`
	// passes over a suite with one pre-existing failure, and it never stopped.
	//
	// EMPTY WITH BaselineRead IS A CLEAN TREE; empty without it is a reading that
	// has not landed. A session that never takes one — every watched session —
	// counts no check as its own, which is the one safe answer when nobody knows
	// what was red to begin with.
	WasFailing []string
}

// unmet lists, in a person's words, what stands between this and finished. An
// empty answer is the only thing that may become [DecideDone].
func (r Remains) unmet() []string {
	var out []string
	if !r.Landed {
		out = append(out, "nothing has been finished yet")
	}
	for _, title := range r.Running {
		out = append(out, title+" is still running")
	}
	// The blocked lines arrive as whole sentences, because what a stuck unit of
	// work is waiting on is the only useful thing anybody can say about it.
	out = append(out, r.Blocked...)
	for _, landing := range r.Landings {
		if !landing.unsatisfied() {
			continue
		}
		// WHAT IS LEFT IS READ FROM THE TREE, NOT FROM A SIBLING'S DEATH. A unit
		// that never found anything out is not a gap in the ask, and neither is
		// one whose work another unit has since done and brought home.
		if !landing.aboutTheWork() {
			continue
		}
		if r.absorbedBy(landing) != "" {
			continue
		}
		if title := strings.TrimSpace(landing.Title); title != "" {
			out = append(out, title+" did not finish")
			continue
		}
		out = append(out, fmt.Sprintf("unit %d did not finish", landing.ID))
	}
	// AND ONLY THE RED THIS WORK TURNED RED IS LEFT. What was already failing
	// before anybody touched the tree is the project's and not this session's,
	// and naming it sends a run that has finished back into somebody else's bug
	// for the rest of its ceiling. It is the same subtraction the task harness
	// makes over its own before-and-after ([verify.NewFailures]), on the check
	// commands rather than on test names, because a session's declared check is
	// a whole command and its answer is whether that command passed.
	//
	// AND WITH NO BASELINE YET, NOTHING IS COUNTED. The reading runs in the
	// background at the start of the run, and until it lands nobody knows which
	// red is the project's — so the honest answer about the checks is silence
	// rather than a guess, and [stewardBrief] says the reading is still going.
	if r.BaselineRead {
		for _, command := range verify.NewFailures(r.WasFailing, r.redChecks()) {
			out = append(out, command+" does not pass")
		}
	}
	return out
}

// redChecks is every declared check that is failing NOW, in the order they ran.
func (r Remains) redChecks() []string {
	var out []string
	for _, check := range r.Checks {
		if !check.Passed {
			out = append(out, check.Command)
		}
	}
	return out
}

// alreadyRed is what this reading found failing that was failing before the work
// began — the checks [Remains.unmet] deliberately did not name.
func (r Remains) alreadyRed() []string {
	red := r.redChecks()
	if !r.BaselineRead {
		return nil
	}
	return verify.Subtract(red, verify.NewFailures(r.WasFailing, red))
}

// absorbedBy names the landing that already did this one's work, and "" when
// nothing did.
//
// A UNIT'S WORK IS ITS FILES. A failed unit whose every file has since been
// changed by a unit that finished AND came home is not a gap in the ask: the
// thing it was for is on the person's branch, put there by somebody else, and
// naming it as unfinished sends the run back to write a file that is already
// written. In the reef cell the parent wrote its child's only file itself, went
// green on its own check and merged — and the child, dead on the wire, was still
// read as work outstanding.
//
// IT IS DELIBERATELY STRICT IN FOUR WAYS. A failed unit that changed NOTHING is
// never absorbed, because nothing of it can be shown to be done. Only a landing
// that is done, MERGED and CHECKED may absorb: work that finished and could not
// come home is not the tree holding anything, and work nobody judged is not
// evidence about anybody's job — least of all somebody else's. And every file
// must be covered: a unit half of whose work somebody else did is a unit with
// work left.
//
// THE RESIDUAL IS A GREEN MERGED REVERT, and it is accepted deliberately. A
// landing that reverted the failed unit's edits, was judged against the ask and
// came home green is the tree AS JUDGED — the check read what would ship and
// said it holds — so the ask is met over the files in question whatever any
// individual edit did to them. Reading the diff here instead would be this
// function second-guessing the one reader in the building that actually looked.
//
// THE WHOLE SET IS READ RATHER THAN WHAT CAME AFTER, because a landing carries
// no clock and the question is not who was first — it is whether the file is
// home NOW. The reef cell settles that on its own: the unit that absorbed the
// dead one has the lower id and landed later.
func (r Remains) absorbedBy(failed Landing) string {
	if len(failed.Files) == 0 {
		return ""
	}
	for _, landing := range r.Landings {
		if landing.ID == failed.ID || landing.State != TaskDone {
			continue
		}
		if !landing.Merged || !landing.Checked {
			continue
		}
		if !covers(landing.Files, failed.Files) {
			continue
		}
		return workWord(landing.Title, landing.ID)
	}
	return ""
}

// absorbed is every "<title> was absorbed by <title>" this reading found, for
// the journal: a unit that stopped counting as unfinished without anybody
// deciding anything must say who did its work, or the file records a gap that
// closed for no reason anybody can read afterwards.
func (r Remains) absorbed() []string {
	var out []string
	for _, landing := range r.Landings {
		if !landing.unsatisfied() || !landing.aboutTheWork() {
			continue
		}
		if by := r.absorbedBy(landing); by != "" {
			out = append(out, workWord(landing.Title, landing.ID)+" was absorbed by "+by)
		}
	}
	return out
}

// covers reports whether every one of want appears in have. The lists are a
// handful of paths each, so the walk is the honest shape.
func covers(have, want []string) bool {
	held := make(map[string]bool, len(have))
	for _, path := range have {
		held[strings.TrimSpace(path)] = true
	}
	for _, path := range want {
		if !held[strings.TrimSpace(path)] {
			return false
		}
	}
	return true
}

// DecisionVerb is one of the three answers there are to a turn that stopped.
type DecisionVerb string

const (
	// DecideCarryOn re-opens the turn on Brief.
	DecideCarryOn DecisionVerb = "carry on"
	// DecideDone says the ask is finished and the turn may end.
	DecideDone DecisionVerb = "done"
	// DecideStop ends the session's work and says why, in Reason.
	DecideStop DecisionVerb = "stop"
)

// Decision is what a principal answered.
//
// Observed is WHAT THE ANSWER WAS TAKEN ON, in the same words a person reads —
// the items a reading actually showed. It rides beside the brief because the
// brief is addressed to the MODEL and is written to be worked from, while the one
// line a person is shown when carrying on stops ([checkpointCarriedOnNote]) has
// to say what was seen and nothing else. A note that had only the brief to go on
// asserted "it is still not finished" as a fact, which is a claim nobody took a
// reading of (#468).
type Decision struct {
	Verb     DecisionVerb
	Brief    string
	Reason   string
	Observed []string
	// Spent marks the ONE stop that is about money and hours rather than about
	// the work: the budget is gone. It is read where a stop may have to end a
	// turn over work that is still moving ([Agent.endTurnUnderSteward]), which
	// only this stop may do — SPENDING IS THE THING A BUDGET FORBIDS, and
	// waiting for the moving work to come home is more of exactly what ran out.
	// Every other stop is about the work and can afford to let the work finish.
	Spent bool
}

// carryOn, done and stop are the three constructors, so no caller in this
// package assembles a Decision field by field and forgets one.
func carryOn(brief string, observed ...string) Decision {
	return Decision{Verb: DecideCarryOn, Brief: brief, Observed: observed}
}
func done() Decision              { return Decision{Verb: DecideDone} }
func stop(reason string) Decision { return Decision{Verb: DecideStop, Reason: reason} }

// stopSpent is the budget's own stop, and it is spelled apart from [stop]
// because the two are answered differently over work that is still moving
// ([Decision.Spent]).
func stopSpent(reason string) Decision {
	return Decision{Verb: DecideStop, Reason: reason, Spent: true}
}

// ── THE PERSON ──────────────────────────────────────────────────────────────

// Person is the principal of an attended session, and IT ADDS NOTHING.
//
// Every method answers what the engine answered before this interface existed,
// and the emptiness is load-bearing rather than a stub waiting to be filled:
// a person holds their own acceptance, spends against their own judgement,
// reads a landing and decides what to do about it themselves, and carries a
// stopped turn on by typing. The one thing it holds is the ask, because
// [Principal.Ask] has to answer something and the session already knows it.
type Person struct {
	mu  sync.Mutex
	ask string
}

// NewPerson builds the principal of an attended session.
func NewPerson() *Person { return &Person{} }

func (p *Person) Ask() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ask
}

// Acceptance is empty for a person, and that is the answer rather than a gap:
// somebody watching the work does not need the done-condition written down for
// them, and writing one on their behalf would be the harness deciding what they
// meant.
func (p *Person) Acceptance() string { return "" }

// Budget is unset for a person: an attended session has always run until they
// stopped it, and the spend rail (rail.go) is the ceiling they can already set.
func (p *Person) Budget() Budget { return Budget{} }

// Report answers nothing, which is what makes a landing's note reach the person
// exactly as it always has: the news arrives, and what happens next is theirs.
func (p *Person) Report(Landing) string { return "" }

// Decide is [Agent.readRemains]'s own rule, moved and not changed: a reader
// with something to say re-opens the turn on it, and silence ends the turn.
// Nothing else a person could be shown is consulted, because nothing else was.
func (p *Person) Decide(r Remains) Decision {
	if line := strings.TrimSpace(r.Reader); line != "" {
		// THE OBSERVATION IS THE LINE ITSELF, which is not an addition to what a
		// person's session decides: the reader's line is the whole of what was
		// read here, and saying so is what lets the one note a person ever sees
		// on this road quote what was seen rather than assert a conclusion.
		return carryOn(line, line)
	}
	return done()
}

// hear records the person's own words, and the LAST of them stands.
//
// It is unexported because it is not part of the interface: the SESSION tells
// its principal what was asked, and no caller outside this package has a reason
// to. Last-wins is [Agent.rememberAskLocked]'s own rule, kept rather than
// improved on — what a person is asking for is the thing they most recently
// said, and this must answer exactly what the engine already answered.
func (p *Person) hear(ask string) {
	ask = strings.TrimSpace(ask)
	if ask == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ask = ask
}

// ── THE STEWARD ─────────────────────────────────────────────────────────────

// stewardRepeats is how many times one failure signature may come home before
// the Steward stops.
//
// THREE, because two is a coincidence and four is an evening. The first
// landing is the work failing; the second is the repair failing the same way,
// which is still news; the third says the road is not going anywhere and every
// further attempt is the same money spent on the same wall. A stop with a
// report is strictly better than a fourth attempt: the report is the thing a
// person can act on, and it exists either way.
const stewardRepeats = 3

// Steward is the principal of an unattended session: it holds the ask, an
// acceptance written for the whole of it, and a budget, and it answers on the
// absent person's behalf out of evidence rather than opinion.
//
// EVERYTHING IT DECIDES IS DECIDED FROM FACTS THE SESSION ALREADY HAS. It never
// calls a model: the reader's line, how the units landed, and what the declared
// checks say are gathered by the caller and handed over ([Remains]), and this
// type is the policy over them. That is what makes every one of its answers
// testable without a network, and it is why the loop guard can be trusted — a
// guard that had to ask a model whether two failures were the same failure
// would be a guard that fails open on a bad evening.
type Steward struct {
	mu         sync.Mutex
	ask        string
	acceptance string

	// wall and money are the CEILINGS; started and spent are how the figures
	// against them are read. spent is a closure onto the session's own
	// journaled usage rather than a number kept here, for rail.go's reason: a
	// second ledger drifts.
	wall    time.Duration
	money   float64
	started time.Time
	spent   func() float64
	now     func() time.Time

	// failures counts landings by signature, and stopped is the reason the
	// guard fired. Once stopped is set every answer is the same stop: a
	// principal that changed its mind after saying stop would be a rail with a
	// hole in it.
	failures map[string]int
	stopped  string

	// carriedUnmet and carriedBrief are THE LAST CARRY-ON THIS STEWARD MINTED,
	// and they are the whole of the standstill floor ([Steward.Decide]).
	//
	// THEY LIVE HERE BECAUSE THE LOOP THEY BOUND IS LONGER THAN A TURN. The
	// per-turn ceiling on carrying on (checkpoint.go's [checkpointCarryOnCap])
	// counts on a meter the turn owns, and every other way out of a turn builds a
	// fresh one — so a session whose task landings kept waking new turns was
	// bounded by a counter that reset before it could ever fire, and the measured
	// run repeated one byte-identical brief until its wall ran out (#468). A
	// floor kept on the principal cannot be restarted by a new turn, which is the
	// only place it means anything.
	//
	// They are empty until the first carry-on, so a session's FIRST identical
	// pair of readings is a carry-on and its second is the stop.
	carriedUnmet string
	carriedBrief string
}

// NewSteward builds the principal of an unattended session.
//
// spent reads the session's accumulated cost in US dollars and MAY BE NIL, in
// which case no money is ever counted against the ceiling — which is honest
// rather than convenient: a build with no cost figures must not stop a run on a
// number it invented.
func NewSteward(ask string, budget Budget, spent func() float64) *Steward {
	return &Steward{
		ask:      strings.TrimSpace(ask),
		wall:     budget.Wall,
		money:    budget.USD,
		started:  time.Now(),
		spent:    spent,
		now:      time.Now,
		failures: map[string]int{},
	}
}

func (s *Steward) Ask() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ask
}

// hear records the goal this session exists to reach, AND IT RECORDS IT ONCE.
//
// This is where a Steward parts company with a [Person], whose ask is simply
// the last thing they typed. The acceptance is written against this sentence
// and frozen against it ([Steward.setAcceptance]); a goal that moved underneath
// it would leave the session measuring its work against a done-condition for
// something else. Somebody who wants a different goal is somebody who is
// present, and what a present person does is start a session.
func (s *Steward) hear(ask string) {
	ask = strings.TrimSpace(ask)
	if ask == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ask != "" {
		return
	}
	s.ask = ask
	// AND A GOAL THAT IS BEING RECORDED IS A STRETCH BEGINNING, so the floor
	// under carrying on starts empty ([Steward.forget]'s reason, said at the
	// other end).
	s.carriedUnmet, s.carriedBrief = "", ""
}

func (s *Steward) Acceptance() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.acceptance
}

// setAcceptance writes the session's acceptance ONCE and never again.
//
// IT IS IMMUTABLE FOR THE SESSION and this is the whole of the enforcement: a
// done-condition a running model can rewrite is a done-condition the model
// grades itself against, which is the exact reading this side of the codebase
// was built to stop relying on. It reports whether the write landed, so the
// caller journals a fact rather than an intention.
func (s *Steward) setAcceptance(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.acceptance != "" {
		return false
	}
	s.acceptance = text
	return true
}

func (s *Steward) Budget() Budget {
	s.mu.Lock()
	wall, money, started, spent, now := s.wall, s.money, s.started, s.spent, s.now
	s.mu.Unlock()
	budget := Budget{Wall: wall, USD: money}
	if !started.IsZero() {
		budget.SpentWall = now().Sub(started)
	}
	if spent != nil {
		budget.SpentUSD = spent()
	}
	return budget
}

// Report turns one landing into the next attempt's brief, and it is the road a
// failed unit of work now has instead of a sentence addressed to nobody.
//
// THE AUDIT'S OWN REPORT IS THE BRIEF. Whoever read the work wrote down what it
// did and what stopped it; that is already the most specific account of the gap
// anybody in this session has, and re-deriving it from a fresh model would be
// paying to be told the same thing less accurately.
//
// A LANDING THAT FINISHED PRODUCES NOTHING. There is nothing to open on the
// strength of a unit that did what it was asked, and a Steward that briefed one
// anyway would be a session that never runs out of work to do.
//
// AND THE GUARD COUNTS BEFORE IT ANSWERS. Three landings with one signature
// (see [stewardRepeats]) stop the session for good, with the report as the
// thing a person reads. An unsigned landing is not counted at all — a guard
// that folded every unsigned failure into one bucket would stop a session that
// was making progress on three different problems.
func (s *Steward) Report(landing Landing) string {
	if !landing.unsatisfied() {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped != "" {
		return ""
	}
	if signature := strings.TrimSpace(landing.Signature); signature != "" {
		s.failures[signature]++
		if s.failures[signature] >= stewardRepeats {
			s.stopped = fmt.Sprintf("the same thing has stopped this %d times running: %s",
				s.failures[signature], stewardReason(landing))
			return ""
		}
	}
	report := strings.TrimSpace(landing.Report)
	if report == "" {
		return ""
	}
	return report
}

// stewardReason is what a stop says about the landing that caused it, in a
// person's words: the report if there is one, the title if there is not, and
// the id when there is nothing else to name it by.
func stewardReason(landing Landing) string {
	if report := strings.TrimSpace(landing.Report); report != "" {
		return firstLine(report)
	}
	if title := strings.TrimSpace(landing.Title); title != "" {
		return title
	}
	return fmt.Sprintf("unit %d", landing.ID)
}

// Decide is the end of a turn, answered on the absent person's behalf.
//
// THE ORDER IS THE POLICY, and it is an order over EVIDENCE:
//
//  1. A GUARD THAT HAS FIRED OUTRANKS EVERYTHING. Once the same failure has come
//     home [stewardRepeats] times the session is over, whatever a reader says.
//  2. AN EXHAUSTED BUDGET STOPS, and it stops with a report rather than with
//     silence — a run that spent its hours and said nothing is a run nobody can
//     learn from.
//  3. WHAT LANDED AND WHAT RAN COME BEFORE ANY READER'S LINE. This is the rung
//     that moved, and it is the whole of #468. A reader's line is a reading of the
//     TRANSCRIPT — what the session said about itself — while a settled landing
//     and a check that ran are readings of the work. So the unmet set is taken
//     first, and a session whose units of work are done and whose checks all
//     passed is FINISHED, however much a reader still has to say about it. The
//     measured run had a task merged home with twenty-two checks green, and was
//     carried on past it for the rest of its wall on a line somebody's sidecar
//     wrote about the transcript.
//  4. WITH SOMETHING GENUINELY LEFT, THE READER'S OWN WORDS ARE THE BRIEF where
//     there are any: the unmet set says THAT work remains and the reader is
//     usually more specific about WHAT, and a brief is read by a model that has to
//     act on it.
//  5. WORK STILL IN FLIGHT IS NEITHER OF THE TWO ENDINGS. An ask with a unit of
//     work still going is not finished, and it is not going round in a circle
//     either — it is waiting, so the floor below is not asked about it and what it
//     remembers is dropped ([Remains.Running]).
//  6. AND THE SAME THING TWICE RUNNING IS A STANDSTILL, not a third go
//     ([Steward.standstill]).
//
// THE FROZEN DONE-CONDITION IS NEVER EVIDENCE HERE. It is written before any work
// happens, out of the ask alone, and it reaches the decision only as the context a
// brief opens with ([stewardBrief]) — a sentence the session wrote for itself is
// not a reading of anything.
func (s *Steward) Decide(r Remains) Decision {
	s.mu.Lock()
	stopped := s.stopped
	s.mu.Unlock()
	if stopped != "" {
		return stop(stopped)
	}
	if spent, why := s.Budget().Exhausted(); spent {
		return stopSpent(why)
	}
	unmet := r.unmet()
	if len(unmet) == 0 {
		// A DONE ANSWER FORGETS WHAT WAS LEFT LAST TIME. The fingerprint below is
		// about ONE STRETCH of carrying on, and an ask that reached done ended
		// that stretch: a session that finishes, is asked for more, and meets the
		// same gap again is meeting it for the first time since — and a floor
		// that remembered across the finish would stop it on its first carry-on.
		s.forget()
		return done()
	}
	brief := strings.TrimSpace(r.Reader)
	if brief == "" {
		brief = stewardBrief(r, unmet)
	}
	// AND NOTHING IS A STANDSTILL WHILE SOMETHING IS STILL MOVING. An unmet set
	// that has not changed because the work has not come home yet is a session
	// waiting, not a session repeating itself, and what the floor remembers from
	// before the wait is about a tree that has since been worked on.
	if len(r.Running) > 0 {
		s.forget()
		return carryOn(brief, unmet...)
	}
	if halted := s.standstill(unmet, brief); halted != "" {
		return stop(halted)
	}
	return carryOn(brief, unmet...)
}

// forget drops what the floor remembers, so the next carry-on is a first one.
// It is called wherever the STRETCH the fingerprint is about has ended: an ask
// that finished, and work that is still moving under it.
func (s *Steward) forget() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.carriedUnmet, s.carriedBrief = "", ""
}

// standstill is the floor under carrying on: it answers a REASON TO STOP when
// this steward is about to say what it said last time, and "" when something has
// moved.
//
// A STANDSTILL IS A STOP AND NOT A CARRY-ON. The evidence that a run is getting
// somewhere is that what is left CHANGES; a session that reaches the end of a turn
// with the same unmet set and writes the same brief has learned nothing from the
// turn it just spent, and every further round is the same money against the same
// wall. The measured run wrote one byte-identical brief four times in a hundred
// seconds and then wrote it until its hours were up (#468).
//
// BOTH READINGS ARE COMPARED, because either one standing still is the same
// event: the unmet set is what the work and the checks showed, and the brief is
// what a reader added on top of it. A change in either is progress enough to go
// again.
//
// AND IT SETS [Steward.stopped], SO IT HOLDS. A floor that only answered this one
// call would be re-asked by the next turn with a fresh meter under it, which is
// exactly the counter that was already there and could not fire.
func (s *Steward) standstill(unmet []string, brief string) string {
	seen := strings.Join(unmet, "; ")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.carriedUnmet != seen || s.carriedBrief != brief {
		s.carriedUnmet, s.carriedBrief = seen, brief
		return ""
	}
	s.stopped = stewardStandstillReason(unmet)
	return s.stopped
}

// stewardStandstillReason is what a standstill says, in the register every stop
// on this road wears: an observation, and then what is being done about it.
//
// IT NAMES WHAT IS STILL LEFT and it says the run stopped RATHER THAN REPEAT
// ITSELF, which are the two things a person coming back to a stopped session
// needs — the second one because a run that went quiet on its own is otherwise
// indistinguishable from one that crashed.
func stewardStandstillReason(unmet []string) string {
	return "nothing moved since the last look and what is left is the same — " +
		strings.Join(unmet, "; ") + " · saying it again would not change it"
}

// stewardBrief writes what is left to do out of what is unmet. It names the
// acceptance first when there is one, because the gaps under it are only worth
// anything against the thing they are gaps in.
func stewardBrief(r Remains, unmet []string) string {
	var out strings.Builder
	if acceptance := strings.TrimSpace(r.Acceptance); acceptance != "" {
		out.WriteString("what was asked is finished when: ")
		out.WriteString(acceptance)
		out.WriteString("\n\n")
	}
	out.WriteString("it is not finished yet — ")
	out.WriteString(strings.Join(unmet, "; "))
	out.WriteString(".")
	// AND WHAT WAS ALREADY BROKEN IS SAID OUT LOUD RATHER THAN SILENTLY DROPPED.
	// A worker handed a brief that does not mention the red it can plainly see
	// will go and fix it, which is the whole failure in its other form; told
	// that it was red before the work and is not being counted, it can leave it
	// alone or say so.
	if !r.BaselineRead && len(r.Checks) > 0 {
		// AND A BRIEF WRITTEN BEFORE THE BASELINE LANDED SAYS SO. A worker told
		// nothing about the checks would read the silence as "they pass"; told
		// that nobody has finished reading them yet, it knows the one thing that
		// is actually true.
		out.WriteString("\n\n")
		out.WriteString(baselineStillReading)
	} else if already := r.alreadyRed(); len(already) > 0 {
		out.WriteString("\n\n")
		out.WriteString(alreadyRedSentence(already))
	}
	return out.String()
}

// baselineStillReading is what a brief says while the before-reading of the
// checks is still running, so silence about them is never read as "they pass".
const baselineStillReading = "what the checks said before this work is still being read, " +
	"so nothing is being counted against them yet"

// alreadyRedSentence says what the tree was already failing before this work, in
// a person's words and with the commands named so nobody has to guess which.
func alreadyRedSentence(already []string) string {
	was := "1 check was"
	if len(already) > 1 {
		was = fmt.Sprintf("%d checks were", len(already))
	}
	return was + " already failing before this work and is not counted: " + strings.Join(already, ", ")
}
