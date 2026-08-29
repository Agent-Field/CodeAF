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
}

// Regressed names the checks that were green before this work and are red after
// it, or nothing when there is no pair of readings to subtract.
func (r Reading) Regressed() []string {
	if !r.comparable() {
		return nil
	}
	// A REGRESSION IS A CHECK THAT PASSED BEFORE, and the only reading that has
	// to ask is one that was WIDENED. Where both halves ran the same command
	// over the same files, subtracting the two failing lists is exactly right
	// and always was: a runner that names its failures and nothing else says
	// nothing about its passes, and reading its silence as "that check did not
	// exist" would excuse every regression in every such project.
	//
	// The after reading takes in the check files the run itself wrote
	// (Strategy.WithOwnChecks), and those are a different matter: they did not
	// exist when the before reading was taken, so a brand-new failing test
	// would subtract out as a regression — the work convicted of breaking a
	// check that did not exist until it wrote it. A red new test is a leaf that
	// has not finished, which is what the acceptance finding is for.
	//
	// Two ways of recognising one, because runners name their checks
	// differently: the failure names one of the files this reading added
	// (pytest's `path::test`, go test's package path), or the before reading
	// kept a roster and that roster never named it. Only checks the widening
	// brought in are weighed this way; everything else subtracts as it always
	// did.
	broke := NewFailures(r.Before.Failing, r.After.Failing)
	added := r.After.Strategy.Widened
	if len(added) == 0 {
		return broke
	}
	reportedBefore := make(map[string]bool, len(r.Before.Reported))
	for _, name := range r.Before.Reported {
		reportedBefore[name] = true
	}
	var stood []string
	for _, name := range broke {
		if namesOneOf(name, added) {
			continue
		}
		if len(reportedBefore) > 0 && !reportedBefore[name] {
			continue
		}
		stood = append(stood, name)
	}
	return stood
}

// namesOneOf says a check's name is a check IN one of these files, which is how
// every runner this program reads spells the check it just ran: pytest's
// `tests/test_thing.py::test_case`, go test's package path, a TAP line that
// leads with the file. A runner that names neither the file nor a roster is one
// this cannot answer for, and there the roster rule above stands alone.
func namesOneOf(check string, files []string) bool {
	check = strings.TrimSpace(check)
	if check == "" {
		return false
	}
	for _, file := range files {
		if file != "" && strings.Contains(check, file) {
			return true
		}
	}
	return false
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
	return r.Taken && r.AfterTaken && !r.Partial &&
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

// Vanished names the checks the suite reported before this work and did not
// report after it — deleted, renamed, or skipped.
//
// It asks the question only when BOTH rosters named something. Two empty
// rosters subtract to nothing, which is arithmetic and not an acquittal, and a
// single empty one is a runner that printed no identities rather than a suite
// that lost all of them — reading either as a disappearance would convict every
// project whose runner is quiet on success. Same fail-safe direction as
// NewFailures, for the same reason.
func (r Reading) Vanished() []string {
	if !r.comparable() {
		return nil
	}
	before, after := r.Before.Reported, r.After.Reported
	if len(before) == 0 || len(after) == 0 {
		return nil
	}
	return Subtract(before, after)
}
