package scheduler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/plandb"
	"github.com/Agent-Field/swe-pro-go/internal/session/merger"
	"github.com/Agent-Field/swe-pro-go/internal/session/mergerecovery"
)

type mergePromptOps struct{}

func (mergePromptOps) ResolvePromptParts(context.Context, string) ([]any, error) {
	return nil, nil
}

func (mergePromptOps) Prompt(context.Context, any) (any, error) { return nil, nil }

func (mergePromptOps) Cancel(context.Context, string) error { return nil }

func stagedRebaseConflict(t *testing.T, taskID string) (workspace, worktree, targetBranch, sourceBranch string) {
	t.Helper()
	workspace = newGitRepository(t)
	targetBranch = gitRun(t, workspace, "branch", "--show-current")
	worktree = filepath.Join(t.TempDir(), "leaf-worktree")
	sourceBranch = "plandb/" + taskID
	gitRun(t, workspace, "worktree", "add", "-b", sourceBranch, worktree, "HEAD")
	if err := os.WriteFile(filepath.Join(worktree, "tracked.txt"), []byte("leaf intent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, worktree, "add", "tracked.txt")
	gitRun(t, worktree, "commit", "-m", "leaf intent")
	if err := os.WriteFile(filepath.Join(workspace, "tracked.txt"), []byte("target intent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, workspace, "add", "tracked.txt")
	gitRun(t, workspace, "commit", "-m", "target intent")
	return workspace, worktree, targetBranch, sourceBranch
}

func TestDefaultMergeStagedConflictDispatchesSemanticMergerWithTSPromptShape(t *testing.T) {
	workspace, worktree, targetBranch, sourceBranch := stagedRebaseConflict(t, "leaf")
	var captured merger.DispatchRequest
	recoveryCalls := 0
	stack := newDefaultMergeStack(MergeStackOptions{
		MergerDispatcher: merger.DispatcherFunc(func(_ context.Context, request merger.DispatchRequest) (merger.DispatchResult, error) {
			captured = request
			if err := os.WriteFile(filepath.Join(worktree, "tracked.txt"), []byte("combined intent\n"), 0o644); err != nil {
				return merger.DispatchResult{}, err
			}
			return merger.DispatchResult{Data: merger.MergerDecision{
				Result: "resolved", Reason: "kept both leaf and target behavior",
				FilesTouched: []merger.FileTouched{{File: "tracked.txt", Summary: "combined"}},
			}}, nil
		}),
		RecoveryClient: mergerecovery.ClientFunc(func(mergerecovery.Request) error {
			recoveryCalls++
			return errors.New("recovery must not run after verified semantic resolution")
		}),
	})
	result, err := stack.Merge(context.Background(), MergeRequest{
		Workspace: workspace, Worktree: worktree, MergeAt: workspace,
		TaskID: "leaf", TaskTitle: "Integrate leaf", SourceBranch: sourceBranch,
		TargetBranch: targetBranch, ParentSessionID: "parent-session",
		PromptOps: mergePromptOps{}, UserGoal: "preserve both behaviors", Language: "language-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || !strings.Contains(result.Summary,
		"→ merger resolved (1 files: kept both leaf and target behavior)") {
		t.Fatalf("merge result = %#v", result)
	}
	if recoveryCalls != 0 {
		t.Fatalf("recovery calls = %d", recoveryCalls)
	}
	if captured.Agent != "merger" || captured.ParentSessionID != "parent-session" ||
		captured.Workspace != worktree || captured.OutputPath != filepath.Join(worktree, ".codeaf", "agents", "merger", "leaf.json") ||
		captured.MaxRetries != 1 || captured.TimeoutMS != 15*60_000 || captured.Label != "merger" ||
		!captured.Tools.Edit || !captured.Tools.ApplyPatch {
		t.Fatalf("semantic dispatch = %#v", captured)
	}
	for _, fragment := range []string{
		"# Semantic merge conflict resolution", "Task: Integrate leaf (leaf)",
		"Source branch: " + sourceBranch, "Target branch: " + targetBranch,
		"## User goal\n\npreserve both behaviors", "## Prior conflict report",
		"## Current git status", "## Conflicted files\n\n(no conflict markers detected — may already be resolved or pipeline state is unusual)",
		"write a single JSON MergerDecision object to " + captured.OutputPath,
	} {
		if !strings.Contains(captured.TaskPrompt, fragment) {
			t.Fatalf("semantic prompt missing %q:\n%s", fragment, captured.TaskPrompt)
		}
	}
}

func TestDefaultMergeSemanticFailuresFallThroughAndPreserveHonestFailure(t *testing.T) {
	tests := []struct {
		name         string
		dispatcher   merger.Dispatcher
		leaveMarkers bool
	}{
		{
			name: "unresolvable",
			dispatcher: merger.DispatcherFunc(func(context.Context, merger.DispatchRequest) (merger.DispatchResult, error) {
				return merger.DispatchResult{Data: merger.MergerDecision{
					Result: "unresolvable", Reason: "intent conflict",
					Unresolved: []merger.UnresolvedFile{{File: "tracked.txt", Detail: "contradiction"}},
				}}, nil
			}),
		},
		{
			name: "dispatch error",
			dispatcher: merger.DispatcherFunc(func(context.Context, merger.DispatchRequest) (merger.DispatchResult, error) {
				return merger.DispatchResult{}, errors.New("agent crashed")
			}),
		},
		{
			name: "resolved but markers remain", leaveMarkers: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace, worktree, targetBranch, sourceBranch := stagedRebaseConflict(t, "leaf")
			recoveryCalls := 0
			dispatcher := test.dispatcher
			if test.leaveMarkers {
				dispatcher = merger.DispatcherFunc(func(context.Context, merger.DispatchRequest) (merger.DispatchResult, error) {
					if err := os.WriteFile(filepath.Join(worktree, "tracked.txt"), []byte("<<<<<<< ours\n=======\n>>>>>>> theirs\n"), 0o644); err != nil {
						return merger.DispatchResult{}, err
					}
					return merger.DispatchResult{Data: merger.MergerDecision{
						Result: "resolved", Reason: "incorrect claim",
					}}, nil
				})
			}
			stack := newDefaultMergeStack(MergeStackOptions{
				MergerDispatcher: dispatcher,
				RecoveryClient: mergerecovery.ClientFunc(func(request mergerecovery.Request) error {
					recoveryCalls++
					tool, ok := request.Tool("report")
					if !ok {
						return errors.New("report tool missing")
					}
					_, err := tool.Execute(map[string]any{"ok": false, "summary": "still conflicted"})
					return err
				}),
			})
			result, err := stack.Merge(context.Background(), MergeRequest{
				Workspace: workspace, Worktree: worktree, MergeAt: workspace,
				TaskID: "leaf", TaskTitle: "Leaf", SourceBranch: sourceBranch,
				TargetBranch: targetBranch, ParentSessionID: "parent",
				PromptOps: mergePromptOps{}, Language: "language-model",
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.OK || recoveryCalls != 1 || !strings.Contains(result.Summary, "→ recovery failed: still conflicted") {
				t.Fatalf("result/calls = %#v/%d", result, recoveryCalls)
			}
			wantConflict := filepath.Join(workspace, ".plandb", "conflicts", "leaf.md")
			if result.ConflictPath != wantConflict {
				t.Fatalf("conflict path = %q, want %q", result.ConflictPath, wantConflict)
			}
		})
	}
}

func TestDefaultMergeWithoutRecoveryPreservesFastFailureExactly(t *testing.T) {
	workspace, worktree, targetBranch, sourceBranch := stagedRebaseConflict(t, "leaf")
	stack := newDefaultMergeStack(MergeStackOptions{})
	result, err := stack.Merge(context.Background(), MergeRequest{
		Workspace: workspace, Worktree: worktree, MergeAt: workspace,
		TaskID: "leaf", TaskTitle: "Leaf", SourceBranch: sourceBranch,
		TargetBranch: targetBranch, ParentSessionID: "parent", Language: "language-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || !strings.Contains(result.Summary, "rebase onto "+targetBranch+" produced conflicts; worktree left at "+worktree) {
		t.Fatalf("fast failure = %#v", result)
	}
	if result.ConflictPath != filepath.Join(workspace, ".plandb", "conflicts", "leaf.md") {
		t.Fatalf("fast conflict path = %q", result.ConflictPath)
	}
}

func TestDefaultMergeStageConditionsMatchTypeScript(t *testing.T) {
	t.Run("fast success skips both model stages", func(t *testing.T) {
		workspace := newGitRepository(t)
		targetBranch := gitRun(t, workspace, "branch", "--show-current")
		worktree := filepath.Join(t.TempDir(), "leaf-worktree")
		gitRun(t, workspace, "worktree", "add", "-b", "plandb/leaf", worktree, "HEAD")
		if err := os.WriteFile(filepath.Join(worktree, "leaf.txt"), []byte("leaf\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, worktree, "add", "leaf.txt")
		gitRun(t, worktree, "commit", "-m", "clean leaf")
		semanticCalls, recoveryCalls := 0, 0
		stack := newDefaultMergeStack(MergeStackOptions{
			MergerDispatcher: merger.DispatcherFunc(func(context.Context, merger.DispatchRequest) (merger.DispatchResult, error) {
				semanticCalls++
				return merger.DispatchResult{}, nil
			}),
			RecoveryClient: mergerecovery.ClientFunc(func(mergerecovery.Request) error {
				recoveryCalls++
				return nil
			}),
		})
		result, err := stack.Merge(context.Background(), MergeRequest{
			Workspace: workspace, Worktree: worktree, MergeAt: workspace,
			TaskID: "leaf", TaskTitle: "Leaf", TargetBranch: targetBranch,
			SourceBranch: "plandb/leaf", ParentSessionID: "parent",
			PromptOps: mergePromptOps{}, Language: "language-model",
		})
		if err != nil || !result.OK || semanticCalls != 0 || recoveryCalls != 0 {
			t.Fatalf("fast result/calls = %#v, %v, %d/%d", result, err, semanticCalls, recoveryCalls)
		}
	})

	t.Run("nil language preserves fast failure and skips both", func(t *testing.T) {
		workspace, worktree, targetBranch, sourceBranch := stagedRebaseConflict(t, "leaf")
		semanticCalls, recoveryCalls := 0, 0
		stack := newDefaultMergeStack(MergeStackOptions{
			MergerDispatcher: merger.DispatcherFunc(func(context.Context, merger.DispatchRequest) (merger.DispatchResult, error) {
				semanticCalls++
				return merger.DispatchResult{}, nil
			}),
			RecoveryClient: mergerecovery.ClientFunc(func(mergerecovery.Request) error {
				recoveryCalls++
				return nil
			}),
		})
		result, err := stack.Merge(context.Background(), MergeRequest{
			Workspace: workspace, Worktree: worktree, MergeAt: workspace,
			TaskID: "leaf", TaskTitle: "Leaf", TargetBranch: targetBranch,
			SourceBranch: sourceBranch, ParentSessionID: "parent", PromptOps: mergePromptOps{},
		})
		if err != nil || result.OK || semanticCalls != 0 || recoveryCalls != 0 {
			t.Fatalf("nil-language result/calls = %#v, %v, %d/%d", result, err, semanticCalls, recoveryCalls)
		}
	})

	for _, test := range []struct {
		name      string
		parentID  string
		promptOps PromptOps
	}{
		{name: "missing session context", promptOps: mergePromptOps{}},
		{name: "missing prompt ops", parentID: "parent"},
	} {
		t.Run(test.name+" skips semantic but runs recovery", func(t *testing.T) {
			workspace, worktree, targetBranch, sourceBranch := stagedRebaseConflict(t, "leaf")
			semanticCalls, recoveryCalls := 0, 0
			stack := newDefaultMergeStack(MergeStackOptions{
				MergerDispatcher: merger.DispatcherFunc(func(context.Context, merger.DispatchRequest) (merger.DispatchResult, error) {
					semanticCalls++
					return merger.DispatchResult{}, nil
				}),
				RecoveryClient: mergerecovery.ClientFunc(func(request mergerecovery.Request) error {
					recoveryCalls++
					tool, _ := request.Tool("report")
					_, err := tool.Execute(map[string]any{"ok": true, "summary": "recovered"})
					return err
				}),
			})
			result, err := stack.Merge(context.Background(), MergeRequest{
				Workspace: workspace, Worktree: worktree, MergeAt: workspace,
				TaskID: "leaf", TaskTitle: "Leaf", TargetBranch: targetBranch,
				SourceBranch: sourceBranch, ParentSessionID: test.parentID,
				PromptOps: test.promptOps, Language: "language-model",
			})
			if err != nil || !result.OK || semanticCalls != 0 || recoveryCalls != 1 ||
				!strings.Contains(result.Summary, "→ recovered: recovered") {
				t.Fatalf("context result/calls = %#v, %v, %d/%d", result, err, semanticCalls, recoveryCalls)
			}
		})
	}
}

func TestFastMergeSucceedsAfterBudgetExhaustion(t *testing.T) {
	workspace := newGitRepository(t)
	targetBranch := gitRun(t, workspace, "branch", "--show-current")
	worktree := filepath.Join(t.TempDir(), "leaf-worktree")
	gitRun(t, workspace, "worktree", "add", "-b", "plandb/expired-leaf", worktree, "HEAD")
	if err := os.WriteFile(filepath.Join(worktree, "leaf.txt"), []byte("leaf\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, worktree, "add", "leaf.txt")
	gitRun(t, worktree, "commit", "-m", "clean leaf")

	semanticCalls := 0
	stack := newDefaultMergeStack(MergeStackOptions{
		MergerDispatcher: merger.DispatcherFunc(func(context.Context, merger.DispatchRequest) (merger.DispatchResult, error) {
			semanticCalls++
			return merger.DispatchResult{}, nil
		}),
	})
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	result, err := stack.Merge(expired, MergeRequest{
		Workspace: workspace, Worktree: worktree, MergeAt: workspace,
		TaskID: "expired-leaf", TaskTitle: "Expired leaf", TargetBranch: targetBranch,
		SourceBranch: "plandb/expired-leaf", ParentSessionID: "parent",
		PromptOps: mergePromptOps{}, Language: "language-model",
	})
	if err != nil || !result.OK {
		t.Fatalf("fast merge after budget exhaustion = %#v, %v", result, err)
	}
	if semanticCalls != 0 {
		t.Fatalf("semantic merger dispatched %d times after fast success", semanticCalls)
	}
}

type conflictStepLoop struct{}

func (conflictStepLoop) RunLeaf(_ context.Context, input LeafRunRequest) (LeafRunResult, error) {
	if err := os.WriteFile(filepath.Join(input.Worktree, "tracked.txt"), []byte("leaf intent\n"), 0o644); err != nil {
		return LeafRunResult{}, err
	}
	if err := runGitCommand(input.Worktree, "add", "tracked.txt"); err != nil {
		return LeafRunResult{}, err
	}
	if err := runGitCommand(input.Worktree, "commit", "-m", "leaf intent"); err != nil {
		return LeafRunResult{}, err
	}
	return LeafRunResult{Parts: []LeafPart{{Type: "text", Text: "implemented"}}}, nil
}

func TestVerifiedSemanticMergeMarksLeafDoneAndUnblocksDependent(t *testing.T) {
	workspace := newGitRepository(t)
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	project := db.Init("semantic-unblock")
	rootDescription := "Preserve both behaviors"
	root, err := db.AddTask(plandb.AddTaskInput{
		Title: "Root", Description: &rootDescription, Project: project.ID, CustomID: "root",
	})
	if err != nil {
		t.Fatal(err)
	}
	leafDescription := "file_scope: tracked.txt\nChange the tracked behavior"
	leaf, err := db.AddTask(plandb.AddTaskInput{
		Title: "Leaf", Description: &leafDescription, Project: project.ID,
		Parent: root.ID, CustomID: "leaf",
	})
	if err != nil {
		t.Fatal(err)
	}
	feed := plandb.DepFeedsInto
	dependentDescription := "file_scope: downstream.txt\nUse the merged behavior"
	dependent, err := db.AddTask(plandb.AddTaskInput{
		Title: "Downstream", Description: &dependentDescription, Project: project.ID,
		Parent: root.ID, CustomID: "downstream",
		Deps: []plandb.DepSpec{{TaskID: leaf.ID, Kind: &feed}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if dependent.Status != plandb.StatusPending {
		t.Fatalf("dependent starts %s, want pending", dependent.Status)
	}

	masterChanged := false
	semanticCalls := 0
	adaptive, cache, frontier := false, false, false
	pump := NewScheduler(SchedulerOptions{
		Workspace: workspace, Agents: phase2Agents{agent: &AgentInfo{Name: "fixer"}},
		StepLoop: conflictStepLoop{}, Pools: phase2Pools{}, Provider: phase2Provider{},
		Gate: gateFunc(func(_ context.Context, input GateInput) (GateResult, error) {
			if !masterChanged {
				masterChanged = true
				if err := os.WriteFile(filepath.Join(workspace, "tracked.txt"), []byte("target intent\n"), 0o644); err != nil {
					return GateResult{}, err
				}
				if err := runGitCommand(workspace, "add", "tracked.txt"); err != nil {
					return GateResult{}, err
				}
				if err := runGitCommand(workspace, "commit", "-m", "target intent"); err != nil {
					return GateResult{}, err
				}
			}
			return GateResult{Status: GatePass, FinalWorktreePath: input.WorktreePath, FinalBranch: input.Branch}, nil
		}),
		DefaultMerge: MergeStackOptions{MergerDispatcher: merger.DispatcherFunc(func(_ context.Context, request merger.DispatchRequest) (merger.DispatchResult, error) {
			semanticCalls++
			if !strings.HasSuffix(request.OutputPath, "/"+leaf.ID+".json") || request.ParentSessionID != "parent" {
				return merger.DispatchResult{}, errors.New("semantic dispatcher received wrong task/session")
			}
			if err := os.WriteFile(filepath.Join(request.Workspace, "tracked.txt"), []byte("combined intent\n"), 0o644); err != nil {
				return merger.DispatchResult{}, err
			}
			return merger.DispatchResult{Data: merger.MergerDecision{
				Result: "resolved", Reason: "combined",
				FilesTouched: []merger.FileTouched{{File: "tracked.txt", Summary: "combined"}},
			}}, nil
		})},
		MergeCoordinator: quickMergeCoordinator(), AdaptiveCuts: &adaptive,
		OutcomeCache: &cache, FrontierEnabled: &frontier,
	})
	maxParallel := 1.0
	result, err := pump.RunSchedulerCycle(context.Background(), SchedulerInput{
		RootTaskID: root.ID, ProjectID: project.ID, DBPath: "/ignored",
		ParentSessionID: "parent", PromptOps: mergePromptOps{}, MaxParallel: &maxParallel,
	})
	if err != nil {
		t.Fatal(err)
	}
	if semanticCalls != 1 || len(result.Dispatched) != 1 || !result.Dispatched[0].Success {
		t.Fatalf("semantic calls/dispatch = %d/%#v", semanticCalls, result.Dispatched)
	}
	if got := db.GetTask(leaf.ID); got == nil || got.Status != plandb.StatusDone {
		t.Fatalf("leaf after semantic merge = %#v", got)
	}
	if got := db.GetTask(dependent.ID); got == nil || got.Status != plandb.StatusReady {
		t.Fatalf("dependent after semantic merge = %#v", got)
	}
}
