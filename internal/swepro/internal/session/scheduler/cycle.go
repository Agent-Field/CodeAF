// This file ports src/session/plandb-scheduler.ts:788-829 and 1653-4562 from
// swe-pro (commit 3b25a1a).
package scheduler

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/engine/steploop"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/plandb"
	"github.com/Agent-Field/swe-pro-go/internal/session/capability"
	"github.com/Agent-Field/swe-pro-go/internal/session/isolation"
	"github.com/Agent-Field/swe-pro-go/internal/session/ledgers"
)

type inFlightDispatchSet struct {
	mu      sync.RWMutex
	present map[string]struct{}
	order   []string
}

func newInFlightDispatchSet() *inFlightDispatchSet {
	return &inFlightDispatchSet{present: map[string]struct{}{}, order: []string{}}
}

func (s *inFlightDispatchSet) addAll(ids []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		if _, exists := s.present[id]; exists {
			continue
		}
		s.present[id] = struct{}{}
		s.order = append(s.order, id)
	}
}

func (s *inFlightDispatchSet) reserve(ids []string, maximum int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	reserved := make([]string, 0, len(ids))
	for _, id := range ids {
		if len(s.present) >= maximum {
			break
		}
		if _, exists := s.present[id]; exists {
			continue
		}
		s.present[id] = struct{}{}
		s.order = append(s.order, id)
		reserved = append(reserved, id)
	}
	return reserved
}

func (s *inFlightDispatchSet) remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.present[id]; !exists {
		return
	}
	delete(s.present, id)
	for index, existing := range s.order {
		if existing == id {
			s.order = append(s.order[:index], s.order[index+1:]...)
			break
		}
	}
}

func (s *inFlightDispatchSet) len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.present)
}

func (s *inFlightDispatchSet) has(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.present[id]
	return ok
}

func (s *inFlightDispatchSet) snapshot() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.order))
	for _, id := range s.order {
		if _, ok := s.present[id]; ok {
			out = append(out, id)
		}
	}
	return out
}

var schedulerInFlightDispatches = newInFlightDispatchSet()

// InFlightDispatchIDs returns a defensive insertion-ordered snapshot of every
// dispatch currently owned by any overlapping scheduler cycle.
func InFlightDispatchIDs() []string {
	return schedulerInFlightDispatches.snapshot()
}

// ReleaseTaskIfNotInFlight orders stale-claim reclamation against dispatch
// publication. A scheduler reserves the same registry lock before it makes a
// claim observable, so a release either happens before that dispatch starts or
// observes and protects it; it cannot release through the claim/publication
// gap.
func ReleaseTaskIfNotInFlight(taskID string) *plandb.Task {
	schedulerInFlightDispatches.mu.RLock()
	defer schedulerInFlightDispatches.mu.RUnlock()
	if _, live := schedulerInFlightDispatches.present[taskID]; live {
		return nil
	}
	return plandb.GetPlanDB().ReleaseTask(taskID)
}

// reapStaleRunningTasks keeps KB-1. PlanDB timestamps are epoch-ms numbers,
// but the source passes them through Date.parse; numeric strings are not valid
// JavaScript date strings, so every real task is skipped.
func reapStaleRunningTasks(
	runner planDBRunner,
	workspace string,
	dbPath string,
	projectID string,
	stallAfter time.Duration,
	now time.Time,
) []string {
	if runner == nil {
		runner = nativePlanDBRunner{}
	}
	_, _ = workspace, dbPath
	list := runner.Run([]string{
		"plandb", "list", "--project", projectID, "--status", "running", "--json",
	})
	var tasks []map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(list.Stdout)))
	decoder.UseNumber()
	if decoder.Decode(&tasks) != nil {
		return []string{}
	}
	reaped := []string{}
	for _, task := range tasks {
		stamp := task["last_heartbeat"]
		if stamp == nil {
			stamp = task["started_at"]
		}
		if stamp == nil {
			stamp = task["claimed_at"]
		}
		stampTime, ok := jsDateParse(stamp)
		if !ok || now.Sub(stampTime) <= stallAfter {
			continue
		}
		id, _ := task["id"].(string)
		if id == "" {
			continue
		}
		minutes := int(math.Floor(now.Sub(stampTime).Minutes()))
		failTaskCascade(runner, workspace, dbPath, id,
			"heartbeat timeout: no update for "+strconv.Itoa(minutes)+" min")
		reaped = append(reaped, id)
	}
	return reaped
}

func jsDateParse(value any) (time.Time, bool) {
	text, ok := value.(string)
	if !ok || text == "" {
		return time.Time{}, false
	}
	// Date.parse("1753000000000") is NaN. Reject the all-numeric spelling
	// before trying the date layouts accepted by the scheduler fixtures.
	numeric := true
	for _, char := range text {
		if char < '0' || char > '9' {
			numeric = false
			break
		}
	}
	if numeric {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC1123, time.RFC1123Z, time.RFC822, time.RFC822Z} {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

type frontierState struct {
	mu       sync.Mutex
	ticks    map[string]int
	inFlight map[string]struct{}
}

var schedulerFrontierState = frontierState{
	ticks: map[string]int{}, inFlight: map[string]struct{}{},
}

func (f *frontierState) begin(projectID string) (int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, active := f.inFlight[projectID]; active || f.ticks[projectID] >= frontierMaxTicks() {
		return 0, false
	}
	f.inFlight[projectID] = struct{}{}
	f.ticks[projectID]++
	return f.ticks[projectID], true
}

func frontierMaxTicks() int {
	return cachedFrontierMaxTicks.get(os.LookupEnv)
}

type frontierMaxTicksCache struct {
	once  sync.Once
	value int
}

var cachedFrontierMaxTicks frontierMaxTicksCache

func (cache *frontierMaxTicksCache) get(
	lookup func(string) (string, bool),
) int {
	cache.once.Do(func() {
		raw, exists := lookup("FRONTIER_MAX_TICKS")
		cache.value = parseFrontierMaxTicks(raw, exists)
	})
	return cache.value
}

func parseFrontierMaxTicks(raw string, exists bool) int {
	const maximum = 20
	if !exists {
		return maximum
	}
	value := jscompat.ToNumber(raw)
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return maximum
	}
	value = math.Floor(value)
	return int(math.Min(maximum, math.Max(0, value)))
}

func (f *frontierState) end(projectID string) {
	f.mu.Lock()
	delete(f.inFlight, projectID)
	f.mu.Unlock()
}

// RunSchedulerCycle is the package-level equivalent of the TS export.
func RunSchedulerCycle(ctx context.Context, scheduler *Scheduler, input SchedulerInput) (SchedulerCycleResult, error) {
	if scheduler == nil {
		return SchedulerCycleResult{}, nil
	}
	return scheduler.RunSchedulerCycle(ctx, input)
}

// RunSchedulerCycle performs one claim-and-dispatch pump. It deliberately
// holds no cycle-wide lock: callers may overlap cycles, and the shared
// in-flight set plus PlanDB's atomic claim gate enforce global capacity.
func (s *Scheduler) RunSchedulerCycle(ctx context.Context, input SchedulerInput) (SchedulerCycleResult, error) {
	maxParallel := schedulerMaxParallel(input)
	envelope, err := deriveDispatchEnvelope(s.workspace, maxParallel, s.adaptiveCuts, s.envelopeReader)
	if err != nil {
		return SchedulerCycleResult{}, err
	}
	if envelope.Status != isolation.StatusProceed {
		schedulerLog.Warn("resource envelope degraded — throttling new dispatch", map[string]any{
			"decision": envelope.Status, "baseWindow": envelope.BaseWindow,
			"effectiveWindow": envelope.ParallelWindow,
		})
	}
	cleaned := gcStaleWorktrees(ctx, s.workspace, input.DBPath, input.ProjectID, gcWorktreeOptions{runner: s.runner})
	if cleaned > 0 {
		schedulerLog.Info("stale worktrees cleaned", map[string]any{"count": cleaned})
	}
	_ = reapStaleRunningTasks(s.planDB, s.workspace, input.DBPath, input.ProjectID,
		time.Hour, s.clock.Now())

	readyResult := s.planDB.Run([]string{
		"plandb", "list", "--project", input.ProjectID, "--status", "ready", "--json",
	})
	allResult := s.planDB.Run([]string{
		"plandb", "list", "--project", input.ProjectID, "--json",
	})
	ready := decodeTaskList(readyResult.Stdout)
	all := decodeTaskList(allResult.Stdout)
	scan := scanCandidates(input.RootTaskID, ready, all)
	result := SchedulerCycleResult{
		Dispatched: []DispatchResult{}, Scanned: jscompat.JSNumber(scan.Scanned), Skipped: scan.Skipped,
	}
	if len(scan.Candidates) == 0 {
		result.QuietReason = CycleQuietNothingReady
		s.maybeRunFrontierTick(ctx, input, "ready-drained")
		return result, nil
	}

	prioritized := prioritizeReadyTasks(s.planDB, s.workspace, input.DBPath, input.ProjectID, scan.Candidates)
	ordered := append([]*plandb.Task(nil), prioritized...)
	assignments := []ModelAssignment{}
	if s.adaptiveCuts && s.pools != nil && s.capability != nil {
		ordered, assignments = orderCandidatesByPlan(prioritized, candidateOrderingOptions{
			AdaptiveCuts: true, HeftEnabled: os.Getenv("CODEAF_HEFT") != "0",
			ParallelWindow: envelope.ParallelWindow, Workspace: s.workspace,
			ProjectID: input.ProjectID, AllTasks: all, Descendants: scan.Descendants,
			PlanDB: s.planDB, Pools: s.pools, Tracker: s.capability,
		})
	}
	if s.pools != nil && s.provider != nil {
		needed := make([]ModelTier, 0, len(ordered))
		for _, task := range ordered {
			needed = append(needed, pickModelTier(suggestedAgent(task)))
		}
		prewarmTierPools(ctx, needed, s.pools, s.provider)
	}

	if envelope.Status == isolation.StatusPause {
		result.QuietReason = CycleQuietPaused
		return result, nil
	}
	capacity := math.Min(
		envelope.ParallelWindow,
		math.Max(0, envelope.ParallelWindow-float64(schedulerInFlightDispatches.len())),
	)
	if capacity == 0 {
		result.QuietReason = CycleQuietWindowFull
		return result, nil
	}
	overrideByTask := map[string]*capabilityModelRef{}
	for _, assignment := range assignments {
		value := &capabilityModelRef{
			ProviderID: assignment.Model.ProviderID, ModelID: assignment.Model.ModelID,
		}
		overrideByTask[assignment.TaskID] = value
	}

	claimed := []dispatchItem{}
	candidates := candidateSlice(ordered, capacity)
	candidateIDs := make([]string, 0, len(candidates))
	byCandidateID := make(map[string]*plandb.Task, len(candidates))
	for _, task := range candidates {
		if task != nil {
			candidateIDs = append(candidateIDs, task.ID)
			byCandidateID[task.ID] = task
		}
	}
	reserved := schedulerInFlightDispatches.reserve(
		candidateIDs, int(math.Floor(envelope.ParallelWindow)),
	)
	if len(reserved) == 0 {
		result.QuietReason = CycleQuietWindowFull
		return result, nil
	}
	// LB-6: candidateSlice is fixed before claims, so failed claims are not
	// backfilled from the remainder of ordered.
	for _, taskID := range reserved {
		task := byCandidateID[taskID]
		agentID := "scheduler:" + input.ParentSessionID
		// The whole capacity-limited wave was reserved before the first atomic
		// PlanDB claim. Stale reclamation takes the registry read lock through
		// ReleaseTask, so no claim can be observable without live publication.
		claim := s.planDB.Run([]string{
			"plandb", "task", "claim", task.ID, "--agent", agentID, "--json",
		})
		var claimedTask plandb.Task
		if claim.Code != 0 || json.Unmarshal(claim.Stdout, &claimedTask) != nil || claimedTask.ID == "" {
			schedulerInFlightDispatches.remove(task.ID)
			reason := jscompat.Trim(string(claim.Stderr))
			if reason == "" {
				reason = jscompat.Trim(string(claim.Stdout))
			}
			result.Skipped = append(result.Skipped, SkippedTask{
				TaskID: task.ID, Reason: "claim failed: " + reason,
			})
			continue
		}
		s.planDB.Run([]string{"plandb", "task", "start", task.ID, "--json"})
		claimed = append(claimed, dispatchItem{
			task: task, agentID: agentID, modelOverride: overrideByTask[task.ID],
		})
	}
	if len(claimed) == 0 {
		result.QuietReason = CycleQuietClaimsLost
		return result, nil
	}

	dispatches := make([]DispatchResult, len(claimed))
	runIndexes := func(indexes []int, limit int) {
		if len(indexes) == 0 {
			return
		}
		if limit < 1 {
			limit = 1
		}
		if limit > len(indexes) {
			limit = len(indexes)
		}
		jobs := make(chan int)
		var workers sync.WaitGroup
		for worker := 0; worker < limit; worker++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for index := range jobs {
					item := claimed[index]
					func() {
						// Effect.ensuring equivalent: removal occurs on success,
						// panic recovery inside dispatchOne, and cancellation.
						defer schedulerInFlightDispatches.remove(item.task.ID)
						dispatches[index] = s.dispatchOne(ctx, input, item, envelope.Aimd)
					}()
				}
			}()
		}
		for _, index := range indexes {
			jobs <- index
		}
		close(jobs)
		workers.Wait()
	}
	parallelLimit := int(math.Floor(envelope.ParallelWindow))
	indexes := make([]int, len(claimed))
	for index := range indexes {
		indexes[index] = index
	}
	pioneerIndex := -1
	if s.adaptiveCuts && len(claimed) >= 2 {
		counts := map[string]int{}
		agentOrder := []string{}
		for _, item := range claimed {
			agent := suggestedAgent(item.task)
			if _, exists := counts[agent]; !exists {
				agentOrder = append(agentOrder, agent)
			}
			counts[agent]++
		}
		sharedAgent := ""
		for _, agent := range agentOrder {
			if counts[agent] >= 2 {
				sharedAgent = agent
				break
			}
		}
		if sharedAgent != "" {
			for index, item := range claimed {
				if suggestedAgent(item.task) == sharedAgent {
					pioneerIndex = index
					break
				}
			}
		}
	}
	if pioneerIndex < 0 {
		runIndexes(indexes, parallelLimit)
	} else {
		var pioneer sync.WaitGroup
		pioneer.Add(1)
		go func() {
			defer pioneer.Done()
			runIndexes([]int{pioneerIndex}, 1)
		}()
		// W7: the pioneer starts immediately; the rest of the shared-prefix
		// wave waits for its provider-prefix cache write opportunity.
		_ = s.clock.Sleep(ctx, 1500*time.Millisecond)
		rest := make([]int, 0, len(indexes)-1)
		for _, index := range indexes {
			if index != pioneerIndex {
				rest = append(rest, index)
			}
		}
		runIndexes(rest, parallelLimit)
		pioneer.Wait()
	}
	result.Dispatched = dispatches

	failedOrEscalated := false
	for _, dispatch := range dispatches {
		if !dispatch.Success || (dispatch.Gate != nil && dispatch.Gate.Status == GateEscalated) {
			failedOrEscalated = true
			break
		}
	}
	if failedOrEscalated {
		s.maybeRunFrontierTick(ctx, input, "leaf-failure-or-escalation")
	}
	return result, nil
}

func decodeTaskList(raw []byte) []*plandb.Task {
	tasks := []*plandb.Task{}
	if json.Unmarshal(raw, &tasks) != nil {
		return []*plandb.Task{}
	}
	return tasks
}

func (s *Scheduler) maybeRunFrontierTick(ctx context.Context, input SchedulerInput, trigger string) bool {
	if !s.frontier || s.planner == nil {
		return false
	}
	residual := s.frontierResidual(input)
	if residual == "" {
		return false
	}
	tickNumber, ok := schedulerFrontierState.begin(input.ProjectID)
	if !ok {
		return false
	}
	defer schedulerFrontierState.end(input.ProjectID)
	tick, err := s.planner.RunFrontierTick(ctx, FrontierTickInput{
		Workspace: s.workspace, DBPath: input.DBPath, ProjectID: input.ProjectID,
		RootTaskID: input.RootTaskID, ParentSessionID: input.ParentSessionID,
		PromptOps: input.PromptOps, Trigger: trigger,
		FrontierContext: s.renderFrontierPrompt(residual), Tick: tickNumber,
	})
	if err != nil {
		return false
	}
	if !tick.ResidualUpdated {
		s.planDB.Run([]string{
			"plandb", "context", "Frontier residual resolved; no remainder reported by planner.",
			"--kind", "residual", "--task", input.RootTaskID, "--json",
		})
	}
	return true
}

func (s *Scheduler) hasFrontierResidual(input SchedulerInput) bool {
	return s.frontierResidual(input) != ""
}

func (s *Scheduler) frontierResidual(input SchedulerInput) string {
	result := s.planDB.Run([]string{
		"plandb", "contexts", "--project", input.ProjectID, "--json",
	})
	var contexts []*plandb.ContextEntry
	if json.Unmarshal(result.Stdout, &contexts) != nil {
		return ""
	}
	for _, entry := range contexts {
		if entry == nil || entry.Kind != "residual" {
			continue
		}
		if entry.TaskID != nil && *entry.TaskID != input.RootTaskID {
			continue
		}
		content := jscompat.Trim(entry.Content)
		if content == "" || strings.HasPrefix(strings.ToLower(content), "frontier residual resolved") {
			return ""
		}
		return content
	}
	return ""
}

func (s *Scheduler) renderFrontierPrompt(residual string) string {
	outcomes := capability.LoadOutcomes(
		filepath.Join(s.workspace, ".codeaf", "outcomes.jsonl"),
	)
	modelIDs := []string{}
	seen := map[string]bool{}
	addModel := func(modelID string) {
		if modelID != "" && !seen[modelID] {
			seen[modelID] = true
			modelIDs = append(modelIDs, modelID)
		}
	}
	for _, outcome := range outcomes {
		if outcome.Model != nil {
			addModel(outcome.Model.ModelID)
		}
	}
	if s.pools != nil {
		for _, tier := range []ModelTier{ModelTierHigh, ModelTierLow} {
			for _, candidate := range s.pools.CandidatesForTier(tier) {
				addModel(modelRefOf(candidate.ID).ModelID)
			}
		}
	}
	outcomeLines := []string{"- (no verified leaf outcomes yet)"}
	if len(outcomes) > 0 {
		outcomeLines = make([]string, 0, len(outcomes))
		for _, outcome := range outcomes {
			modelID := ""
			if outcome.Model != nil {
				modelID = outcome.Model.ModelID
			}
			outcomeLines = append(outcomeLines,
				"- task="+outcome.TaskID+
					" model="+modelID+
					" band="+string(outcome.SizeBand)+
					" verdict="+string(outcome.Verdict)+
					" repairs="+jscompat.FormatNumber(float64(outcome.RepairRounds))+
					" turns="+jscompat.FormatNumber(float64(outcome.Turns))+
					" costUsd="+jscompat.FormatNumber(float64(outcome.CostUsd))+
					" wallMs="+jscompat.FormatNumber(float64(outcome.WallMs))+
					" mergeConflict="+strconv.FormatBool(outcome.MergeConflict),
			)
		}
	}
	sort.SliceStable(modelIDs, func(i, j int) bool {
		left, right := utf16.Encode([]rune(modelIDs[i])), utf16.Encode([]rune(modelIDs[j]))
		for index := 0; index < len(left) && index < len(right); index++ {
			if left[index] != right[index] {
				return left[index] < right[index]
			}
		}
		return len(left) < len(right)
	})
	capabilityLines := []string{}
	if s.capability != nil {
		for _, modelID := range modelIDs {
			band := s.capability.MaxReliableBand(capability.ModelRef{ModelID: modelID})
			capabilityLines = append(capabilityLines,
				"- model="+modelID+" reliableBand="+string(band))
		}
	}
	if len(capabilityLines) == 0 {
		capabilityLines = []string{"- (no routed models available)"}
	}
	lines := []string{
		"## Frontier residual",
		residual,
		"",
		"## Verified LeafOutcomes so far (complete run-to-date set)",
	}
	lines = append(lines, outcomeLines...)
	lines = append(lines,
		"",
		"## Per-model reliable-band summary (read-only capability state)",
	)
	lines = append(lines, capabilityLines...)
	return strings.Join(lines, "\n")
}

// Pump makes Scheduler directly usable as steploop.Scheduler.
func (s *Scheduler) Pump(ctx context.Context, input steploop.SchedulerInput) (string, error) {
	cycle, err := s.RunSchedulerCycle(ctx, SchedulerInput{
		RootTaskID: input.Plan.RootTaskID, ProjectID: input.Plan.ProjectID,
		DBPath: input.Plan.DBPath, ParentSessionID: input.SessionID,
	})
	if err != nil {
		return "", err
	}
	s.quietMu.Lock()
	s.lastQuietReason = cycle.QuietReason
	s.quietMu.Unlock()
	summary := FormatSchedulerSummary(cycle, input.Plan.RootTaskID)
	if summary == nil {
		return "", nil
	}
	return *summary, nil
}

// LastCycleQuietReason exposes the most recent Pump outcome to orchestration
// wrappers without encoding liveness state in a synthetic prompt.
func (s *Scheduler) LastCycleQuietReason() CycleQuietReason {
	s.quietMu.RLock()
	defer s.quietMu.RUnlock()
	return s.lastQuietReason
}

type digestHashRegistry struct {
	mu     sync.Mutex
	byRoot map[string]string
}

var schedulerDigestHashes = digestHashRegistry{byRoot: map[string]string{}}

func digestForInjection(rootTaskID string) *string {
	digest := ledgers.RenderFailureDigest(rootTaskID)
	if digest == nil {
		return nil
	}
	sum := sha1.Sum([]byte(*digest))
	hash := hex.EncodeToString(sum[:])
	schedulerDigestHashes.mu.Lock()
	defer schedulerDigestHashes.mu.Unlock()
	if schedulerDigestHashes.byRoot[rootTaskID] == hash {
		value := "failure ledger unchanged (" + strconv.Itoa(ledgers.FailureCount(rootTaskID)) + " entries)"
		return &value
	}
	schedulerDigestHashes.byRoot[rootTaskID] = hash
	return digest
}

// FormatSchedulerSummary ports plandb-scheduler.ts:4579-4653.
func FormatSchedulerSummary(result SchedulerCycleResult, rootTaskID ...string) *string {
	var digest *string
	if len(rootTaskID) > 0 && rootTaskID[0] != "" {
		digest = digestForInjection(rootTaskID[0])
	}
	if len(result.Dispatched) == 0 {
		if digest == nil {
			return nil
		}
		value := strings.Join([]string{"<system-reminder>", *digest, "</system-reminder>"}, "\n")
		return &value
	}
	lines := []string{
		"<system-reminder>",
		"PlanDB scheduler completed dispatches since your last turn:",
	}
	blockers := []DispatchResult{}
	for _, dispatch := range result.Dispatched {
		badge := ""
		if dispatch.Gate != nil {
			repair := ""
			if dispatch.Gate.RepairAttempts != nil && float64(*dispatch.Gate.RepairAttempts) > 0 {
				number := jscompat.FormatNumber(float64(*dispatch.Gate.RepairAttempts))
				suffix := "s"
				if float64(*dispatch.Gate.RepairAttempts) == 1 {
					suffix = ""
				}
				repair = " (after " + number + " repair" + suffix + ")"
			}
			switch dispatch.Gate.Status {
			case GatePass:
				confidence := ""
				if dispatch.Gate.Confidence != nil {
					confidence = "/" + string(*dispatch.Gate.Confidence)
				}
				badge = " [gate=pass" + confidence + repair + "]"
			case GateFail:
				reason := "see plandb context"
				if dispatch.Gate.Reason != nil {
					reason = *dispatch.Gate.Reason
				} else if dispatch.Gate.Verdict != nil {
					reason = string(*dispatch.Gate.Verdict)
				}
				badge = " [gate=FAIL: " + compactJS(reason, 80) + repair + "]"
				blockers = append(blockers, dispatch)
			case GateSkipped:
				reason := "no-op"
				if dispatch.Gate.Reason != nil {
					reason = *dispatch.Gate.Reason
				}
				badge = " [gate=skipped: " + reason + "]"
			}
		}
		if dispatch.Success {
			summary := ""
			if dispatch.Summary != nil {
				summary = *dispatch.Summary
			}
			lines = append(lines, "- "+dispatch.TaskID+" (@"+dispatch.SubagentType+")"+badge+": "+compactJS(summary, 400))
		} else {
			errorText := ""
			if dispatch.Error != nil {
				errorText = *dispatch.Error
			}
			lines = append(lines, "- "+dispatch.TaskID+" (@"+dispatch.SubagentType+")"+badge+": FAILED — "+errorText)
		}
	}
	if len(blockers) > 0 {
		lines = append(lines, "",
			"⚠️  "+strconv.Itoa(len(blockers))+" task(s) above hit the quality gate after repair-cap exhaustion.",
			"These tasks are in status=failed in plandb and BLOCK every descendant that depends on them.",
			"Your run is NOT complete while any of these remain unhandled.",
			"",
			"Cap-exhausted leaves:",
		)
		for _, blocker := range blockers {
			reason := "review failed"
			if blocker.Gate != nil && blocker.Gate.Reason != nil {
				reason = *blocker.Gate.Reason
			}
			lines = append(lines, "  - "+blocker.TaskID+": "+compactJS(reason, 240))
		}
		lines = append(lines, "",
			"REQUIRED next action: for EACH leaf above, choose one — split / pivot / amend / accept.",
			`(See orchestrator workflow §7 "Recover from gate failures" for the playbook.)`,
			"",
			"Inspect first:",
			"  `plandb contexts --task <id> --kind blocker`  (full reviewer bugs + repair_hints)",
			"  `plandb contexts --task <id> --kind review`   (every review attempt verbatim)",
			"Recurring failures on the same file or same kind of bug usually mean SPLIT, not retry.",
		)
	}
	if digest != nil {
		lines = append(lines, "", *digest)
	}
	lines = append(lines, "</system-reminder>")
	value := strings.Join(lines, "\n")
	return &value
}

var _ steploop.Scheduler = (*Scheduler)(nil)
