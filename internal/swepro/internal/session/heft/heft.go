// Package heft is a bug-for-bug port of src/session/heft.ts — Heterogeneous
// Earliest Finish Time (HEFT) list scheduling for model-backed DAGs.
//
// Pure by design: the TS module reads no clock and no RNG (estimates come from
// the caller), so unlike internal/plandb there is deliberately NO injectable
// nowMillis/random hook here — adding one would be inventing surface the
// original does not have.
//
// Fidelity notes (deliberate, do not "fix"):
//   - Dependency edges are deduplicated through a Set keyed by
//     `${dependency}\u0000${taskID}` and estimates through a Map keyed by
//     `${taskID}\u0000${modelID}`. IDs are free-form strings, so an ID that
//     itself contains U+0000 can collide with a different (dependency, task) or
//     (task, model) pair: the duplicate edge is silently dropped and the later
//     estimate silently overwrites the earlier one. Ported verbatim; the
//     fixtures pin both collisions.
//   - The priority list is rebuilt by re-sorting a MUTATING ready set on every
//     iteration and shifting the head off. Array.prototype.sort is stable in
//     V8, and the comparator returns 0 for equal-rank IDs that collate equal
//     (e.g. "ab" vs "a\u0000b", since U+0000 is completely ignorable in the
//     root collation), so insertion order decides those ties —
//     sort.SliceStable, never sort.Slice.
//   - `rank[right] - rank[left] || left.localeCompare(right)` uses JS `||`
//     truthiness on a float: 0, -0 and NaN all fall through to the collation
//     tie-break. Reproduced literally.
//   - Every Map that the TS iterates is insertion-ordered
//     (jscompat.OrderedMap); Maps it only get/sets are plain Go maps, since
//     their order never reaches output.
//   - All output-bearing numbers are jscompat.JSNumber so an overflow to
//     ±Infinity (reachable: finishMs = startMs + durationMs with two ~1e308
//     durations) marshals as JSON null, exactly like JSON.stringify.
package heft

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/fixflag"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// ── exported shapes (JSON tags mirror the TS object-literal key order) ────

type HeftTask struct {
	ID   string   `json:"id"`
	Deps []string `json:"deps"`
}

// HeftModel mirrors `{ id: string; slots?: number }`. Slots is a pointer so
// "absent" and "explicit null" both arrive as nil, which is what `?? 1`
// collapses them to in TS.
type HeftModel struct {
	ID    string   `json:"id"`
	Slots *float64 `json:"slots"`
}

type HeftEstimate struct {
	DurationMs jscompat.JSNumber `json:"durationMs"`
	CostUsd    jscompat.JSNumber `json:"costUsd"`
}

type HeftOptions struct {
	Tasks    []HeftTask                                       `json:"tasks"`
	Models   []HeftModel                                      `json:"models"`
	Estimate func(taskID string, modelID string) HeftEstimate `json:"-"`
}

// CompareToUniformOptions mirrors the TS intersection type
// `HeftOptions & { uniformModelId: string }`, which has no TS name of its own.
type CompareToUniformOptions struct {
	HeftOptions
	UniformModelID string `json:"uniformModelId"`
}

type HeftAssignment struct {
	TaskID   string            `json:"taskId"`
	ModelID  string            `json:"modelId"`
	StartMs  jscompat.JSNumber `json:"startMs"`
	FinishMs jscompat.JSNumber `json:"finishMs"`
	CostUsd  jscompat.JSNumber `json:"costUsd"`
}

// HeftScheduleResult is the TS interface `HeftSchedule`.
//
// DIVERGENCE (naming only): TS exports both `interface HeftSchedule` and
// `function heftSchedule`, which collide in a single Go package namespace. The
// function keeps the mirrored name (HeftSchedule) and the interface takes the
// Result suffix. Field names, JSON tags and values are unchanged.
type HeftScheduleResult struct {
	Assignments  []HeftAssignment  `json:"assignments"`
	MakespanMs   jscompat.JSNumber `json:"makespanMs"`
	TotalCostUsd jscompat.JSNumber `json:"totalCostUsd"`
}

type UniformComparison struct {
	HeftMakespanMs    jscompat.JSNumber `json:"heftMakespanMs"`
	UniformMakespanMs jscompat.JSNumber `json:"uniformMakespanMs"`
	SavedMs           jscompat.JSNumber `json:"savedMs"`
}

// ── typed errors (the TS `class ... extends Error` pair) ─────────────────

// HeftCycleError is thrown when the task graph is not a DAG.
type HeftCycleError struct {
	TaskIDs []string
	message string
}

func NewHeftCycleError(taskIDs []string) *HeftCycleError {
	return &HeftCycleError{
		TaskIDs: taskIDs,
		message: fmt.Sprintf("HEFT scheduling requires a DAG; cycle involves: %s", strings.Join(taskIDs, ", ")),
	}
}

func (e *HeftCycleError) Error() string { return e.message }

// Name mirrors the TS `error.name` property.
func (e *HeftCycleError) Name() string { return "HeftCycleError" }

// HeftInputError is thrown for malformed scheduler input.
type HeftInputError struct {
	message string
}

func NewHeftInputError(message string) *HeftInputError {
	return &HeftInputError{message: message}
}

func (e *HeftInputError) Error() string { return e.message }

// Name mirrors the TS `error.name` property.
func (e *HeftInputError) Name() string { return "HeftInputError" }

// ── internals ────────────────────────────────────────────────────────────

type preparedInput struct {
	tasks      []HeftTask
	models     []HeftModel
	estimates  map[string]HeftEstimate
	rank       map[string]float64
	successors map[string][]string
	order      []string
}

type lane struct {
	availableAt float64
}

const defaultSlots = 1

// HeftSchedule computes upward ranks and schedules each task on the
// model/lane with the earliest append-only finish time. EFT ties are resolved
// by model ID.
func HeftSchedule(opts HeftOptions) (HeftScheduleResult, error) {
	prepared, err := prepare(opts)
	if err != nil {
		return HeftScheduleResult{}, err
	}
	return schedulePrepared(prepared, prepared.models)
}

// CompareToUniform compares HEFT with assigning every task to one selected model.
func CompareToUniform(opts CompareToUniformOptions) (UniformComparison, error) {
	prepared, err := prepare(opts.HeftOptions)
	if err != nil {
		return UniformComparison{}, err
	}
	heft, err := schedulePrepared(prepared, prepared.models)
	if err != nil {
		return UniformComparison{}, err
	}
	var uniformModel *HeftModel
	for i := range prepared.models {
		if prepared.models[i].ID == opts.UniformModelID {
			uniformModel = &prepared.models[i]
			break
		}
	}
	if uniformModel == nil {
		return UniformComparison{}, NewHeftInputError(fmt.Sprintf("Unknown uniform model: %s", opts.UniformModelID))
	}
	uniform, err := schedulePrepared(prepared, []HeftModel{*uniformModel})
	if err != nil {
		return UniformComparison{}, err
	}

	return UniformComparison{
		HeftMakespanMs:    heft.MakespanMs,
		UniformMakespanMs: uniform.MakespanMs,
		SavedMs:           jscompat.JSNumber(float64(uniform.MakespanMs) - float64(heft.MakespanMs)),
	}, nil
}

func prepare(opts HeftOptions) (*preparedInput, error) {
	if len(opts.Models) == 0 {
		return nil, NewHeftInputError("At least one model is required")
	}

	taskByID := jscompat.NewOrderedMap[string, HeftTask]()
	for _, task := range opts.Tasks {
		// `!task.id` — the only falsy string is "".
		if task.ID == "" {
			return nil, NewHeftInputError("Task IDs must be non-empty")
		}
		if taskByID.Has(task.ID) {
			return nil, NewHeftInputError(fmt.Sprintf("Duplicate task ID: %s", task.ID))
		}
		deps := make([]string, len(task.Deps))
		copy(deps, task.Deps)
		taskByID.Set(task.ID, HeftTask{ID: task.ID, Deps: deps})
	}

	modelByID := jscompat.NewOrderedMap[string, HeftModel]()
	for _, model := range opts.Models {
		if model.ID == "" {
			return nil, NewHeftInputError("Model IDs must be non-empty")
		}
		if modelByID.Has(model.ID) {
			return nil, NewHeftInputError(fmt.Sprintf("Duplicate model ID: %s", model.ID))
		}
		slots := float64(defaultSlots)
		if model.Slots != nil {
			slots = *model.Slots
		}
		if !isInteger(slots) || slots < 1 {
			return nil, NewHeftInputError(fmt.Sprintf("Slots for model %s must be a positive integer", model.ID))
		}
		if fixflag.Enabled("CODEAF_GO_FIX_HEFT_SLOTS_CAP") && slots > 4096 {
			return nil, NewHeftInputError("model slots out of range")
		}
		resolved := slots
		modelByID.Set(model.ID, HeftModel{ID: model.ID, Slots: &resolved})
	}

	predecessors := map[string][]string{}
	successors := map[string][]string{}
	indegree := map[string]float64{}
	for _, task := range taskByID.Values() {
		predecessors[task.ID] = []string{}
		successors[task.ID] = []string{}
		indegree[task.ID] = 0
	}

	// Duplicate dependency entries are one edge, matching the critical-path
	// module's treatment and preventing a repeated row from fabricating a cycle.
	seenEdges := map[string]bool{}
	for _, task := range taskByID.Values() {
		for _, dependency := range task.Deps {
			if !taskByID.Has(dependency) {
				return nil, NewHeftInputError(fmt.Sprintf("Dependency references unknown task: %s -> %s", dependency, task.ID))
			}
			edgeKey := dependency + "\u0000" + task.ID
			if seenEdges[edgeKey] {
				continue
			}
			seenEdges[edgeKey] = true
			predecessors[task.ID] = append(predecessors[task.ID], dependency)
			successors[dependency] = append(successors[dependency], task.ID)
			indegree[task.ID] = indegree[task.ID] + 1
		}
	}

	ready := []string{}
	for _, task := range taskByID.Values() {
		if indegree[task.ID] == 0 {
			ready = append(ready, task.ID)
		}
	}
	topological := []string{}
	for cursor := 0; cursor < len(ready); cursor++ {
		id := ready[cursor]
		topological = append(topological, id)
		for _, successor := range successors[id] {
			remaining := indegree[successor] - 1
			indegree[successor] = remaining
			if remaining == 0 {
				ready = append(ready, successor)
			}
		}
	}
	if len(topological) != taskByID.Len() {
		unresolved := []string{}
		for _, task := range taskByID.Values() {
			if indegree[task.ID] > 0 {
				unresolved = append(unresolved, task.ID)
			}
		}
		return nil, NewHeftCycleError(unresolved)
	}

	estimates := map[string]HeftEstimate{}
	for _, task := range taskByID.Values() {
		for _, model := range modelByID.Values() {
			estimate := opts.Estimate(task.ID, model.ID)
			if !isFinite(float64(estimate.DurationMs)) || float64(estimate.DurationMs) < 0 {
				return nil, NewHeftInputError(fmt.Sprintf("Duration for %s on %s must be finite and non-negative", task.ID, model.ID))
			}
			if !isFinite(float64(estimate.CostUsd)) || float64(estimate.CostUsd) < 0 {
				return nil, NewHeftInputError(fmt.Sprintf("Cost for %s on %s must be finite and non-negative", task.ID, model.ID))
			}
			estimates[key(task.ID, model.ID)] = HeftEstimate{DurationMs: estimate.DurationMs, CostUsd: estimate.CostUsd}
		}
	}

	rank := map[string]float64{}
	for index := len(topological) - 1; index >= 0; index-- {
		taskID := topological[index]
		meanDuration := meanModelDuration(taskID, modelByID.Values(), estimates)
		successorRank := 0.0
		for _, successor := range successors[taskID] {
			successorRank = math.Max(successorRank, rank[successor])
		}
		rank[taskID] = meanDuration + successorRank
	}

	// HEFT's priority list is rank-descending, with task ID as the tie-breaker.
	// Restricting each choice to ready tasks keeps zero-duration equal-rank
	// dependencies valid while preserving that priority among eligible tasks.
	remaining := map[string]int{}
	for _, task := range taskByID.Values() {
		remaining[task.ID] = len(predecessors[task.ID])
	}
	priorityReady := []string{}
	for _, task := range taskByID.Values() {
		if remaining[task.ID] == 0 {
			priorityReady = append(priorityReady, task.ID)
		}
	}
	order := []string{}
	for len(priorityReady) > 0 {
		sort.SliceStable(priorityReady, func(i, j int) bool {
			left, right := priorityReady[i], priorityReady[j]
			// JS: `rank.get(right)! - rank.get(left)! || left.localeCompare(right)`.
			// A difference of 0, -0 or NaN is falsy and falls through.
			difference := rank[right] - rank[left]
			if difference != 0 && !math.IsNaN(difference) {
				return difference < 0
			}
			return localeCompare(left, right) < 0
		})
		taskID := priorityReady[0]
		priorityReady = priorityReady[1:]
		order = append(order, taskID)
		for _, successor := range successors[taskID] {
			count := remaining[successor] - 1
			remaining[successor] = count
			if count == 0 {
				priorityReady = append(priorityReady, successor)
			}
		}
	}

	return &preparedInput{
		tasks:      taskByID.Values(),
		models:     modelByID.Values(),
		estimates:  estimates,
		rank:       rank,
		successors: successors,
		order:      order,
	}, nil
}

type selection struct {
	model     HeftModel
	laneIndex int
	startMs   float64
	finishMs  float64
	estimate  HeftEstimate
}

func schedulePrepared(prepared *preparedInput, allowedModels []HeftModel) (HeftScheduleResult, error) {
	lanes := map[string][]*lane{}
	for _, model := range allowedModels {
		count := defaultSlots
		if model.Slots != nil {
			count = int(*model.Slots)
		}
		modelLanes := make([]*lane, count)
		for i := range modelLanes {
			modelLanes[i] = &lane{availableAt: 0}
		}
		lanes[model.ID] = modelLanes
	}

	finishByTask := map[string]float64{}
	assignments := []HeftAssignment{}
	totalCostUsd := 0.0

	for _, taskID := range prepared.order {
		// `prepared.tasks.find(...)!` — order only ever holds IDs that are in
		// tasks, so the miss branch is unreachable in both implementations.
		var task HeftTask
		for _, candidate := range prepared.tasks {
			if candidate.ID == taskID {
				task = candidate
				break
			}
		}
		dependencyFinish := 0.0
		for _, dependency := range task.Deps {
			finish, ok := finishByTask[dependency]
			if !ok {
				return HeftScheduleResult{}, NewHeftInputError(fmt.Sprintf("Task order could not resolve dependency: %s -> %s", dependency, task.ID))
			}
			dependencyFinish = math.Max(dependencyFinish, finish)
		}

		var selected *selection
		for _, model := range allowedModels {
			modelLanes := lanes[model.ID]
			// reduce((best, lane, index) => lane.availableAt < modelLanes[best].availableAt ? index : best, 0)
			// — strict <, so the FIRST minimum wins; index 0 is compared with itself.
			laneIndex := 0
			for index, candidate := range modelLanes {
				if candidate.availableAt < modelLanes[laneIndex].availableAt {
					laneIndex = index
				}
			}
			estimate := prepared.estimates[key(task.ID, model.ID)]
			startMs := math.Max(dependencyFinish, modelLanes[laneIndex].availableAt)
			finishMs := startMs + float64(estimate.DurationMs)
			if selected == nil ||
				finishMs < selected.finishMs ||
				(finishMs == selected.finishMs && localeCompare(model.ID, selected.model.ID) < 0) {
				selected = &selection{model: model, laneIndex: laneIndex, startMs: startMs, finishMs: finishMs, estimate: estimate}
			}
		}

		choice := selected
		lanes[choice.model.ID][choice.laneIndex].availableAt = choice.finishMs
		finishByTask[task.ID] = choice.finishMs
		assignments = append(assignments, HeftAssignment{
			TaskID:   task.ID,
			ModelID:  choice.model.ID,
			StartMs:  jscompat.JSNumber(choice.startMs),
			FinishMs: jscompat.JSNumber(choice.finishMs),
			CostUsd:  choice.estimate.CostUsd,
		})
		totalCostUsd += float64(choice.estimate.CostUsd)
	}

	makespanMs := 0.0
	for _, assignment := range assignments {
		makespanMs = math.Max(makespanMs, float64(assignment.FinishMs))
	}

	return HeftScheduleResult{
		Assignments:  assignments,
		MakespanMs:   jscompat.JSNumber(makespanMs),
		TotalCostUsd: jscompat.JSNumber(totalCostUsd),
	}, nil
}

func meanModelDuration(taskID string, models []HeftModel, estimates map[string]HeftEstimate) float64 {
	sum := 0.0
	for _, model := range models {
		sum += float64(estimates[key(taskID, model.ID)].DurationMs)
	}
	return sum / float64(len(models))
}

func key(taskID string, modelID string) string {
	return taskID + "\u0000" + modelID
}

// isFinite is `Number.isFinite`.
func isFinite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// isInteger is `Number.isInteger`.
func isInteger(f float64) bool {
	return isFinite(f) && f == math.Trunc(f)
}
