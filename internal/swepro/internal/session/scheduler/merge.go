// This file ports src/session/plandb-scheduler.ts:580-613, 1310-1428, and
// 4012-4277 from swe-pro (commit 3b25a1a).
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/leafmerger"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/mergecoordinator"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/merger"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/mergerecovery"
)

// MergeStackOptions supplies the two model-driven recovery clients used after
// LeafMerger's procedural fast path.
type MergeStackOptions struct {
	MergerDispatcher merger.Dispatcher
	RecoveryClient   mergerecovery.Client
}

type defaultMergeStack struct {
	options MergeStackOptions
}

func newDefaultMergeStack(options MergeStackOptions) MergeStack {
	return defaultMergeStack{options: options}
}

func (m defaultMergeStack) Merge(ctx context.Context, input MergeRequest) (MergeResult, error) {
	// The fast rebase/merge is bookkeeping over a diff that already exists and
	// must survive budget exhaustion. Downstream model-driven recovery keeps the
	// original context and remains cancellable.
	var source *string
	if input.SourceBranch != "" {
		value := input.SourceBranch
		source = &value
	}
	sourceBranch := input.SourceBranch
	if sourceBranch == "" {
		sourceBranch = "plandb/" + input.TaskID
	}
	fast, err := leafmerger.New(leafmerger.Input{
		Workspace: input.Workspace, Worktree: input.Worktree, MergeAt: input.MergeAt,
		TaskID: input.TaskID, TaskTitle: input.TaskTitle, TargetBranch: input.TargetBranch,
		SourceBranch: source,
	}, nil).Run()
	if err != nil {
		return MergeResult{}, err
	}
	if fast.OK || input.Language == nil {
		return MergeResult{OK: fast.OK, Summary: fast.Summary, ConflictPath: fast.ConflictPath}, nil
	}

	if input.ParentSessionID != "" && input.PromptOps != nil && m.options.MergerDispatcher != nil {
		userGoal := input.UserGoal
		if userGoal == "" {
			userGoal = "(user goal unavailable)"
		}
		semantic := merger.DispatchMerger(merger.Input{
			Workspace: input.Workspace, ParentSessionID: input.ParentSessionID,
			PromptOps: input.PromptOps, Worktree: input.Worktree, MergeAt: input.MergeAt,
			TaskID: input.TaskID, TaskTitle: input.TaskTitle, SourceBranch: sourceBranch,
			TargetBranch: input.TargetBranch, UserGoal: userGoal, PriorFailure: fast.Summary,
		}, merger.Dependencies{Dispatcher: m.options.MergerDispatcher})
		if semantic.Data.Result == "resolved" {
			verified := merger.VerifyMergeResolved(input.Worktree)
			if verified.OK {
				summary := fast.Summary + " → merger resolved (" +
					fmt.Sprintf("%d", len(semantic.Data.FilesTouched)) + " files: " +
					utf16Prefix(semantic.Data.Reason, 200) + ")"
				return MergeResult{OK: true, Summary: summary}, nil
			}
		}
	}

	if m.options.RecoveryClient == nil {
		return MergeResult{OK: fast.OK, Summary: fast.Summary, ConflictPath: fast.ConflictPath}, nil
	}
	var conflictPath *string
	if fast.ConflictPath != "" {
		value := fast.ConflictPath
		conflictPath = &value
	}
	recovered := mergerecovery.LLMMergeRecovery(mergerecovery.Input{
		Language: input.Language, Client: m.options.RecoveryClient,
		Workspace: input.Workspace, Worktree: input.Worktree, MergeAt: input.MergeAt,
		TaskID: input.TaskID, TaskTitle: input.TaskTitle, TargetBranch: input.TargetBranch,
		SourceBranch: sourceBranch,
		PriorFailure: mergerecovery.PriorFailure{
			Summary: fast.Summary, ConflictPath: conflictPath,
		},
	})
	conflictResultPath := recovered.ConflictPath
	if conflictResultPath == "" {
		conflictResultPath = fast.ConflictPath
	}
	if recovered.OK {
		return MergeResult{
			OK: true, Summary: fast.Summary + " → recovered: " + recovered.Summary,
			ConflictPath: conflictResultPath,
		}, nil
	}
	return MergeResult{
		OK: false, Summary: fast.Summary + " → recovery failed: " + recovered.Summary,
		ConflictPath: conflictResultPath,
	}, nil
}

var processMergeCoordinator = mergecoordinator.New()

type PendingChildMerge struct {
	WorktreePath       string
	TaskID             string
	TaskTitle          string
	ParentBranch       string
	ParentWorktreePath string
	SourceBranch       string
}

type pendingChildMergeRegistry struct {
	mu       sync.Mutex
	byParent map[string][]PendingChildMerge
}

var schedulerPendingChildMerges = pendingChildMergeRegistry{
	byParent: map[string][]PendingChildMerge{},
}

func (r *pendingChildMergeRegistry) enqueue(parentTaskID string, item PendingChildMerge) {
	r.mu.Lock()
	r.byParent[parentTaskID] = append(r.byParent[parentTaskID], item)
	r.mu.Unlock()
}

func (r *pendingChildMergeRegistry) take(parentTaskID string) []PendingChildMerge {
	r.mu.Lock()
	items := append([]PendingChildMerge(nil), r.byParent[parentTaskID]...)
	delete(r.byParent, parentTaskID)
	r.mu.Unlock()
	return items
}

func (r *pendingChildMergeRegistry) snapshot(parentTaskID string) []PendingChildMerge {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]PendingChildMerge(nil), r.byParent[parentTaskID]...)
}

type mergeLeafInput struct {
	SchedulerInput SchedulerInput
	Task           *plandb.Task
	Worktree       *WorktreeAllocation
	FinalWorktree  string
	FinalBranch    string
	IsDirectChild  bool
	ParentID       string
	ParentBranch   string
	ParentWorktree string
	MergeAt        string
	TargetBranch   string
	RootExcerpt    string
	Language       any
	Aimd           *lockedAimd
}

func (s *Scheduler) mergeLeaf(ctx context.Context, input mergeLeafInput) (MergeResult, string, bool) {
	parentIsActive := false
	if !input.IsDirectChild && input.TargetBranch == input.ParentBranch {
		if _, err := os.Stat(input.ParentWorktree); err == nil {
			show := s.planDB.Run([]string{"plandb", "show", input.ParentID, "--json"})
			var parent plandb.Task
			if unmarshalTask(show.Stdout, &parent) {
				switch parent.Status {
				case plandb.StatusPending, plandb.StatusReady, plandb.StatusClaimed, plandb.StatusRunning:
					parentIsActive = true
				}
			}
		}
	}
	if parentIsActive {
		schedulerPendingChildMerges.enqueue(input.ParentID, PendingChildMerge{
			WorktreePath: input.FinalWorktree, TaskID: input.Task.ID,
			TaskTitle: input.Task.Title, ParentBranch: input.ParentBranch,
			ParentWorktreePath: input.ParentWorktree, SourceBranch: input.FinalBranch,
		})
		note := "\n\nMerge: deferred — parent " + input.ParentID +
			" still active; queued for parent's finalization"
		return MergeResult{OK: true, Summary: "deferred"}, note, true
	}

	children := schedulerPendingChildMerges.take(input.Task.ID)
	type scoredChild struct {
		child PendingChildMerge
		risk  float64
		index int
	}
	scored := make([]scoredChild, len(children))
	var riskWG sync.WaitGroup
	for index, child := range children {
		riskWG.Add(1)
		go func(index int, child PendingChildMerge) {
			defer riskWG.Done()
			scored[index] = scoredChild{
				child: child,
				risk:  mergecoordinator.ComputeMergeRisk(child.WorktreePath, child.ParentBranch),
				index: index,
			}
		}(index, child)
	}
	riskWG.Wait()
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].risk == scored[j].risk {
			return scored[i].index < scored[j].index
		}
		return scored[i].risk < scored[j].risk
	})
	for _, row := range scored {
		child := row.child
		value, err := s.mergeCoordinator.Run(child.TaskID, row.risk, func() (any, error) {
			return s.mergeStack.Merge(ctx, MergeRequest{
				Workspace: s.workspace, Worktree: child.WorktreePath,
				MergeAt: child.ParentWorktreePath, TaskID: child.TaskID,
				TaskTitle: child.TaskTitle, TargetBranch: child.ParentBranch,
				SourceBranch:    child.SourceBranch,
				ParentSessionID: input.SchedulerInput.ParentSessionID,
				PromptOps:       input.SchedulerInput.PromptOps,
				UserGoal:        input.RootExcerpt, Language: input.Language,
			})
		})
		result, ok := value.(MergeResult)
		if err != nil || !ok || !result.OK {
			if input.Aimd != nil {
				input.Aimd.observe(false)
			}
			// LB-18: a failed deferred child merge is logged and otherwise
			// ignored; the already-done child is not failed or cascaded.
			continue
		}
		if input.Aimd != nil {
			input.Aimd.observe(true)
		}
	}

	selfRisk := mergecoordinator.ComputeMergeRisk(input.FinalWorktree, input.Worktree.BaseSHA)
	value, err := s.mergeCoordinator.Run(input.Task.ID, selfRisk, func() (any, error) {
		return s.mergeStack.Merge(ctx, MergeRequest{
			Workspace: s.workspace, Worktree: input.FinalWorktree, MergeAt: input.MergeAt,
			TaskID: input.Task.ID, TaskTitle: input.Task.Title,
			TargetBranch: input.TargetBranch, SourceBranch: input.FinalBranch,
			ParentSessionID: input.SchedulerInput.ParentSessionID,
			PromptOps:       input.SchedulerInput.PromptOps,
			UserGoal:        input.RootExcerpt, Language: input.Language,
		})
	})
	if err != nil {
		return MergeResult{OK: false, Summary: err.Error()}, "", false
	}
	result, ok := value.(MergeResult)
	if !ok {
		return MergeResult{OK: false, Summary: "merge worker returned invalid result"}, "", false
	}
	if !result.OK {
		return result, "", false
	}
	return result, "\n\nMerge: " + result.Summary, false
}

func unmarshalTask(raw []byte, task *plandb.Task) bool {
	return len(raw) > 0 && jsonUnmarshal(raw, task) == nil
}

// jsonUnmarshal is a variable only to keep merge.go's import list focused in
// tests that inject malformed PlanDB output.
var jsonUnmarshal = func(raw []byte, value any) error {
	return json.Unmarshal(raw, value)
}

func buildMergeBlocker(
	task *plandb.Task,
	targetBranch string,
	worktreePath string,
	result MergeResult,
) string {
	lines := []string{
		"Merge conflict on task " + task.ID + " (" + compactJS(task.Title, 80) + ").",
		"Summary: " + compactJS(result.Summary, 300),
	}
	if result.ConflictPath != "" {
		lines = append(lines, "Report: "+result.ConflictPath)
	}
	lines = append(lines,
		"Target branch: "+targetBranch+".",
		"The leaf's worktree was left intact at "+worktreePath+" for manual inspection.",
	)
	return strings.Join(lines, "\n")
}
