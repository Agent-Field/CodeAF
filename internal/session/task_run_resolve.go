package session

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

const completedRunReview = "the plan finished before the restart, but its final checks and landing were not recorded; inspect the saved working copy, then accept or refute it on the task page or ask this conversation to do so"

// resolveCompletedRun is the existing explicit decision door for completed
// work whose process disappeared before its landing. It never starts workers
// and a completed store alone never supplies a successful check receipt.
func (a *Agent) resolveCompletedRun(ctx context.Context, id uint64, resolution TaskResolution, why string, by TaskAskOwner) (string, bool, error) {
	g, found := a.completedRunOwner(id)
	if !found {
		return "", false, nil
	}
	epoch, err := a.beltAdmissionEpoch()
	if err != nil {
		return "", true, err
	}
	a.lockBeltStart()
	defer a.beltStartMu.Unlock()
	if err := a.checkBeltAdmission(epoch); err != nil {
		return "", true, err
	}
	kept, found := runRowOf(g, id)
	if kept.RecoveredCompletion && kept.Copy == nil {
		return "", true, errNoRunCopy
	}
	if !found || kept.Program != "" || kept.State != TaskUnverified {
		return "", true, settledAlready(id, kept.State)
	}
	path, root, err := a.completedRunPlan(id)
	if err != nil {
		return "", true, err
	}
	switch resolution {
	case TaskReaudit:
		return "", true, fmt.Errorf("task %d finished before the restart without a final check receipt; inspect its saved work, then accept or refute it", id)
	case TaskAccept:
		return a.acceptCompletedRun(ctx, g, kept, path, root, epoch, why, by)
	case TaskRefute:
		kept.State, kept.Ending = TaskFailed, TaskEndingRefused
		kept.Report = withReport(refutedLine(why, by), kept.Report)
		return a.saveCompletedRunOutcome(g, kept, path, root.ID)
	default:
		return "", true, fmt.Errorf("%q is not a resolution: say %s, %s or %s", resolution, TaskAccept, TaskReaudit, TaskRefute)
	}
}

// completedRunOwner keeps the fallback inside this conversation. A worker's
// shared task graph does not authorize it to settle its parent or siblings.
func (a *Agent) completedRunOwner(id uint64) (*TaskGraph, bool) {
	if a.config.InTask || a.config.taskID != 0 {
		return nil, false
	}
	g := a.tasker()
	if g == nil {
		return nil, false
	}
	if kept, found := runRowOf(g, id); !found || !kept.RecoveredCompletion {
		return nil, false
	}
	return g, true
}

// completedRunPlan reads only this conversation's exact completed root, including
// an archived store. A newer task at the live path is never adopted in its place.
func (a *Agent) completedRunPlan(id uint64) (string, *plandb.Task, error) {
	stores, plan, closeStores := a.openPlanReadHandles()
	defer closeStores()
	want := strconv.FormatUint(id, 10)
	for _, store := range stores {
		if store.RootID() != want {
			continue
		}
		root := store.Task(want)
		if root == nil || root.Chat != plan.chat || root.Status != plandb.StatusDone {
			break
		}
		return store.Path(), root, nil
	}
	return "", nil, fmt.Errorf("task %d has no completed plan in this conversation; its saved work has not been changed", id)
}

func (a *Agent) acceptCompletedRun(ctx context.Context, g *TaskGraph, kept TaskNotice, path string, root *plandb.Task, epoch uint64, why string, by TaskAskOwner) (string, bool, error) {
	tree, err := runCopyTree(kept.Copy, a.config.Place)
	if err != nil {
		return "", true, err
	}
	// A directory that now belongs to another branch is not the saved work.
	// The ordinary landing also checks the recorded destination branch and head.
	if kept.Copy.Branch != "" && currentBranch(tree.dir) != kept.Copy.Branch {
		return "", true, fmt.Errorf("task %d's saved working copy now stands on a different branch; restore %s before accepting it", kept.ID, kept.Copy.Branch)
	}
	if err := ctx.Err(); err != nil {
		return "", true, err
	}
	branch, changed, refused, err := LandRunTree(tree.dir, tree.checkBase, kept.Title, kept.Model)
	if err != nil {
		return "", true, err
	}
	if refused != "" && refused != runNothingToLand {
		return "", true, fmt.Errorf("task %d could not land: %s", kept.ID, refused)
	}
	commit := runTreeHead(tree.dir)
	if err := a.admitCompletedRunHomecoming(ctx, epoch); err != nil {
		return "", true, err
	}
	merge, detail, _, _ := tree.comeHome(kept.Title, nil, a.signsGitWork())
	kept.Changed, kept.Branch, kept.Merge = changed, branch, merge
	kept.Report = withReport(acceptedLine(strings.TrimSpace(why), by), withReport(root.Result, detail))
	if commit != "" {
		kept.Report = withReport(kept.Report, "work commit "+commit)
	}
	if cameHome(merge) {
		kept.State = TaskDone
	} else {
		kept.State = TaskUnverified
	}
	return a.saveCompletedRunOutcome(g, kept, path, root.ID)
}

// saveCompletedRunOutcome checks the checkpoint write rather than relying on
// publishRunRow's best-effort logging. An irreversible merge keeps its actual
// in-memory state and commit even when either durable receipt cannot be saved.
func (a *Agent) saveCompletedRunOutcome(g *TaskGraph, kept TaskNotice, path, rootID string) (string, bool, error) {
	a.publishRunRow(g, kept)
	g.mu.Lock()
	checkpoint := g.store
	g.mu.Unlock()
	checkpointErr := checkpoint.write(g)
	pageErr := completedRunReceiptWriter(path, rootID, kept.Report)
	receipt := fmt.Sprintf("task %d is %s\n%s", kept.ID, ProjectTask(kept.StatusFacts()).Word, kept.Report)
	if checkpointErr != nil {
		if pageErr != nil {
			return "", true, fmt.Errorf("%s\nThe task checkpoint and plan-page receipt could not be saved: %v; %w. Inspect the actual work before retrying", receipt, checkpointErr, pageErr)
		}
		return "", true, fmt.Errorf("%s\nThe plan page holds the receipt, but its task checkpoint could not be saved: %w. Inspect the actual work before retrying", receipt, checkpointErr)
	}
	if pageErr != nil {
		return "", true, fmt.Errorf("%s\nIts outcome was saved, but the plan page could not save that receipt: %w; read task %d for its actual outcome before retrying", receipt, pageErr, kept.ID)
	}
	return receipt, true, nil
}

var completedRunReceiptWriter = recordCompletedRunReceipt

// recordCompletedRunReceipt reports a failed page write after preserving the
// actual landing in the checkpoint, so a retry cannot repeat a completed merge.
func recordCompletedRunReceipt(path, id, receipt string) error {
	store, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		return err
	}
	defer store.Close()
	_, err = store.AddNote(id, id, receipt)
	return err
}

// completedLandingQuestion offers only decisions the recovered copy can answer.
// A missing checker is not a live node to hand to an automatic merge round.
func completedLandingQuestion(q Question, notice TaskNotice) Question {
	if !notice.RecoveredCompletion {
		return q
	}
	q.Kind = QuestionLanding
	var options []AnswerOption
	for _, option := range q.Options {
		switch option.Key {
		case LandingYesKey:
			option.Label = taskAskCheckYes
		case LandingNoKey:
			option.Label = taskAskCheckNo
		default:
			continue
		}
		options = append(options, option)
	}
	q.Options = options
	if notice.Merge != mergeConflicted && notice.Merge != mergeAborted {
		q.Reason = completedRunReview
	}
	return q
}

// completedRunHomecomingAt exposes both sides of the irreversible boundary to
// deterministic cancellation regressions. It does no work in the product.
var completedRunHomecomingAt func(admitted bool)

// admitCompletedRunHomecoming is the last cancellable boundary before the main
// merge, under the same lock StopWork uses to close admission. A successful
// check admits the existing atomic merge; a later Stop preserves its actual
// receipt and joins this acceptance through beltStartMu, held by the caller
// until both landing and checkpoint publication have finished.
func (a *Agent) admitCompletedRunHomecoming(ctx context.Context, epoch uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if completedRunHomecomingAt != nil {
		completedRunHomecomingAt(false)
	}
	if err := a.checkBeltAdmission(epoch); err != nil {
		return err
	}
	if completedRunHomecomingAt != nil {
		completedRunHomecomingAt(true)
	}
	return nil
}
