// This file covers pure CLI/resume decisions from swe-pro/src/cli/cmd/resume.ts:1-429.
package main

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/plandb"
	"github.com/Agent-Field/swe-pro-go/internal/session/fixgenerator"
	"github.com/Agent-Field/swe-pro-go/internal/session/ledgers"
)

func TestParseArgsAllowsOptionsAroundMessage(t *testing.T) {
	got, err := parseArgs([]string{
		"resume", "finish", "--dir", "/tmp/repo", "the", "work",
		"--hard", "--frontier=x/y",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Command != "resume" || got.Message != "finish the work" ||
		got.Directory != "/tmp/repo" || !got.Hard || got.Frontier != "x/y" {
		t.Fatalf("parsed args = %#v", got)
	}
}

func TestResumeRestoresPersistedAuditCycleContract(t *testing.T) {
	// Parity audit contract 8: resume carries the persisted fix-cycle counter
	// into the next production audit instead of resetting to cycle one.
	workspace := t.TempDir()
	if err := writeFile(filepath.Join(workspace, "Makefile"), "build:\n\t@true\n\ntest:\n\t@true\n"); err != nil {
		t.Fatal(err)
	}
	if err := fixgenerator.WriteAuditCycles(workspace, 2); err != nil {
		t.Fatal(err)
	}
	writeTerminalCheckpoint(workspace, resumeCheckpointFile{
		Goal: "continue", FinalStatus: "fail", Cycle: 1,
	})
	checkpoint := rehydrateCheckpoint(workspace)
	if checkpoint.LastCycle != 2 {
		t.Fatalf("rehydrated audit cycle = %v, want 2", checkpoint.LastCycle)
	}
	t.Setenv("CODEAF_AUDITOR", "0")
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Backend: &capturingBackend{}, Events: newEventWriter(io.Discard),
	})
	defer runner.runtime.Close()
	runner.restoredAuditCycle = checkpoint.LastCycle
	_, cycle, err := runner.auditFixLoop(context.Background(), "continue", "", "project", "root")
	if err != nil || cycle != 3 {
		t.Fatalf("resumed audit cycle = %d, %v; want 3", cycle, err)
	}
}

func TestBuildResumeSeedCarriesDurableState(t *testing.T) {
	seed := buildResumeSeed(
		"finish migration",
		resumeCheckpoint{
			OpenBlockers: []ledgers.OpenBlocker{{
				BlockerID: "b1", Text: "test is red", CycleOpened: 2,
			}},
			PendingTaskCount: 2,
			FailureSignals:   []string{"missing behavior"},
		},
		[]ledgers.AttemptRecord{{
			Attempt: 1, Approach: "old approach", Outcome: "failed",
		}},
		staleClaims{Claimed: []string{"t1"}},
	)
	for _, fragment := range []string{
		"# Resumed run (fresh context)", "2 fix-task(s)", "1 blocker(s)",
		"Released 1 stale task(s)", "finish migration", "missing behavior",
		"test is red", "old approach",
	} {
		if !strings.Contains(seed, fragment) {
			t.Errorf("seed missing %q:\n%s", fragment, seed)
		}
	}
}

func TestDiscoverRunRootPrefersPersistedOpenGraph(t *testing.T) {
	// Validation contract: resume re-enters unfinished orchestration even when
	// a newer, already-terminal root also exists in the durable database.
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	openProject := db.Init("open-run", "unfinished")
	description := "root"
	openRoot, err := db.AddTask(plandb.AddTaskInput{
		Title: "Open root", Description: &description,
		Project: openProject.ID, Tags: []string{"codeaf:root"},
	})
	if err != nil {
		t.Fatal(err)
	}
	childDescription := "unfinished child"
	if _, err := db.AddTask(plandb.AddTaskInput{
		Title: "Open child", Description: &childDescription,
		Project: openProject.ID, Parent: openRoot.ID,
	}); err != nil {
		t.Fatal(err)
	}
	closedProject := db.Init("closed-run", "finished")
	closedRoot, err := db.AddTask(plandb.AddTaskInput{
		Title: "Closed root", Description: &description,
		Project: closedProject.ID, Tags: []string{"codeaf:root"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DoneTask(plandb.TaskID(closedRoot.ID), plandb.DoneOpts{}); err != nil {
		t.Fatal(err)
	}

	projectID, rootID := discoverRunRoot()
	if projectID != openProject.ID || rootID != openRoot.ID {
		t.Fatalf("resume root = %s/%s, want %s/%s", projectID, rootID, openProject.ID, openRoot.ID)
	}
}

func TestResumeLeavesCompletedTasksClosed(t *testing.T) {
	// Validation contract: resume counts/requeues only unfinished PlanDB work;
	// a task already completed by the interrupted run is not done again.
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	done, err := db.AddTask(plandb.AddTaskInput{CustomID: "done-task", Title: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DoneTask(done.ID, plandb.DoneOpts{Result: "complete"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddTask(plandb.AddTaskInput{CustomID: "ready-task", Title: "ready"}); err != nil {
		t.Fatal(err)
	}

	checkpoint := rehydrateCheckpoint(t.TempDir())
	if checkpoint.PendingTaskCount != 1 {
		t.Fatalf("pending resume tasks = %d, want only ready-task", checkpoint.PendingTaskCount)
	}
	stale := releaseStaleClaims()
	if len(stale.Claimed) != 0 || len(stale.Running) != 0 {
		t.Fatalf("completed task was treated as stale: %#v", stale)
	}
	if got := db.GetTask(done.ID); got == nil || got.Status != plandb.StatusDone {
		t.Fatalf("completed task after resume rehydration = %#v", got)
	}
}

func TestDrainStaleReleaseProtectsLiveDispatch(t *testing.T) {
	// C5: reclamation applies only when no scheduler dispatch owns the claim.
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
	db := plandb.GetPlanDB()
	task, err := db.AddTask(plandb.AddTaskInput{CustomID: "live-task", Title: "live"})
	if err != nil {
		t.Fatal(err)
	}
	if db.ClaimTask(task.ID, "scheduler") == nil {
		t.Fatal("claim failed")
	}
	eligible := map[string]struct{}{task.ID: {}}
	live := map[string]struct{}{task.ID: {}}
	protected := releaseStaleClaimsExcept(eligible, live)
	if len(protected.Claimed) != 0 || db.GetTask(task.ID).Status != plandb.StatusClaimed {
		t.Fatalf("live claim was reclaimed: %#v task=%#v", protected, db.GetTask(task.ID))
	}

	released := releaseStaleClaimsExcept(eligible, nil)
	if len(released.Claimed) != 1 || released.Claimed[0] != task.ID ||
		db.GetTask(task.ID).Status != plandb.StatusReady {
		t.Fatalf("orphaned claim was not requeued: %#v task=%#v", released, db.GetTask(task.ID))
	}
}
