package subharness

import (
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/tui2/reltime"
)

// THE ONE LINE A LIST ROW SAYS ABOUT THE LAST TIME A PROGRAM RAN.
//
// A subharness has two doors — `/subharness`, which lists everything runnable
// here, and `/harness`, which lists the pages a design wrote — and until this
// file existed each door spelled the same fact its own way. One row read
// `2h ago · finished · $0.12` and the other read `last ok, 2h ago`, about the
// same program, on the same screen, five keystrokes apart. Two vocabularies for
// one fact is two things to learn, and the one that is wrong is whichever one
// the reader did not check.
//
// So the sentence lives here, where both doors can reach it, for the reason
// [Card] lives here: one rendering, every surface. What each door owns is the
// READING — where its history is kept — and what neither of them owns is the
// words.
//
// ── THE WORDS ──
//
// `finished` and `incomplete`, and nothing else. Those are two of the five
// sanctioned words for the state of work, and a run that did not finish is
// never called a failure on a row somebody is scanning: a person who declined
// at a gate did not break anything, and a row that said `failed` about them
// would be punishing the gate for working.
//
// THE EMPTINESS LAW REACHES EVERY PART OF IT. A program nobody has run draws
// NOTHING here — never `never run`, never `0 runs` — and a cost nobody reported
// is not drawn either, because a provider that published no figure left "nobody
// said" behind rather than "free".

// LastRun is the whole of what a list row needs about a run: when it was, how it
// ended, and what it cost. It is a reading rather than a record — the record is
// a trace beside the page (store.go) or a note beside the bundle
// (internal/substore's runs.go), and this is what both of those come back as.
type LastRun struct {
	// At is when the run happened. A zero time is a program nobody has run, and
	// it is the one case that draws nothing at all.
	At time.Time
	// Finished is whether the run reached the end it promised.
	Finished bool
	// CostUSD is what it cost, and zero is "nobody said" rather than "free".
	CostUSD float64
}

// TraceRun is a saved trace read as that reading.
//
// ONLY `ok` IS FINISHED. The other four statuses (exec.go) are a person saying
// no at a gate, a person taking the run over, a context that died, and a node
// that failed — none of which reached the end the program promised, and all of
// which are `incomplete` in the row's own two words.
//
// The cost is deliberately absent: a trace records the walk and not the ledger,
// so a row drawn from one says when and how and stops there.
func TraceRun(t Trace) LastRun {
	return LastRun{At: t.Started, Finished: t.status() == StatusOK}
}

// LastRunLine is the reading as one short line: when, how it went, and what it
// cost.
//
// The run's own sentence about what ran out is NOT on this line. It was written
// to be read in full, this is one dim line under a name, and a row that wrapped
// would cost the list the scannability the line exists for.
func LastRunLine(run LastRun, now time.Time) string {
	when := reltime.Short(run.At, now)
	if when == "" {
		return ""
	}
	parts := []string{when, "incomplete"}
	if run.Finished {
		parts[1] = "finished"
	}
	if run.CostUSD > 0 {
		parts = append(parts, LastRunCost(run.CostUSD))
	}
	return strings.Join(parts, " · ")
}

// LastRunCost is what a run cost, in the cells a dim row can spare. Cents while
// a run is cheap, so a program that spent a fraction of one is not drawn as
// though it had spent nothing — which is the same ladder the status line's own
// reading takes, minus its zero rung: a zero never reaches here, because a cost
// nobody reported is not drawn at all.
func LastRunCost(usd float64) string {
	if usd < 0.01 {
		return fmt.Sprintf("$%.4f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}
