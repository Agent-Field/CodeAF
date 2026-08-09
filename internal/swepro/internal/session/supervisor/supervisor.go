// Package supervisor ports src/session/supervisor.ts:1-166 from swe-pro commit
// 3b25a1a. The Effect-bound auditor reader is represented by the narrow
// VerdictReader seam used by ReadRatchetSnapshot.
package supervisor

import (
	"errors"
	"os"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/session/ledgers"
)

// VerdictStatus is the on-disk auditor verdict tag.
type VerdictStatus string

const (
	VerdictPass VerdictStatus = "pass"
	VerdictFail VerdictStatus = "fail"
)

// RatchetSnapshot is the objective, durable distance-from-done view.
type RatchetSnapshot struct {
	OpenBlockers    float64        `json:"openBlockers"`
	VerdictBlockers float64        `json:"verdictBlockers"`
	VerdictStatus   *VerdictStatus `json:"verdictStatus"`
}

// RatchetMetric is the scalar distance from done.
func RatchetMetric(snapshot RatchetSnapshot) float64 {
	return snapshot.OpenBlockers + snapshot.VerdictBlockers
}

// SnapshotIndicatesPass requires both a pass verdict and metric zero.
func SnapshotIndicatesPass(snapshot RatchetSnapshot) bool {
	return snapshot.VerdictStatus != nil &&
		*snapshot.VerdictStatus == VerdictPass &&
		RatchetMetric(snapshot) == 0
}

// DiskVerdict is the narrow projection supervisor reads from auditor-gate.
type DiskVerdict struct {
	Verdict  VerdictStatus
	Blockers []any
}

// VerdictReader is the unported auditor-gate readVerdictFile seam.
type VerdictReader interface {
	ReadVerdictFile(workspace string) (*DiskVerdict, error)
}

// VerdictReaderFunc adapts a function to VerdictReader.
type VerdictReaderFunc func(workspace string) (*DiskVerdict, error)

func (f VerdictReaderFunc) ReadVerdictFile(workspace string) (*DiskVerdict, error) {
	return f(workspace)
}

// ReadRatchetSnapshot combines the ported blocker ledger with the narrow
// auditor verdict reader. Wiring the full auditor-gate package is intentionally
// left to its own port.
func ReadRatchetSnapshot(workspace string, reader VerdictReader) (RatchetSnapshot, error) {
	if reader == nil {
		return RatchetSnapshot{}, errors.New("supervisor: verdict reader is required")
	}
	verdict, err := reader.ReadVerdictFile(workspace)
	if err != nil {
		return RatchetSnapshot{}, err
	}
	snapshot := RatchetSnapshot{OpenBlockers: float64(len(ledgers.LoadOpenBlockers(workspace)))}
	if verdict != nil {
		status := verdict.Verdict
		snapshot.VerdictStatus = &status
		snapshot.VerdictBlockers = float64(len(verdict.Blockers))
	}
	return snapshot, nil
}

// SupervisorDeps are the side-effect seams around the ratchet decision core.
type SupervisorDeps struct {
	ResumeOnce      func(attempt int) error
	Snapshot        func() (RatchetSnapshot, error)
	BudgetExhausted func() BudgetExhaustion
	Log             func(message string)
}

// BudgetExhaustion is the subset of runbudget.BudgetExhaustion used here.
type BudgetExhaustion struct {
	Yes    bool
	Reason *string
}

// SupervisorOutcome is one of the ratchet terminal states.
type SupervisorOutcome string

const (
	OutcomePassed          SupervisorOutcome = "passed"
	OutcomeBudgetExhausted SupervisorOutcome = "budget-exhausted"
	OutcomeNoProgress      SupervisorOutcome = "no-progress"
	OutcomeAttemptCap      SupervisorOutcome = "attempt-cap"
)

// SupervisorResult is RunSupervisor's terminal decision.
type SupervisorResult struct {
	Outcome  SupervisorOutcome `json:"outcome"`
	Attempts int               `json:"attempts"`
	Reason   string            `json:"reason"`
}

// DefaultMaxStalls is the number of consecutive stalled resumes tolerated.
const DefaultMaxStalls = 2

// SupervisorOptions are the optional ratchet bounds. Pointers preserve
// nullish-default behavior: explicit zero and fractional values are retained.
type SupervisorOptions struct {
	MaxStalls   *float64 `json:"maxStalls"`
	MaxAttempts *float64 `json:"maxAttempts"`
}

func format(n float64) string { return jscompat.FormatNumber(n) }

// RunSupervisor drives the pure ratchet policy. Dependency errors propagate
// like rejected promises from the TS implementation.
func RunSupervisor(deps SupervisorDeps, opts *SupervisorOptions) (SupervisorResult, error) {
	maxStalls := float64(DefaultMaxStalls)
	if opts != nil && opts.MaxStalls != nil {
		maxStalls = *opts.MaxStalls
	}
	log := deps.Log
	if log == nil {
		log = func(string) {}
	}

	prev, err := deps.Snapshot()
	if err != nil {
		return SupervisorResult{}, err
	}
	if SnapshotIndicatesPass(prev) {
		return SupervisorResult{
			Outcome: OutcomePassed, Attempts: 0,
			Reason: "goal already passing before any resume",
		}, nil
	}

	stalls, attempts := 0, 0
	for {
		budget := deps.BudgetExhausted()
		if budget.Yes {
			logReason := ""
			resultReason := "budget exhausted"
			if budget.Reason != nil {
				logReason = *budget.Reason
				resultReason = *budget.Reason
			}
			log("[supervisor] budget exhausted before resume " + format(float64(attempts+1)) +
				": " + logReason + "\n")
			return SupervisorResult{
				Outcome: OutcomeBudgetExhausted, Attempts: attempts, Reason: resultReason,
			}, nil
		}
		if opts != nil && opts.MaxAttempts != nil &&
			float64(attempts) >= *opts.MaxAttempts {
			return SupervisorResult{
				Outcome: OutcomeAttemptCap, Attempts: attempts,
				Reason: "reached maxAttempts " + format(*opts.MaxAttempts),
			}, nil
		}

		log("[supervisor] ratchet: resume attempt " + format(float64(attempts+1)) +
			" (distance-from-done=" + format(RatchetMetric(prev)) +
			", stalls=" + format(float64(stalls)) + "/" + format(maxStalls) + ")\n")
		if err := deps.ResumeOnce(attempts + 1); err != nil {
			return SupervisorResult{}, err
		}
		attempts++

		cur, err := deps.Snapshot()
		if err != nil {
			return SupervisorResult{}, err
		}
		if SnapshotIndicatesPass(cur) {
			log("[supervisor] ratchet: goal passed after " + format(float64(attempts)) +
				" resume(s) — done\n")
			return SupervisorResult{
				Outcome: OutcomePassed, Attempts: attempts,
				Reason: "goal passed after " + format(float64(attempts)) + " resume(s)",
			}, nil
		}

		prevMetric, curMetric := RatchetMetric(prev), RatchetMetric(cur)
		if curMetric < prevMetric {
			log("[supervisor] ratchet: objective progress " + format(prevMetric) +
				" → " + format(curMetric) + " — continuing\n")
			stalls = 0
		} else {
			stalls++
			log("[supervisor] ratchet: no objective progress (" + format(prevMetric) +
				" → " + format(curMetric) + "); stall " + format(float64(stalls)) +
				"/" + format(maxStalls) + "\n")
			if float64(stalls) >= maxStalls {
				return SupervisorResult{
					Outcome: OutcomeNoProgress, Attempts: attempts,
					Reason: "no objective progress across " + format(maxStalls) +
						" consecutive resumes",
				}, nil
			}
		}
		prev = cur
	}
}

// AutoResumeEnabled is default-on and disabled only by the exact string "0".
// A nil map reads the process environment; a non-nil empty map is explicit.
func AutoResumeEnabled(env map[string]string) bool {
	var value string
	if env == nil {
		value = os.Getenv("CODEAF_AUTORESUME")
	} else {
		value = env["CODEAF_AUTORESUME"]
	}
	return value != "0"
}
