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

import "time"

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
	Before Result `json:"before,omitzero"`
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
	if !r.Taken || !r.AfterTaken {
		return nil
	}
	return NewFailures(r.Before.Failing, r.After.Failing)
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
	if !r.Taken || !r.AfterTaken {
		return nil
	}
	before, after := r.Before.Reported, r.After.Reported
	if len(before) == 0 || len(after) == 0 {
		return nil
	}
	return Subtract(before, after)
}
