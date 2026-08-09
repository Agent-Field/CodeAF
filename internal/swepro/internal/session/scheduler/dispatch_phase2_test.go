package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/plandb"
	"github.com/Agent-Field/swe-pro-go/internal/session/leafoutcome"
	"github.com/Agent-Field/swe-pro-go/internal/session/loopguard"
	"github.com/Agent-Field/swe-pro-go/internal/session/mergecoordinator"
)

type phase2PlanDB struct {
	mu           sync.Mutex
	commands     [][]string
	root         *plandb.Task
	parentStatus plandb.TaskStatus
}

func (f *phase2PlanDB) Run(argv []string) plandb.RunResult {
	f.mu.Lock()
	f.commands = append(f.commands, append([]string(nil), argv...))
	f.mu.Unlock()
	if len(argv) < 2 {
		return plandb.RunResult{Code: 1, Stderr: []byte("missing op")}
	}
	switch argv[1] {
	case "show":
		id := ""
		if len(argv) > 2 {
			id = argv[2]
		}
		if f.root != nil && id == f.root.ID {
			return jsonPlanDBResult(f.root)
		}
		return jsonPlanDBResult(&plandb.Task{ID: id, Status: f.parentStatus})
	case "contexts", "what-unlocks":
		return jsonPlanDBResult([]any{})
	case "task":
		if len(argv) > 2 && argv[2] == "overview" {
			return jsonPlanDBResult(map[string]any{"project": nil, "status": map[string]any{}, "recent": []any{}})
		}
		return jsonPlanDBResult(map[string]any{"id": phase2Arg(argv, 3)})
	case "done":
		return jsonPlanDBResult(map[string]any{"id": phase2Arg(argv, 2)})
	case "context":
		return jsonPlanDBResult(map[string]any{"id": "ctx"})
	default:
		return jsonPlanDBResult(map[string]any{})
	}
}

func jsonPlanDBResult(value any) plandb.RunResult {
	raw, _ := json.Marshal(value)
	return plandb.RunResult{Code: 0, Stdout: raw, Stderr: []byte{}}
}

func phase2Arg(argv []string, index int) string {
	if index < len(argv) {
		return argv[index]
	}
	return ""
}

func (f *phase2PlanDB) hasCommand(parts ...string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, command := range f.commands {
		if containsSubsequence(command, parts) {
			return true
		}
	}
	return false
}

func containsSubsequence(command, parts []string) bool {
	if len(parts) == 0 || len(parts) > len(command) {
		return false
	}
	for start := 0; start+len(parts) <= len(command); start++ {
		match := true
		for index := range parts {
			if command[start+index] != parts[index] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

type phase2Runner struct {
	failAllocation bool
}

func (r phase2Runner) Run(_ context.Context, argv []string, _ runOptions) runResult {
	if containsSubsequence(argv, []string{"symbolic-ref", "--short", "HEAD"}) {
		return runResult{Code: 0, Stdout: []byte("main\n")}
	}
	if len(argv) >= 2 && argv[0] == "git" && argv[1] == "rev-parse" {
		if r.failAllocation && !containsSubsequence(argv, []string{"--verify"}) {
			return runResult{Code: 1}
		}
		return runResult{Code: 0, Stdout: []byte("base-sha\n")}
	}
	return runResult{Code: 0, Stdout: []byte{}}
}

type phase2Agents struct {
	agent *AgentInfo
	err   error
}

func (a phase2Agents) Get(context.Context, string) (*AgentInfo, error) {
	return a.agent, a.err
}

type phase2Pools struct{}

func (phase2Pools) CandidatesForTier(ModelTier) []ModelCandidate {
	return []ModelCandidate{{ID: "provider/model"}}
}

type phase2Provider struct {
	noModel bool
}

func (p phase2Provider) GetModel(context.Context, string, string) (any, error) {
	if p.noModel {
		return nil, errors.New("not found")
	}
	return ProviderModel{ProviderID: "provider", ID: "model"}, nil
}

func (phase2Provider) GetLanguage(context.Context, any) (any, error) {
	return "language", nil
}

type scriptedStepLoop struct {
	mu       sync.Mutex
	results  []LeafRunResult
	requests []LeafRunRequest
	calls    int
}

func (s *scriptedStepLoop) RunLeaf(_ context.Context, request LeafRunRequest) (LeafRunResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, request)
	index := s.calls
	s.calls++
	if index >= len(s.results) {
		return LeafRunResult{}, nil
	}
	return s.results[index], nil
}

type gateFunc func(context.Context, GateInput) (GateResult, error)

func (f gateFunc) GateLeaf(ctx context.Context, input GateInput) (GateResult, error) {
	return f(ctx, input)
}

type mergeStackFunc func(context.Context, MergeRequest) (MergeResult, error)

func (f mergeStackFunc) Merge(ctx context.Context, input MergeRequest) (MergeResult, error) {
	return f(ctx, input)
}

type phase2Replanner struct {
	enabled bool
	result  ReplanResult
	err     error
	calls   int
}

type permissiveLoopGuard struct{}

func (permissiveLoopGuard) Observe(loopguard.LoopAction) loopguard.LoopVerdict {
	return loopguard.LoopVerdict{Status: loopguard.LoopStatusOK}
}

func (permissiveLoopGuard) ObserveCost(*float64) loopguard.LoopVerdict {
	return loopguard.LoopVerdict{Status: loopguard.LoopStatusOK}
}

func (permissiveLoopGuard) Snapshot() loopguard.LoopGuardSnapshot {
	return loopguard.LoopGuardSnapshot{}
}

func (permissiveLoopGuard) Restore(*loopguard.LoopGuardSnapshot) {}

func (r *phase2Replanner) ShouldReplan(context.Context, string) bool {
	return r.enabled
}

func (r *phase2Replanner) Replan(context.Context, ReplanInput) (ReplanResult, error) {
	r.calls++
	return r.result, r.err
}

type phase2Timer struct {
	timer *time.Timer
}

func (t phase2Timer) Stop() { t.timer.Stop() }

func quickMergeCoordinator() *mergecoordinator.MergeCoordinator {
	return mergecoordinator.New(mergecoordinator.Options{
		TimerFactory: func(_ float64, fn func()) mergecoordinator.Timer {
			return phase2Timer{timer: time.AfterFunc(time.Millisecond, fn)}
		},
	})
}

func phase2Task(parent string) *plandb.Task {
	description := "file_scope: src/leaf.go\nImplement the leaf."
	return &plandb.Task{
		ID: "leaf", ParentTaskID: stringPointer(parent), Title: "Leaf",
		Description: &description, Status: plandb.StatusRunning, Tags: []string{},
	}
}

type terminalHarness struct {
	scheduler   *Scheduler
	db          *phase2PlanDB
	step        *scriptedStepLoop
	preserved   int
	emitted     int
	gateCalls   int
	mergeCalls  int
	lastOutcome *leafoutcome.LeafOutcome
}

func newTerminalHarness(t *testing.T, result LeafRunResult) *terminalHarness {
	t.Helper()
	adaptive, cache := false, false
	workspace := t.TempDir()
	rootDescription := "User request: ship it"
	db := &phase2PlanDB{root: &plandb.Task{
		ID: "root", Title: "Root", Description: &rootDescription,
	}}
	step := &scriptedStepLoop{results: []LeafRunResult{result}}
	h := &terminalHarness{db: db, step: step}
	h.scheduler = NewScheduler(SchedulerOptions{
		Workspace: workspace,
		Agents:    phase2Agents{agent: &AgentInfo{Name: "fixer"}},
		StepLoop:  step, Pools: phase2Pools{}, Provider: phase2Provider{},
		AdaptiveCuts: &adaptive, OutcomeCache: &cache,
		MergeCoordinator: quickMergeCoordinator(),
		PreserveRejectedWork: func(leafoutcome.PreserveRejectedWorkArgs) {
			h.preserved++
		},
		EmitLeafOutcome: func(args leafoutcome.EmitLeafOutcomeArgs) {
			h.emitted++
			outcome := args.Outcome
			h.lastOutcome = &outcome
		},
	})
	h.scheduler.planDB = db
	h.scheduler.runner = phase2Runner{}
	h.scheduler.gate = gateFunc(func(_ context.Context, input GateInput) (GateResult, error) {
		h.gateCalls++
		return GateResult{
			Status: GatePass, FinalWorktreePath: input.WorktreePath, FinalBranch: input.Branch,
		}, nil
	})
	h.scheduler.mergeStack = mergeStackFunc(func(context.Context, MergeRequest) (MergeResult, error) {
		h.mergeCalls++
		return MergeResult{OK: true, Summary: "merged"}, nil
	})
	return h
}

func (h *terminalHarness) dispatch(task *plandb.Task) DispatchResult {
	return h.scheduler.dispatchOne(context.Background(), SchedulerInput{
		RootTaskID: "root", ProjectID: "project", DBPath: "/ignored",
		ParentSessionID: "parent",
	}, dispatchItem{task: task, agentID: "scheduler:parent"}, nil)
}

func TestDispatchTerminalSideEffectMatrix(t *testing.T) {
	textResult := LeafRunResult{Parts: []LeafPart{{Type: "text", Text: "implemented"}}}
	tests := []struct {
		name        string
		mutate      func(*terminalHarness, *plandb.Task)
		success     bool
		preserve    int
		emit        int
		cascade     bool
		rawFailOnly bool
		gateCalls   int
		mergeCalls  int
	}{
		{
			name: "allocation-fail",
			mutate: func(h *terminalHarness, _ *plandb.Task) {
				h.scheduler.runner = phase2Runner{failAllocation: true}
			},
			preserve: 0, emit: 0, cascade: true,
		},
		{
			name: "unknown-agent",
			mutate: func(h *terminalHarness, _ *plandb.Task) {
				h.scheduler.agents = phase2Agents{}
			},
			preserve: 0, emit: 0, cascade: true,
		},
		{
			name: "unresolvable-model-raw-fail",
			mutate: func(h *terminalHarness, _ *plandb.Task) {
				h.scheduler.provider = phase2Provider{noModel: true}
			},
			preserve: 0, emit: 0, rawFailOnly: true,
		},
		{
			name: "empty-output",
			mutate: func(h *terminalHarness, _ *plandb.Task) {
				h.step.results = []LeafRunResult{{}}
			},
			preserve: 0, emit: 0, cascade: true,
		},
		{
			name: "read-only-success",
			mutate: func(_ *terminalHarness, task *plandb.Task) {
				description := "worktree: none\nReview the repository."
				task.Description = &description
			},
			success: true, preserve: 0, emit: 0, gateCalls: 0, mergeCalls: 0,
		},
		{
			name: "gate-fail",
			mutate: func(h *terminalHarness, _ *plandb.Task) {
				h.scheduler.gate = gateFunc(func(context.Context, GateInput) (GateResult, error) {
					h.gateCalls++
					return GateResult{Status: GateFail, Reason: "review failed"}, nil
				})
			},
			preserve: 1, emit: 1, cascade: true, gateCalls: 1,
		},
		{
			name: "gate-escalated",
			mutate: func(h *terminalHarness, _ *plandb.Task) {
				h.scheduler.gate = gateFunc(func(context.Context, GateInput) (GateResult, error) {
					h.gateCalls++
					return GateResult{Status: GateEscalated, Reason: "needs a stronger plan"}, nil
				})
			},
			preserve: 1, emit: 1, cascade: true, gateCalls: 1,
		},
		{
			name: "merge-fail",
			mutate: func(h *terminalHarness, _ *plandb.Task) {
				h.scheduler.mergeStack = mergeStackFunc(func(context.Context, MergeRequest) (MergeResult, error) {
					h.mergeCalls++
					return MergeResult{OK: false, Summary: "conflict"}, nil
				})
			},
			preserve: 1, emit: 1, cascade: true, gateCalls: 1, mergeCalls: 1,
		},
		{
			name: "done-partial-merges",
			mutate: func(h *terminalHarness, _ *plandb.Task) {
				h.scheduler.gate = gateFunc(func(context.Context, GateInput) (GateResult, error) {
					h.gateCalls++
					return GateResult{Status: GateDonePartial}, nil
				})
			},
			success: true, preserve: 0, emit: 1, gateCalls: 1, mergeCalls: 1,
		},
		{
			name: "skipped-gate-merges",
			mutate: func(h *terminalHarness, _ *plandb.Task) {
				h.scheduler.gate = gateFunc(func(context.Context, GateInput) (GateResult, error) {
					h.gateCalls++
					return GateResult{Status: GateSkipped, Reason: "no-op"}, nil
				})
			},
			success: true, preserve: 0, emit: 1, gateCalls: 1, mergeCalls: 1,
		},
		{
			name:    "success",
			mutate:  func(*terminalHarness, *plandb.Task) {},
			success: true, preserve: 0, emit: 1, gateCalls: 1, mergeCalls: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newTerminalHarness(t, textResult)
			task := phase2Task("root")
			test.mutate(h, task)
			got := h.dispatch(task)
			if got.Success != test.success {
				t.Fatalf("success = %v, result=%#v", got.Success, got)
			}
			if h.preserved != test.preserve || h.emitted != test.emit {
				t.Fatalf("preserve/emit = %d/%d, want %d/%d", h.preserved, h.emitted, test.preserve, test.emit)
			}
			cascaded := h.db.hasCommand("what-unlocks", task.ID)
			if cascaded != test.cascade {
				t.Fatalf("cascade = %v, commands=%#v", cascaded, h.db.commands)
			}
			if test.rawFailOnly {
				if !h.db.hasCommand("task", "fail", task.ID) || cascaded {
					t.Fatalf("unresolvable model did not keep raw task-fail path: %#v", h.db.commands)
				}
			}
			if h.gateCalls != test.gateCalls || h.mergeCalls != test.mergeCalls {
				t.Fatalf("gate/merge calls = %d/%d, want %d/%d", h.gateCalls, h.mergeCalls, test.gateCalls, test.mergeCalls)
			}
		})
	}
}

func TestDispatchEscalatedReplannerAbortAndOuterCatch(t *testing.T) {
	t.Run("replanner-abort", func(t *testing.T) {
		h := newTerminalHarness(t, LeafRunResult{Parts: []LeafPart{{Type: "text", Text: "implemented"}}})
		h.scheduler.gate = gateFunc(func(context.Context, GateInput) (GateResult, error) {
			h.gateCalls++
			return GateResult{Status: GateEscalated, Reason: "review uncertain"}, nil
		})
		replanner := &phase2Replanner{
			enabled: true,
			result:  ReplanResult{Abort: true, Summary: "budget exhausted"},
		}
		h.scheduler.replanner = replanner
		got := h.dispatch(phase2Task("root"))
		if got.Success || replanner.calls != 1 || h.preserved != 1 || h.emitted != 1 {
			t.Fatalf("result=%#v replans=%d preserved=%d emitted=%d", got, replanner.calls, h.preserved, h.emitted)
		}
		if got.Gate == nil || got.Gate.Status != GateFail ||
			got.Error == nil || *got.Error != "replanner abort: budget exhausted" {
			t.Fatalf("unexpected abort result: %#v", got)
		}
	})

	t.Run("gate-error-is-caught-and-cascaded-without-preserve-or-outcome", func(t *testing.T) {
		h := newTerminalHarness(t, LeafRunResult{Parts: []LeafPart{{Type: "text", Text: "implemented"}}})
		h.scheduler.gate = gateFunc(func(context.Context, GateInput) (GateResult, error) {
			return GateResult{}, errors.New("gate service unavailable")
		})
		got := h.dispatch(phase2Task("root"))
		if got.Success || got.Error == nil || *got.Error != "gate service unavailable" {
			t.Fatalf("result=%#v", got)
		}
		if h.preserved != 0 || h.emitted != 0 || !h.db.hasCommand("what-unlocks", "leaf") {
			t.Fatalf("preserved=%d emitted=%d commands=%#v", h.preserved, h.emitted, h.db.commands)
		}
	})
}

func TestNewSchedulerUsesOutcomeCacheAndFrontierEnvironmentDefaults(t *testing.T) {
	adaptive := true
	t.Run("explicitly-enabled", func(t *testing.T) {
		t.Setenv("CODEAF_OUTCOME_CACHE", "1")
		t.Setenv("CODEAF_FRONTIER", "1")
		dispatcher := NewScheduler(SchedulerOptions{AdaptiveCuts: &adaptive})
		if !dispatcher.outcomeCache || !dispatcher.frontier {
			t.Fatalf("cache/frontier = %v/%v, want enabled", dispatcher.outcomeCache, dispatcher.frontier)
		}
	})
	t.Run("kill-switches", func(t *testing.T) {
		t.Setenv("CODEAF_OUTCOME_CACHE", "0")
		t.Setenv("CODEAF_FRONTIER", "0")
		dispatcher := NewScheduler(SchedulerOptions{AdaptiveCuts: &adaptive})
		if dispatcher.outcomeCache || dispatcher.frontier {
			t.Fatalf("cache/frontier = %v/%v, want disabled", dispatcher.outcomeCache, dispatcher.frontier)
		}
	})
}

func TestDispatchProviderModelNotFoundRetryAndMessagePrecedence(t *testing.T) {
	t.Run("name-retries-once", func(t *testing.T) {
		h := newTerminalHarness(t, LeafRunResult{})
		h.step.results = []LeafRunResult{
			{ErrorName: "ProviderModelNotFoundError"},
			{Parts: []LeafPart{{Type: "text", Text: "second attempt"}}},
		}
		got := h.dispatch(phase2Task("root"))
		if !got.Success || h.step.calls != 2 || h.preserved != 1 {
			t.Fatalf("result=%#v calls=%d preserved=%d", got, h.step.calls, h.preserved)
		}
	})

	t.Run("data-message-masks-name-kept-bug", func(t *testing.T) {
		h := newTerminalHarness(t, LeafRunResult{
			ErrorName: "ProviderModelNotFoundError", ErrorMessage: "catalog unavailable",
		})
		got := h.dispatch(phase2Task("root"))
		if got.Success || h.step.calls != 1 || h.preserved != 0 || h.emitted != 0 {
			t.Fatalf("result=%#v calls=%d preserved=%d emitted=%d", got, h.step.calls, h.preserved, h.emitted)
		}
	})
}

func TestDispatchLoopGuardStopPreservesCascadesAndEmits(t *testing.T) {
	h := newTerminalHarness(t, LeafRunResult{Parts: []LeafPart{
		{Type: "tool", Tool: "read", ArgsKey: "same"},
		{Type: "tool", Tool: "read", ArgsKey: "same"},
		{Type: "tool", Tool: "read", ArgsKey: "same"},
		{Type: "text", Text: "still looping"},
	}})
	h.scheduler.adaptiveCuts = true
	got := h.dispatch(phase2Task("root"))
	if got.Success || h.preserved != 1 || h.emitted != 1 || !h.db.hasCommand("what-unlocks", "leaf") {
		t.Fatalf("result=%#v preserved=%d emitted=%d commands=%#v", got, h.preserved, h.emitted, h.db.commands)
	}
}

func TestDispatchFreshContextRetryAndTooHardSplit(t *testing.T) {
	t.Run("fresh-context-retries-exhaust", func(t *testing.T) {
		h := newTerminalHarness(t, LeafRunResult{})
		h.scheduler.adaptiveCuts = true
		h.scheduler.loopGuard = func(loopguard.LoopGuardOptions) loopguard.LoopGuard {
			return permissiveLoopGuard{}
		}
		h.step.results = []LeafRunResult{
			{
				Messages: assistantMessages(9),
				Parts:    []LeafPart{{Type: "text", Text: "still investigating"}},
			},
			{Parts: []LeafPart{{Type: "text", Text: "implemented after retry"}}},
			{Parts: []LeafPart{{Type: "text", Text: "implemented after second retry"}}},
		}
		got := h.dispatch(phase2Task("root"))
		if got.Success || h.step.calls != 3 || h.preserved != 3 || h.emitted != 1 {
			t.Fatalf("result=%#v calls=%d preserved=%d emitted=%d", got, h.step.calls, h.preserved, h.emitted)
		}
	})

	t.Run("too-hard-split", func(t *testing.T) {
		h := newTerminalHarness(t, LeafRunResult{})
		h.scheduler.adaptiveCuts = true
		h.scheduler.frontier = true
		h.scheduler.loopGuard = func(loopguard.LoopGuardOptions) loopguard.LoopGuard {
			return permissiveLoopGuard{}
		}
		h.step.results = []LeafRunResult{{
			Messages: assistantMessages(12),
			Parts: []LeafPart{
				{Type: "tool", Tool: "edit", ArgsKey: "src/a.go"},
				{Type: "tool", Tool: "edit", ArgsKey: "src/b.go"},
				{Type: "tool", Tool: "edit", ArgsKey: "src/c.go"},
				{Type: "tool", Tool: "edit", ArgsKey: "src/d.go"},
				{Type: "text", Text: "AssertionError: expected all cases to pass"},
			},
		}}
		task := phase2Task("root")
		description := "file_scope: src/a.go, src/b.go, src/c.go, src/d.go\nImplement the large leaf."
		task.Description = &description
		got := h.dispatch(task)
		if got.Success || got.Error == nil ||
			*got.Error != "split-requested: 4 parts (0 created in place)" {
			t.Fatalf("result=%#v", got)
		}
		if h.preserved != 1 || h.emitted != 1 || !h.db.hasCommand("what-unlocks", "leaf") {
			t.Fatalf("preserved=%d emitted=%d commands=%#v", h.preserved, h.emitted, h.db.commands)
		}
	})
}

func assistantMessages(count int) []*leafoutcome.SessionMessage {
	role := "assistant"
	messages := make([]*leafoutcome.SessionMessage, count)
	for index := range messages {
		messages[index] = &leafoutcome.SessionMessage{
			Info:  &leafoutcome.SessionMessageInfo{Role: &role},
			Parts: []*leafoutcome.SessionMessagePart{},
		}
	}
	return messages
}

func TestDeferredChildIsDoneBeforeMergeAndFailedDrainDoesNotCascadeChild(t *testing.T) {
	t.Run("deferred-child-done", func(t *testing.T) {
		h := newTerminalHarness(t, LeafRunResult{Parts: []LeafPart{{Type: "text", Text: "child work"}}})
		h.scheduler.gate = gateFunc(func(_ context.Context, input GateInput) (GateResult, error) {
			h.gateCalls++
			return GateResult{
				Status:            GatePass,
				FinalWorktreePath: input.WorktreePath,
				FinalBranch:       "plandb/repair-child",
			}, nil
		})
		parentDir := filepath.Join(h.scheduler.workspace, ".plandb", "wt-parent")
		if err := os.MkdirAll(parentDir, 0o755); err != nil {
			t.Fatal(err)
		}
		h.db.parentStatus = plandb.StatusRunning
		child := phase2Task("parent")
		child.ID = "child"
		got := h.dispatch(child)
		if !got.Success || h.mergeCalls != 0 || !h.db.hasCommand("done", "child") {
			t.Fatalf("result=%#v mergeCalls=%d commands=%#v", got, h.mergeCalls, h.db.commands)
		}
		queued := schedulerPendingChildMerges.take("parent")
		if len(queued) != 1 || queued[0].TaskID != "child" ||
			queued[0].SourceBranch != "plandb/repair-child" {
			t.Fatalf("queued = %#v", queued)
		}
		if got.Summary == nil || !strings.Contains(*got.Summary, "Merge: deferred — parent parent still active") {
			t.Fatalf("summary = %#v", got.Summary)
		}
	})

	t.Run("failed-drain-leaves-child-done", func(t *testing.T) {
		h := newTerminalHarness(t, LeafRunResult{Parts: []LeafPart{{Type: "text", Text: "parent work"}}})
		schedulerPendingChildMerges.take("leaf")
		schedulerPendingChildMerges.enqueue("leaf", PendingChildMerge{
			WorktreePath: "/missing/child", TaskID: "child", TaskTitle: "Child",
			ParentBranch: "plandb/leaf", ParentWorktreePath: "/missing/parent",
			SourceBranch: "plandb/repair-child",
		})
		h.scheduler.mergeStack = mergeStackFunc(func(_ context.Context, input MergeRequest) (MergeResult, error) {
			h.mergeCalls++
			if input.TaskID == "child" {
				if input.SourceBranch != "plandb/repair-child" {
					t.Fatalf("deferred child source branch = %q", input.SourceBranch)
				}
				return MergeResult{OK: false, Summary: "child conflict"}, nil
			}
			return MergeResult{OK: true, Summary: "parent merged"}, nil
		})
		got := h.dispatch(phase2Task("root"))
		if !got.Success || h.mergeCalls != 2 {
			t.Fatalf("result=%#v mergeCalls=%d", got, h.mergeCalls)
		}
		if h.db.hasCommand("task", "fail", "child") || h.db.hasCommand("what-unlocks", "child") {
			t.Fatalf("failed deferred child was cascaded: %#v", h.db.commands)
		}
	})
}
