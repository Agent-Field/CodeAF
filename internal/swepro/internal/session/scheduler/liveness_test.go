package scheduler

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/leafoutcome"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/resourceguard"
)

func clearSchedulerInFlight(t *testing.T) {
	t.Helper()
	for _, id := range InFlightDispatchIDs() {
		schedulerInFlightDispatches.remove(id)
	}
	t.Cleanup(func() {
		for _, id := range InFlightDispatchIDs() {
			schedulerInFlightDispatches.remove(id)
		}
	})
}

func TestSchedulerCycleExposesQuietReason(t *testing.T) {
	clearSchedulerInFlight(t)
	t.Run("nothing ready", func(t *testing.T) {
		projectID, _ := seedCycleTasks(t, 0)
		dispatcher := NewScheduler(SchedulerOptions{Workspace: t.TempDir()})
		result, err := dispatcher.RunSchedulerCycle(context.Background(), SchedulerInput{
			RootTaskID: "root", ProjectID: projectID,
		})
		if err != nil || result.QuietReason != CycleQuietNothingReady {
			t.Fatalf("quiet result = %#v, %v", result, err)
		}
	})
	t.Run("resource pause", func(t *testing.T) {
		projectID, _ := seedCycleTasks(t, 1)
		dispatcher := NewScheduler(SchedulerOptions{Workspace: t.TempDir()})
		dispatcher.envelopeReader = fakeEnvelopeReader{envelope: resourceguard.DiskEnvelope{
			FreeGB: 2, FloorGB: 5, OK: false,
		}}
		result, err := dispatcher.RunSchedulerCycle(context.Background(), SchedulerInput{
			RootTaskID: "root", ProjectID: projectID,
		})
		if err != nil || result.QuietReason != CycleQuietPaused {
			t.Fatalf("pause result = %#v, %v", result, err)
		}
	})
	t.Run("window full", func(t *testing.T) {
		projectID, _ := seedCycleTasks(t, 1)
		schedulerInFlightDispatches.addAll([]string{"other-live-task"})
		t.Cleanup(func() { schedulerInFlightDispatches.remove("other-live-task") })
		dispatcher := NewScheduler(SchedulerOptions{Workspace: t.TempDir()})
		maximum := 1.0
		result, err := dispatcher.RunSchedulerCycle(context.Background(), SchedulerInput{
			RootTaskID: "root", ProjectID: projectID, MaxParallel: &maximum,
		})
		if err != nil || result.QuietReason != CycleQuietWindowFull {
			t.Fatalf("window result = %#v, %v", result, err)
		}
	})
}

type claimGapPlanDB struct {
	onClaim chan struct{}
	release chan struct{}
	once    sync.Once
}

func (db *claimGapPlanDB) Run(argv []string) plandb.RunResult {
	result := nativePlanDBRunner{}.Run(argv)
	if len(argv) >= 4 && reflect.DeepEqual(argv[:3], []string{"plandb", "task", "claim"}) {
		db.once.Do(func() { close(db.onClaim) })
		<-db.release
	}
	return result
}

func TestClaimIsPublishedBeforeItBecomesObservable(t *testing.T) {
	// F5.1/F5.2: stop immediately after PlanDB has made the claim observable,
	// but before the claim command returns to the cycle. The global registry is
	// already populated and lock-ordered stale release cannot reclaim it.
	clearSchedulerInFlight(t)
	projectID, ids := seedCycleTasks(t, 1)
	adaptive, cache := false, false
	dispatcher := NewScheduler(SchedulerOptions{
		Workspace: t.TempDir(), Agents: phase2Agents{agent: &AgentInfo{Name: "fixer"}},
		StepLoop: &scriptedStepLoop{results: []LeafRunResult{{Parts: []LeafPart{{Type: "text", Text: "done"}}}}},
		Gate: gateFunc(func(_ context.Context, input GateInput) (GateResult, error) {
			return GateResult{Status: GatePass, FinalWorktreePath: input.WorktreePath, FinalBranch: input.Branch}, nil
		}),
		Pools: phase2Pools{}, Provider: phase2Provider{},
		MergeStack: mergeStackFunc(func(context.Context, MergeRequest) (MergeResult, error) {
			return MergeResult{OK: true, Summary: "merged"}, nil
		}),
		MergeCoordinator: quickMergeCoordinator(), AdaptiveCuts: &adaptive, OutcomeCache: &cache,
		PreserveRejectedWork: func(leafoutcome.PreserveRejectedWorkArgs) {},
	})
	dispatcher.runner = phase2Runner{}
	gap := &claimGapPlanDB{onClaim: make(chan struct{}), release: make(chan struct{})}
	dispatcher.planDB = gap
	done := make(chan error, 1)
	go func() {
		_, err := dispatcher.RunSchedulerCycle(context.Background(), SchedulerInput{
			RootTaskID: "root", ProjectID: projectID, ParentSessionID: "parent",
		})
		done <- err
	}()
	select {
	case <-gap.onClaim:
	case <-time.After(phase2LivenessTimeout):
		t.Fatal("cycle never reached observable claim")
	}
	if task := plandb.GetPlanDB().GetTask(ids[0]); task == nil || task.Status != plandb.StatusClaimed {
		t.Fatalf("task in claim gap = %#v", task)
	}
	if got := InFlightDispatchIDs(); len(got) != 1 || got[0] != ids[0] {
		t.Fatalf("published claim = %#v, want %q", got, ids[0])
	}
	if released := ReleaseTaskIfNotInFlight(ids[0]); released != nil {
		t.Fatalf("live claim was released: %#v", released)
	}
	close(gap.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(phase2LivenessTimeout):
		t.Fatal("cycle did not leave claim gap")
	}
}
