package session

// ── THE LEDGER, ONE CALL AT A TIME: a visitor over exactly the lines a query matches ──
//
// [QueryUsageLedger] answers runs and calls, [QueryUsageReport] answers cells;
// a per-call export wants neither. It wants every ledger LINE the query matches,
// once, in the order they were recorded, with nothing grouped and nothing held.
// [EachUsageCall] is that and nothing more: one pass over a fixed-length prefix
// of the file, through the same [scanUsageLines] reader, the same
// [UsageQuery.matches] filter and the same checked [UsageLedgerTotals.count]
// that the page and the report use — so the same filter cannot select one set of
// lines here and another there.
//
// ONE LINE IS ONE CALL RECORD. A line from an older build may cover many
// provider requests ([UsageLine.Calls]); it stays one record whose Requests is
// what the line said, and it is never split into invented single calls.
//
// WHAT IT HOLDS IS ONE LINE. There is no slice of calls, no map of runs, and no
// set of anything it has met: the totals are a handful of counters, so the
// memory beside the scan does not grow with the history, only the time does.
// (That is a claim about what this function retains, not about the process's
// resident memory.)
//
// A FIGURE THAT CANNOT BE ADDED UP IS REFUSED, NEVER WRAPPED OR CUT. Besides
// everything [UsageLedgerTotals.count] refuses, a negative cost or a negative
// request count is refused here: the ledger totals would read the first as
// "unpriced" and the second as one request, and a per-call file must not print
// a figure its own line never held.
//
// IT TAKES THE SAME TWO SCAN SLOTS AS EVERY OTHER LEDGER READER, and a third
// reader is refused at once with [ErrUsageScanBusy] rather than queued.

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// UsageCall is one ledger line as a call record. Its strings are the line's own,
// trimmed; nothing is joined to it from today's agents, tasks or crews.
type UsageCall struct {
	// Offset is the line's byte offset in the ledger, which never moves because
	// the ledger is only ever appended to.
	Offset int64
	// At is the instant the call was journaled.
	At time.Time
	// Owner is the recorded root conversation, else the line's own session
	// ([UsageOwner]); empty where the line recorded neither.
	Owner string
	// Session, Root, Task and Run are the line's own ids.
	Session, Root, Task, Run string
	// Model is the provider's model id, and empty where nobody said.
	Model string
	// Requests is how many provider requests the line says it covers, never
	// less than one.
	Requests int
	// Input and Output are the line's tokens.
	Input, Output int64
	// Priced reports whether the line carried a figure. USD is meaningful only
	// then: a line with none was not free, it was not priced.
	Priced bool
	USD    float64
	// Unbilled marks a missing provider receipt.
	Unbilled bool
}

// UsageCallTotals is exactly the calls [EachUsageCall] handed over. It carries
// no run count: the pass does not keep the runs it meets, so it cannot say how
// many there were.
type UsageCallTotals struct {
	Records, Requests int
	Input, Output     int64
	// USD is every priced line; it is meaningful only where Priced.
	USD                float64
	Unpriced, Unbilled int
}

// Priced reports whether any line in the range carried a figure.
func (t UsageCallTotals) Priced() bool { return t.Records > t.Unpriced }

// EachUsageCall hands fn every ledger line the query matches, in LEDGER ORDER,
// and answers the totals of exactly the lines it handed over.
//
// The pass reads no further than the ledger's length when it opened, so a line
// appended meanwhile is the next question's. An error from fn, or a figure that
// cannot be added up, ends the pass at once and answers no totals. A ledger that
// is not there is a machine that has spent nothing: no calls, empty totals.
func EachUsageCall(path string, q UsageQuery, fn func(UsageCall) error) (UsageCallTotals, UsageScanStats, error) {
	q, err := q.normalized()
	if err != nil {
		return UsageCallTotals{}, UsageScanStats{}, err
	}
	file, done, err := openUsageScan(path)
	if err != nil {
		return UsageCallTotals{}, UsageScanStats{}, err
	}
	defer done()
	if file == nil {
		return UsageCallTotals{}, UsageScanStats{}, nil
	}
	info, err := file.Stat()
	if err != nil {
		return UsageCallTotals{}, UsageScanStats{}, err
	}
	var counted UsageLedgerTotals
	stats, err := scanUsageLines(file, info.Size(), func(offset int64, line UsageLine) error {
		line.Run = strings.TrimSpace(line.Run)
		if !q.matches(line) {
			return nil
		}
		if line.USD < 0 || line.Calls < 0 || math.IsNaN(line.USD) {
			return fmt.Errorf("%w (a call at byte %d has a negative or invalid figure)", ErrUsageLedgerFigures, offset)
		}
		if err := counted.count(line); err != nil {
			return err
		}
		return fn(UsageCall{
			Offset: offset, At: line.At, Owner: UsageOwner(line),
			Session: strings.TrimSpace(line.Session), Root: strings.TrimSpace(line.Root),
			Task: strings.TrimSpace(line.Task), Run: line.Run, Model: strings.TrimSpace(line.Model),
			Requests: max(line.Calls, 1), Input: int64(line.Input), Output: int64(line.Output),
			Priced: line.USD > 0, USD: line.USD, Unbilled: line.Unbilled,
		})
	})
	if err != nil {
		return UsageCallTotals{}, stats, err
	}
	return UsageCallTotals{
		Records: counted.Records, Requests: counted.Calls, Input: counted.Input, Output: counted.Output,
		USD: counted.USD, Unpriced: counted.Unpriced, Unbilled: counted.Unbilled,
	}, stats, nil
}
