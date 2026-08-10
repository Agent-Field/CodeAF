// This file ports src/session/plandb-scheduler.ts:615-628, 887-985,
// 1568-1602, and 1853-2088 from swe-pro (commit 3b25a1a).
package scheduler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/capability"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/criticalpath"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/cutpolicy"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/heftassign"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/leafoutcome"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/policyline"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/sizeband"
)

type nativePlanDBRunner struct{}

func (nativePlanDBRunner) Run(argv []string) plandb.RunResult {
	return plandb.RunPlanDB(argv)
}

// hasOwnTagProperty reproduces Object.prototype.hasOwnProperty.call(tags,
// name), including array index keys and the non-enumerable own "length"
// property. Looking for a tag VALUE in []string therefore never succeeds.
func hasOwnTagProperty(tags any, name string) bool {
	if tags == nil {
		return false
	}
	switch value := tags.(type) {
	case []string:
		return arrayHasOwnProperty(len(value), name)
	case []any:
		return arrayHasOwnProperty(len(value), name)
	case string:
		return arrayHasOwnProperty(len(utf16.Encode([]rune(value))), name)
	case map[string]any:
		_, ok := value[name]
		return ok
	case map[string]string:
		_, ok := value[name]
		return ok
	case map[string]*string:
		_, ok := value[name]
		return ok
	default:
		return false
	}
}

func arrayHasOwnProperty(length int, name string) bool {
	if name == "length" {
		return true
	}
	index, err := strconv.Atoi(name)
	if err != nil || index < 0 || index >= length {
		return false
	}
	return strconv.Itoa(index) == name
}

func suggestedAgent(task *plandb.Task) string {
	if task != nil {
		for _, tag := range task.Tags {
			if strings.HasPrefix(tag, "agent:") {
				name := jscompat.Trim(tag[len("agent:"):])
				if name != "" {
					return name
				}
			}
		}
		if agent := policyline.PolicyLine(task.Description, "agent"); agent != nil && *agent != "" {
			if !strings.HasPrefix(*agent, "explore:") && !strings.HasPrefix(*agent, "orchestrator") {
				return *agent
			}
		}
		if role := policyline.PolicyLine(task.Description, "task_role"); role != nil {
			switch *role {
			case "probe", "research":
				return "explore"
			case "implementation", "review", "qa", "security":
				return "fixer"
			case "architecture":
				return "oracle"
			}
		}
	}
	return "fixer"
}

// isAutoDispatchable keeps KB-3: the auto tag is looked up as an array
// property rather than an element, then suggestedAgent always falls through
// to "fixer." Consequently this function always returns true.
func isAutoDispatchable(task *plandb.Task) bool {
	var tags any
	if task != nil {
		tags = task.Tags
	}
	if hasOwnTagProperty(tags, schedulerAutoTag) {
		return true
	}
	return suggestedAgent(task) != ""
}

func sameNullableString(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func isDuplicateTask(task *plandb.Task, tasks []*plandb.Task) string {
	if task == nil {
		return ""
	}
	sameGroup := make([]*plandb.Task, 0, len(tasks))
	for _, other := range tasks {
		if other == nil ||
			other.Title != task.Title ||
			!sameNullableString(other.ParentTaskID, task.ParentTaskID) ||
			!sameNullableString(other.Description, task.Description) {
			continue
		}
		sameGroup = append(sameGroup, other)
	}
	if len(sameGroup) <= 1 {
		return ""
	}
	sort.SliceStable(sameGroup, func(i, j int) bool {
		return jscompat.LocaleCompare(sameGroup[i].ID, sameGroup[j].ID) < 0
	})
	if sameGroup[0].ID == task.ID {
		return ""
	}
	return sameGroup[0].ID
}

func descendantTaskIDs(rootTaskID string, allTasks []*plandb.Task) (map[string]struct{}, []string) {
	descendants := map[string]struct{}{}
	ordered := []string{}
	queue := []string{rootTaskID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, task := range allTasks {
			if task == nil || task.ParentTaskID == nil || *task.ParentTaskID != id {
				continue
			}
			if _, seen := descendants[task.ID]; seen {
				continue
			}
			descendants[task.ID] = struct{}{}
			ordered = append(ordered, task.ID)
			queue = append(queue, task.ID)
		}
	}
	return descendants, ordered
}

type candidateScan struct {
	Candidates    []*plandb.Task
	Skipped       []SkippedTask
	Descendants   map[string]struct{}
	DescendantIDs []string
	Scanned       int
}

// scanCandidates ports the ready-list filter at
// plandb-scheduler.ts:1853-1927. Duplicate detection intentionally receives
// only the ready list, not allTasks.
func scanCandidates(rootTaskID string, readyTasks, allTasks []*plandb.Task) candidateScan {
	descendants, descendantIDs := descendantTaskIDs(rootTaskID, allTasks)
	result := candidateScan{
		Candidates:    []*plandb.Task{},
		Skipped:       []SkippedTask{},
		Descendants:   descendants,
		DescendantIDs: descendantIDs,
		Scanned:       len(readyTasks),
	}
	for _, task := range readyTasks {
		if task == nil || task.ID == "" {
			continue
		}
		if duplicateOf := isDuplicateTask(task, readyTasks); duplicateOf != "" {
			result.Skipped = append(result.Skipped, SkippedTask{
				TaskID: task.ID,
				Reason: "duplicate of " + duplicateOf,
			})
			continue
		}
		if _, ok := descendants[task.ID]; !ok {
			continue
		}
		if task.IsComposite {
			result.Skipped = append(result.Skipped, SkippedTask{TaskID: task.ID, Reason: "composite"})
			continue
		}
		if !isAutoDispatchable(task) {
			result.Skipped = append(result.Skipped, SkippedTask{TaskID: task.ID, Reason: "not auto-dispatchable"})
			continue
		}
		if suggestedAgent(task) == "" {
			result.Skipped = append(result.Skipped, SkippedTask{TaskID: task.ID, Reason: "no suggested agent"})
			continue
		}
		result.Candidates = append(result.Candidates, task)
	}
	return result
}

// prioritizeReadyTasks ports plandb-scheduler.ts:891-946. It deliberately
// decodes the real critical-path and bottleneck outputs into the wrong shapes.
// The resulting rank keys are [1, 0, originalIndex], so the output is FIFO.
func prioritizeReadyTasks(
	runner planDBRunner,
	cwd string,
	dbPath string,
	projectID string,
	candidates []*plandb.Task,
) []*plandb.Task {
	if len(candidates) <= 1 {
		return candidates
	}
	if runner == nil {
		runner = nativePlanDBRunner{}
	}
	// cwd and dbPath are intentionally ignored by the bridge.
	_, _ = cwd, dbPath
	criticalPathResult := runner.Run([]string{"plandb", "critical-path", "--project", projectID, "--json"})
	bottleneckResult := runner.Run([]string{"plandb", "bottlenecks", "--project", projectID, "--limit", "20", "--json"})

	criticalIDs := map[string]struct{}{}
	var wrongCriticalShape struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(criticalPathResult.Stdout, &wrongCriticalShape); err == nil && wrongCriticalShape.Path != "" {
		for _, id := range strings.FieldsFunc(wrongCriticalShape.Path, func(r rune) bool { return r == '>' }) {
			id = jscompat.Trim(id)
			if id != "" {
				criticalIDs[id] = struct{}{}
			}
		}
	}

	bottleneckScore := map[string]float64{}
	var wrongBottleneckShape []struct {
		TaskID          string  `json:"task_id"`
		DownstreamCount float64 `json:"downstream_count"`
	}
	if err := json.Unmarshal(bottleneckResult.Stdout, &wrongBottleneckShape); err == nil {
		for _, row := range wrongBottleneckShape {
			if row.TaskID != "" {
				bottleneckScore[row.TaskID] = row.DownstreamCount
			}
		}
	}

	type rankedTask struct {
		task       *plandb.Task
		onPath     int
		downstream float64
		index      int
	}
	ranked := make([]rankedTask, 0, len(candidates))
	for i, task := range candidates {
		onPath := 1
		if task != nil {
			if _, ok := criticalIDs[task.ID]; ok {
				onPath = 0
			}
		}
		score := 0.0
		if task != nil {
			score = -bottleneckScore[task.ID]
		}
		ranked = append(ranked, rankedTask{task: task, onPath: onPath, downstream: score, index: i})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if a.onPath != b.onPath {
			return a.onPath < b.onPath
		}
		if a.downstream != b.downstream {
			return a.downstream < b.downstream
		}
		return a.index < b.index
	})
	out := make([]*plandb.Task, 0, len(ranked))
	for _, row := range ranked {
		out = append(out, row.task)
	}
	return out
}

// DepResults is fetchDepResults' return object.
type DepResults struct {
	Lines []string `json:"lines"`
	FanIn float64  `json:"fanIn"`
}

// fetchDepResults keeps KB-5 by asking task overview for fields it never
// returns. The complete intended path remains implemented for an injected
// matching shape.
func fetchDepResults(
	runner planDBRunner,
	cwd string,
	dbPath string,
	projectID string,
	taskID string,
	limitPerDep ...int,
) DepResults {
	if runner == nil {
		runner = nativePlanDBRunner{}
	}
	_, _ = cwd, dbPath
	limit := 1200
	if len(limitPerDep) > 0 {
		limit = limitPerDep[0]
	}
	overview := runner.Run([]string{"plandb", "task", "overview", "--project", projectID, "--json"})
	var data struct {
		Dependencies []struct {
			Kind     string `json:"kind"`
			ToTask   string `json:"to_task"`
			FromTask string `json:"from_task"`
		} `json:"dependencies"`
		Tasks []map[string]any `json:"tasks"`
	}
	decoder := json.NewDecoder(bytes.NewReader(overview.Stdout))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil {
		return DepResults{Lines: []string{}, FanIn: 0}
	}
	tasksByID := map[string]map[string]any{}
	for _, task := range data.Tasks {
		if id, ok := task["id"].(string); ok {
			tasksByID[id] = task
		}
	}
	feedsInto := make([]struct {
		Kind     string `json:"kind"`
		ToTask   string `json:"to_task"`
		FromTask string `json:"from_task"`
	}, 0, len(data.Dependencies))
	for _, dependency := range data.Dependencies {
		if dependency.Kind == "feeds_into" && dependency.ToTask == taskID && dependency.FromTask != "" {
			feedsInto = append(feedsInto, dependency)
		}
	}
	if len(feedsInto) == 0 {
		return DepResults{Lines: []string{}, FanIn: 0}
	}
	lines := []string{}
	for i, dependency := range feedsInto {
		if i >= 6 {
			break
		}
		upstream := tasksByID[dependency.FromTask]
		result := upstream["result"]
		if result == nil {
			result = upstream["last_result"]
		}
		if !jsTruthy(result) {
			continue
		}
		resultText, ok := result.(string)
		if !ok {
			encoded, err := jscompat.Stringify(result)
			if err != nil {
				continue
			}
			resultText = string(encoded)
		}
		lines = append(lines, "- [from "+dependency.FromTask+"] "+compactJS(resultText, limit))
	}
	return DepResults{Lines: lines, FanIn: float64(len(feedsInto))}
}

func jsTruthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case float64:
		return v != 0 && !math.IsNaN(v)
	case json.Number:
		number, err := v.Float64()
		return err == nil && number != 0 && !math.IsNaN(number)
	default:
		return true
	}
}

func compactJS(text string, limit int) string {
	units := utf16.Encode([]rune(text))
	if len(units) <= limit {
		return text
	}
	end := limit - 3
	if end < 0 {
		end = len(units) + end
		if end < 0 {
			end = 0
		}
	}
	if end > len(units) {
		end = len(units)
	}
	return string(utf16.Decode(units[:end])) + "..."
}

func modelRefOf(fullID string) capability.ModelRef {
	slash := strings.Index(fullID, "/")
	if slash <= 0 {
		return capability.ModelRef{ProviderID: "", ModelID: fullID}
	}
	return capability.ModelRef{ProviderID: fullID[:slash], ModelID: fullID[slash+1:]}
}

func taskBand(task *plandb.Task) sizeband.SizeBand {
	if task == nil {
		return sizeband.EstimateSizeBand(sizeband.EstimateSizeBandInput{})
	}
	description := ""
	if task.Description != nil {
		description = *task.Description
	}
	return sizeband.EstimateSizeBand(sizeband.EstimateSizeBandInput{
		Description: description,
		Tags:        task.Tags,
	})
}

type fileLeafOutcomeReader struct{}

func (fileLeafOutcomeReader) ReadLeafOutcomes(workspace string) []leafoutcome.LeafOutcome {
	raw, err := os.ReadFile(filepath.Join(workspace, ".codeaf", "outcomes.jsonl"))
	if err != nil {
		return []leafoutcome.LeafOutcome{}
	}
	outcomes := []leafoutcome.LeafOutcome{}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), len(raw)+1)
	for scanner.Scan() {
		var outcome leafoutcome.LeafOutcome
		if err := json.Unmarshal(scanner.Bytes(), &outcome); err != nil {
			continue
		}
		if outcome.TaskID != "" && outcome.Model.ModelID != "" {
			outcomes = append(outcomes, outcome)
		}
	}
	return outcomes
}

type candidateOrderingOptions struct {
	AdaptiveCuts   bool
	HeftEnabled    bool
	ParallelWindow float64
	Workspace      string
	ProjectID      string
	AllTasks       []*plandb.Task
	Descendants    map[string]struct{}
	PlanDB         planDBRunner
	Pools          modelPoolResolver
	Tracker        capabilityReader
	Outcomes       leafOutcomeReader
}

var fallbackDuration = map[sizeband.SizeBand]float64{
	sizeband.BandXS: 1_000,
	sizeband.BandS:  5_000,
	sizeband.BandM:  15_000,
	sizeband.BandL:  30_000,
	sizeband.BandXL: 60_000,
}

// orderCandidatesByPlan ports the HEFT/slack pricing block at
// plandb-scheduler.ts:1942-2088. task overview's wrong shape deliberately
// supplies an empty edge set (KB-5).
func orderCandidatesByPlan(
	prioritized []*plandb.Task,
	options candidateOrderingOptions,
) ([]*plandb.Task, []ModelAssignment) {
	ordered := append([]*plandb.Task(nil), prioritized...)
	assignments := []ModelAssignment{}
	if !options.AdaptiveCuts || len(prioritized) < 2 ||
		options.Pools == nil || options.Tracker == nil {
		return ordered, assignments
	}
	highPool := options.Pools.CandidatesForTier(ModelTierHigh)
	lowPool := options.Pools.CandidatesForTier(ModelTierLow)
	if len(highPool) == 0 || len(lowPool) == 0 {
		return ordered, assignments
	}

	remaining := make([]*plandb.Task, 0, len(options.AllTasks))
	remainingIDs := map[string]struct{}{}
	for _, task := range options.AllTasks {
		if task == nil {
			continue
		}
		if _, ok := options.Descendants[task.ID]; !ok {
			continue
		}
		switch task.Status {
		case plandb.StatusDone, plandb.StatusDonePartial, plandb.StatusFailed, plandb.StatusCancelled:
			continue
		}
		remaining = append(remaining, task)
		remainingIDs[task.ID] = struct{}{}
	}

	runner := options.PlanDB
	if runner == nil {
		runner = nativePlanDBRunner{}
	}
	overview := runner.Run([]string{"plandb", "task", "overview", "--project", options.ProjectID, "--json"})
	var wrongOverview struct {
		Dependencies []plandb.Dependency `json:"dependencies"`
	}
	_ = json.Unmarshal(overview.Stdout, &wrongOverview)
	edges := make([]plandb.Dependency, 0, len(wrongOverview.Dependencies))
	for _, edge := range wrongOverview.Dependencies {
		if edge.Kind != plandb.DepFeedsInto && edge.Kind != plandb.DepBlocks {
			continue
		}
		_, fromOK := remainingIDs[edge.FromTask]
		_, toOK := remainingIDs[edge.ToTask]
		if fromOK && toOK {
			edges = append(edges, edge)
		}
	}

	outcomeReader := options.Outcomes
	if outcomeReader == nil {
		outcomeReader = fileLeafOutcomeReader{}
	}
	wallByBand := map[sizeband.SizeBand][]float64{}
	for _, outcome := range outcomeReader.ReadLeafOutcomes(options.Workspace) {
		wall := float64(outcome.WallMs)
		if !math.IsNaN(wall) && !math.IsInf(wall, 0) && wall > 0 {
			wallByBand[outcome.SizeBand] = append(wallByBand[outcome.SizeBand], wall)
		}
	}
	durationFor := func(task *plandb.Task) float64 {
		band := taskBand(task)
		weighted := heftassign.RecencyWeightedMean(wallByBand[band])
		if !math.IsNaN(weighted) && !math.IsInf(weighted, 0) {
			return weighted
		}
		return fallbackDuration[band]
	}

	analysis, err := criticalpath.AnalyzeCriticalPath(remaining, edges, durationFor)
	if err != nil {
		return ordered, assignments
	}
	highModel := modelRefOf(highPool[0].ID)
	lowModel := modelRefOf(lowPool[0].ID)
	lowCanTakeTask := func(task *plandb.Task) bool {
		return cutpolicy.BandToIndex(options.Tracker.MaxReliableBand(lowModel)) >=
			cutpolicy.BandToIndex(taskBand(task))
	}
	byID := map[string]*plandb.Task{}
	heftRemaining := make([]heftassign.RemainingTask, 0, len(remaining))
	for _, task := range remaining {
		byID[task.ID] = task
		heftRemaining = append(heftRemaining, heftassign.RemainingTask{ID: task.ID})
	}
	heftEdges := make([]heftassign.Edge, 0, len(edges))
	for _, edge := range edges {
		heftEdges = append(heftEdges, heftassign.Edge{FromTask: edge.FromTask, ToTask: edge.ToTask})
	}

	var heft *heftassign.HeftAssignResult
	if options.HeftEnabled {
		heft = heftassign.ComputeHeftAssignments(heftassign.HeftAssignInput{
			Remaining: heftRemaining,
			Edges:     heftEdges,
			DurationMsFor: func(id string) float64 {
				if task := byID[id]; task != nil {
					return durationFor(task)
				}
				return fallbackDuration[sizeband.BandM]
			},
			HighModelID:    highModel.ModelID,
			LowModelID:     lowModel.ModelID,
			ParallelWindow: options.ParallelWindow,
			CriticalIDs:    analysis.CriticalIDs,
			LowCanTake: func(id string) bool {
				if task := byID[id]; task != nil {
					return lowCanTakeTask(task)
				}
				return false
			},
		})
	}

	if heft != nil {
		for _, task := range prioritized {
			if task == nil {
				continue
			}
			lane, ok := heft.Tier.Get(task.ID)
			if !ok {
				continue
			}
			duration := durationFor(task)
			slack, ok := analysis.SlackByTask.Get(task.ID)
			if !ok {
				slack = 0
			}
			if lane == heftassign.TierLow && !(slack > duration) {
				continue
			}
			preferred := lowModel
			if lane == heftassign.TierHigh {
				preferred = highModel
			}
			assignments = append(assignments, ModelAssignment{TaskID: task.ID, Model: preferred})
		}
		ordered = heftassign.OrderByHeftStartBy(
			prioritized,
			heft.StartMs,
			func(task *plandb.Task) string {
				if task == nil {
					return ""
				}
				return task.ID
			},
		)
		return ordered, assignments
	}

	critical := map[string]struct{}{}
	for _, id := range analysis.CriticalIDs {
		critical[id] = struct{}{}
	}
	for _, task := range prioritized {
		if task == nil {
			continue
		}
		duration := durationFor(task)
		slack, ok := analysis.SlackByTask.Get(task.ID)
		if !ok {
			slack = 0
		}
		_, isCritical := critical[task.ID]
		var preferred *capability.ModelRef
		switch {
		case isCritical:
			model := highModel
			preferred = &model
		case slack > duration && lowCanTakeTask(task):
			model := lowModel
			preferred = &model
		}
		if preferred != nil {
			assignments = append(assignments, ModelAssignment{TaskID: task.ID, Model: *preferred})
		}
	}
	return ordered, assignments
}

// candidateSlice is ordered.slice(0, capacity). Negative and zero capacity
// produce an empty list; an oversized capacity returns a defensive copy.
func candidateSlice(ordered []*plandb.Task, capacity float64) []*plandb.Task {
	if math.IsNaN(capacity) || capacity <= 0 {
		return []*plandb.Task{}
	}
	end := len(ordered)
	if !math.IsInf(capacity, 1) && capacity < float64(end) {
		end = int(math.Floor(capacity))
	}
	return append([]*plandb.Task(nil), ordered[:end]...)
}
