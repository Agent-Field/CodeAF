package codeaf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/baked"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/project"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/auditorgate"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/scheduler"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/tool"
)

// runRootOrchestrator owns the durable top-level model session used by the
// decomposed path. Leaf sessions continue to enter through runtime.RunLeaf.
func (runner *pipeline) runRootOrchestrator(
	ctx context.Context, promptText, projectID, rootID string,
	pump steploop.Scheduler,
) error {
	markdown, ok := baked.GetBakedAgent(baked.EntryAgent)
	if !ok {
		return fmt.Errorf("unknown entry agent: %s", baked.EntryAgent)
	}
	model := agentjsonModel(firstModel(runner.pool.high))
	ctx = project.WithContext(ctx, project.InstanceContext{
		Directory: runner.workspace, Worktree: runner.workspace,
		Project: project.Info{
			ID: project.ID(projectID), Worktree: runner.workspace,
		},
		PlanDB: &project.PlanDB{
			DBPath: runner.dbPath, ProjectID: projectID, RootTaskID: rootID,
		},
	})
	configured, err := runner.runtime.config.configureTurn(turn{
		SessionID: runner.sessionID, SessionTitle: promptText,
		Agent: baked.EntryAgent, AgentMarkdown: markdown,
		Workspace: runner.workspace, ProviderID: model.ProviderID,
		ModelID: model.ModelID, Prompt: promptText,
	})
	if err != nil {
		return err
	}
	configured.LowModels = runner.pool.values("low")
	configured.PlanDB = &project.PlanDB{
		DBPath: runner.dbPath, ProjectID: projectID, RootTaskID: rootID,
	}
	configured.Scheduler = pump
	configured.ExitGuard = steploop.PlanDBExitGuard{
		DB: plandb.GetPlanDB(), Ledger: runFailureLedgerAdapter{},
	}
	configured.Tools = runner.runtime.definitionsFor(
		configured.ProviderID, configured.ModelID, baked.EntryAgent, nil,
	)
	configured.Execute = runner.runtime.registry.Execute
	configured.SystemInstructions = runner.runtime.registry.SystemInstructions(ctx)
	configured.LoadInstructions = runner.runtime.registry.SystemInstructions
	configured.AfterAssistant = runner.runtime.registry.ClearInstructionClaims
	response, err := runner.runtime.runTurn(ctx, configured)
	runner.runtime.observeTurn(baked.EntryAgent, response)
	runner.runtime.addCost(response.CostUSD)
	if err != nil {
		return err
	}
	if exhausted, reason := runner.budgetExhausted(); exhausted {
		// The direct backend test seam does not enter schedulerPump, and a
		// provider call itself may cross the cost ceiling. Preserve the same
		// run.ts:1628-1630 terminal instead of attempting to close open work.
		return fmt.Errorf("%w: %s", errRunBudget, reason)
	}
	if strings.TrimSpace(response.Text) == "" && len(response.Parts) == 0 {
		return errors.New("root orchestrator returned no result")
	}
	if rootID != "" {
		agent := "orchestrator"
		result := prefixUTF16(strings.TrimSpace(response.Text), 4_000)
		done, doneErr := plandb.GetPlanDB().DoneTask(
			plandb.TaskID(rootID), plandb.DoneOpts{Result: result, Agent: &agent},
		)
		if doneErr != nil {
			open := openDescendantTasks(projectID, rootID)
			if len(open) == 0 {
				return fmt.Errorf("root close failed: %w", doneErr)
			}
			// Frozen TS src/cli/cmd/run.ts:1287-1532 keeps dispatching while
			// PlanDB has active rows and crosses into audit only after the graph is
			// drained. A DoneTask refusal is therefore a dispatch signal, not a
			// terminal harness error.
			runner.note(fmt.Sprintf(
				"[codeaf] root close refused with %d open descendant task(s) — draining before retry\n",
				len(open),
			))
			if runner.events != nil {
				runner.events.stage("root-orchestrator", "draining", map[string]any{
					"root_task_id": rootID, "open_tasks": openTaskData(open),
				})
			}
			if drainErr := runner.dispatchUntilQuiet(ctx, projectID, rootID, ""); drainErr != nil {
				if runner.events != nil && errors.Is(drainErr, errRootDrainStalled) {
					runner.events.stage("root-orchestrator", "stalled", map[string]any{
						"root_task_id": rootID, "error": drainErr.Error(),
						"open_tasks": openTaskData(openDescendantTasks(projectID, rootID)),
					})
				}
				return drainErr
			}
			done, doneErr = plandb.GetPlanDB().DoneTask(
				plandb.TaskID(rootID), plandb.DoneOpts{Result: result, Agent: &agent},
			)
			if doneErr != nil {
				return newRootDrainStallError(
					openDescendantTasks(projectID, rootID),
					"root close was still refused after the graph drain: "+doneErr.Error(),
				)
			}
		}
		if done != nil && runner.events != nil {
			runner.events.stage("root-orchestrator", "root-complete", map[string]any{
				"root_task_id": rootID,
			})
		}
	}
	return nil
}

func buildAuditFixRootPrompt(cycle int, verdict auditorgate.AuditorVerdict) string {
	lines := []string{
		fmt.Sprintf("# Audit fix cycle %d", cycle),
		"",
		"The fresh auditor found the blockers below. Fix-generator has already translated them into durable PlanDB tasks under the existing root.",
		"Do not rebuild the graph. Review the ready tasks, make any narrowly necessary recovery decision, and yield so the scheduler can dispatch the current fix wave.",
		"",
		"## Blockers",
	}
	for _, blocker := range verdict.Blockers {
		lines = append(lines, "- "+blocker.Detail)
	}
	if len(verdict.RepairHints) > 0 {
		lines = append(lines, "", "## Repair hints")
		for _, hint := range verdict.RepairHints {
			lines = append(lines, "- "+hint)
		}
	}
	return strings.Join(lines, "\n")
}

// Quiet open cycles have three separate bounds: ordinary deadlock detection,
// a larger pause-specific backpressure budget, and a high absolute ceiling on
// synthetic continuations. Window-full cycles reset the short bound only when
// the scheduler registry confirms a genuinely live descendant dispatch.
const (
	stallCycleThreshold        = 3
	pauseCycleThreshold        = 12
	syntheticContinuationLimit = 256
	pauseWallTimeout           = 5 * time.Minute
	rootDrainSweepMinAge       = 30 * time.Second
	// The last-resort sweep runs when a quiet stall is about to be declared, so
	// its floor only has to outlast the tools' create-then-reserve window rather
	// than the regular sweep's full race guard.
	rootDrainLastResortMinAge = 2 * time.Second
)

type pipelineRootScheduler struct {
	runner             *pipeline
	delegate           steploop.Scheduler
	cycle              int
	quietCycles        int
	pauseCycles        int
	continuationCycles int
	pauseSince         time.Time
	sweepMinAge        time.Duration
	lastResortMinAge   time.Duration
}

type schedulerQuietReasonReporter interface {
	LastCycleQuietReason() scheduler.CycleQuietReason
}

func (runner *pipeline) liveDispatchIDs() []string {
	ids := scheduler.InFlightDispatchIDs()
	if runner.inFlightDispatchIDs != nil {
		ids = runner.inFlightDispatchIDs()
	}
	// Tool calls execute concurrently with scheduler pumps, so a guard-bound
	// PlanDB task backing an in-flight bash/edit/write is live work even
	// though no scheduler dispatch exists for it. Without this union the
	// drain sweep closed a bookkeeping package 17s before its command
	// finished (run N, t-8zxe) and could release a reused planner task into
	// duplicate concurrent execution.
	if runner.runtime != nil && runner.runtime.registry != nil {
		ids = append(ids, runner.runtime.registry.GuardInFlightTaskIDs()...)
	}
	if len(ids) < 2 {
		return ids
	}
	seen := make(map[string]struct{}, len(ids))
	unique := ids[:0]
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}

func liveOpenDispatchCount(tasks []*plandb.Task, ids []string) int {
	open := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		open[task.ID] = struct{}{}
	}
	count := 0
	for _, id := range ids {
		if _, ok := open[id]; ok {
			count++
		}
	}
	return count
}

type rootDrainSweep struct {
	closed    []string
	released  []string
	unblocked []string
}

func (sweep rootDrainSweep) madeProgress() bool {
	return len(sweep.closed) > 0 || len(sweep.released) > 0 ||
		len(sweep.unblocked) > 0
}

// unblockFailedUpstreamTasks revives dependents that failTaskCascade softened
// to pending behind a terminally failed predecessor.
//
// The cascade's soften branch (scheduler/dispatch.go) deliberately declines to
// hard-cancel a dependent, recording "softened to pending (not hard-cancelled)
// so a later replan/resume can revive this dependent". No such revival existed:
// a failed upstream never satisfies its hard edge, so the dependent stayed
// pending forever, the scheduler dispatched nothing, and the run died holding
// whatever the failed leaf had produced. Two benchmark runs (urfave/cli #2263,
// node-semver #775) ended exactly this way, each delivering an empty diff while
// a correct patch sat in .codeaf/rejected-work/.
//
// This is the missing revival, and it runs only from the drain sweep — i.e.
// only once the graph is already quiet and the alternative is a certain stall.
// A dependent still waiting on any upstream that can yet complete is left
// alone; only a task blocked *solely* by terminally failed predecessors moves.
func unblockFailedUpstreamTasks(db *plandb.PlanDB, tasks []*plandb.Task) []string {
	unblocked := []string{}
	for _, task := range tasks {
		if task == nil || task.Status != plandb.StatusPending {
			continue
		}
		blockers := db.ReleaseFailedUpstreamBlock(task.ID)
		if len(blockers) == 0 {
			continue
		}
		ids := make([]string, 0, len(blockers))
		for _, blocker := range blockers {
			ids = append(ids, string(blocker))
		}
		// The leaf must know its input never arrived, or it will assume the
		// predecessor's work is present and build on top of nothing.
		_, _ = db.AmendTask(task.ID, "\n\n## Predecessor failed\nDependenc"+
			"y task(s) "+strings.Join(ids, ", ")+" failed terminally, so the "+
			"work they were to produce is NOT present in the tree. This task "+
			"was unblocked anyway so the run can make progress. Verify the "+
			"current state yourself before assuming any prerequisite exists, "+
			"and implement whatever is missing that this task depends on.\n",
			"append")
		unblocked = append(unblocked, string(task.ID))
	}
	return unblocked
}

func sweepRootDrainTasks(db *plandb.PlanDB, tasks []*plandb.Task, liveIDs []string, minAge time.Duration) rootDrainSweep {
	live := make(map[string]struct{}, len(liveIDs))
	for _, id := range liveIDs {
		live[id] = struct{}{}
	}
	result := rootDrainSweep{}
	nowMillis := time.Now().UnixMilli()
	isOldEnough := func(task *plandb.Task) bool {
		if minAge <= 0 {
			return true
		}
		updatedMillis := task.UpdatedAt / 4096
		return nowMillis-updatedMillis >= minAge.Milliseconds()
	}
	isHarness := func(task *plandb.Task) bool {
		description := ""
		if task.Description != nil {
			description = *task.Description
		}
		return tool.IsHarnessDirectPackageDescription(description) ||
			tool.IsHarnessSubagentPackageDescription(description)
	}
	// Harness bookkeeping packages close regardless of composite shape: a
	// subagent package turned composite by a nested dispatch (run-T shape)
	// closes after its harness children do, so the loop re-runs until a full
	// pass makes no progress. DoneTask itself refuses while any open
	// descendant remains, so a package holding real planner work stays open.
	closedHarness := map[plandb.TaskID]struct{}{}
	for again := true; again; {
		again = false
		for _, task := range tasks {
			if !isOldEnough(task) {
				continue
			}
			if _, alreadyClosed := closedHarness[task.ID]; alreadyClosed {
				continue
			}
			if _, ok := live[string(task.ID)]; ok {
				continue
			}
			if !isHarness(task) {
				continue
			}
			closed, err := db.DoneTask(task.ID, plandb.DoneOpts{
				Result: "auto-closed by root drain: harness bookkeeping package with no live dispatch",
			})
			if err == nil && closed != nil {
				closedHarness[task.ID] = struct{}{}
				result.closed = append(result.closed, string(task.ID))
				again = true
			}
		}
	}
	for _, task := range tasks {
		if !isOldEnough(task) {
			continue
		}
		if task.IsComposite {
			continue
		}
		if _, ok := live[string(task.ID)]; ok {
			continue
		}
		if _, alreadyClosed := closedHarness[task.ID]; alreadyClosed {
			continue
		}
		if isHarness(task) {
			continue
		}
		if task.AgentID != nil && *task.AgentID == "root-orchestrator" &&
			(task.Status == plandb.StatusClaimed || task.Status == plandb.StatusRunning) {
			if released := db.ReleaseTask(task.ID); released != nil {
				result.released = append(result.released, string(task.ID))
			}
		}
	}
	// Last: a dependent stranded behind a terminally failed predecessor is the
	// one open-task shape neither pass above can move, and it never resolves on
	// its own.
	result.unblocked = unblockFailedUpstreamTasks(db, tasks)
	return result
}

func openDescendantStatusCounts(projectID, rootTaskID string) map[plandb.TaskStatus]int {
	counts := map[plandb.TaskStatus]int{}
	for _, task := range openDescendantTasks(projectID, rootTaskID) {
		counts[task.Status]++
	}
	return counts
}

func openDescendantTasks(projectID, rootTaskID string) []*plandb.Task {
	tasks := plandb.GetPlanDB().ListTasks(&plandb.ListTasksFilter{Project: projectID})
	return openDescendantTasksFrom(tasks, rootTaskID)
}

func openDescendantTasksFrom(tasks []*plandb.Task, rootTaskID string) []*plandb.Task {
	descendants := map[plandb.TaskID]bool{plandb.TaskID(rootTaskID): true}
	for changed := true; changed; {
		changed = false
		for _, task := range tasks {
			if descendants[task.ID] || task.ParentTaskID == nil || !descendants[*task.ParentTaskID] {
				continue
			}
			descendants[task.ID] = true
			changed = true
		}
	}
	open := make([]*plandb.Task, 0)
	for _, task := range tasks {
		if task.ID == plandb.TaskID(rootTaskID) || !descendants[task.ID] {
			continue
		}
		switch task.Status {
		case plandb.StatusPending, plandb.StatusReady, plandb.StatusClaimed, plandb.StatusRunning:
			open = append(open, task)
		}
	}
	sort.Slice(open, func(i, j int) bool { return open[i].ID < open[j].ID })
	return open
}

func openTaskData(tasks []*plandb.Task) []map[string]string {
	rows := make([]map[string]string, 0, len(tasks))
	for _, task := range tasks {
		rows = append(rows, map[string]string{
			"id": task.ID, "status": string(task.Status), "title": task.Title,
		})
	}
	return rows
}

func formatOpenTasks(tasks []*plandb.Task) string {
	parts := make([]string, 0, len(tasks))
	for _, task := range tasks {
		parts = append(parts, fmt.Sprintf("%s (%s, %q)", task.ID, task.Status, task.Title))
	}
	return strings.Join(parts, "; ")
}

func newRootDrainStallError(tasks []*plandb.Task, reason string) error {
	if reason == "" {
		reason = "dispatch made no progress"
	}
	detail := formatOpenTasks(tasks)
	if detail == "" {
		detail = "(open task list unavailable)"
	}
	return fmt.Errorf(
		"%w: %s; %d open task(s): %s",
		errRootDrainStalled, reason, len(tasks), detail,
	)
}

func formatOpenDescendantCounts(counts map[plandb.TaskStatus]int) (int, string) {
	statuses := []plandb.TaskStatus{
		plandb.StatusPending, plandb.StatusReady, plandb.StatusClaimed, plandb.StatusRunning,
	}
	total := 0
	parts := make([]string, 0, len(statuses))
	for _, status := range statuses {
		if counts[status] == 0 {
			continue
		}
		total += counts[status]
		parts = append(parts, fmt.Sprintf("%s=%d", status, counts[status]))
	}
	return total, strings.Join(parts, ", ")
}

func (pump *pipelineRootScheduler) Pump(
	ctx context.Context, input steploop.SchedulerInput,
) (string, error) {
	if exhausted, reason := pump.runner.budgetExhausted(); exhausted {
		pump.runner.note("[codeaf] root scheduler stopped: " + reason + "\n")
		pump.runner.events.stage("scheduler", "budget-exhausted", map[string]any{
			"reason": reason,
		})
		// run.ts:1628-1630 checkpoints budget exhaustion as a successful
		// terminal. Returning quiet here would let prompt.ts:1612-1620 exit the
		// step loop and make the root close misclassify open work as a stall.
		return "", fmt.Errorf("%w: %s", errRunBudget, reason)
	}
	pump.cycle++
	pump.runner.events.stage("scheduler", "cycle", map[string]any{"cycle": pump.cycle})
	summary, err := pump.delegate.Pump(ctx, input)
	quietReason := scheduler.CycleQuietNone
	if reporter, ok := pump.delegate.(schedulerQuietReasonReporter); ok {
		quietReason = reporter.LastCycleQuietReason()
	}
	status := "cycle-complete"
	data := map[string]any{"cycle": pump.cycle, "summary": summary != ""}
	if err != nil {
		status = "cycle-failed"
		data["error"] = err.Error()
	}
	if summary == "" || quietReason != scheduler.CycleQuietNone {
		counts := openDescendantStatusCounts(input.Plan.ProjectID, input.Plan.RootTaskID)
		total, detail := formatOpenDescendantCounts(counts)
		openTasks := openDescendantTasks(input.Plan.ProjectID, input.Plan.RootTaskID)
		liveIDs := pump.runner.liveDispatchIDs()
		live := liveOpenDispatchCount(openTasks, liveIDs)
		sweepProgress := false
		if quietReason != scheduler.CycleQuietPaused && total > 0 && live == 0 {
			sweep := sweepRootDrainTasks(plandb.GetPlanDB(), openTasks, liveIDs, pump.sweepMinAge)
			if sweep.madeProgress() {
				sweepProgress = true
				pump.quietCycles = 0
				pump.runner.events.stage("scheduler", "drain-sweep", map[string]any{
					"closed_count": len(sweep.closed), "closed_task_ids": sweep.closed,
					"released_count": len(sweep.released), "released_task_ids": sweep.released,
					"unblocked_count": len(sweep.unblocked), "unblocked_task_ids": sweep.unblocked,
				})
				counts = openDescendantStatusCounts(input.Plan.ProjectID, input.Plan.RootTaskID)
				total, detail = formatOpenDescendantCounts(counts)
				openTasks = openDescendantTasks(input.Plan.ProjectID, input.Plan.RootTaskID)
				live = liveOpenDispatchCount(openTasks, liveIDs)
			}
		}
		if total > 0 {
			pump.continuationCycles++
		}
		if quietReason == scheduler.CycleQuietPaused && total > 0 {
			pump.quietCycles = 0
			pump.pauseCycles++
			if pump.pauseSince.IsZero() {
				pump.pauseSince = pump.runner.now()
			}
		} else {
			pump.pauseCycles = 0
			pump.pauseSince = time.Time{}
			if sweepProgress {
				pump.quietCycles = 0
			} else if total > 0 && live == 0 {
				pump.quietCycles++
			} else {
				// PlanDB claimed/running state is not evidence of work. Only a
				// dispatch in the scheduler's live registry resets the stall count.
				pump.quietCycles = 0
			}
		}
		pauseExpired := quietReason == scheduler.CycleQuietPaused && total > 0 &&
			(pump.pauseCycles >= pauseCycleThreshold ||
				pump.runner.now().Sub(pump.pauseSince) >= pauseWallTimeout)
		continuationExpired := total > 0 && pump.continuationCycles >= syntheticContinuationLimit
		stallPending := pauseExpired || continuationExpired || pump.quietCycles >= stallCycleThreshold
		lastResortProgress := false
		// Only the short quiet-cycle stall gets a second chance. continuationCycles
		// is the drain's one monotone counter, so a bookkeeping sweep must never
		// reset it or clear its expiry: a run that mints a fresh harness package
		// every turn would otherwise sweep-and-reset forever, and every cycle is a
		// paid model turn on an unbounded loop.
		if total > 0 && err == nil && !continuationExpired && !pauseExpired &&
			pump.quietCycles >= stallCycleThreshold {
			// A harness package can remain younger than the regular sweep's race
			// guard for the entire short stall window. Before declaring a healthy
			// cycle stalled, retry once with a floor just long enough to cover the
			// tools' create-then-reserve window. Re-read the live registry first so
			// a dispatch that registered since this cycle started is excluded.
			liveIDs = pump.runner.liveDispatchIDs()
			sweep := sweepRootDrainTasks(plandb.GetPlanDB(), openTasks, liveIDs, pump.lastResortMinAge)
			if sweep.madeProgress() {
				lastResortProgress = true
				pump.quietCycles = 0
				pump.runner.events.stage("scheduler", "drain-sweep", map[string]any{
					"closed_count": len(sweep.closed), "closed_task_ids": sweep.closed,
					"released_count": len(sweep.released), "released_task_ids": sweep.released,
					"unblocked_count": len(sweep.unblocked), "unblocked_task_ids": sweep.unblocked,
					"last_resort": true,
				})
				counts = openDescendantStatusCounts(input.Plan.ProjectID, input.Plan.RootTaskID)
				total, detail = formatOpenDescendantCounts(counts)
				openTasks = openDescendantTasks(input.Plan.ProjectID, input.Plan.RootTaskID)
				live = liveOpenDispatchCount(openTasks, liveIDs)
			}
		}
		if total > 0 && !lastResortProgress && (err != nil || stallPending) {
			if err != nil {
				err = fmt.Errorf(
					"root scheduler stalled: cycle failed while %d open descendant task(s) remain (%s): %w",
					total, detail, err,
				)
			} else if pauseExpired {
				err = fmt.Errorf(
					"root scheduler stalled: resource pause persisted for %d cycles (%s) while %d open descendant task(s) remain (%s)",
					pump.pauseCycles, pump.runner.now().Sub(pump.pauseSince).Round(time.Second), total, detail,
				)
			} else if continuationExpired {
				err = fmt.Errorf(
					"root scheduler stalled: synthetic continuation cycle limit %d reached while %d open descendant task(s) remain (%s)",
					syntheticContinuationLimit, total, detail,
				)
			} else {
				err = fmt.Errorf(
					"root scheduler stalled: %d consecutive cycles dispatched no work while %d open descendant task(s) remain (%s)",
					pump.quietCycles, total, detail,
				)
			}
			err = newRootDrainStallError(openTasks, err.Error())
			status = "stalled"
			data["error"] = err.Error()
			data["open_descendants"] = total
			data["open_tasks"] = openTaskData(openTasks)
			data["statuses"] = detail
			data["quiet_cycles"] = pump.quietCycles
			data["pause_cycles"] = pump.pauseCycles
			data["quiet_reason"] = quietReason
			data["live_dispatches"] = live
			pump.runner.note("[codeaf] " + err.Error() + "\n")
		} else if total > 0 {
			// A synthetic user turn is the step-loop continuation signal. Without
			// it, prompt.ts:1612-1620 considers the prior assistant finished and
			// exits before the graph can drain (run.ts:1287-1351).
			summary = fmt.Sprintf(
				"<system-reminder>Scheduler cycle dispatched no work, but %d open descendant task(s) remain (%s). Keep the root orchestration alive and yield for the next scheduler cycle.</system-reminder>",
				total, detail,
			)
		}
	} else {
		pump.quietCycles = 0
		pump.pauseCycles = 0
		pump.continuationCycles = 0
		pump.pauseSince = time.Time{}
	}
	data["summary"] = summary != ""
	pump.runner.events.stage("scheduler", status, data)
	return summary, err
}

func (runner *pipeline) rootScheduler(delegate steploop.Scheduler) steploop.Scheduler {
	return &pipelineRootScheduler{
		runner: runner, delegate: delegate,
		sweepMinAge: rootDrainSweepMinAge, lastResortMinAge: rootDrainLastResortMinAge,
	}
}

func (runner *pipeline) bootstrapRootPlan(goal string) (rootPlan, error) {
	db := plandb.GetPlanDB()
	projectRow := db.Init("codeaf-"+runner.sessionID, goal)
	description := strings.Join([]string{
		"Harness-created root task for the current user request.",
		"",
		"The orchestrator owns this root task and creates child work packages when decomposition is useful.",
		"Do not use bash for PlanDB.",
		"",
		"session_id: " + runner.sessionID,
		"task_role: integration",
		"access: integration",
		"parallel: serial",
		"worktree: none",
		"agent: orchestrator",
		"acceptance: user request is completed, verified, and summarized",
		"",
		"User request:",
		goal,
	}, "\n")
	root, err := db.AddTask(plandb.AddTaskInput{
		Title: truncate(goal, 100), Description: &description,
		Project: projectRow.ID, Kind: "generic", Tags: []string{
			"codeaf:root", "session:" + runner.sessionID,
		},
	})
	if err != nil {
		return rootPlan{}, err
	}
	db.ClaimTask(plandb.TaskID(root.ID), "orchestrator")
	db.StartTask(plandb.TaskID(root.ID))
	return rootPlan{ProjectID: projectRow.ID, RootID: root.ID}, nil
}

func buildRootOrchestratorPrompt(goal string, plan rootPlan) string {
	blocks := []string{goal}
	references := []string{}
	if plan.ProductPath != "" {
		if _, err := os.Stat(plan.ProductPath); err == nil {
			references = append(references, "- Product brief: "+plan.ProductPath)
		}
	}
	if plan.ArchitecturePath != "" {
		if _, err := os.Stat(plan.ArchitecturePath); err == nil {
			references = append(references,
				"- Architecture:  "+plan.ArchitecturePath+" (single source of truth for interfaces and module boundaries)",
			)
		}
	}
	if len(references) > 0 {
		blocks = append(blocks, "## Reference files (read on demand with `read`)\n"+
			strings.Join(references, "\n"))
	}
	if plan.Prepopulated {
		blocks = append(blocks, strings.Join([]string{
			"## Task graph pre-populated (DO NOT REPLAN)",
			"",
			fmt.Sprintf("%d plandb tasks already exist under root %s (%d dependency edges; %d issue files).", plan.TaskCount, plan.RootID, plan.EdgeCount, plan.IssueCount),
			"Each task with an `issue_file:` policy line has a self-contained spec under `.codeaf/issues`; its coder reads that file first.",
			"",
			"YOUR JOB THIS RUN — strict:",
			"  1. DO NOT call the planner subagent; the architecture is already decomposed.",
			"  2. DO NOT recreate implementation tasks or re-orient the whole codebase.",
			"  3. Inspect ready work with the `plandb` tool, then let the scheduler dispatch each ready leaf in parallel.",
			"  4. Use PlanDB mutations only for genuine replan deltas after a reported architectural problem.",
			"  5. Yield after the current ready wave; scheduler results arrive between your turns and unblock the next wave.",
			"",
			"Pre-built plandb project: " + plan.ProjectID + ".",
		}, "\n"))
	}
	return strings.Join(blocks, "\n\n")
}

func rootPlanDBReminder(
	sessionID, messageID string, plan project.PlanDB,
) msgmodel.TextPart {
	synthetic := true
	return msgmodel.TextPart{
		PartBase: msgmodel.PartBase{
			ID: steploop.NewAscendingID("prt"), SessionID: sessionID,
			MessageID: messageID,
		},
		Synthetic: &synthetic,
		Text: strings.Join([]string{
			"<system-reminder>",
			"PlanDB has already been initialized by the harness for this user request.",
			"Project: " + plan.ProjectID,
			"Root task: " + plan.RootTaskID + " (already claimed/running by orchestrator)",
			"Follow your own agent prompt's planning protocol. If it mandates delegating decomposition to a planner subagent, do that BEFORE any reads, greps, or plandb writes.",
			"Do not create one PlanDB node per read/search/edit tool call.",
			"Do not use bash to run PlanDB commands; use the `plandb` tool.",
			"",
			"Auto-dispatch by the harness scheduler — leverage this to parallelize:",
			"  - Every leaf gets its own git worktree branched from its parent (or main if you are the root).",
			"  - The scheduler claims and dispatches ready leaves in parallel between your turns.",
			"  - On leaf completion the merge worker integrates the leaf branch back into its parent.",
			"</system-reminder>",
		}, "\n"),
	}
}
