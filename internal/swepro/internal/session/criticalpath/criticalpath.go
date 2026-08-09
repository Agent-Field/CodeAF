// Package criticalpath is a faithful Go port of swe-pro's
// src/session/critical-path.ts (commit 3b25a1a): a forward/backward CPM pass
// that reports total slack per PlanDB task plus the ids that sit on the
// critical path.
//
// The TS module is pure by design — no clock, no RNG, no I/O — so there is
// nothing to inject here. `durationOf` is the sole source of variability and
// the caller owns it.
//
// Fidelity notes (deliberate, do NOT "fix"):
//
//   - The empty-task fast path returns before ANY edge validation, so
//     AnalyzeCriticalPath(nil, edgesReferencingUnknownTasks, f) succeeds with
//     an empty analysis instead of raising CriticalPathInputError.
//     (critical-path.ts:46-48)
//
//   - Dependency.Kind is ignored: a "suggests" edge constrains the schedule
//     exactly as hard as a "blocks" edge. (critical-path.ts:77-89)
//
//   - The de-dup key is `from + "\x00" + to`, so two DIFFERENT edges whose ids
//     contain U+0000 can collide and the second is silently dropped.
//     (critical-path.ts:83)
//
//   - The cycle error carries every task left with a positive indegree, which
//     includes nodes merely DOWNSTREAM of the cycle, not only its members —
//     despite the message reading "cycle involves". (critical-path.ts:110)
//
//   - slackByTask is a JS plain object, so its JSON key order is
//     array-index-like ids in ascending numeric order FIRST and every other id
//     in insertion (tasks) order. SlackRecord reproduces that; it is
//     observable whenever a task id is a canonical non-negative integer
//     string. (critical-path.ts:139)
//
//   - The epsilon that snaps near-zero slack to exact zero SCALES with the
//     makespan (1e-9 * max(1, makespan)), so on a very large project a task
//     with genuinely non-zero slack is reported critical. (critical-path.ts:141)
//
//   - Durations must each be finite and non-negative, but their SUMS may
//     overflow to +Inf; the resulting Inf-Inf yields NaN slack, which
//     JSON.stringify emits as null and which is NOT critical (NaN === 0 is
//     false). A duration of -0 passes the `estimate < 0` guard.
package criticalpath

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
)

const criticalEpsilon = 1e-9 // W4-TODO(knobs)

// ── JSON.stringify-exact primitives ──────────────────────────────────────
//
// The analysis is hand-marshalled instead of leaning on encoding/json for two
// reasons: (a) slackByTask needs JS plain-object key order, which
// map[string]float64 cannot express, and (b) encoding/json escapes U+2028 /
// U+2029 inside strings unconditionally while JSON.stringify emits them raw —
// and task ids are arbitrary text.

const lowerHex = "0123456789abcdef"

// appendJSString writes s as a JSON string literal using ECMA-262
// QuoteJSONString rules: only ", \, the five short escapes and C0 controls are
// escaped (lowercase \u00xx), and every byte >= 0x80 is copied verbatim so
// multi-byte UTF-8 — including U+2028/U+2029 — survives unescaped.
func appendJSString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c != '"' && c != '\\' {
			continue
		}
		if start < i {
			buf.WriteString(s[start:i])
		}
		switch c {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			buf.WriteString(`\u00`)
			buf.WriteByte(lowerHex[c>>4])
			buf.WriteByte(lowerHex[c&0x0f])
		}
		start = i + 1
	}
	if start < len(s) {
		buf.WriteString(s[start:])
	}
	buf.WriteByte('"')
}

// jsNumberLiteral renders a float the way JSON.stringify renders a number:
// V8's shortest round-trip decimal, or null for NaN/±Infinity.
func jsNumberLiteral(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "null"
	}
	return jscompat.FormatNumber(f)
}

// isArrayIndexKey reports whether k is an "array index" property key per
// ES2023 6.1.7: a canonical numeric String whose value is in [0, 2^32-2].
// Those keys enumerate first, in ascending numeric order, ahead of every
// other string key.
func isArrayIndexKey(k string) bool {
	if k == "" || len(k) > 10 {
		return false
	}
	if k == "0" {
		return true
	}
	if k[0] < '1' || k[0] > '9' {
		return false
	}
	for i := 1; i < len(k); i++ {
		if k[i] < '0' || k[i] > '9' {
			return false
		}
	}
	n, err := strconv.ParseUint(k, 10, 64)
	if err != nil {
		return false
	}
	return n <= 4294967294
}

// ── SlackRecord: a JS plain object keyed by task id ──────────────────────

// SlackRecord reproduces the `Record<string, number>` the TS module builds for
// slackByTask. It is NOT a JS Map: ordinary objects enumerate array-index-like
// keys first in ascending numeric order and all remaining keys in insertion
// order, and that ordering reaches JSON.stringify output.
type SlackRecord struct {
	keys []string
	vals map[string]float64
}

// NewSlackRecord returns an empty record (JSON `{}`).
func NewSlackRecord() *SlackRecord {
	return &SlackRecord{vals: make(map[string]float64)}
}

// Set assigns k, keeping an existing key's original insertion position — the
// same as `obj[k] = v` in JS.
func (r *SlackRecord) Set(k string, v float64) {
	if _, ok := r.vals[k]; !ok {
		r.keys = append(r.keys, k)
	}
	r.vals[k] = v
}

// Get returns the slack recorded for k.
func (r *SlackRecord) Get(k string) (float64, bool) {
	if r == nil {
		return 0, false
	}
	v, ok := r.vals[k]
	return v, ok
}

// Len returns the number of keys.
func (r *SlackRecord) Len() int {
	if r == nil {
		return 0
	}
	return len(r.keys)
}

// Keys returns the keys in JS own-property enumeration order: array-index-like
// keys ascending, then the rest in insertion order. Mirrors Object.keys(obj).
func (r *SlackRecord) Keys() []string {
	if r == nil {
		return []string{}
	}
	indexKeys := make([]string, 0, len(r.keys))
	stringKeys := make([]string, 0, len(r.keys))
	for _, k := range r.keys {
		if isArrayIndexKey(k) {
			indexKeys = append(indexKeys, k)
		} else {
			stringKeys = append(stringKeys, k)
		}
	}
	// Canonical index keys are unique, so no comparator ties are possible;
	// SliceStable anyway, per the port's blanket JS-sort-is-stable rule.
	sort.SliceStable(indexKeys, func(i, j int) bool {
		a, _ := strconv.ParseUint(indexKeys[i], 10, 64)
		b, _ := strconv.ParseUint(indexKeys[j], 10, 64)
		return a < b
	})
	return append(indexKeys, stringKeys...)
}

// MarshalJSON emits the record exactly as JSON.stringify would.
func (r *SlackRecord) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	if r != nil {
		for i, k := range r.Keys() {
			if i > 0 {
				buf.WriteByte(',')
			}
			appendJSString(&buf, k)
			buf.WriteByte(':')
			buf.WriteString(jsNumberLiteral(r.vals[k]))
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// ── result + errors ──────────────────────────────────────────────────────

// CriticalPathAnalysis mirrors the TS interface. Field order is the JSON
// emission order of the `{ criticalIds, slackByTask, makespanEstimate }`
// object literal; the tags document that contract even though MarshalJSON
// below is what actually produces the bytes.
type CriticalPathAnalysis struct {
	CriticalIDs      []string     `json:"criticalIds"`
	SlackByTask      *SlackRecord `json:"slackByTask"`
	MakespanEstimate float64      `json:"makespanEstimate"`
}

// MarshalJSON produces byte-identical output to JSON.stringify(analysis).
func (a CriticalPathAnalysis) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"criticalIds":[`)
	for i, id := range a.CriticalIDs {
		if i > 0 {
			buf.WriteByte(',')
		}
		appendJSString(&buf, id)
	}
	buf.WriteString(`],"slackByTask":`)
	slack, err := a.SlackByTask.MarshalJSON()
	if err != nil {
		return nil, err
	}
	buf.Write(slack)
	buf.WriteString(`,"makespanEstimate":`)
	buf.WriteString(jsNumberLiteral(a.MakespanEstimate))
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// CriticalPathCycleError is the port of the TS class: a dependency cycle makes
// earliest/latest start times undefined.
type CriticalPathCycleError struct {
	// TaskIDs mirrors the readonly `taskIds` field. See the package doc: it is
	// every task with a leftover positive indegree, not only the cycle members.
	TaskIDs []string
	message string
}

// NewCriticalPathCycleError mirrors `new CriticalPathCycleError(taskIds)`.
func NewCriticalPathCycleError(taskIDs []string) *CriticalPathCycleError {
	return &CriticalPathCycleError{
		TaskIDs: taskIDs,
		message: "Critical-path analysis requires a DAG; cycle involves: " + joinComma(taskIDs),
	}
}

func (e *CriticalPathCycleError) Error() string { return e.message }

// Name mirrors `this.name = "CriticalPathCycleError"`.
func (e *CriticalPathCycleError) Name() string { return "CriticalPathCycleError" }

// CriticalPathInputError is the port of the TS class: invalid task IDs,
// dependency endpoints, or durations are rejected eagerly.
type CriticalPathInputError struct {
	message string
}

// NewCriticalPathInputError mirrors `new CriticalPathInputError(message)`.
func NewCriticalPathInputError(message string) *CriticalPathInputError {
	return &CriticalPathInputError{message: message}
}

func (e *CriticalPathInputError) Error() string { return e.message }

// Name mirrors `this.name = "CriticalPathInputError"`.
func (e *CriticalPathInputError) Name() string { return "CriticalPathInputError" }

// joinComma reproduces Array.prototype.join(", ").
func joinComma(parts []string) string {
	var buf bytes.Buffer
	for i, p := range parts {
		if i > 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(p)
	}
	return buf.String()
}

// ── the analysis ─────────────────────────────────────────────────────────

// AnalyzeCriticalPath computes project-level total slack with a
// forward/backward CPM pass.
//
// Every supplied dependency is treated as a precedence edge from FromTask to
// ToTask. Disconnected components share a virtual project start/end: their
// sources start at zero and their sinks are measured against the longest
// component's finish time.
func AnalyzeCriticalPath(
	tasks []*plandb.Task,
	edges []plandb.Dependency,
	durationOf func(task *plandb.Task) float64,
) (CriticalPathAnalysis, error) {
	if len(tasks) == 0 {
		return CriticalPathAnalysis{
			CriticalIDs:      []string{},
			SlackByTask:      NewSlackRecord(),
			MakespanEstimate: 0,
		}, nil
	}

	// Lookup-only maps: none of these is ever RANGED over, so a plain Go map
	// cannot leak nondeterministic order into the output. Adjacency lives in
	// slices, which preserve caller order exactly like the JS arrays.
	taskByID := make(map[string]*plandb.Task, len(tasks))
	predecessors := make(map[string][]string, len(tasks))
	successors := make(map[string][]string, len(tasks))
	indegree := make(map[string]int, len(tasks))
	duration := make(map[string]float64, len(tasks))

	for _, task := range tasks {
		if task.ID == "" {
			return CriticalPathAnalysis{}, NewCriticalPathInputError("Task IDs must be non-empty")
		}
		if _, ok := taskByID[task.ID]; ok {
			return CriticalPathAnalysis{}, NewCriticalPathInputError(fmt.Sprintf("Duplicate task ID: %s", task.ID))
		}

		estimate := durationOf(task)
		// !Number.isFinite(estimate) || estimate < 0. Note -0 passes: -0 < 0 is
		// false in both languages.
		if math.IsNaN(estimate) || math.IsInf(estimate, 0) || estimate < 0 {
			return CriticalPathAnalysis{}, NewCriticalPathInputError(
				fmt.Sprintf("Duration for task %s must be finite and non-negative", task.ID))
		}

		taskByID[task.ID] = task
		predecessors[task.ID] = []string{}
		successors[task.ID] = []string{}
		indegree[task.ID] = 0
		duration[task.ID] = estimate
	}

	// Deduplicate identical edges so repeated dependency rows do not create a
	// false cycle by incrementing indegree more than once.
	seenEdges := make(map[string]bool)
	for _, edge := range edges {
		from := edge.FromTask
		to := edge.ToTask
		_, hasFrom := taskByID[from]
		_, hasTo := taskByID[to]
		if !hasFrom || !hasTo {
			return CriticalPathAnalysis{}, NewCriticalPathInputError(
				fmt.Sprintf("Dependency references unknown task: %s -> %s", from, to))
		}
		edgeKey := from + "\u0000" + to
		if seenEdges[edgeKey] {
			continue
		}
		seenEdges[edgeKey] = true
		successors[from] = append(successors[from], to)
		predecessors[to] = append(predecessors[to], from)
		indegree[to] = indegree[to] + 1
	}

	// Kahn's algorithm supplies a deterministic topological order: initially
	// ready nodes and newly-ready siblings retain caller task/edge order.
	ready := []string{}
	for _, task := range tasks {
		if indegree[task.ID] == 0 {
			ready = append(ready, task.ID)
		}
	}

	topological := []string{}
	for cursor := 0; cursor < len(ready); cursor++ {
		id := ready[cursor]
		topological = append(topological, id)
		for _, next := range successors[id] {
			remaining := indegree[next] - 1
			indegree[next] = remaining
			if remaining == 0 {
				ready = append(ready, next)
			}
		}
	}

	if len(topological) != len(tasks) {
		unresolved := []string{}
		for _, task := range tasks {
			if indegree[task.ID] > 0 {
				unresolved = append(unresolved, task.ID)
			}
		}
		return CriticalPathAnalysis{}, NewCriticalPathCycleError(unresolved)
	}

	// Forward pass: ES(v) = max(EF(predecessors)), with ES(source) = 0.
	earliestStart := make(map[string]float64, len(tasks))
	makespanEstimate := 0.0
	for _, id := range topological {
		start := 0.0
		for _, predecessor := range predecessors[id] {
			start = math.Max(start, earliestStart[predecessor]+duration[predecessor])
		}
		earliestStart[id] = start
		makespanEstimate = math.Max(makespanEstimate, start+duration[id])
	}

	// Backward pass: LS(v) = min(LS(successors)) - duration(v). All sinks use
	// the global makespan, which gives shorter disconnected components slack.
	latestStart := make(map[string]float64, len(tasks))
	for i := len(topological) - 1; i >= 0; i-- {
		id := topological[i]
		next := successors[id]
		var latestFinish float64
		if len(next) == 0 {
			latestFinish = makespanEstimate
		} else {
			latestFinish = math.Inf(1)
			for _, successor := range next {
				latestFinish = math.Min(latestFinish, latestStart[successor])
			}
		}
		latestStart[id] = latestFinish - duration[id]
	}

	slackByTask := NewSlackRecord()
	criticalIDs := []string{}
	epsilon := criticalEpsilon * math.Max(1, makespanEstimate)
	for _, task := range tasks {
		rawSlack := latestStart[task.ID] - earliestStart[task.ID]
		slack := rawSlack
		if math.Abs(rawSlack) <= epsilon {
			slack = 0
		}
		slackByTask.Set(task.ID, slack)
		if slack == 0 {
			criticalIDs = append(criticalIDs, task.ID)
		}
	}

	return CriticalPathAnalysis{
		CriticalIDs:      criticalIDs,
		SlackByTask:      slackByTask,
		MakespanEstimate: makespanEstimate,
	}, nil
}
