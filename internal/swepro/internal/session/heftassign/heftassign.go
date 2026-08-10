// Package heftassign is a bug-for-bug port of src/session/heft-assign.ts —
// HEFT-backed tier assignment for the ready-dispatch cycle (W4c).
//
// Bridges the pure HEFT scheduler (internal/session/heft) into the plandb
// scheduler's slack-pricing seam. HEFT plans the WHOLE remaining graph (not
// just the ready set) with lane contention: two lanes (HIGH/LOW tier), slots
// split from the live parallel window, per-task durations from observed leaf
// outcomes. Its output refines dispatch ORDER (upward-rank start times) and
// tier ASSIGNMENT for slack tasks.
//
// Quality invariants inherited from the slack-pricing rule — enforced here as
// hard constraints, never left to HEFT's tie-breaking:
//  1. Critical-path tasks are always assigned HIGH (HEFT's EFT tie-break by
//     model id could otherwise park them on the LOW lane).
//  2. A task the LOW tier cannot reliably take (capability tracker) is never
//     assigned LOW.
//
// Durations are modeled tier-symmetric on purpose: we have no per-tier
// wall-clock evidence, so assignment differences come from lane contention and
// graph structure — facts we know — not invented speed ratios.
//
// Fidelity notes (deliberate, do not "fix"):
//   - The module is pure: no clock, no RNG. There is therefore deliberately NO
//     injectable nowMillis/random hook here, unlike internal/plandb.
//   - ComputeHeftAssignments returns nil (the TS `null`) instead of an error on
//     cycles, degenerate input and identical lane ids. The TS wraps heftSchedule
//     in a bare `try { … } catch { return null }`, so EVERY throw out of the
//     scheduler collapses to null — including HeftInputError for a fractional /
//     NaN / Infinite parallel window, a NaN duration, or a negative cost per
//     second. Reproduced by mapping any error from heft.HeftSchedule to nil.
//   - Every JS Map/Set that the port carries is insertion-ordered: the result's
//     tier/startMs are jscompat.OrderedMap (the TS caller iterates
//     `[...result.tier.values()]`), and the per-task dependency Set is an
//     ordered set, because `[...deps]` feeds heft.HeftTask.Deps in edge-arrival
//     order and that order reaches the schedule through predecessors/successors.
//     The lookup-only Maps/Sets (ids, depsByTask, critical) are plain Go maps —
//     their order never reaches output.
//   - Slot arithmetic keeps the TS expression shape exactly
//     (`Math.max(1, window - highSlots)`, not a re-derived floor), because a
//     fractional or non-finite parallelWindow must reach heft's
//     Number.isInteger check with the same value the TS produces.
//   - RecencyWeightedMean returns NaN on empty input, exactly like the TS
//     `Number.NaN`; callers supply their own fallback.
//   - OrderByHeftStart uses sort.SliceStable, which is exact for every start the
//     module can produce. It diverges from the TS only for a caller-supplied NaN
//     start, where the comparator is non-transitive; see the comment on the sort
//     and knownDivergences in fixtures_test.go.
package heftassign

import (
	"math"
	"sort"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/heft"
)

// Tier is the TS string-literal union `"high" | "low"`. TS has no name for it;
// the Go port needs one because it is the value type of the result map.
type Tier string

const (
	TierHigh Tier = "high"
	TierLow  Tier = "low"
)

// RemainingTask is the anonymous TS shape `{ id: string }` used for
// HeftAssignInput.remaining. Go needs a named type; the JSON tag matches.
type RemainingTask struct {
	ID string `json:"id"`
}

// Edge is the anonymous TS shape `{ from_task: string; to_task: string }`.
type Edge struct {
	FromTask string `json:"from_task"`
	ToTask   string `json:"to_task"`
}

// HeftAssignInput mirrors the TS interface field-for-field and in declaration
// order. The two callbacks carry `json:"-"` because functions are not
// serializable; the two optional cost fields are pointers so that "absent"
// (TS undefined) is distinguishable from an explicit 0, which is what `??`
// requires.
type HeftAssignInput struct {
	// Remaining is every not-yet-terminal task in the plan (ready or not).
	Remaining []RemainingTask `json:"remaining"`
	// Edges are the feeds_into/blocks edges between remaining tasks.
	Edges         []Edge                      `json:"edges"`
	DurationMsFor func(taskID string) float64 `json:"-"`
	HighModelID   string                      `json:"highModelId"`
	LowModelID    string                      `json:"lowModelId"`
	// ParallelWindow is the current AIMD/scheduler parallel window; lanes split from it.
	ParallelWindow float64 `json:"parallelWindow"`
	// CriticalIDs are the task ids on the critical path — forced HIGH.
	CriticalIDs []string `json:"criticalIds"`
	// LowCanTake is the capability guard: may LOW reliably take this task?
	LowCanTake func(taskID string) bool `json:"-"`
	// HighCostPerSec / LowCostPerSec are the relative cost per second per lane
	// (informational only).
	HighCostPerSec *float64 `json:"highCostPerSec"`
	LowCostPerSec  *float64 `json:"lowCostPerSec"`
}

// HeftAssignResult mirrors the TS interface. Tier and StartMs are JS Maps, so
// they are insertion-ordered here too.
type HeftAssignResult struct {
	// Tier maps taskId -> lane, after invariant enforcement.
	Tier *jscompat.OrderedMap[string, Tier] `json:"tier"`
	// StartMs maps taskId -> planned start offset (ms) — earlier = dispatch first.
	StartMs    *jscompat.OrderedMap[string, jscompat.JSNumber] `json:"startMs"`
	MakespanMs jscompat.JSNumber                               `json:"makespanMs"`
}

// orderedStringSet is `new Set<string>()`: insertion-ordered, dedup on add.
type orderedStringSet struct {
	keys []string
	seen map[string]bool
}

func newOrderedStringSet() *orderedStringSet {
	return &orderedStringSet{seen: map[string]bool{}}
}

func (s *orderedStringSet) add(value string) {
	if s.seen[value] {
		return
	}
	s.seen[value] = true
	s.keys = append(s.keys, value)
}

// values is `[...set]`.
func (s *orderedStringSet) values() []string {
	out := make([]string, len(s.keys))
	copy(out, s.keys)
	return out
}

// ComputeHeftAssignments returns nil (caller falls back to slack-pricing)
// instead of throwing: on cycles, degenerate input, or identical lane model
// ids.
func ComputeHeftAssignments(input HeftAssignInput) *HeftAssignResult {
	remaining := input.Remaining
	edges := input.Edges
	durationMsFor := input.DurationMsFor
	highModelID := input.HighModelID
	lowModelID := input.LowModelID
	parallelWindow := input.ParallelWindow
	criticalIDs := input.CriticalIDs
	lowCanTake := input.LowCanTake

	if len(remaining) == 0 {
		return nil
	}
	// `!highModelId || !lowModelId` — the only falsy string is "".
	if highModelID == "" || lowModelID == "" || highModelID == lowModelID {
		return nil
	}

	ids := make(map[string]bool, len(remaining))
	for _, t := range remaining {
		ids[t.ID] = true
	}
	depsByTask := make(map[string]*orderedStringSet, len(remaining))
	// A duplicate id in `remaining` RESETS its dep set to empty here (Map.set
	// overwrite) and then makes heft.prepare reject the whole input with
	// "Duplicate task ID", which the catch turns into nil. Preserved.
	for _, t := range remaining {
		depsByTask[t.ID] = newOrderedStringSet()
	}
	for _, e := range edges {
		if !ids[e.FromTask] || !ids[e.ToTask] || e.FromTask == e.ToTask {
			continue
		}
		depsByTask[e.ToTask].add(e.FromTask)
	}

	window := math.Max(2, parallelWindow)
	highSlots := math.Max(1, math.Ceil(window/2))
	lowSlots := math.Max(1, window-highSlots)
	highCostPerSec := 3.0
	if input.HighCostPerSec != nil {
		highCostPerSec = *input.HighCostPerSec
	}
	lowCostPerSec := 1.0
	if input.LowCostPerSec != nil {
		lowCostPerSec = *input.LowCostPerSec
	}

	tasks := make([]heft.HeftTask, 0, len(remaining))
	for _, t := range remaining {
		tasks = append(tasks, heft.HeftTask{ID: t.ID, Deps: depsByTask[t.ID].values()})
	}

	// The TS `catch { return null }` is a blanket catch. In Go the scheduler
	// reports failure as an error and durationMsFor cannot throw, so every
	// reachable TS throw arrives here as a non-nil err.
	//
	// DIVERGENCE (unreachable from the TS surface): a Go callback that panics
	// is NOT recovered, where a JS callback that throws inside heftSchedule
	// would be swallowed into null.
	schedule, err := heft.HeftSchedule(heft.HeftOptions{
		Tasks: tasks,
		Models: []heft.HeftModel{
			{ID: highModelID, Slots: &highSlots},
			{ID: lowModelID, Slots: &lowSlots},
		},
		Estimate: func(taskID string, modelID string) heft.HeftEstimate {
			durationMs := math.Max(1, durationMsFor(taskID))
			perSec := lowCostPerSec
			if modelID == highModelID {
				perSec = highCostPerSec
			}
			return heft.HeftEstimate{
				DurationMs: jscompat.JSNumber(durationMs),
				CostUsd:    jscompat.JSNumber((durationMs / 1000) * perSec),
			}
		},
	})
	if err != nil {
		return nil
	}

	critical := make(map[string]bool, len(criticalIDs))
	for _, id := range criticalIDs {
		critical[id] = true
	}
	tier := jscompat.NewOrderedMap[string, Tier]()
	startMs := jscompat.NewOrderedMap[string, jscompat.JSNumber]()
	for _, a := range schedule.Assignments {
		lane := TierLow
		if a.ModelID == highModelID {
			lane = TierHigh
		}
		if critical[a.TaskID] {
			lane = TierHigh
		}
		if lane == TierLow && !lowCanTake(a.TaskID) {
			lane = TierHigh
		}
		tier.Set(a.TaskID, lane)
		startMs.Set(a.TaskID, a.StartMs)
	}
	return &HeftAssignResult{Tier: tier, StartMs: startMs, MakespanMs: schedule.MakespanMs}
}

// RecencyWeightedMean is a recency-weighted mean (Kalman-lite): later
// observations dominate so the estimate tracks capability/toolchain drift
// instead of averaging over a whole run's history.
// weight_i = 0.5^((n-1-i)/halfLife); with ~8 samples of half-life the newest
// sample counts ~2x the one a half-life older. Falls back to NaN on empty
// input (caller supplies its own fallback).
//
// halfLife is variadic to model the TS default parameter `halfLife = 8`:
// omitting it takes the default, and extra elements are ignored the way JS
// ignores extra arguments.
func RecencyWeightedMean(values []float64, halfLife ...float64) float64 {
	life := 8.0
	if len(halfLife) > 0 {
		life = halfLife[0]
	}
	if len(values) == 0 {
		return math.NaN()
	}
	sum := 0.0
	weightSum := 0.0
	for i := 0; i < len(values); i++ {
		weight := math.Pow(0.5, float64(len(values)-1-i)/life)
		sum += values[i] * weight
		weightSum += weight
	}
	return sum / weightSum
}

// IDed is the Go stand-in for the TS generic constraint `T extends { id: string }`.
// Go generics cannot require a struct FIELD, so the mirrored-arity
// OrderByHeftStart asks for a method instead; callers whose element type has a
// plain `ID` field use OrderByHeftStartBy.
type IDed interface {
	ID() string
}

// OrderByHeftStart is a stable reorder of the ready candidates by HEFT planned
// start time. Tasks HEFT did not schedule keep their relative position at the
// end of their original order (stable sort, missing = +Infinity).
func OrderByHeftStart[T IDed](candidates []T, startMs *jscompat.OrderedMap[string, jscompat.JSNumber]) []T {
	return OrderByHeftStartBy(candidates, startMs, func(candidate T) string { return candidate.ID() })
}

// OrderByHeftStartBy is OrderByHeftStart with an explicit id accessor. It has
// no TS counterpart — it exists only because Go cannot express `T extends
// { id: string }` — and carries the entire implementation.
func OrderByHeftStartBy[T any](candidates []T, startMs *jscompat.OrderedMap[string, jscompat.JSNumber], idOf func(T) string) []T {
	type entry struct {
		task  T
		index int
		start float64
	}
	entries := make([]entry, 0, len(candidates))
	for index, task := range candidates {
		start := math.Inf(1)
		if value, ok := startMs.Get(idOf(task)); ok {
			start = float64(value)
		}
		entries = append(entries, entry{task: task, index: index, start: start})
	}
	// `(a, b) => (a.start === b.start ? a.index - b.index : a.start - b.start)`.
	// The index tie-break makes the comparator a strict total order for every
	// finite/±Infinity start, so any stable sort reproduces the TS exactly.
	//
	// KNOWN DIVERGENCE, NaN starts only: `NaN === NaN` is false and `NaN - x` is
	// NaN, which SortCompare coerces to +0 ("equal"), so a NaN start compares
	// equal to everything while its neighbours stay strictly ordered — the
	// comparator stops being transitive and the result becomes a readout of the
	// host engine's sort internals (bun = JavaScriptCore), which sort.SliceStable
	// does not reproduce. Unreachable from ComputeHeftAssignments, whose starts
	// are always finite; see knownDivergences in fixtures_test.go.
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.start == b.start {
			return a.index < b.index
		}
		return a.start < b.start
	})
	out := make([]T, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.task)
	}
	return out
}
