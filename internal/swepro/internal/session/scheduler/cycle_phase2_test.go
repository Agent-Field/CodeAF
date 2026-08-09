package scheduler

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/plandb"
	"github.com/Agent-Field/swe-pro-go/internal/session/capability"
	"github.com/Agent-Field/swe-pro-go/internal/session/leafbriefing"
	"github.com/Agent-Field/swe-pro-go/internal/session/leafoutcome"
	"github.com/Agent-Field/swe-pro-go/internal/session/mergecoordinator"
)

// phase2LivenessTimeout bounds "did the goroutine get there at all", not how
// fast it gets there. A tight bound here fails under parallel-package load
// even though the behavior under test is correct; Go's own test timeout still
// catches a genuine hang.
const phase2LivenessTimeout = 60 * time.Second

func seedCycleTasks(t *testing.T, count int) (string, []string) {
	t.Helper()
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	project := db.Init("cycle-project")
	rootDescription := "User request: run scheduler cycle"
	root, err := db.AddTask(plandb.AddTaskInput{
		Title: "Root", Description: &rootDescription, Project: project.ID, CustomID: "root",
	})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, count)
	for index := 0; index < count; index++ {
		id := fmt.Sprintf("leaf-%d", index+1)
		description := "file_scope: src/" + id + ".txt\nImplement " + id
		task, addErr := db.AddTask(plandb.AddTaskInput{
			Title: id, Description: &description, Project: project.ID,
			Parent: root.ID, CustomID: id,
		})
		if addErr != nil {
			t.Fatal(addErr)
		}
		if task.Status != plandb.StatusReady {
			t.Fatalf("%s status = %s, want ready", id, task.Status)
		}
		ids = append(ids, id)
	}
	return project.ID, ids
}

type concurrentGitStepLoop struct {
	mu         sync.Mutex
	expected   int
	active     int
	maxActive  int
	started    int
	allStarted chan struct{}
	lineCount  map[string]int
}

func (s *concurrentGitStepLoop) RunLeaf(_ context.Context, input LeafRunRequest) (LeafRunResult, error) {
	s.mu.Lock()
	s.active++
	if s.active > s.maxActive {
		s.maxActive = s.active
	}
	s.started++
	if s.started == s.expected {
		close(s.allStarted)
	}
	s.mu.Unlock()

	select {
	case <-s.allStarted:
	case <-time.After(phase2LivenessTimeout):
		return LeafRunResult{}, errorsNew("parallel leaves did not all start")
	}
	path := filepath.Join(input.Worktree, input.TaskID+".txt")
	lines := s.lineCount[input.TaskID]
	if err := os.WriteFile(path, []byte(strings.Repeat(input.TaskID+"\n", lines)), 0o644); err != nil {
		return LeafRunResult{}, err
	}
	if err := runGitCommand(input.Worktree, "add", input.TaskID+".txt"); err != nil {
		return LeafRunResult{}, err
	}
	if err := runGitCommand(input.Worktree, "commit", "-m", "implement "+input.TaskID); err != nil {
		return LeafRunResult{}, err
	}

	s.mu.Lock()
	s.active--
	s.mu.Unlock()
	return LeafRunResult{Parts: []LeafPart{{Type: "text", Text: "implemented " + input.TaskID}}}, nil
}

func errorsNew(message string) error { return fmt.Errorf("%s", message) }

func runGitCommand(cwd string, args ...string) error {
	command := exec.Command("git", args...)
	command.Dir = cwd
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=scheduler-test",
		"GIT_AUTHOR_EMAIL=scheduler@example.test",
		"GIT_COMMITTER_NAME=scheduler-test",
		"GIT_COMMITTER_EMAIL=scheduler@example.test",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, output)
	}
	return nil
}

type trackingRealMerge struct {
	mu        sync.Mutex
	active    int
	maxActive int
	order     []string
}

func (m *trackingRealMerge) Merge(ctx context.Context, input MergeRequest) (MergeResult, error) {
	m.mu.Lock()
	m.active++
	if m.active > m.maxActive {
		m.maxActive = m.active
	}
	m.order = append(m.order, input.TaskID)
	m.mu.Unlock()
	result, err := (defaultMergeStack{}).Merge(ctx, input)
	m.mu.Lock()
	m.active--
	m.mu.Unlock()
	return result, err
}

func settlingCoordinator(delay time.Duration) *mergecoordinator.MergeCoordinator {
	return mergecoordinator.New(mergecoordinator.Options{
		TimerFactory: func(_ float64, fn func()) mergecoordinator.Timer {
			return phase2Timer{timer: time.AfterFunc(delay, fn)}
		},
	})
}

func TestCycleRunsLeavesInParallelAndSerializesRealGitMergesByRisk(t *testing.T) {
	workspace := newGitRepository(t)
	projectID, ids := seedCycleTasks(t, 3)
	step := &concurrentGitStepLoop{
		expected: 3, allStarted: make(chan struct{}),
		lineCount: map[string]int{ids[0]: 9, ids[1]: 1, ids[2]: 4},
	}
	merges := &trackingRealMerge{}
	adaptive, cache := false, false
	scheduler := NewScheduler(SchedulerOptions{
		Workspace: workspace, Agents: phase2Agents{agent: &AgentInfo{Name: "fixer"}},
		StepLoop: step, Gate: gateFunc(func(_ context.Context, input GateInput) (GateResult, error) {
			return GateResult{
				Status: GatePass, FinalWorktreePath: input.WorktreePath, FinalBranch: input.Branch,
			}, nil
		}),
		Pools: phase2Pools{}, Provider: phase2Provider{}, MergeStack: merges,
		MergeCoordinator: settlingCoordinator(350 * time.Millisecond),
		AdaptiveCuts:     &adaptive, OutcomeCache: &cache,
	})
	maxParallel := 3.0
	result, err := scheduler.RunSchedulerCycle(context.Background(), SchedulerInput{
		RootTaskID: "root", ProjectID: projectID, DBPath: "/ignored",
		ParentSessionID: "parent", MaxParallel: &maxParallel,
	})
	if err != nil {
		t.Fatal(err)
	}
	if step.maxActive < 3 {
		t.Fatalf("max parallel leaf runs = %d, want at least 3", step.maxActive)
	}
	if merges.maxActive != 1 {
		t.Fatalf("max parallel merges = %d, want 1", merges.maxActive)
	}
	wantMergeOrder := []string{ids[1], ids[2], ids[0]}
	if !reflect.DeepEqual(merges.order, wantMergeOrder) {
		t.Fatalf("merge order = %#v, want risk order %#v", merges.order, wantMergeOrder)
	}
	if got := dispatchResultIDs(result.Dispatched); !reflect.DeepEqual(got, ids) {
		t.Fatalf("dispatch result order = %#v, want stable FIFO %#v", got, ids)
	}
	for _, id := range ids {
		if _, err := os.Stat(filepath.Join(workspace, id+".txt")); err != nil {
			t.Fatalf("merged file %s missing: %v", id, err)
		}
		task := plandb.GetPlanDB().GetTask(id)
		if task == nil || task.Status != plandb.StatusDone {
			t.Fatalf("%s status = %#v", id, task)
		}
	}
	if got := InFlightDispatchIDs(); len(got) != 0 {
		t.Fatalf("in-flight after cycle = %#v", got)
	}
}

func dispatchResultIDs(results []DispatchResult) []string {
	ids := make([]string, len(results))
	for index, result := range results {
		ids[index] = result.TaskID
	}
	return ids
}

type outcomeObserverRecorder struct {
	outcomes []leafoutcome.LeafOutcome
}

type requestCapturingStepLoop struct {
	request LeafRunRequest
}

type frontierPlannerRecorder struct {
	calls  []FrontierTickInput
	result FrontierTickResult
	errors []error
}

func (planner *frontierPlannerRecorder) RunFrontierTick(
	_ context.Context, input FrontierTickInput,
) (FrontierTickResult, error) {
	planner.calls = append(planner.calls, input)
	if len(planner.errors) > 0 {
		err := planner.errors[0]
		planner.errors = planner.errors[1:]
		if err != nil {
			return FrontierTickResult{}, err
		}
	}
	return planner.result, nil
}

func (step *requestCapturingStepLoop) RunLeaf(
	_ context.Context, request LeafRunRequest,
) (LeafRunResult, error) {
	step.request = request
	passed := true
	return LeafRunResult{
		Parts: []LeafPart{{Type: "text", Text: "implemented"}}, TestPassed: &passed,
	}, nil
}

func (recorder *outcomeObserverRecorder) Observe(outcome leafoutcome.LeafOutcome) {
	recorder.outcomes = append(recorder.outcomes, outcome)
}

func TestCycleBroadcastsLeafOutcomeToCapabilityAndBridgeObservers(t *testing.T) {
	projectID, _ := seedCycleTasks(t, 1)
	adaptive, cache, frontier := true, false, false
	defaultTier := capability.TierLow
	tracker := capability.NewCapabilityTracker(&capability.CapabilityOptions{
		AdaptiveCutsEnabled: &adaptive,
		TierMap: map[string]capability.ModelTierName{
			"model": capability.TierHigh,
		},
		DefaultTier: &defaultTier,
	})
	bridge := &outcomeObserverRecorder{}
	if got := BroadcastOutcomeObservers(nil, bridge); got != bridge {
		t.Fatal("single-observer broadcast did not preserve observer identity")
	}
	passed := true
	dispatcher := NewScheduler(SchedulerOptions{
		Workspace: t.TempDir(), Agents: phase2Agents{agent: &AgentInfo{Name: "fixer"}},
		StepLoop: &scriptedStepLoop{results: []LeafRunResult{{
			Parts: []LeafPart{{Type: "text", Text: "implemented"}}, TestPassed: &passed,
		}}},
		Gate: gateFunc(func(_ context.Context, input GateInput) (GateResult, error) {
			return GateResult{
				Status: GatePass, FinalWorktreePath: input.WorktreePath, FinalBranch: input.Branch,
			}, nil
		}),
		Pools: phase2Pools{}, Provider: phase2Provider{}, Capability: tracker,
		MergeStack: mergeStackFunc(func(context.Context, MergeRequest) (MergeResult, error) {
			return MergeResult{OK: true, Summary: "merged"}, nil
		}),
		MergeCoordinator: quickMergeCoordinator(),
		AdaptiveCuts:     &adaptive, OutcomeCache: &cache, FrontierEnabled: &frontier,
		EmitLeafOutcome: func(leafoutcome.EmitLeafOutcomeArgs) {},
		OutcomeObserver: BroadcastOutcomeObservers(
			bridge, capability.NewOutcomeObserver(tracker),
		),
	})
	dispatcher.runner = phase2Runner{}
	maxParallel := 1.0
	result, err := dispatcher.RunSchedulerCycle(context.Background(), SchedulerInput{
		RootTaskID: "root", ProjectID: projectID, DBPath: "/ignored",
		ParentSessionID: "parent", MaxParallel: &maxParallel,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Contract 2: one scheduler cycle feeds the persisted leaf outcome to both observers.
	if len(result.Dispatched) != 1 || len(bridge.outcomes) != 1 {
		t.Fatalf("dispatches/bridge observations = %d/%d", len(result.Dispatched), len(bridge.outcomes))
	}
	if keys := tracker.Snapshot().Cells.Keys(); !reflect.DeepEqual(keys, []string{"model"}) {
		t.Fatalf("capability models = %#v, want observed model", keys)
	}
}

func TestCycleDispatchesLeafWithRepoMapBriefingAndContextRefs(t *testing.T) {
	workspace := newGitRepository(t)
	markerPath := filepath.Join(workspace, "src", "marker.go")
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markerPath, []byte("package marker\n\nfunc RepositoryMarker() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runGitCommand(workspace, "add", "src/marker.go"); err != nil {
		t.Fatal(err)
	}
	if err := runGitCommand(workspace, "commit", "-m", "add marker"); err != nil {
		t.Fatal(err)
	}
	projectID, ids := seedCycleTasks(t, 1)
	task := plandb.GetPlanDB().GetTask(ids[0])
	description := "agent: explore\nfile_scope: src/marker.go\nImplement the repository marker."
	task.Description = &description
	task.Tags = []string{"scope:medium"}

	step := &requestCapturingStepLoop{}
	adaptive, cache, frontier := true, false, false
	dispatcher := NewScheduler(SchedulerOptions{
		Workspace: workspace, Agents: phase2Agents{agent: &AgentInfo{Name: "fixer"}},
		StepLoop: step,
		Gate: gateFunc(func(_ context.Context, input GateInput) (GateResult, error) {
			return GateResult{
				Status: GatePass, FinalWorktreePath: input.WorktreePath, FinalBranch: input.Branch,
			}, nil
		}),
		Pools: phase2Pools{}, Provider: phase2Provider{}, Briefing: leafbriefing.NewDefaultBuilder(),
		MergeStack: mergeStackFunc(func(context.Context, MergeRequest) (MergeResult, error) {
			return MergeResult{OK: true, Summary: "merged"}, nil
		}),
		MergeCoordinator: quickMergeCoordinator(),
		AdaptiveCuts:     &adaptive, OutcomeCache: &cache, FrontierEnabled: &frontier,
		EmitLeafOutcome: func(leafoutcome.EmitLeafOutcomeArgs) {},
	})
	dispatcher.runner = phase2Runner{}
	maxParallel := 1.0
	result, err := dispatcher.RunSchedulerCycle(context.Background(), SchedulerInput{
		RootTaskID: "root", ProjectID: projectID, DBPath: "/ignored",
		ParentSessionID: "parent", MaxParallel: &maxParallel,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Contract 3: a medium scheduler leaf receives facts, repo-map content, and context references.
	if len(result.Dispatched) != 1 {
		t.Fatalf("dispatch count = %d", len(result.Dispatched))
	}
	for _, want := range []string{
		"## Repo facts (precomputed — do not re-derive)", "function RepositoryMarker (line 3)",
		"[ctx:leaf-briefing-repo-map", "[ctx:task-description-",
	} {
		combined := step.request.SystemReminder + "\n" + step.request.Prompt
		if !strings.Contains(combined, want) {
			t.Fatalf("leaf brief missing %q:\n%s", want, combined)
		}
	}
}

func TestCycleConsultsReplannerOnEscalatedReview(t *testing.T) {
	projectID, _ := seedCycleTasks(t, 1)
	replanner := &phase2Replanner{enabled: true, result: ReplanResult{Summary: "continue"}}
	adaptive, cache, frontier := false, false, false
	dispatcher := NewScheduler(SchedulerOptions{
		Workspace: t.TempDir(), Agents: phase2Agents{agent: &AgentInfo{Name: "fixer"}},
		StepLoop: &scriptedStepLoop{results: []LeafRunResult{{
			Parts: []LeafPart{{Type: "text", Text: "implemented"}},
		}}},
		Gate: gateFunc(func(context.Context, GateInput) (GateResult, error) {
			return GateResult{Status: GateEscalated, Reason: "plan no longer fits"}, nil
		}),
		Replanner: replanner, Pools: phase2Pools{}, Provider: phase2Provider{},
		MergeCoordinator: quickMergeCoordinator(),
		AdaptiveCuts:     &adaptive, OutcomeCache: &cache, FrontierEnabled: &frontier,
		EmitLeafOutcome: func(leafoutcome.EmitLeafOutcomeArgs) {},
	})
	dispatcher.runner = phase2Runner{}
	maxParallel := 1.0
	result, err := dispatcher.RunSchedulerCycle(context.Background(), SchedulerInput{
		RootTaskID: "root", ProjectID: projectID, DBPath: "/ignored",
		ParentSessionID: "parent", MaxParallel: &maxParallel,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Contract 4: the TS escalation trigger consults the scheduler replanner once.
	if len(result.Dispatched) != 1 || replanner.calls != 1 {
		t.Fatalf("dispatches/replans = %d/%d", len(result.Dispatched), replanner.calls)
	}
}

func TestCycleConsultsPlannerWhenReadyFrontierDrains(t *testing.T) {
	projectID, _ := seedCycleTasks(t, 0)
	plandb.GetPlanDB().AddContext("implement the remaining adapter", plandb.AddContextOpts{
		Project: projectID, TaskID: "root", Kind: "residual",
	})
	planner := &frontierPlannerRecorder{result: FrontierTickResult{
		Status: "applied", TaskCount: 1, ResidualUpdated: true,
	}}
	adaptive, cache, frontier := true, false, true
	dispatcher := NewScheduler(SchedulerOptions{
		Workspace: t.TempDir(), Planner: planner, Pools: phase2Pools{},
		AdaptiveCuts: &adaptive, OutcomeCache: &cache, FrontierEnabled: &frontier,
	})
	dispatcher.runner = phase2Runner{}
	result, err := dispatcher.RunSchedulerCycle(context.Background(), SchedulerInput{
		RootTaskID: "root", ProjectID: projectID, DBPath: "/ignored",
		ParentSessionID: "parent",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Contract 4: a drained ready queue with residual work fires the planner adapter.
	if len(result.Dispatched) != 0 || len(planner.calls) != 1 ||
		planner.calls[0].Trigger != "ready-drained" {
		t.Fatalf("dispatches/planner calls = %d/%#v", len(result.Dispatched), planner.calls)
	}
}

func TestTransientFrontierPlannerFailureLeavesResidualAndRetriesNextTick(t *testing.T) {
	// Round-2 retry contract: a planner failure is incomplete, so it must not
	// persist the residual-resolved marker and a later scheduler tick retries.
	projectID, _ := seedCycleTasks(t, 0)
	plandb.GetPlanDB().AddContext("implement the remaining adapter", plandb.AddContextOpts{
		Project: projectID, TaskID: "root", Kind: "residual",
	})
	planner := &frontierPlannerRecorder{
		result: FrontierTickResult{Status: "applied", TaskCount: 1, ResidualUpdated: true},
		errors: []error{fmt.Errorf("planner temporarily unavailable")},
	}
	adaptive, cache, frontier := true, false, true
	dispatcher := NewScheduler(SchedulerOptions{
		Workspace: t.TempDir(), Planner: planner, Pools: phase2Pools{},
		AdaptiveCuts: &adaptive, OutcomeCache: &cache, FrontierEnabled: &frontier,
	})
	dispatcher.runner = phase2Runner{}
	input := SchedulerInput{
		RootTaskID: "root", ProjectID: projectID, DBPath: "/ignored", ParentSessionID: "parent",
	}
	if _, err := dispatcher.RunSchedulerCycle(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if residual := dispatcher.frontierResidual(input); residual != "implement the remaining adapter" {
		t.Fatalf("planner failure replaced residual with %q", residual)
	}
	if _, err := dispatcher.RunSchedulerCycle(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if len(planner.calls) != 2 || planner.calls[1].Tick != planner.calls[0].Tick+1 {
		t.Fatalf("planner calls = %#v", planner.calls)
	}
}

func TestFrontierMaxTicksEnvironmentUsesTSPrecedenceAndClamping(t *testing.T) {
	// Finding 7 contract: JS Number coercion applies before flooring/clamping.
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{name: "empty is zero", raw: "", want: 0},
		{name: "hex coerces", raw: "0x10", want: 16},
		{name: "binary coerces", raw: "0b11", want: 3},
		{name: "negative clamps zero", raw: "-4", want: 0},
		{name: "fraction floors", raw: "2.9", want: 2},
		{name: "large clamps ceiling", raw: "99", want: 20},
		{name: "invalid defaults", raw: "nope", want: 20},
		{name: "infinite defaults", raw: "Infinity", want: 20},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseFrontierMaxTicks(test.raw, true); got != test.want {
				t.Fatalf("parsed ticks = %d, want %d", got, test.want)
			}
		})
	}
}

func TestFrontierMaxTicksReadsEnvironmentOnce(t *testing.T) {
	// Finding 7 contract: the first lookup is cached like TS module init.
	raw := "2"
	cache := frontierMaxTicksCache{}
	lookup := func(string) (string, bool) { return raw, true }
	if got := cache.get(lookup); got != 2 {
		t.Fatalf("first value = %d", got)
	}
	raw = "9"
	if got := cache.get(lookup); got != 2 {
		t.Fatalf("cached value changed to %d", got)
	}
}

type blockingStepLoop struct {
	mu         sync.Mutex
	expected   int
	started    int
	allStarted chan struct{}
	release    chan struct{}
}

func (s *blockingStepLoop) RunLeaf(ctx context.Context, _ LeafRunRequest) (LeafRunResult, error) {
	s.mu.Lock()
	s.started++
	if s.started == s.expected {
		close(s.allStarted)
	}
	s.mu.Unlock()
	select {
	case <-s.release:
		return LeafRunResult{Parts: []LeafPart{{Type: "text", Text: "done"}}}, nil
	case <-ctx.Done():
		return LeafRunResult{}, ctx.Err()
	}
}

type staggerClock struct {
	now          time.Time
	sleepStarted chan time.Duration
	release      chan struct{}
}

func (c *staggerClock) Now() time.Time {
	return c.now
}

func (c *staggerClock) Sleep(ctx context.Context, delay time.Duration) error {
	c.sleepStarted <- delay
	select {
	case <-c.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type staggerStepLoop struct {
	started chan string
	release chan struct{}
}

func (s *staggerStepLoop) RunLeaf(ctx context.Context, input LeafRunRequest) (LeafRunResult, error) {
	s.started <- input.TaskID
	select {
	case <-s.release:
		return LeafRunResult{Parts: []LeafPart{{Type: "text", Text: "done " + input.TaskID}}}, nil
	case <-ctx.Done():
		return LeafRunResult{}, ctx.Err()
	}
}

func TestAdaptiveFanOutStartsPioneerThenDelaysSharedAgentPeers(t *testing.T) {
	for _, id := range InFlightDispatchIDs() {
		schedulerInFlightDispatches.remove(id)
	}
	projectID, ids := seedCycleTasks(t, 3)
	step := &staggerStepLoop{
		started: make(chan string, 3),
		release: make(chan struct{}),
	}
	clock := &staggerClock{
		now:          time.UnixMilli(1_700_000_000_000),
		sleepStarted: make(chan time.Duration, 1),
		release:      make(chan struct{}),
	}
	adaptive, cache := true, false
	scheduler := NewScheduler(SchedulerOptions{
		Workspace: t.TempDir(), Agents: phase2Agents{agent: &AgentInfo{Name: "fixer"}},
		StepLoop: step, Gate: gateFunc(func(_ context.Context, input GateInput) (GateResult, error) {
			return GateResult{
				Status: GatePass, FinalWorktreePath: input.WorktreePath, FinalBranch: input.Branch,
			}, nil
		}),
		Pools: phase2Pools{}, Provider: phase2Provider{},
		MergeStack: mergeStackFunc(func(context.Context, MergeRequest) (MergeResult, error) {
			return MergeResult{OK: true, Summary: "merged"}, nil
		}),
		MergeCoordinator: quickMergeCoordinator(),
		Clock:            clock,
		AdaptiveCuts:     &adaptive,
		OutcomeCache:     &cache,
	})
	scheduler.runner = phase2Runner{}
	maxParallel := 3.0
	done := make(chan SchedulerCycleResult, 1)
	go func() {
		result, _ := scheduler.RunSchedulerCycle(context.Background(), SchedulerInput{
			RootTaskID: "root", ProjectID: projectID, DBPath: "/ignored",
			ParentSessionID: "parent", MaxParallel: &maxParallel,
		})
		done <- result
	}()

	select {
	case first := <-step.started:
		if first != ids[0] {
			t.Fatalf("pioneer = %q, want first claimed %q", first, ids[0])
		}
	case <-time.After(phase2LivenessTimeout):
		t.Fatal("pioneer did not start")
	}
	select {
	case delay := <-clock.sleepStarted:
		if delay != 1500*time.Millisecond {
			t.Fatalf("stagger delay = %v", delay)
		}
	case <-time.After(phase2LivenessTimeout):
		t.Fatal("stagger sleep did not begin")
	}
	select {
	case peer := <-step.started:
		t.Fatalf("peer %q started before stagger elapsed", peer)
	case <-time.After(50 * time.Millisecond):
	}
	close(clock.release)
	peers := []string{<-step.started, <-step.started}
	peerSet := map[string]bool{peers[0]: true, peers[1]: true}
	if !peerSet[ids[1]] || !peerSet[ids[2]] {
		t.Fatalf("peer starts = %#v, want %q and %q", peers, ids[1], ids[2])
	}
	close(step.release)
	select {
	case result := <-done:
		if got := dispatchResultIDs(result.Dispatched); !reflect.DeepEqual(got, ids) {
			t.Fatalf("dispatch result order = %#v, want %#v", got, ids)
		}
	case <-time.After(phase2LivenessTimeout):
		t.Fatal("staggered cycle did not finish")
	}
}

func TestOverlappingCycleSeesGlobalInFlightCapacity(t *testing.T) {
	for _, id := range InFlightDispatchIDs() {
		schedulerInFlightDispatches.remove(id)
	}
	t.Cleanup(func() {
		for _, id := range InFlightDispatchIDs() {
			schedulerInFlightDispatches.remove(id)
		}
	})
	projectID, _ := seedCycleTasks(t, 4)
	step := &blockingStepLoop{
		expected: 2, allStarted: make(chan struct{}), release: make(chan struct{}),
	}
	adaptive, cache := false, false
	scheduler := NewScheduler(SchedulerOptions{
		Workspace: t.TempDir(), Agents: phase2Agents{agent: &AgentInfo{Name: "fixer"}},
		StepLoop: step, Gate: gateFunc(func(_ context.Context, input GateInput) (GateResult, error) {
			return GateResult{
				Status: GatePass, FinalWorktreePath: input.WorktreePath, FinalBranch: input.Branch,
			}, nil
		}),
		Pools: phase2Pools{}, Provider: phase2Provider{},
		MergeStack: mergeStackFunc(func(context.Context, MergeRequest) (MergeResult, error) {
			return MergeResult{OK: true, Summary: "merged"}, nil
		}),
		MergeCoordinator: quickMergeCoordinator(),
		AdaptiveCuts:     &adaptive, OutcomeCache: &cache,
	})
	scheduler.runner = phase2Runner{}
	maxParallel := 2.0
	input := SchedulerInput{
		RootTaskID: "root", ProjectID: projectID, DBPath: "/ignored",
		ParentSessionID: "parent", MaxParallel: &maxParallel,
	}
	firstDone := make(chan SchedulerCycleResult, 1)
	go func() {
		result, _ := scheduler.RunSchedulerCycle(context.Background(), input)
		firstDone <- result
	}()
	select {
	case <-step.allStarted:
	case <-time.After(phase2LivenessTimeout):
		t.Fatal("first cycle did not start two leaves")
	}
	if got := len(InFlightDispatchIDs()); got != 2 {
		t.Fatalf("in-flight = %d, want 2", got)
	}
	second, err := scheduler.RunSchedulerCycle(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Dispatched) != 0 {
		t.Fatalf("overlapping cycle dispatched at capacity: %#v", second.Dispatched)
	}
	close(step.release)
	select {
	case first := <-firstDone:
		if len(first.Dispatched) != 2 {
			t.Fatalf("first dispatch count = %d", len(first.Dispatched))
		}
	case <-time.After(phase2LivenessTimeout):
		t.Fatal("first cycle did not finish")
	}
	if got := InFlightDispatchIDs(); len(got) != 0 {
		t.Fatalf("in-flight after release = %#v", got)
	}
}

func TestInFlightSnapshotIsDefensiveAndRaceSafe(t *testing.T) {
	set := newInFlightDispatchSet()
	set.addAll([]string{"a", "b", "c"})
	snapshot := set.snapshot()
	snapshot[0] = "mutated"
	if got := set.snapshot(); got[0] != "a" {
		t.Fatalf("snapshot aliases registry: %#v", got)
	}
	var workers sync.WaitGroup
	for worker := 0; worker < 20; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			id := fmt.Sprintf("worker-%d", worker)
			for iteration := 0; iteration < 100; iteration++ {
				set.addAll([]string{id})
				_ = set.has(id)
				_ = set.len()
				_ = set.snapshot()
				set.remove(id)
			}
		}(worker)
	}
	workers.Wait()
}
