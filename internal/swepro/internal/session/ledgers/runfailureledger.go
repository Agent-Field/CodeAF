package ledgers

// Port of src/session/run-failure-ledger.ts:1-107 — the process-local,
// per-root history of failed leaves and its model-visible prose digest.
//
// The store deliberately keeps pointers: JS stores the original failure
// object and getRecentFailures returns a shallow array copy, so mutating a
// returned object mutates the stored record too. Limits follow Array.slice's
// numeric coercion, including fractional, NaN, and infinite values.

import (
	"math"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// FailureBug is one bug entry carried by a LeafFailure.
type FailureBug struct {
	Severity *string            `json:"severity,omitempty"`
	File     *string            `json:"file,omitempty"`
	Line     *jscompat.JSNumber `json:"line,omitempty"`
	Detail   string             `json:"detail"`
}

// MarshalJSON emits the input/type declaration order and uses the ledgers'
// V8-compatible string quoting.
func (b FailureBug) MarshalJSON() ([]byte, error) {
	o := newJSObj()
	if b.Severity != nil {
		o.set("severity", stringVal(*b.Severity))
	}
	if b.File != nil {
		o.set("file", stringVal(*b.File))
	}
	if b.Line != nil {
		o.set("line", numberVal(float64(*b.Line)))
	}
	o.set("detail", stringVal(b.Detail))
	return objectVal(o).appendJSON(nil), nil
}

// LeafFailure mirrors the LeafFailure type.
type LeafFailure struct {
	TaskID      string             `json:"taskID"`
	Title       string             `json:"title"`
	Reason      string             `json:"reason"`
	Bugs        []FailureBug       `json:"bugs"`
	RepairHints []string           `json:"repairHints"`
	Confidence  *string            `json:"confidence,omitempty"`
	Attempt     *jscompat.JSNumber `json:"attempt,omitempty"`
	Timestamp   jscompat.JSNumber  `json:"timestamp"`
}

// MarshalJSON preserves property order, raw U+2028/U+2029, and JS numbers.
func (f LeafFailure) MarshalJSON() ([]byte, error) {
	o := newJSObj()
	o.set("taskID", stringVal(f.TaskID))
	o.set("title", stringVal(f.Title))
	o.set("reason", stringVal(f.Reason))
	bugs := make([]jsVal, 0, len(f.Bugs))
	for _, bug := range f.Bugs {
		b := newJSObj()
		if bug.Severity != nil {
			b.set("severity", stringVal(*bug.Severity))
		}
		if bug.File != nil {
			b.set("file", stringVal(*bug.File))
		}
		if bug.Line != nil {
			b.set("line", numberVal(float64(*bug.Line)))
		}
		b.set("detail", stringVal(bug.Detail))
		bugs = append(bugs, objectVal(b))
	}
	o.set("bugs", arrayVal(bugs))
	hints := make([]jsVal, 0, len(f.RepairHints))
	for _, hint := range f.RepairHints {
		hints = append(hints, stringVal(hint))
	}
	o.set("repairHints", arrayVal(hints))
	if f.Confidence != nil {
		o.set("confidence", stringVal(*f.Confidence))
	}
	if f.Attempt != nil {
		o.set("attempt", numberVal(float64(*f.Attempt)))
	}
	o.set("timestamp", numberVal(float64(f.Timestamp)))
	return objectVal(o).appendJSON(nil), nil
}

const (
	maxFailuresPerRoot = 50
	defaultDigestLimit = 8
)

var (
	failuresByRoot = map[string][]*LeafFailure{}
	nudgedByRoot   = map[string]map[string]bool{}
)

// RecordLeafFailure mirrors recordLeafFailure.
func RecordLeafFailure(rootTaskID string, failure *LeafFailure) {
	if rootTaskID == "" {
		return
	}
	existing := failuresByRoot[rootTaskID]
	existing = append(existing, failure)
	if len(existing) > maxFailuresPerRoot {
		existing = existing[len(existing)-maxFailuresPerRoot:]
	}
	failuresByRoot[rootTaskID] = existing
}

// MarkFailureNudged mirrors markFailureNudged and is idempotent.
func MarkFailureNudged(rootTaskID, taskID string) {
	if rootTaskID == "" || taskID == "" {
		return
	}
	set := nudgedByRoot[rootTaskID]
	if set == nil {
		set = map[string]bool{}
	}
	set[taskID] = true
	nudgedByRoot[rootTaskID] = set
}

// WasFailureNudged mirrors wasFailureNudged.
func WasFailureNudged(rootTaskID, taskID string) bool {
	return nudgedByRoot[rootTaskID][taskID]
}

func requestedFailureLimit(limit []float64) float64 {
	if len(limit) == 0 {
		return defaultDigestLimit
	}
	return limit[0]
}

// GetRecentFailures mirrors getRecentFailures. The returned slice is a new
// shallow slice and contains the original record pointers.
func GetRecentFailures(rootTaskID string, limit ...float64) []*LeafFailure {
	all := failuresByRoot[rootTaskID]
	n := requestedFailureLimit(limit)
	if float64(len(all)) <= n {
		return append([]*LeafFailure{}, all...)
	}
	start := jsSliceStart(float64(len(all))-n, len(all))
	return append([]*LeafFailure{}, all[start:]...)
}

func jsSliceStart(start float64, length int) int {
	switch {
	case math.IsNaN(start):
		return 0
	case math.IsInf(start, 1):
		return length
	case math.IsInf(start, -1):
		return 0
	}
	n := math.Trunc(start)
	if n < 0 {
		n += float64(length)
		if n < 0 {
			return 0
		}
	}
	if n > float64(length) {
		return length
	}
	return int(n)
}

// FailureCount mirrors failureCount.
func FailureCount(rootTaskID string) int {
	return len(failuresByRoot[rootTaskID])
}

// ClearLedger mirrors clearLedger. Deliberately only the failure history is
// deleted; the separate nudged set survives, exactly as in TS.
func ClearLedger(rootTaskID string) {
	delete(failuresByRoot, rootTaskID)
}

// RenderFailureDigest mirrors renderFailureDigest. Nil represents TS
// undefined when no failures are available.
func RenderFailureDigest(rootTaskID string, limit ...float64) *string {
	recent := GetRecentFailures(rootTaskID, limit...)
	if len(recent) == 0 {
		return nil
	}
	lines := []string{
		"Run failure ledger (" + jscompat.FormatNumber(float64(len(recent))) + " of " +
			jscompat.FormatNumber(float64(FailureCount(rootTaskID))) + " total leaf failures this run):",
	}
	for _, failure := range recent {
		lines = append(lines,
			"- "+failure.TaskID+" \""+truncateFailureText(failure.Title, 80)+"\": "+
				truncateFailureText(failure.Reason, 160),
		)
		bugCount := len(failure.Bugs)
		if bugCount > 3 {
			bugCount = 3
		}
		for _, bug := range failure.Bugs[:bugCount] {
			where := ""
			if bug.File != nil && *bug.File != "" {
				where = *bug.File
				if bug.Line != nil && jscompat.Truthy(float64(*bug.Line)) {
					where += ":" + jscompat.FormatNumber(float64(*bug.Line))
				}
			}
			prefix := ""
			if bug.Severity != nil && *bug.Severity != "" {
				prefix += "[" + *bug.Severity + "] "
			}
			if where != "" {
				prefix += where + " — "
			}
			lines = append(lines, "    bug: "+prefix+truncateFailureText(bug.Detail, 140))
		}
		hintCount := len(failure.RepairHints)
		if hintCount > 2 {
			hintCount = 2
		}
		for _, hint := range failure.RepairHints[:hintCount] {
			lines = append(lines, "    hint: "+truncateFailureText(hint, 140))
		}
	}
	lines = append(lines,
		"Use this history when replanning. Recurring failure patterns above suggest a structural fix (split tasks differently, add prerequisite work, change approach) rather than another retry of the same shape.",
	)
	out := strings.Join(lines, "\n")
	return &out
}

func truncateFailureText(s string, n int) string {
	if s == "" {
		return ""
	}
	if utf16Length(s) > n {
		return utf16SliceTo(s, n-1) + "…"
	}
	return s
}
