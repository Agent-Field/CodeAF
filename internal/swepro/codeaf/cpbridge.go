package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/afield"
	"github.com/Agent-Field/swe-pro-go/internal/plandb"
	"github.com/Agent-Field/swe-pro-go/internal/session/leafoutcome"
	"github.com/Agent-Field/swe-pro-go/internal/session/scheduler"
)

const leafPollInterval = 2 * time.Second

type cpStageNode struct {
	executionID string
	reasoner    string
	stage       string
	key         string
	openedAt    int64
}

type cpLeafNode struct {
	executionID string
	reasoner    string
	taskID      string
	title       string
	parent      string
	openedAt    int64
}

// cpBridge is a Go-only projection of codeaf's parity-frozen NDJSON events
// onto the AgentField control-plane DAG.
type cpBridge struct {
	reporter *afield.Reporter
	runID    string
	rootID   string
	rootName string

	mu               sync.Mutex
	rootInput        map[string]any
	rootOpenedAt     int64
	rootTerminal     bool
	openStages       map[string]*cpStageNode
	stageSequence    map[string]int
	currentScheduler string
	leaves           map[string]*cpLeafNode
	seenLeaves       map[string]bool
	lastInFlight     map[string]bool
	pollCancel       context.CancelFunc
	pollDone         chan struct{}

	note func(text string, tags ...string)
}

func newCPBridge(reporter *afield.Reporter, parentExecID, goal string) *cpBridge {
	runID := reporter.RunID()
	rootName := afield.IntentLabel(goal)
	if rootName == "" {
		rootName = "codeaf-run"
	}
	now := time.Now().UnixMilli()
	bridge := &cpBridge{
		reporter: reporter,
		runID:    runID,
		rootID:   runID + "-root",
		rootName: rootName,
		rootInput: map[string]any{
			"goal": truncate(goal, 500),
		},
		rootOpenedAt:  now,
		openStages:    map[string]*cpStageNode{},
		stageSequence: map[string]int{},
		leaves:        map[string]*cpLeafNode{},
		seenLeaves:    map[string]bool{},
		lastInFlight:  map[string]bool{},
	}
	// The control plane rejects notes without an execution id; anchor the
	// run's note feed to the root node.
	bridge.note = func(text string, tags ...string) {
		reporter.NoteExec(bridge.rootID, text, tags...)
	}
	reporter.Post(afield.Event{
		ExecutionID: bridge.rootID,
		Reasoner:    bridge.rootName,
		Status:      "running",
		Parent:      parentExecID,
		Input:       cloneMap(bridge.rootInput),
	})
	bridge.note("starting: "+truncate(goal, 120), "codeaf", "run")
	return bridge
}

// setRootInput enriches the already-created root without attempting to rename
// it. The control plane permits input enrichment until the terminal event.
func (bridge *cpBridge) setRootInput(input map[string]any) {
	if bridge == nil || len(input) == 0 {
		return
	}
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	if bridge.rootTerminal {
		return
	}
	for key, value := range input {
		if value != nil {
			bridge.rootInput[key] = value
		}
	}
	bridge.reporter.Post(afield.Event{
		ExecutionID: bridge.rootID,
		Reasoner:    bridge.rootName,
		Status:      "running",
		Input:       cloneMap(bridge.rootInput),
	})
}

func (bridge *cpBridge) consume(value event) {
	if bridge == nil {
		return
	}
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	if bridge.rootTerminal {
		return
	}
	switch value.Type {
	case "stage":
		bridge.consumeStageLocked(value)
	case "terminal":
		bridge.consumeTerminalLocked(value)
	}
}

func (bridge *cpBridge) consumeStageLocked(value event) {
	if value.Stage == "scheduler" && value.Status == "quiet-wait" {
		return
	}
	key := stageKey(value.Stage, value.Data)
	if open := bridge.openStages[key]; open != nil {
		bridge.closeStageLocked(open, value)
		bridge.stageNoteLocked(value)
		return
	}

	opens := value.Status == "running" ||
		(value.Stage == "scheduler" && value.Status == "cycle")
	if opens {
		bridge.openStageLocked(value, key)
		bridge.stageNoteLocked(value)
		return
	}

	bridge.stageSequence[value.Stage]++
	executionID := fmt.Sprintf(
		"%s-%s-%d", bridge.runID, value.Stage, bridge.stageSequence[value.Stage],
	)
	duration := int64(0)
	bridge.reporter.Post(afield.Event{
		ExecutionID: executionID,
		Reasoner:    afield.StageLabel(value.Stage, value.Data),
		Status:      terminalCPStatus(value.Status),
		Parent:      bridge.rootID,
		Result:      cloneMap(value.Data),
		DurationMS:  &duration,
	})
	bridge.stageNoteLocked(value)
}

func (bridge *cpBridge) openStageLocked(value event, key string) {
	bridge.stageSequence[value.Stage]++
	node := &cpStageNode{
		executionID: fmt.Sprintf(
			"%s-%s-%d", bridge.runID, value.Stage, bridge.stageSequence[value.Stage],
		),
		reasoner: afield.StageLabel(value.Stage, value.Data),
		stage:    value.Stage,
		key:      key,
		openedAt: value.Timestamp,
	}
	if node.openedAt == 0 {
		node.openedAt = time.Now().UnixMilli()
	}
	bridge.openStages[key] = node
	if value.Stage == "scheduler" {
		bridge.currentScheduler = node.executionID
	}
	bridge.reporter.Post(afield.Event{
		ExecutionID: node.executionID,
		Reasoner:    node.reasoner,
		Status:      "running",
		Parent:      bridge.rootID,
		Input:       cloneMap(value.Data),
	})
}

func (bridge *cpBridge) closeStageLocked(node *cpStageNode, value event) {
	delete(bridge.openStages, node.key)
	if bridge.currentScheduler == node.executionID {
		bridge.currentScheduler = ""
	}
	duration := elapsedMS(node.openedAt, value.Timestamp)
	bridge.reporter.Post(afield.Event{
		ExecutionID: node.executionID,
		Reasoner:    node.reasoner,
		Status:      terminalCPStatus(value.Status),
		Parent:      bridge.rootID,
		Result:      cloneMap(value.Data),
		DurationMS:  &duration,
	})
}

func (bridge *cpBridge) consumeTerminalLocked(value event) {
	for _, node := range bridge.openStages {
		duration := elapsedMS(node.openedAt, value.Timestamp)
		bridge.reporter.Post(afield.Event{
			ExecutionID: node.executionID,
			Reasoner:    node.reasoner,
			Status:      "succeeded",
			Parent:      bridge.rootID,
			DurationMS:  &duration,
		})
	}
	bridge.openStages = map[string]*cpStageNode{}
	bridge.currentScheduler = ""

	result := cloneMap(value.Data)
	if result == nil {
		result = map[string]any{}
	}
	if _, exists := result["status"]; !exists {
		result["status"] = value.Status
	}
	if value.Message != "" {
		if _, exists := result["reason"]; !exists {
			result["reason"] = value.Message
		}
	}
	duration := elapsedMS(bridge.rootOpenedAt, value.Timestamp)
	// Use the same classification as terminalCPStatus: a deliberate no-op
	// (refused, budget-exhausted) is a successful terminal, not a work
	// failure. CP consumers alert and retry on failed executions, so mapping
	// every non-pass to failed presented an intake refusal as an outage.
	status := terminalCPStatus(value.Status)
	bridge.reporter.Post(afield.Event{
		ExecutionID: bridge.rootID,
		Reasoner:    bridge.rootName,
		Status:      status,
		Result:      result,
		DurationMS:  &duration,
	})
	bridge.rootTerminal = true

	if cost, ok := numberValue(value.Data["cost_usd"]); ok {
		bridge.note(
			fmt.Sprintf("finished: %s — $%.4f", value.Status, cost),
			"codeaf", "terminal",
		)
	} else {
		bridge.note("finished: "+value.Status, "codeaf", "terminal")
	}
}

func (bridge *cpBridge) stageNoteLocked(value event) {
	switch value.Stage {
	case "classifier":
		if value.Status != "running" {
			bridge.note("classified as "+value.Status, "codeaf", value.Stage)
		}
	case "planner":
		if value.Status != "running" {
			if tasks, ok := countValue(value.Data["tasks"]); ok {
				bridge.note(fmt.Sprintf("planned %d tasks", tasks), "codeaf", value.Stage)
			} else {
				bridge.note("planned tasks", "codeaf", value.Stage)
			}
		}
	case "issue-writer":
		if value.Status == "completed" {
			if written, ok := countValue(value.Data["written"]); ok {
				bridge.note(fmt.Sprintf("wrote %d issues", written), "codeaf", value.Stage)
			} else {
				bridge.note("wrote issues", "codeaf", value.Stage)
			}
		}
	case "scheduler":
		if value.Status == "cycle-complete" {
			bridge.note(schedulerCycleNote(value.Data), "codeaf", value.Stage)
		}
	case "audit":
		if value.Status != "running" {
			if cycle, ok := intValue(value.Data["cycle"]); ok {
				bridge.note(
					fmt.Sprintf("audit cycle %d: %s", cycle, value.Status),
					"codeaf", value.Stage,
				)
			} else {
				bridge.note("audit: "+value.Status, "codeaf", value.Stage)
			}
		}
	case "fix-generator":
		if value.Status == "running" {
			if cycle, ok := intValue(value.Data["cycle"]); ok {
				bridge.note(
					fmt.Sprintf("generating fixes (cycle %d)", cycle),
					"codeaf", value.Stage,
				)
			} else {
				bridge.note("generating fixes", "codeaf", value.Stage)
			}
		}
	case "stale-reaper":
		if count, ok := countValue(value.Data["tasks"]); ok {
			bridge.note(
				fmt.Sprintf("released %d stale tasks", count),
				"codeaf", value.Stage,
			)
		} else {
			bridge.note("released stale tasks", "codeaf", value.Stage)
		}
	case "pr-ready":
		if value.Status != "running" {
			bridge.note("pr-ready: "+value.Status, "codeaf", value.Stage)
		}
	case "root-cut":
		switch value.Status {
		case "selected":
			bridge.note("fast path: root cut", "codeaf", value.Stage)
		case "decompose":
			bridge.note("decomposing into task graph", "codeaf", value.Stage)
		}
	}
}

func schedulerCycleNote(data map[string]any) string {
	cycle, haveCycle := intValue(data["cycle"])
	dispatched, haveDispatched := countValue(data["dispatched"])
	failed, haveFailed := countValue(data["failed"])
	if haveCycle && haveDispatched && haveFailed {
		return fmt.Sprintf(
			"cycle %d: dispatched %d leaves (%d failed)",
			cycle, dispatched, failed,
		)
	}
	parts := []string{}
	if haveCycle {
		parts = append(parts, fmt.Sprintf("cycle %d:", cycle))
	}
	if haveDispatched {
		parts = append(parts, fmt.Sprintf("dispatched %d leaves", dispatched))
	} else {
		parts = append(parts, "dispatch complete")
	}
	if haveFailed {
		parts = append(parts, fmt.Sprintf("(%d failed)", failed))
	}
	return strings.Join(parts, " ")
}

func (bridge *cpBridge) startLeafPoller(ctx context.Context) func() {
	if bridge == nil {
		return func() {}
	}
	bridge.mu.Lock()
	if bridge.pollCancel != nil {
		bridge.mu.Unlock()
		return bridge.stopLeafPoller
	}
	pollCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	bridge.pollCancel = cancel
	bridge.pollDone = done
	bridge.mu.Unlock()

	go func() {
		defer close(done)
		bridge.pollLeaves()
		ticker := time.NewTicker(leafPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-pollCtx.Done():
				return
			case <-ticker.C:
				bridge.pollLeaves()
			}
		}
	}()
	return bridge.stopLeafPoller
}

func (bridge *cpBridge) stopLeafPoller() {
	if bridge == nil {
		return
	}
	bridge.mu.Lock()
	cancel, done := bridge.pollCancel, bridge.pollDone
	bridge.pollCancel = nil
	bridge.pollDone = nil
	bridge.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}

	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	for taskID := range bridge.leaves {
		bridge.closeLeafFromTaskStateLocked(taskID)
	}
	bridge.lastInFlight = map[string]bool{}
}

func (bridge *cpBridge) pollLeaves() {
	current := map[string]bool{}
	for _, taskID := range scheduler.InFlightDispatchIDs() {
		current[taskID] = true
	}

	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	for taskID := range current {
		if !bridge.seenLeaves[taskID] {
			bridge.openLeafLocked(taskID)
		}
	}
	for taskID := range bridge.lastInFlight {
		if !current[taskID] {
			bridge.closeLeafFromTaskStateLocked(taskID)
		}
	}
	bridge.lastInFlight = current
}

// Observe implements scheduler.OutcomeObserver.
func (bridge *cpBridge) Observe(outcome leafoutcome.LeafOutcome) {
	if bridge == nil {
		return
	}
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	if bridge.rootTerminal {
		return
	}
	if bridge.leaves[outcome.TaskID] == nil {
		bridge.openLeafLocked(outcome.TaskID)
	}
	leaf := bridge.leaves[outcome.TaskID]
	if leaf == nil {
		return
	}
	delete(bridge.leaves, outcome.TaskID)
	duration := elapsedMS(leaf.openedAt, time.Now().UnixMilli())
	status := "succeeded"
	if outcome.Verdict == leafoutcome.VerdictFail ||
		outcome.Verdict == leafoutcome.VerdictEscalated {
		status = "failed"
	}
	bridge.reporter.Post(afield.Event{
		ExecutionID: leaf.executionID,
		Reasoner:    leaf.reasoner,
		Status:      status,
		Parent:      leaf.parent,
		Result:      compactLeafOutcome(outcome),
		DurationMS:  &duration,
	})
	bridge.note(
		fmt.Sprintf("leaf %s: %s", outcome.TaskID, outcome.Verdict),
		"codeaf", "leaf",
	)
}

func (bridge *cpBridge) openLeafLocked(taskID string) {
	if taskID == "" || bridge.seenLeaves[taskID] || bridge.rootTerminal {
		return
	}
	task := lookupTask(taskID)
	title := ""
	if task != nil {
		title = task.Title
	}
	reasoner := afield.Normalize(firstWords(title, 3))
	if reasoner == "" {
		reasoner = afield.Normalize(taskID)
	}
	parent := bridge.currentScheduler
	if parent == "" {
		parent = bridge.rootID
	}
	leaf := &cpLeafNode{
		executionID: bridge.runID + "-leaf-" + taskID,
		reasoner:    reasoner,
		taskID:      taskID,
		title:       title,
		parent:      parent,
		openedAt:    time.Now().UnixMilli(),
	}
	bridge.seenLeaves[taskID] = true
	bridge.leaves[taskID] = leaf
	bridge.reporter.Post(afield.Event{
		ExecutionID: leaf.executionID,
		Reasoner:    leaf.reasoner,
		Status:      "running",
		Parent:      leaf.parent,
		Input: map[string]any{
			"task_id": taskID,
			"title":   title,
		},
	})
	bridge.note(
		fmt.Sprintf("dispatched %s: %s", taskID, truncate(title, 80)),
		"codeaf", "leaf",
	)
}

func (bridge *cpBridge) closeLeafFromTaskStateLocked(taskID string) {
	leaf := bridge.leaves[taskID]
	if leaf == nil {
		return
	}
	delete(bridge.leaves, taskID)
	task := lookupTask(taskID)
	taskStatus := "unknown"
	result := map[string]any{
		"task_id": taskID,
		"title":   leaf.title,
	}
	if task != nil {
		taskStatus = string(task.Status)
		result["task_status"] = taskStatus
		if len(task.Result) > 0 {
			var compact any
			if json.Unmarshal(task.Result, &compact) == nil {
				result["task_result"] = compact
			}
		}
		if task.Error != nil {
			result["error"] = *task.Error
		}
	}
	duration := elapsedMS(leaf.openedAt, time.Now().UnixMilli())
	bridge.reporter.Post(afield.Event{
		ExecutionID: leaf.executionID,
		Reasoner:    leaf.reasoner,
		Status:      "succeeded",
		Parent:      leaf.parent,
		Result:      result,
		DurationMS:  &duration,
	})
	bridge.note(
		fmt.Sprintf("leaf %s: %s", taskID, taskStatus),
		"codeaf", "leaf",
	)
}

func compactLeafOutcome(outcome leafoutcome.LeafOutcome) map[string]any {
	result := map[string]any{
		"task_id":        outcome.TaskID,
		"verdict":        string(outcome.Verdict),
		"size_band":      string(outcome.SizeBand),
		"repair_rounds":  safeNumber(float64(outcome.RepairRounds)),
		"turns":          safeNumber(float64(outcome.Turns)),
		"tool_errors":    safeNumber(float64(outcome.ToolErrors)),
		"cost_usd":       safeNumber(float64(outcome.CostUsd)),
		"wall_ms":        safeNumber(float64(outcome.WallMs)),
		"merge_conflict": outcome.MergeConflict,
		"model": map[string]any{
			"provider_id": outcome.Model.ProviderID,
			"model_id":    outcome.Model.ModelID,
		},
	}
	if outcome.Evidence != nil {
		result["evidence"] = outcome.Evidence
	}
	return result
}

func stageKey(stage string, data map[string]any) string {
	if cycle, ok := intValue(data["cycle"]); ok {
		return stage + ":" + strconv.Itoa(cycle)
	}
	return stage
}

// terminalCPStatus classifies a codeaf terminal for CP consumers, which alert
// and retry on failed executions. Deliberate no-ops (refused) and resource
// stops (budget-exhausted) are successful terminals; escalated means the run
// gave up on the work with blockers standing, which a consumer should see as
// a failure, not a quiet success.
func terminalCPStatus(status string) string {
	switch status {
	case "fail", "failed", "error", "crashed", "halt-invalid", "escalated":
		return "failed"
	default:
		return "succeeded"
	}
}

func elapsedMS(start, end int64) int64 {
	if end == 0 {
		end = time.Now().UnixMilli()
	}
	if end < start {
		return 0
	}
	return end - start
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func lookupTask(taskID string) *plandb.Task {
	for _, task := range plandb.GetPlanDB().ListTasks(nil) {
		if task.ID == taskID {
			return task
		}
	}
	return nil
}

func firstWords(value string, limit int) string {
	words := strings.Fields(value)
	if len(words) > limit {
		words = words[:limit]
	}
	// Truncation can leave a dangling separator ("Implement ast.go —").
	return strings.TrimRight(strings.Join(words, " "), " —–-:,;.")
}

func safeNumber(value float64) any {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	return value
}

func countValue(value any) (int, bool) {
	if number, ok := intValue(value); ok {
		return number, true
	}
	if value == nil {
		return 0, false
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Array, reflect.Slice, reflect.Map:
		return reflected.Len(), true
	default:
		return 0, false
	}
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	default:
		return 0, false
	}
}

func intValue(value any) (int, bool) {
	number, ok := numberValue(value)
	if !ok {
		return 0, false
	}
	return int(number), true
}
