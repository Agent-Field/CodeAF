package codeaf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/project"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/scheduler"
)

type rootSchedulerScript struct {
	t           *testing.T
	summary     string
	calls       int
	wantPlan    steploop.PlanDBInfo
	onPump      func(int) error
	quietReason scheduler.CycleQuietReason
}

func (script *rootSchedulerScript) LastCycleQuietReason() scheduler.CycleQuietReason {
	return script.quietReason
}

func (script *rootSchedulerScript) Pump(
	_ context.Context, input steploop.SchedulerInput,
) (string, error) {
	script.calls++
	if script.onPump != nil {
		if err := script.onPump(script.calls); err != nil {
			return "", err
		}
	}
	if input.Plan != script.wantPlan {
		script.t.Fatalf("scheduler plan = %#v, want %#v", input.Plan, script.wantPlan)
	}
	if script.calls == 1 {
		return script.summary, nil
	}
	return "", nil
}

func TestRootOrchestratorUsesDurableEntrySession(t *testing.T) {
	// Validation contract: decomposed work enters the root-agent session, while
	// PlanDB coordinates stay bound to its tool/runtime context.
	var calls []turn
	backend := backendFunc(func(ctx context.Context, request turn) (turnResult, error) {
		instance, ok := project.FromContext(ctx)
		if !ok || instance.PlanDB == nil ||
			instance.PlanDB.ProjectID != "p-root" ||
			instance.PlanDB.RootTaskID != "t-root" {
			t.Fatalf("root project context = %#v, %v", instance, ok)
		}
		calls = append(calls, request)
		return turnResult{SessionID: request.SessionID, Text: "planned"}, nil
	})
	runner := newPipeline(cliArgs{
		High: "openrouter/vendor/high", EntryAgent: "root-orchestrator",
	}, t.TempDir(), pipelineDeps{Backend: backend})
	t.Cleanup(runner.runtime.Close)

	if err := runner.runRootOrchestrator(
		context.Background(), "implement the request", "p-root", "t-root", nil,
	); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("root calls = %d, want 1", len(calls))
	}
	request := calls[0]
	if request.SessionID != runner.sessionID || request.Agent != "root-orchestrator" ||
		request.Prompt != "implement the request" || request.Store == nil ||
		!request.PromptPersisted || request.PromptMessageID == "" ||
		request.PlanDB == nil || request.ExitGuard == nil {
		t.Fatalf("root request = %#v", request)
	}
}

func TestRootOrchestratorAlternatesTurnsWithSchedulerSummaries(t *testing.T) {
	// Validation contract: a decomposed transcript alternates orchestrator
	// turns with synthetic scheduler-cycle summaries in one durable session.
	requests := [][]byte{}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		requests = append(requests, raw)
		return recordedResponse(
			request, http.StatusOK, "text/event-stream", chatReply("orchestrated", 10),
		), nil
	})}
	workspace := t.TempDir()
	runner := newPipeline(cliArgs{
		High: "openrouter/vendor/high", EntryAgent: "root-orchestrator",
	}, workspace, pipelineDeps{Backend: &openRouterBackend{
		apiKey: "test", client: client,
	}})
	t.Cleanup(runner.runtime.Close)
	pump := &rootSchedulerScript{
		t: t, summary: "<system-reminder>scheduler cycle one</system-reminder>",
		wantPlan: steploop.PlanDBInfo{
			ProjectID: "p-root", RootTaskID: "t-root",
		},
	}

	if err := runner.runRootOrchestrator(
		context.Background(), "implement the request", "p-root", "t-root", pump,
	); err != nil {
		t.Fatal(err)
	}
	if pump.calls != 2 || len(requests) != 2 {
		t.Fatalf("pump calls=%d model turns=%d, want 2/2", pump.calls, len(requests))
	}
	if !strings.Contains(string(requests[1]), pump.summary) {
		t.Fatalf("second model turn omitted scheduler summary: %s", requests[1])
	}
	messages, err := runner.runtime.durable.Messages(context.Background(), runner.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 4 {
		t.Fatalf("transcript messages = %d, want 4", len(messages))
	}
	initial, ok := messages[0].Info.(msgmodel.User)
	if !ok || initial.Agent != "root-orchestrator" || len(messages[0].Parts) != 2 {
		t.Fatalf("initial root message = %#v", messages[0])
	}
	bootstrap := messages[0].Parts[1].(msgmodel.TextPart)
	if !strings.Contains(bootstrap.Text, "Project: p-root\nRoot task: t-root") ||
		bootstrap.Synthetic == nil || !*bootstrap.Synthetic {
		t.Fatalf("PlanDB bootstrap = %#v", bootstrap)
	}
	summaryUser, ok := messages[2].Info.(msgmodel.User)
	if !ok || summaryUser.Agent != "root-orchestrator" || len(messages[2].Parts) != 1 {
		t.Fatalf("scheduler message = %#v", messages[2])
	}
	summary := messages[2].Parts[0].(msgmodel.TextPart)
	if summary.Text != pump.summary || summary.Synthetic == nil || !*summary.Synthetic {
		t.Fatalf("scheduler part = %#v", summary)
	}
}

func TestRootOrchestratorQuietOpenCycleContinuesUntilGraphDrains(t *testing.T) {
	// C1/C7: the first empty cycle has open work and must persist a
	// continuation turn; once that work drains, the next empty cycle may exit
	// and the root closes cleanly.
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	projectRow := db.Init("quiet-then-drained")
	root, err := db.AddTask(plandb.AddTaskInput{
		Title: "root", Project: projectRow.ID, CustomID: "t-root",
	})
	if err != nil {
		t.Fatal(err)
	}
	child, err := db.AddTask(plandb.AddTaskInput{
		Title: "child", Project: projectRow.ID, Parent: root.ID, CustomID: "t-child",
	})
	if err != nil {
		t.Fatal(err)
	}

	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return recordedResponse(
			request, http.StatusOK, "text/event-stream", chatReply("orchestrated", 10),
		), nil
	})}
	runner := newPipeline(cliArgs{
		High: "openrouter/vendor/high", EntryAgent: "root-orchestrator",
	}, t.TempDir(), pipelineDeps{Backend: &openRouterBackend{
		apiKey: "test", client: client,
	}, Events: newEventWriter(io.Discard)})
	t.Cleanup(runner.runtime.Close)
	plan := steploop.PlanDBInfo{
		ProjectID: string(projectRow.ID), RootTaskID: string(root.ID),
	}
	delegate := &rootSchedulerScript{
		t: t, wantPlan: plan,
		onPump: func(cycle int) error {
			if cycle != 2 {
				return nil
			}
			_, doneErr := db.DoneTask(child.ID, plandb.DoneOpts{Result: "done"})
			return doneErr
		},
	}

	if err := runner.runRootOrchestrator(
		context.Background(), "implement", plan.ProjectID, plan.RootTaskID,
		runner.rootScheduler(delegate),
	); err != nil {
		t.Fatal(err)
	}
	if requests != 2 || delegate.calls != 2 {
		t.Fatalf("model turns=%d scheduler cycles=%d, want 2/2", requests, delegate.calls)
	}
	if got := db.GetTask(root.ID); got == nil || got.Status != plandb.StatusDone {
		t.Fatalf("drained root status = %#v", got)
	}
}

func TestRootSchedulerRejectsQuietCycleWithOpenDescendants(t *testing.T) {
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	projectRow := db.Init("quiet-open")
	root, err := db.AddTask(plandb.AddTaskInput{
		Title: "root", Project: projectRow.ID, CustomID: "t-root",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddTask(plandb.AddTaskInput{
		Title: "blocked child", Project: projectRow.ID, Parent: root.ID, CustomID: "t-child",
	}); err != nil {
		t.Fatal(err)
	}

	var notes bytes.Buffer
	var events []event
	writer := newEventWriter(io.Discard)
	writer.setHook(func(value event) { events = append(events, value) })
	runner := newPipeline(cliArgs{}, t.TempDir(), pipelineDeps{
		Backend: backendFunc(func(context.Context, turn) (turnResult, error) {
			return turnResult{}, nil
		}),
		Events: writer,
		Notes:  &notes,
	})
	t.Cleanup(runner.runtime.Close)
	plan := steploop.PlanDBInfo{
		ProjectID: string(projectRow.ID), RootTaskID: string(root.ID),
	}
	pump := runner.rootScheduler(&rootSchedulerScript{t: t, wantPlan: plan})

	// A single quiet cycle is not a stall: cycle.go:299 returns zero dispatches
	// with a nil error whenever the resource envelope pauses or the parallel
	// window is already full. Only a sustained run of them is a deadlock.
	for cycle := 1; cycle < stallCycleThreshold; cycle++ {
		summary, quietErr := pump.Pump(
			context.Background(), steploop.SchedulerInput{Plan: plan},
		)
		if quietErr != nil {
			t.Fatalf("cycle %d ended the run before the stall threshold: %v", cycle, quietErr)
		}
		if !strings.Contains(summary, "open descendant") {
			t.Fatalf("cycle %d summary = %q, want continuation for open work", cycle, summary)
		}
	}
	_, err = pump.Pump(context.Background(), steploop.SchedulerInput{Plan: plan})
	if err == nil || !strings.Contains(err.Error(), "stalled") || !strings.Contains(err.Error(), "1 open descendant") {
		t.Fatalf("quiet-cycle error = %v", err)
	}
	if !strings.Contains(notes.String(), "scheduler stalled") {
		t.Fatalf("stalled condition missing from stderr note: %q", notes.String())
	}
	found := false
	for _, value := range events {
		if value.Stage == "scheduler" && value.Status == "stalled" {
			found = true
		}
	}
	if !found {
		t.Fatalf("stalled scheduler event missing: %#v", events)
	}
}

func TestRootSchedulerLastResortSweepRecoversYoungHarnessLeak(t *testing.T) {
	// M1: verdict writes kept refreshing this mutation-backfill package, so
	// every regular sweep saw it as younger than the 30-second race guard even
	// when the three-cycle quiet-stall threshold arrived.
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	projectRow := db.Init("young-harness-drain-leak")
	root, err := db.AddTask(plandb.AddTaskInput{
		Title: "root", Project: projectRow.ID, CustomID: "t-young-root",
	})
	if err != nil {
		t.Fatal(err)
	}
	description := "Harness-created direct mutation package (backfill: agent wrote outside the scheduler dispatch flow).\n\nfiles: .codeaf/auditor-verdict.json"
	bookkeeping, err := db.AddTask(plandb.AddTaskInput{
		Title: "Direct mutation backfill", Description: &description,
		Project: projectRow.ID, Parent: root.ID, CustomID: "t-young-backfill", Kind: "code",
	})
	if err != nil {
		t.Fatal(err)
	}
	if db.ClaimTask(bookkeeping.ID, "root-orchestrator") == nil || db.StartTask(bookkeeping.ID) == nil {
		t.Fatal("could not stage the running mutation-backfill package")
	}
	if age := time.Since(time.UnixMilli(db.GetTask(bookkeeping.ID).UpdatedAt / 4096)); age >= rootDrainSweepMinAge {
		t.Fatalf("bookkeeping package age = %s, want younger than %s", age, rootDrainSweepMinAge)
	}

	var events []event
	writer := newEventWriter(io.Discard)
	writer.setHook(func(value event) { events = append(events, value) })
	runner := newPipeline(cliArgs{}, t.TempDir(), pipelineDeps{Events: writer, Notes: io.Discard})
	t.Cleanup(runner.runtime.Close)
	plan := steploop.PlanDBInfo{ProjectID: projectRow.ID, RootTaskID: root.ID}
	pump := runner.rootScheduler(&rootSchedulerScript{t: t, wantPlan: plan}).(*pipelineRootScheduler)
	pump.lastResortMinAge = 0

	for cycle := 1; cycle < stallCycleThreshold; cycle++ {
		summary, pumpErr := pump.Pump(context.Background(), steploop.SchedulerInput{Plan: plan})
		if pumpErr != nil || !strings.Contains(summary, "open descendant") {
			t.Fatalf("young leak cycle %d summary=%q error=%v", cycle, summary, pumpErr)
		}
		if got := db.GetTask(bookkeeping.ID); got == nil || got.Status != plandb.StatusRunning {
			t.Fatalf("regular age-gated sweep touched the young package on cycle %d: %#v", cycle, got)
		}
	}
	summary, err := pump.Pump(context.Background(), steploop.SchedulerInput{Plan: plan})
	if err != nil || summary != "" {
		t.Fatalf("last-resort drain summary=%q error=%v", summary, err)
	}
	if got := db.GetTask(bookkeeping.ID); got == nil || got.Status != plandb.StatusDone ||
		!strings.Contains(string(got.Result), "auto-closed by root drain") {
		t.Fatalf("last-resort sweep did not close the young harness package: %#v", got)
	}
	if pump.quietCycles != 0 {
		t.Fatalf("quiet stall counter was not reset after recovery: %#v", pump)
	}
	if pump.continuationCycles < stallCycleThreshold {
		t.Fatalf(
			"continuationCycles = %d, want it still counting every drain turn (the absolute ceiling must survive a sweep)",
			pump.continuationCycles,
		)
	}
	foundLastResort := false
	for _, value := range events {
		if value.Stage == "scheduler" && value.Status == "drain-sweep" && value.Data["last_resort"] == true {
			foundLastResort = true
		}
	}
	if !foundLastResort {
		t.Fatalf("last-resort drain-sweep event missing: %#v", events)
	}
}

// A run whose model mints a fresh harness bookkeeping package every drain turn
// gives the last-resort sweep something to close forever. The absolute
// continuation ceiling is the only monotone bound on that loop — and every
// cycle it fails to stop is a paid model turn — so the sweep must never reset
// it or postpone its expiry.
func TestRootSchedulerLastResortSweepStillHonorsContinuationCeiling(t *testing.T) {
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	projectRow := db.Init("perpetual-harness-leak")
	root, err := db.AddTask(plandb.AddTaskInput{
		Title: "root", Project: projectRow.ID, CustomID: "t-perpetual-root",
	})
	if err != nil {
		t.Fatal(err)
	}
	stuckDescription := "Planner-authored work that never completes."
	stuck, err := db.AddTask(plandb.AddTaskInput{
		Title: "Stuck planner task", Description: &stuckDescription,
		Project: projectRow.ID, Parent: root.ID, CustomID: "t-perpetual-stuck", Kind: "code",
	})
	if err != nil {
		t.Fatal(err)
	}
	if db.ClaimTask(stuck.ID, "coder") == nil || db.StartTask(stuck.ID) == nil {
		t.Fatal("could not stage the stuck planner task")
	}

	backfill := "Harness-created direct mutation package (backfill: agent wrote outside the scheduler dispatch flow).\n\nfiles: .codeaf/auditor-verdict.json"
	minted := 0
	script := &rootSchedulerScript{t: t, wantPlan: steploop.PlanDBInfo{
		ProjectID: projectRow.ID, RootTaskID: root.ID,
	}}
	script.onPump = func(call int) error {
		// Every orchestrator turn writes a file, so the guard backfills another
		// bookkeeping package — always fresh, always sweepable.
		fresh, addErr := db.AddTask(plandb.AddTaskInput{
			Title: "Direct mutation backfill", Description: &backfill,
			Project: projectRow.ID, Parent: root.ID,
			CustomID: fmt.Sprintf("t-perpetual-leak-%d", call), Kind: "code",
		})
		if addErr != nil {
			return addErr
		}
		if db.ClaimTask(fresh.ID, "root-orchestrator") == nil || db.StartTask(fresh.ID) == nil {
			return errors.New("could not stage the perpetual backfill package")
		}
		minted++
		return nil
	}

	runner := newPipeline(cliArgs{}, t.TempDir(), pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	t.Cleanup(runner.runtime.Close)
	plan := steploop.PlanDBInfo{ProjectID: projectRow.ID, RootTaskID: root.ID}
	pump := runner.rootScheduler(script).(*pipelineRootScheduler)
	pump.lastResortMinAge = 0

	limit := syntheticContinuationLimit * 4
	for cycle := 1; cycle <= limit; cycle++ {
		_, pumpErr := pump.Pump(context.Background(), steploop.SchedulerInput{Plan: plan})
		if pumpErr == nil {
			continue
		}
		if !errors.Is(pumpErr, errRootDrainStalled) ||
			!strings.Contains(pumpErr.Error(), "synthetic continuation cycle limit") {
			t.Fatalf("drain ended on cycle %d with an unexpected error: %v", cycle, pumpErr)
		}
		if cycle > syntheticContinuationLimit+stallCycleThreshold {
			t.Fatalf("drain took %d cycles to hit the %d-cycle ceiling", cycle, syntheticContinuationLimit)
		}
		if minted < stallCycleThreshold {
			t.Fatalf("probe minted only %d packages; the sweep never had work to do", minted)
		}
		if got := db.GetTask(stuck.ID); got == nil || got.Status != plandb.StatusRunning {
			t.Fatalf("stuck planner task was closed by a sweep: %#v", got)
		}
		return
	}
	t.Fatalf("drain never terminated in %d cycles (minted %d harness packages)", limit, minted)
}

func TestRootSchedulerLastResortSweepPreservesRealPlannerWork(t *testing.T) {
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	projectRow := db.Init("young-real-planner-work")
	root, err := db.AddTask(plandb.AddTaskInput{
		Title: "root", Project: projectRow.ID, CustomID: "t-real-root",
	})
	if err != nil {
		t.Fatal(err)
	}
	description := "Planner-authored work that must be completed by a real worker."
	plannerTask, err := db.AddTask(plandb.AddTaskInput{
		Title: "Implement planner task", Description: &description,
		Project: projectRow.ID, Parent: root.ID, CustomID: "t-real-work", Kind: "code",
	})
	if err != nil {
		t.Fatal(err)
	}
	if db.ClaimTask(plannerTask.ID, "coder") == nil || db.StartTask(plannerTask.ID) == nil {
		t.Fatal("could not stage the running planner task")
	}

	runner := newPipeline(cliArgs{}, t.TempDir(), pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	t.Cleanup(runner.runtime.Close)
	plan := steploop.PlanDBInfo{ProjectID: projectRow.ID, RootTaskID: root.ID}
	pump := runner.rootScheduler(&rootSchedulerScript{t: t, wantPlan: plan}).(*pipelineRootScheduler)
	for cycle := 1; cycle < stallCycleThreshold; cycle++ {
		summary, pumpErr := pump.Pump(context.Background(), steploop.SchedulerInput{Plan: plan})
		if pumpErr != nil || !strings.Contains(summary, "open descendant") {
			t.Fatalf("planner cycle %d summary=%q error=%v", cycle, summary, pumpErr)
		}
	}
	_, err = pump.Pump(context.Background(), steploop.SchedulerInput{Plan: plan})
	if !errors.Is(err, errRootDrainStalled) || !strings.Contains(err.Error(), "3 consecutive cycles") {
		t.Fatalf("real planner work did not produce the expected stall: %v", err)
	}
	if got := db.GetTask(plannerTask.ID); got == nil || got.Status != plandb.StatusRunning || got.Result != nil {
		t.Fatalf("last-resort sweep closed or mutated real planner work: %#v", got)
	}
}

func TestRootOrchestratorDrainsAndRetriesDoneTaskRefusal(t *testing.T) {
	// C1: a refused root close means the graph still has work. Re-enter the
	// dispatch loop, then retry the authoritative DoneTask transition after the
	// child makes progress.
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	projectRow := db.Init("unfinished-root")
	root, err := db.AddTask(plandb.AddTaskInput{
		Title: "root", Project: projectRow.ID, CustomID: "t-root",
	})
	if err != nil {
		t.Fatal(err)
	}
	child, err := db.AddTask(plandb.AddTaskInput{
		Title: "child", Project: projectRow.ID, Parent: root.ID, CustomID: "t-child",
	})
	if err != nil {
		t.Fatal(err)
	}

	var notes bytes.Buffer
	var events []event
	writer := newEventWriter(io.Discard)
	writer.setHook(func(value event) { events = append(events, value) })
	runner := newPipeline(cliArgs{
		High: "openrouter/vendor/high", EntryAgent: "root-orchestrator",
	}, t.TempDir(), pipelineDeps{
		Backend: backendFunc(func(_ context.Context, request turn) (turnResult, error) {
			return turnResult{SessionID: request.SessionID, Text: "root result"}, nil
		}),
		Events: writer,
		Notes:  &notes,
	})
	t.Cleanup(runner.runtime.Close)
	drainCycles := 0
	runner.runSchedulerCycle = func(
		_ context.Context, _ *scheduler.Scheduler, input scheduler.SchedulerInput,
	) (scheduler.SchedulerCycleResult, error) {
		drainCycles++
		if input.ProjectID != projectRow.ID || input.RootTaskID != root.ID {
			t.Fatalf("drain input = %#v", input)
		}
		if _, doneErr := db.DoneTask(child.ID, plandb.DoneOpts{Result: "done"}); doneErr != nil {
			return scheduler.SchedulerCycleResult{}, doneErr
		}
		return scheduler.SchedulerCycleResult{Dispatched: []scheduler.DispatchResult{{
			TaskID: child.ID, Success: true,
		}}}, nil
	}

	err = runner.runRootOrchestrator(
		context.Background(), "implement", string(projectRow.ID), string(root.ID), nil,
	)
	if err != nil {
		t.Fatalf("DoneTask refusal was terminal: %v", err)
	}
	if drainCycles != 1 {
		t.Fatalf("drain cycles = %d, want 1", drainCycles)
	}
	if got := db.GetTask(root.ID); got == nil || got.Status != plandb.StatusDone {
		t.Fatalf("drained root did not transition to done: %#v", got)
	}
	draining, completed := false, false
	for _, value := range events {
		if value.Stage == "root-orchestrator" && value.Status == "draining" {
			draining = true
		}
		if value.Stage == "root-orchestrator" && value.Status == "root-complete" {
			completed = true
		}
	}
	if !draining || !completed || strings.Contains(notes.String(), "stalled") {
		t.Fatalf("drain events=%#v notes=%q", events, notes.String())
	}
}

func TestPipelineReclaimsAbandonedRunningTaskBeforeAudit(t *testing.T) {
	// C2/C5: reproduce the live shape—a root-created bookkeeping task was
	// started without a scheduler dispatch. Draining must reuse stale-claim
	// release, dispatch the requeued row, close the root, and reach audit/pass.
	workspace := validityTestRepo(t)
	setValidityTestEnv(t, workspace)
	t.Setenv("CODEAF_VALIDITY", "0")
	t.Setenv("CODEAF_PRE_GATES", "0")
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")

	auditBackend := &validityBackend{}
	auditCalls := 0
	var abandonedID plandb.TaskID
	backend := backendFunc(func(ctx context.Context, request turn) (turnResult, error) {
		switch request.Agent {
		case "root-orchestrator":
			if abandonedID == "" {
				if writeErr := writeFile(request.Workspace+"/deliverable.txt", "complete\n"); writeErr != nil {
					return turnResult{}, writeErr
				}
				task, addErr := plandb.GetPlanDB().AddTask(plandb.AddTaskInput{
					Title: "Re-audit after fixing both blockers", Project: request.PlanDB.ProjectID,
					Parent: plandb.TaskID(request.PlanDB.RootTaskID), CustomID: "t-abandoned",
				})
				if addErr != nil {
					return turnResult{}, addErr
				}
				abandonedID = task.ID
				plandb.GetPlanDB().ClaimTask(task.ID, "orchestrator")
				plandb.GetPlanDB().StartTask(task.ID)
			}
			return turnResult{SessionID: request.SessionID, Text: "deliverable complete"}, nil
		case "auditor", "auditor-light":
			auditCalls++
			return auditBackend.Run(ctx, request)
		default:
			return turnResult{Text: "completed"}, nil
		}
	})
	var events []event
	writer := newEventWriter(io.Discard)
	writer.setHook(func(value event) { events = append(events, value) })
	runner := newPipeline(cliArgs{
		High: "provider/high", EntryAgent: "root-orchestrator",
	}, workspace, pipelineDeps{Backend: backend, Events: writer})
	t.Cleanup(runner.runtime.Close)
	dispatched := 0
	runner.runSchedulerCycle = func(
		_ context.Context, _ *scheduler.Scheduler, _ scheduler.SchedulerInput,
	) (scheduler.SchedulerCycleResult, error) {
		dispatched++
		got := plandb.GetPlanDB().GetTask(abandonedID)
		if got == nil || got.Status != plandb.StatusReady {
			t.Fatalf("abandoned task was not released before dispatch: %#v", got)
		}
		if _, doneErr := plandb.GetPlanDB().DoneTask(
			abandonedID, plandb.DoneOpts{Result: "already verified"},
		); doneErr != nil {
			return scheduler.SchedulerCycleResult{}, doneErr
		}
		return scheduler.SchedulerCycleResult{Dispatched: []scheduler.DispatchResult{{
			TaskID: string(abandonedID), Success: true,
		}}}, nil
	}

	result, err := runner.run(context.Background(), "implement", pipelineOptions{})
	if err != nil || result.Status != "pass" {
		t.Fatalf("live-shaped drain result=%#v error=%v", result, err)
	}
	if dispatched != 1 || auditCalls == 0 {
		t.Fatalf("dispatches=%d audit calls=%d", dispatched, auditCalls)
	}
	foundRelease := false
	for _, value := range events {
		if value.Stage == "stale-reaper" && value.Status == "released" {
			foundRelease = true
		}
	}
	if !foundRelease {
		t.Fatalf("stale release event missing: %#v", events)
	}
}

func TestPipelineDoesNotAuditQuietOpenGraph(t *testing.T) {
	// C3/C4/C6: genuinely unfinished work remains blocked after the bounded
	// drain. It becomes an explicit resumable fail, never audit/pass or crash.
	workspace := validityTestRepo(t)
	setValidityTestEnv(t, workspace)
	t.Setenv("CODEAF_VALIDITY", "0")
	t.Setenv("CODEAF_PRE_GATES", "0")
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")
	t.Setenv("CODEAF_DISPATCH_MAX_LOOPS", "2")

	auditCalls := 0
	childAdded := false
	backend := backendFunc(func(_ context.Context, request turn) (turnResult, error) {
		switch request.Agent {
		case "root-orchestrator":
			if !childAdded {
				childAdded = true
				if _, err := plandb.GetPlanDB().AddTask(plandb.AddTaskInput{
					Title: "unfinished child", Project: request.PlanDB.ProjectID,
					Parent: plandb.TaskID(request.PlanDB.RootTaskID), CustomID: "t-unfinished",
				}); err != nil {
					return turnResult{}, err
				}
			}
			return turnResult{SessionID: request.SessionID, Text: "quiet root result"}, nil
		case "auditor", "auditor-light":
			auditCalls++
			return turnResult{Text: "pass"}, nil
		default:
			return turnResult{Text: "completed"}, nil
		}
	})
	var events []event
	writer := newEventWriter(io.Discard)
	writer.setHook(func(value event) { events = append(events, value) })
	runner := newPipeline(cliArgs{
		High: "provider/high", EntryAgent: "root-orchestrator",
	}, workspace, pipelineDeps{Backend: backend, Events: writer})
	t.Cleanup(runner.runtime.Close)
	drainCycles := 0
	runner.runSchedulerCycle = func(
		context.Context, *scheduler.Scheduler, scheduler.SchedulerInput,
	) (scheduler.SchedulerCycleResult, error) {
		drainCycles++
		return scheduler.SchedulerCycleResult{}, nil
	}

	result, err := runner.run(context.Background(), "implement", pipelineOptions{})
	if err != nil || result.Status != "fail" {
		t.Fatalf("pipeline result=%#v error=%v", result, err)
	}
	if result.Status == "pass" || result.Status == "crashed" || auditCalls != 0 {
		t.Fatalf("quiet open graph reached audit/pass: result=%#v audit calls=%d", result, auditCalls)
	}
	if drainCycles != 2 || !strings.Contains(result.Reason, "t-unfinished") ||
		!strings.Contains(result.Reason, "unfinished child") ||
		!strings.Contains(result.Reason, "ready") {
		t.Fatalf("bounded drain cycles=%d reason=%q", drainCycles, result.Reason)
	}
	for _, value := range events {
		if value.Stage == "root-orchestrator" && value.Status == "completed" {
			t.Fatalf("quiet open graph reported root completed: %#v", events)
		}
		if value.Stage == "audit" {
			t.Fatalf("quiet open graph emitted audit event: %#v", events)
		}
	}
}

func TestRootOrchestratorCompletesDrainedGraph(t *testing.T) {
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	projectRow := db.Init("drained-root")
	root, err := db.AddTask(plandb.AddTaskInput{
		Title: "root", Project: projectRow.ID, CustomID: "t-root",
	})
	if err != nil {
		t.Fatal(err)
	}
	child, err := db.AddTask(plandb.AddTaskInput{
		Title: "child", Project: projectRow.ID, Parent: root.ID, CustomID: "t-child",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DoneTask(child.ID, plandb.DoneOpts{Result: "done"}); err != nil {
		t.Fatal(err)
	}

	runner := newPipeline(cliArgs{
		High: "openrouter/vendor/high", EntryAgent: "root-orchestrator",
	}, t.TempDir(), pipelineDeps{
		Backend: backendFunc(func(
			_ context.Context, request turn,
		) (turnResult, error) {
			return turnResult{SessionID: request.SessionID, Text: "root result"}, nil
		}),
		Events: newEventWriter(io.Discard),
	})
	t.Cleanup(runner.runtime.Close)

	if err := runner.runRootOrchestrator(
		context.Background(), "implement", string(projectRow.ID), string(root.ID), nil,
	); err != nil {
		t.Fatal(err)
	}
	if got := db.GetTask(root.ID); got == nil || got.Status != plandb.StatusDone {
		t.Fatalf("drained root status = %#v", got)
	}
}

func TestRootSchedulerDrainSweepClosesHarnessBookkeepingWithoutStallCredit(t *testing.T) {
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	projectRow := db.Init("drain-sweep-harness")
	root, err := db.AddTask(plandb.AddTaskInput{
		Title: "root", Project: projectRow.ID, CustomID: "t-sweep-root",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DoneTask(root.ID, plandb.DoneOpts{Result: "root complete"}); err != nil {
		t.Fatal(err)
	}
	description := "Harness-created direct verification package.\n\ntask_role: qa\naccess: read"
	bookkeeping, err := db.AddTask(plandb.AddTaskInput{
		Title: "Direct verification: go build ./...", Description: &description,
		Project: projectRow.ID, Parent: root.ID, CustomID: "t-xue4", Kind: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if db.ClaimTask(bookkeeping.ID, "root-orchestrator") == nil || db.StartTask(bookkeeping.ID) == nil {
		t.Fatal("could not reproduce the running run-M bookkeeping task")
	}

	var events []event
	writer := newEventWriter(io.Discard)
	writer.setHook(func(value event) { events = append(events, value) })
	runner := newPipeline(cliArgs{}, t.TempDir(), pipelineDeps{Events: writer, Notes: io.Discard})
	t.Cleanup(runner.runtime.Close)
	plan := steploop.PlanDBInfo{ProjectID: projectRow.ID, RootTaskID: root.ID}
	pump := runner.rootScheduler(&rootSchedulerScript{t: t, wantPlan: plan}).(*pipelineRootScheduler)
	pump.sweepMinAge = 0
	summary, err := pump.Pump(context.Background(), steploop.SchedulerInput{Plan: plan})
	if err != nil || summary != "" {
		t.Fatalf("swept quiet cycle summary=%q error=%v", summary, err)
	}
	if got := db.GetTask(bookkeeping.ID); got == nil || got.Status != plandb.StatusDone ||
		!strings.Contains(string(got.Result), "auto-closed by root drain") {
		t.Fatalf("bookkeeping task was not swept closed: %#v", got)
	}
	if pump.quietCycles != 0 {
		t.Fatalf("sweep counted toward stall threshold: quietCycles=%d", pump.quietCycles)
	}
	found := false
	for _, value := range events {
		if value.Stage == "scheduler" && value.Status == "drain-sweep" &&
			value.Data["closed_count"] == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("drain-sweep close event missing: %#v", events)
	}
}

func TestRootSchedulerDrainSweepReleasesStaleRootClaimForDispatch(t *testing.T) {
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	projectRow := db.Init("drain-sweep-release")
	root, err := db.AddTask(plandb.AddTaskInput{
		Title: "root", Project: projectRow.ID, CustomID: "t-release-root",
	})
	if err != nil {
		t.Fatal(err)
	}
	description := "Planner-authored QA work.\n\ntask_role: qa\naccess: read"
	child, err := db.AddTask(plandb.AddTaskInput{
		Title: "real QA work", Description: &description, Project: projectRow.ID,
		Parent: root.ID, CustomID: "t-release-child", Kind: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if db.ClaimTask(child.ID, "root-orchestrator") == nil || db.StartTask(child.ID) == nil {
		t.Fatal("could not create stale root-orchestrator claim")
	}

	var events []event
	writer := newEventWriter(io.Discard)
	writer.setHook(func(value event) { events = append(events, value) })
	runner := newPipeline(cliArgs{}, t.TempDir(), pipelineDeps{Events: writer, Notes: io.Discard})
	t.Cleanup(runner.runtime.Close)
	plan := steploop.PlanDBInfo{ProjectID: projectRow.ID, RootTaskID: root.ID}
	pump := runner.rootScheduler(&rootSchedulerScript{t: t, wantPlan: plan}).(*pipelineRootScheduler)
	pump.sweepMinAge = 0
	summary, err := pump.Pump(context.Background(), steploop.SchedulerInput{Plan: plan})
	if err != nil || !strings.Contains(summary, "open descendant") {
		t.Fatalf("release cycle summary=%q error=%v", summary, err)
	}
	if got := db.GetTask(child.ID); got == nil || got.Status != plandb.StatusReady || got.AgentID != nil {
		t.Fatalf("stale root claim was not released to the ready frontier: %#v", got)
	}
	if pump.quietCycles != 0 {
		t.Fatalf("release counted toward stall threshold: quietCycles=%d", pump.quietCycles)
	}
	if claimed := db.ClaimTask(child.ID, "qa-worker"); claimed == nil {
		t.Fatal("scheduler could not subsequently claim the released task")
	}
	found := false
	for _, value := range events {
		if value.Stage == "scheduler" && value.Status == "drain-sweep" &&
			value.Data["released_count"] == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("drain-sweep release event missing: %#v", events)
	}
}

func TestRootSchedulerRejectsOrphanedClaimAsProgress(t *testing.T) {
	// F3.1/F3.2 corrects the old regression: a bare PlanDB claim has no live
	// dispatch behind it and must reach the ordinary stall threshold.
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	projectRow := db.Init("quiet-busy")
	root, err := db.AddTask(plandb.AddTaskInput{
		Title: "root", Project: projectRow.ID, CustomID: "t-busy-root",
	})
	if err != nil {
		t.Fatal(err)
	}
	child, err := db.AddTask(plandb.AddTaskInput{
		Title: "running child", Project: projectRow.ID, Parent: root.ID,
		CustomID: "t-busy-child",
	})
	if err != nil {
		t.Fatal(err)
	}
	if claimed := db.ClaimTask(child.ID, "leaf"); claimed == nil {
		t.Fatal("claiming the child task failed")
	}

	runner := newPipeline(cliArgs{}, t.TempDir(), pipelineDeps{
		Backend: backendFunc(func(context.Context, turn) (turnResult, error) {
			return turnResult{}, nil
		}),
		Events: newEventWriter(io.Discard),
		Notes:  io.Discard,
	})
	t.Cleanup(runner.runtime.Close)
	plan := steploop.PlanDBInfo{
		ProjectID: string(projectRow.ID), RootTaskID: string(root.ID),
	}
	pump := runner.rootScheduler(&rootSchedulerScript{t: t, wantPlan: plan})
	for cycle := 1; cycle < stallCycleThreshold; cycle++ {
		summary, err := pump.Pump(
			context.Background(), steploop.SchedulerInput{Plan: plan},
		)
		if err != nil {
			t.Fatalf("orphan cycle %d stalled early: %v", cycle, err)
		}
		if !strings.Contains(summary, "open descendant") {
			t.Fatalf("orphan cycle %d summary = %q", cycle, summary)
		}
	}
	_, err = pump.Pump(context.Background(), steploop.SchedulerInput{Plan: plan})
	if !errors.Is(err, errRootDrainStalled) || !strings.Contains(err.Error(), "3 consecutive cycles") {
		t.Fatalf("orphaned claim result = %v", err)
	}
}

func TestRootSchedulerUsesLiveDispatchTruthAndBoundsContinuations(t *testing.T) {
	// F3.3/F3.4 and F2.3: a window-full cycle backed by a real live registry
	// entry resets the short stall counter, while the independent continuation
	// ceiling still prevents unbounded provider turns.
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	projectRow := db.Init("live-busy")
	root, err := db.AddTask(plandb.AddTaskInput{Title: "root", Project: projectRow.ID, CustomID: "live-root"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := db.AddTask(plandb.AddTaskInput{Title: "child", Project: projectRow.ID, Parent: root.ID, CustomID: "live-child"})
	if err != nil || db.ClaimTask(child.ID, "leaf") == nil {
		t.Fatalf("live child setup: %v", err)
	}
	runner := newPipeline(cliArgs{}, t.TempDir(), pipelineDeps{Events: newEventWriter(io.Discard), Notes: io.Discard})
	t.Cleanup(runner.runtime.Close)
	runner.inFlightDispatchIDs = func() []string { return []string{child.ID} }
	plan := steploop.PlanDBInfo{ProjectID: projectRow.ID, RootTaskID: root.ID}
	pump := runner.rootScheduler(&rootSchedulerScript{t: t, wantPlan: plan, quietReason: scheduler.CycleQuietWindowFull})
	for cycle := 1; cycle < syntheticContinuationLimit; cycle++ {
		if _, pumpErr := pump.Pump(context.Background(), steploop.SchedulerInput{Plan: plan}); pumpErr != nil {
			t.Fatalf("live cycle %d stalled before continuation bound: %v", cycle, pumpErr)
		}
	}
	_, err = pump.Pump(context.Background(), steploop.SchedulerInput{Plan: plan})
	if !errors.Is(err, errRootDrainStalled) || !strings.Contains(err.Error(), "synthetic continuation cycle limit") {
		t.Fatalf("pathological live continuation result = %v", err)
	}
}

func TestRootSchedulerBoundsResourcePauseSeparately(t *testing.T) {
	// F2.1/F2.2: pause cycles do not increment the three-cycle deadlock counter,
	// but a permanent pause terminates at its larger pause-specific bound.
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	projectRow := db.Init("paused")
	root, _ := db.AddTask(plandb.AddTaskInput{Title: "root", Project: projectRow.ID, CustomID: "pause-root"})
	_, _ = db.AddTask(plandb.AddTaskInput{Title: "child", Project: projectRow.ID, Parent: root.ID, CustomID: "pause-child"})
	runner := newPipeline(cliArgs{}, t.TempDir(), pipelineDeps{Events: newEventWriter(io.Discard), Notes: io.Discard})
	t.Cleanup(runner.runtime.Close)
	plan := steploop.PlanDBInfo{ProjectID: projectRow.ID, RootTaskID: root.ID}
	pump := runner.rootScheduler(&rootSchedulerScript{t: t, wantPlan: plan, quietReason: scheduler.CycleQuietPaused})
	for cycle := 1; cycle < pauseCycleThreshold; cycle++ {
		if _, pumpErr := pump.Pump(context.Background(), steploop.SchedulerInput{Plan: plan}); pumpErr != nil {
			t.Fatalf("pause cycle %d stalled early: %v", cycle, pumpErr)
		}
	}
	_, err := pump.Pump(context.Background(), steploop.SchedulerInput{Plan: plan})
	if !errors.Is(err, errRootDrainStalled) || !strings.Contains(err.Error(), "resource pause persisted") {
		t.Fatalf("permanent pause result = %v", err)
	}
}
