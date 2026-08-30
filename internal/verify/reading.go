package verify

// What one photograph of a project's own verification may cost, and the pair of
// readings a run takes of it.
//
// The arithmetic used to live in internal/exec/bare, where exactly one worker
// could reach it — the same visibility mistake this package was created to
// undo. The delivery gate reads the same photograph the worker took, and where
// no photograph exists it takes one itself; two callers reading two copies of
// one cap is how a number in this repository drifts. PERF.md, "The verification
// photograph's budget", is where this is stated for a person, and the rule is
// that changing either constant changes that page in the same commit.

import (
	"strings"
	"time"
)

// WallShare is the denominator of the leaf's own wall that ONE reading of the
// project's own verification may spend.
//
// It is a share and not a duration because the thing being bounded is not a
// suite — it is the fraction of a run's life spent measuring instead of
// working. An eighth each way is a quarter of the wall at the very worst, and
// the worst is rare: the second reading is taken only when the tree changed.
const WallShare = 8

// ShortestUsefulReading is the floor under that share, and it is the refusal
// this file exists to state as a law: A RUN WHOSE WALL CANNOT AFFORD A REAL
// READING TAKES NO READING AT ALL, rather than spending an eighth of its life
// on a command that will be killed before it says anything.
//
// One minute is derived from the fastest whole suite measured in the sweep this
// work comes from — igel's two passing project tests, "2 passed in 27.86s" —
// doubled to leave room for an interpreter, an import graph and a compile. A
// budget under that cannot hold even the cheapest observed project suite, so a
// reading taken with it would time out, name nothing, and cost an eighth of a
// wall for a Result that says nothing at all.
const ShortestUsefulReading = time.Minute

// ReadingBudget is what one reading of the project's own verification may
// spend, given the whole wall its caller was granted.
//
// ok is false when the wall is too short to afford the measurement, which is
// the refusal above. A ninety-minute wall affords 11m15s a reading; a
// sixty-second wall affords 7.5s, which is under the floor, so it photographs
// nothing. The shortest wall that photographs at all is eight minutes.
func ReadingBudget(wall time.Duration) (time.Duration, bool) {
	if wall <= 0 {
		return 0, false
	}
	budget := wall / WallShare
	if budget < ShortestUsefulReading {
		return 0, false
	}
	return budget, true
}

// Reading is a run's photograph of the project's own verification: what the
// suite said before the work, what it said after, and the entrypoint and budget
// both readings were taken with.
//
// A zero value — which is what an unaffordable wall, an undiscoverable
// entrypoint or a hung first reading all produce — is a reading that was never
// taken. Taken says which of the two it is, because an empty Before reads
// downstream as NO CLAIM and never as NOBODY LOOKED, and the whole reason this
// type is carried rather than reduced to a list of names is that the delivery
// gate needs to tell those apart.
//
// It travels on the outcome so the gate can weigh what the world said instead
// of what the deliverable claims the world said. The gate may also fill After
// itself, on Budget, when the work moved after the last photograph was taken.
type Reading struct {
	Plan   Plan          `json:"plan,omitzero"`
	Budget time.Duration `json:"budget,omitempty"`
	// Strategy is the rung that was reached — the command that ran, or the one
	// that would have. It is set even when nothing was read, because "what
	// would have run" is half of why nothing did.
	Strategy Strategy `json:"strategy,omitzero"`
	// Unread is why there is no reading, in one sentence, and it is the whole
	// repair of the silence this type used to keep. A zero Reading has four
	// causes — a project that declares no verification, a wall too short to
	// afford one, a shell the preamble cannot be trusted in, and a command
	// killed at its ceiling — and downstream they mean different things and
	// cost different amounts. textual's s6 leaf spent five and a half minutes
	// on the fourth of them and left no trace of having done so.
	//
	// It is empty when Taken is true. NOBODY LOOKED IS A FACT, AND A FACT
	// ABOUT THE RUN REACHES THE RECORD (FAILSAFE.md clause 4).
	Unread string `json:"unread,omitempty"`
	// Partial says the reading that was taken was CUT: the command was killed
	// at its ceiling having already named some checks. It is a real roster and
	// it is not a comparable one — the checks it never reached are missing
	// because the clock ran out, and subtracting them would report the whole
	// tail of a suite as checks that disappeared.
	//
	// It exists because throwing the names away was worse. ink s7's `npx ava
	// --tap` was killed at 1m53s having streamed part of its 922 checks; the
	// whole reading was discarded, the round-2 gate had no roster at all, and a
	// deliverable at 13 of 25 hidden checks passed with nothing to weigh. A
	// PARTIAL ROSTER ANSWERS "DOES A CHECK FOR THIS EXIST" PERFECTLY WELL; it
	// answers "did this work break something" not at all, and those are two
	// questions.
	Partial bool `json:"partial,omitempty"`
	// CutAfter is how long the cut reading ran before it was killed. It is what
	// the run learned about this project's PACE, and it is remembered against
	// the job for the same reason the refusal is: a container running amd64
	// under qemu is five to ten times slower than the machine the budget's
	// arithmetic assumes, and a job that discovered that must not spend another
	// eighth of its wall discovering it again.
	CutAfter time.Duration `json:"cut_after,omitempty"`
	Before   Result        `json:"before,omitzero"`
	// After is the second reading, of the tree as it was handed over. It is
	// separate from Before rather than replacing it because the whole value of
	// a photograph is the subtraction, and a run holding one reading cannot
	// tell a check this work broke from a check the repository arrived broken.
	After      Result `json:"after,omitzero"`
	Taken      bool   `json:"taken,omitempty"`
	AfterTaken bool   `json:"after_taken,omitempty"`
	// Surface is the OTHER half of the photograph: the public names the tree
	// spelled before the job's first change, by file.
	//
	// It is here rather than beside itself because it is one measurement of one
	// tree at one moment, taken with the check-level reading and inherited by
	// every round of the job for the identical reason — a repair round standing
	// in a tree its own job already changed must be compared against what the
	// JOB found, not against what the previous round left.
	//
	// It never reaches the journal whole: a repository's public surface is tens
	// of thousands of short strings and what a reader wants is the DIFFERENCE.
	// See Surface.Removed, and store.EventSurface.
	Surface Surface `json:"-"`
}

// Regressed names the checks that were green before this work and are red after
// it, or nothing when there is no pair of readings to subtract.
func (r Reading) Regressed() []string {
	if !r.comparable() {
		return nil
	}
	// A REGRESSION IS A CHECK THAT WAS NAMED GREEN AT THE BASELINE AND IS RED
	// NOW. Nothing else is one, and every earlier spelling of this rule was a
	// way of approximating it.
	//
	// Subtracting the two FAILING lists is the approximation, and it holds only
	// while both readings run the same set of checks. They do not. A run writes
	// checks — that is most of what a run does — and happy-dom's nemotron n1 run
	// rewrote the very file its reading was scoped to, taking it from 4 checks
	// to 33. Both halves ran the identical command, so nothing had been widened
	// and nothing looked suspicious, and eighteen of the run's OWN new checks
	// were red. The gate failed the delivery with `This work broke checks that
	// were passing before it: IntersectionObserver initial observation
	// queuing …` — checks that did not exist when the baseline was taken — while
	// the grader scored that same tree 9 of 9.
	//
	// So the baseline's ROSTER is the authority, not its failure list. A name it
	// reported and did not report failing was green; a name it never reported at
	// all is not this work's to have broken, whatever the reason it is there now.
	//
	// Where the baseline kept no roster the question cannot be asked, and there
	// the old subtraction stands: plenty of runners print their failures and
	// nothing else, and reading that silence as "no check existed" would excuse
	// every regression in every such project.
	broke := NewFailures(r.Before.Failing, r.After.Failing)
	greenBefore, roster := r.Before.greenRoster()
	if !roster {
		return broke
	}
	var stood []string
	for _, name := range broke {
		if greenBefore[name] {
			stood = append(stood, name)
		}
	}
	return stood
}

// OwnFailing names the red checks that FIRST APPEARED AFTER THE BASELINE: the
// ones this run wrote itself, and did not get passing.
//
// It is the other half of what the failure list used to be read as, and it is a
// different finding with different words. A leaf whose own new checks are red
// has not finished; a leaf that turned somebody else's check red has broken the
// repository. Both are worth a repair round and only one of them is true of a
// run that wrote thirty new tests and got twelve of them right.
//
// Only where the baseline kept a roster, for the reason Regressed states: with
// no roster there is no way to tell a new check from an old one, and inventing
// the distinction would put every failure in a runner that prints only failures
// into this list instead of the other.
func (r Reading) OwnFailing() []string {
	if !r.comparable() {
		return nil
	}
	knownBefore, roster := r.Before.greenRoster()
	if !roster {
		return nil
	}
	var own []string
	for _, name := range r.After.Failing {
		if name = strings.TrimSpace(name); name == "" {
			continue
		}
		if _, held := knownBefore[name]; !held {
			own = append(own, name)
		}
	}
	return own
}

// greenRoster is which of this reading's checks were passing, and whether it
// kept a roster worth asking that of at all.
//
// ok is false where the reading named NO GREEN CHECK — which is not the same as
// naming nothing. Plenty of runners print their failures and nothing else, and
// against those the reported list IS the failure list: it carries no evidence
// that any check passed, so it cannot tell a check that was green and is missing
// from a check that never existed. Reading it as a roster would file every
// regression in every such project as a test the run wrote itself, which is the
// opposite of the mistake this rule was written to fix. There the old
// failing-list subtraction stands, exactly as it always did.
func (r Result) greenRoster() (green map[string]bool, ok bool) {
	if len(r.Reported) == 0 {
		return nil, false
	}
	green = make(map[string]bool, len(r.Reported))
	for _, name := range r.Reported {
		green[name] = true
	}
	for _, name := range r.Failing {
		green[name] = false
	}
	for _, passing := range green {
		if passing {
			return green, true
		}
	}
	return nil, false
}

// comparable says this photograph has two halves that are photographs of the
// SAME thing, which is the only condition under which subtracting them means
// anything.
//
// The after reading must COVER the before one, rather than match it: it is
// deliberately the wider of the two, because it takes in the checks the run
// itself wrote and those were not there to be read the first time. Growing a
// roster can only add names, so nothing extra can vanish; what it could have
// added is a false regression, and Regressed closes that by requiring a check to
// have PASSED before it can be said to have broken.
//
// Two readings and the same command was the whole of the old test, and it was
// half of the rule. A reading is of a command IN A PLACE AT A SCOPE — the
// package of a monorepo it ran in, and the checks it was told to run — and two
// of those three moved when readings learned to be scoped. A before reading of
// a whole suite minus an after reading of three files is every check that was
// not selected reported as one that disappeared. See Strategy.comparable.
func (r Reading) comparable() bool {
	// A CUT READING IS NOT A COMPARABLE ONE. Its roster stops where the clock
	// did, so every check the suite had not reached would subtract out as one
	// that stopped existing — a finding per untouched test, from a fact about a
	// ceiling.
	// AND A READING WITH NO TEST RECORD IS NOT ONE EITHER. A suite that failed
	// to collect never ran a check, so it holds no roster to subtract — and what
	// the shared vocabulary scrapes out of an error dump is a guess about a
	// suite that did not run. ofetch's nemotron n1 run subtracted one such guess
	// against a baseline of 28 named checks and failed the delivery over a check
	// called `to`. Either half is enough to refuse: a baseline that could not
	// collect cannot acquit, and an after reading that could not collect cannot
	// convict.
	return r.Taken && r.AfterTaken && !r.Partial &&
		!r.Before.Uncollected && !r.After.Uncollected &&
		r.After.Strategy.covers(r.Before.Strategy)
}

// Pace is what a reading that was CUT measured about how fast this project's
// checks run: the time it spent, and how many check files it had been asked for.
//
// It is the only thing a run ever learns about the speed of the machine under
// it, and it is a fact about the project and the machine rather than about the
// round — so it is remembered against the job with the baseline and spent by
// the next round, which is what "never the same blind ceiling twice" means. A
// zero value is a job that has measured nothing, and its reading is sized the
// way every reading here was.
type Pace struct {
	Spent time.Duration
	Files int
}

// Known says this pace was actually measured.
func (p Pace) Known() bool { return p.Spent > 0 && p.Files > 1 }

// Affords is how many check files this pace says fit in a budget, and it is
// written to be honest about what a CUT measured rather than to look precise.
//
// A cut proves one thing: this selection costs MORE than Spent. So Spent over
// Files is a lower bound on the per-file cost, and the count it yields is a
// CEILING on what fits — never a target. Taken as a target it says a reading
// killed at 1m53s over forty files can be retaken over thirty-six, which is the
// same reading again.
//
// So the ceiling is one of two bounds and the other is a halving, which is the
// only certain thing about a size that did not fit: the next one must be
// materially smaller. A tenth is held back on top for the difference between an
// average and a worst case.
func (p Pace) Affords(budget time.Duration) int {
	if !p.Known() || budget <= 0 {
		return 0
	}
	perFile := p.Spent / time.Duration(p.Files)
	if perFile <= 0 {
		return 0
	}
	return min(int((budget-budget/10)/perFile), p.Files/2)
}

// Pace is what this reading measured about the project's speed, if it measured
// anything. Only a cut reading does: a reading that finished says how long its
// own selection took and nothing about the ceiling it never reached.
func (r Reading) Pace() Pace {
	return Pace{Spent: r.CutAfter, Files: len(r.Strategy.Selected)}
}

// Retakeable says this remembered answer is one a later round should NOT simply
// inherit: a scoped reading cut at its ceiling having named nothing.
//
// Everything else about a failed reading is a fact about the tree, the project
// and the wall, and none of those move between rounds — that is why the refusal
// is remembered at all. A ceiling hit by a selection THIS PROGRAM CHOSE is not
// one of them: it is a fact about a size, the cut measured the pace that would
// have chosen a better one, and inheriting it is how textual s8 spent its one
// reading on forty files and then declined to look again.
func (r Reading) Retakeable() bool {
	return !r.Taken && r.Pace().Known()
}

// Declared says the project SAID how it is checked, whether or not a reading was
// taken of it.
//
// It is the difference between the two silences that used to be one. A project
// with no verification at all leaves the coverage question unanswerable and
// nobody is at fault; a project that declares a suite this run could not read
// leaves it unanswered, which is a fact about the run and must not deliver as
// whole. ink s7 passed at 13 of 25 hidden checks on the second of those.
func (r Reading) Declared() bool {
	for _, entrypoint := range r.Plan.Entrypoints {
		if entrypoint.Kind == KindTest {
			return true
		}
	}
	return false
}

// Vanished names the checks the suite reported before this work, did not report
// after it, and whose SUBJECT no check after it covers — deleted, or skipped,
// and never merely rewritten.
//
// It asks the question only when BOTH rosters named something. Two empty
// rosters subtract to nothing, which is arithmetic and not an acquittal, and a
// single empty one is a runner that printed no identities rather than a suite
// that lost all of them — reading either as a disappearance would convict every
// project whose runner is quiet on success. Same fail-safe direction as
// NewFailures, for the same reason.
//
// A NAME IS NOT A SUBJECT. The subtraction alone reports a rewritten check as a
// deleted one, and a run's whole job is often to rewrite checks: happy-dom's
// v4-flash s13 replaced four stubs with real ones under the identical describe
// path, and every repair round it bought re-raised the same removal finding
// against a tree the grader scored 9 of 9. See SplitReplaced and Replaced.
func (r Reading) Vanished() []string {
	removed, _ := r.vanishedSplit()
	return removed
}

// Replaced names the checks that stopped being reported and the checks that
// took their subject over, before to after.
//
// It is a record and never a finding: nothing follows from a rewritten check
// except that it was not a removed one. It is carried so an autopsy of a run
// that raised no removal finding can see WHY — a mechanism that silently
// declines to convict is indistinguishable from one that was never reached
// (FAILSAFE.md clause 4).
func (r Reading) Replaced() []Replacement {
	_, replaced := r.vanishedSplit()
	return replaced
}

// vanishedSplit is the one subtraction both readings above are halves of.
func (r Reading) vanishedSplit() (removed []string, replaced []Replacement) {
	if !r.comparable() {
		return nil, nil
	}
	before, after := r.Before.Reported, r.After.Reported
	if len(before) == 0 || len(after) == 0 {
		return nil, nil
	}
	return SplitReplaced(Subtract(before, after), after)
}
