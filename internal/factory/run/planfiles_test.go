package run

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestSetPhaseStateStampsOnlyAnEnding(t *testing.T) {
	now := time.Date(2026, 10, 11, 15, 4, 0, 0, time.UTC)
	later := now.Add(time.Minute)
	var ph factory.Phase
	setPhaseState(&ph, factory.PhaseRunning, now)
	if !ph.At.IsZero() || ph.State != factory.PhaseRunning {
		t.Fatalf("running %+v", ph)
	}
	setPhaseState(&ph, factory.PhaseDone, now)
	if !ph.At.Equal(now) || ph.State != factory.PhaseDone {
		t.Fatalf("done %+v", ph)
	}
	setPhaseState(&ph, factory.PhasePending, later)
	if !ph.At.IsZero() {
		t.Fatalf("sent back kept %v", ph.At)
	}
	setPhaseState(&ph, factory.PhaseFailed, later)
	if !ph.At.Equal(later) {
		t.Fatalf("failed at %v", ph.At)
	}
	setPhaseState(&ph, factory.PhaseWaiting, now)
	if !ph.At.Equal(later) || ph.State != factory.PhaseWaiting {
		t.Fatalf("waiting moved the stamp %+v", ph)
	}
}

func TestApplyReportStampsTheFinishAndKeepsThePlansFiles(t *testing.T) {
	at := time.Date(2026, 10, 11, 15, 4, 0, 0, time.UTC)
	it := factory.Item{ID: 7, State: factory.StateRunning,
		Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}, {Name: "write", Kind: factory.StageChat, On: true}},
		Stream: &factory.Stream{Started: at, Phases: []factory.Phase{
			{Name: "plan", Kind: factory.StageChat, State: factory.PhaseRunning},
			{Name: "write", Kind: factory.StageChat, State: factory.PhasePending},
		}}}
	err := ApplyReport(&it, factory.RunReport{Stages: []factory.StageReport{{
		Name: "plan", State: factory.PhaseDone, Round: 1,
		Result: &factory.StageResult{Done: true, Output: "files:\n- internal/a.go\n- internal/b.go\n"},
	}}}, at)
	if err != nil {
		t.Fatal(err)
	}
	ph := it.Stream.Phases[0]
	if ph.State != factory.PhaseDone || !ph.At.Equal(at) {
		t.Fatalf("plan phase %+v", ph)
	}
	if !it.Stream.Phases[1].At.IsZero() {
		t.Fatalf("write has not ended, at %v", it.Stream.Phases[1].At)
	}
	if !reflect.DeepEqual(it.PlannedFiles, []string{"internal/a.go", "internal/b.go"}) {
		t.Fatalf("planned %v", it.PlannedFiles)
	}

	// A write that lists files is not the plan.
	err = ApplyReport(&it, factory.RunReport{Stages: []factory.StageReport{{
		Name: "write", State: factory.PhaseDone,
		Result: &factory.StageResult{Done: true, Output: "files:\n- internal/c.go\n"},
	}}}, at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(it.PlannedFiles, []string{"internal/a.go", "internal/b.go"}) {
		t.Fatalf("write replaced the plan's files: %v", it.PlannedFiles)
	}
	if got := factory.PlanFilesOf(it); !reflect.DeepEqual(got, []string{"internal/a.go", "internal/b.go"}) {
		t.Fatalf("write spoke for the plan on read: %v", got)
	}
	if !it.Stream.Phases[1].At.Equal(at.Add(time.Minute)) {
		t.Fatalf("write finished at %v", it.Stream.Phases[1].At)
	}
}

func planReport(t *testing.T, it *factory.Item, at time.Time, res factory.StageResult) {
	t.Helper()
	if err := ApplyReport(it, factory.RunReport{Stages: []factory.StageReport{{
		Name: "plan", State: factory.PhaseDone, Result: &res,
	}}}, at); err != nil {
		t.Fatal(err)
	}
}

// Claims are prose. A plan that declares no list has no planned files, however
// many path-shaped tokens its claims hold, and a later complete plan without a
// list does not leave the earlier plan's files behind.
func TestApplyReportDoesNotReadPlanClaimsAndClearsAStaleList(t *testing.T) {
	at := time.Date(2026, 10, 11, 15, 4, 0, 0, time.UTC)
	it := factory.Item{ID: 8, State: factory.StateRunning,
		Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}},
		Stream: &factory.Stream{Started: at, Phases: []factory.Phase{{Name: "plan", Kind: factory.StageChat, State: factory.PhaseRunning}}}}
	claims := []factory.Claim{
		{Text: "Entry.Amount is read in ledger.go", Evidence: "README.md:12"},
		{Text: "read, grep and ls show 4 files"},
	}
	planReport(t, &it, at, factory.StageResult{Done: true, Output: "files: a.go, b.go", Claims: claims})
	if !reflect.DeepEqual(it.PlannedFiles, []string{"a.go", "b.go"}) {
		t.Fatalf("declared %v", it.PlannedFiles)
	}
	// A partial report holds no complete plan and leaves what is known.
	planReport(t, &it, at, factory.StageResult{Output: "thinking about c.go"})
	if !reflect.DeepEqual(it.PlannedFiles, []string{"a.go", "b.go"}) {
		t.Fatalf("a partial report changed %v", it.PlannedFiles)
	}
	// What the wire shows is the saved result, and that is now a partial one.
	if got := factory.PlanFilesOf(it); got != nil {
		t.Fatalf("a partial report still declared %v", got)
	}
	planReport(t, &it, at, factory.StageResult{Done: true, Output: "backoff in the deliverer", Claims: claims})
	if it.PlannedFiles != nil || factory.PlanFilesOf(it) != nil {
		t.Fatalf("stale or claim-derived files: %v / %v", it.PlannedFiles, factory.PlanFilesOf(it))
	}
}

func TestAPhaseEndStampsFinishTimeAndThePlanNamesFiles(t *testing.T) {
	at := time.Date(2026, 10, 11, 15, 4, 0, 0, time.UTC)
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
			return factory.StageResult{
				Done:   true,
				Output: "files to touch:\n- internal/factory/run/loop.go\n- `internal/factory/api/codec.go`\n",
				Claims: []factory.Claim{{Text: "also internal/other.go"}},
			}, nil
		}),
	}, func(o *Options) { o.Clock = func() time.Time { return at } })
	id := g.add("fix it", chat("plan"), factory.Stage{Name: factory.ApproveName, Kind: factory.StageGate}, chat("write"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.asked(id, factory.ApproveQuestion("plan"))
	ph := it.Stream.Phases[0]
	if ph.State != factory.PhaseDone || !ph.At.Equal(at) {
		t.Fatalf("plan phase %+v", ph)
	}
	if it.Stream.Phases[1].State != factory.PhaseWaiting || !it.Stream.Phases[1].At.IsZero() {
		t.Fatalf("approve is not still open: %+v", it.Stream.Phases[1])
	}
	want := []string{"internal/factory/run/loop.go", "internal/factory/api/codec.go"}
	if !reflect.DeepEqual(it.PlannedFiles, want) {
		t.Fatalf("planned %v, want %v", it.PlannedFiles, want)
	}
}

// sentBackRig is a job at its approve step with round 1's plan finished,
// reached through the real report door.
func sentBackRig(t *testing.T, at time.Time) *factory.Item {
	t.Helper()
	it := factory.Item{ID: 9, State: factory.StateRunning,
		Stages: []factory.Stage{
			{Name: "plan", Kind: factory.StageChat, On: true},
			{Name: factory.ApproveName, Kind: factory.StageGate, On: true},
			{Name: "write", Kind: factory.StageChat, On: true},
		},
		Stream: &factory.Stream{Started: at}}
	report := func(r factory.RunReport) {
		t.Helper()
		if err := ApplyReport(&it, r, at); err != nil {
			t.Fatal(err)
		}
	}
	report(factory.RunReport{Stages: []factory.StageReport{{
		Name: "plan", State: factory.PhaseDone, Round: 1,
		Result: &factory.StageResult{Done: true, Output: "files: a.go"},
	}}})
	report(factory.RunReport{
		Question:     &factory.Asked{Stage: factory.ApproveName, Question: "approve the plan?"},
		QuestionKind: factory.QKindApprove,
	})
	if got := factory.PlanFilesOf(it); !reflect.DeepEqual(got, []string{"a.go"}) {
		t.Fatalf("round 1 awaiting approval: %v", got)
	}
	if err := AnswerUnheld(&it, false, "use b.go", at); err != nil {
		t.Fatal(err)
	}
	if p := it.Stream.Phases[0]; p.State != factory.PhasePending || p.Round != 2 || p.Result == nil {
		t.Fatalf("send back did not leave round 1's result under round 2: %+v", p)
	}
	return &it
}

// The rejected plan's list must not come back when the revised plan asks the
// person something mid-round: its result is still round 1's, and round 1 DID
// finish, so Done cannot tell them apart. Only the round can.
func TestARevisedPlanThatAsksDoesNotShowTheRejectedPlansFiles(t *testing.T) {
	at := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	for _, kind := range []string{"step", factory.QKindApprove} {
		it := sentBackRig(t, at)
		must := func(r factory.RunReport) {
			t.Helper()
			if err := ApplyReport(it, r, at); err != nil {
				t.Fatal(err)
			}
		}
		it.State = factory.StateRunning
		must(factory.RunReport{Stages: []factory.StageReport{{Name: "plan", State: factory.PhaseRunning, Round: 2}}})
		must(factory.RunReport{Question: &factory.Asked{Stage: "plan", Question: "which file?"}, QuestionKind: kind})
		if got := factory.PlanFilesOf(*it); got != nil {
			t.Fatalf("%s ask showed the rejected plan's files: %v", kind, got)
		}
	}
}

// The revised plan finishing speaks for itself, and only then.
func TestARevisedPlanSpeaksOnceItsOwnRoundFolds(t *testing.T) {
	at := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	it := sentBackRig(t, at)
	it.State = factory.StateRunning
	if err := ApplyReport(it, factory.RunReport{Stages: []factory.StageReport{{
		Name: "plan", State: factory.PhaseDone, Round: 2,
		Result: &factory.StageResult{Done: true, Output: "files: b.go"},
	}}}, at); err != nil {
		t.Fatal(err)
	}
	if got := factory.PlanFilesOf(*it); !reflect.DeepEqual(got, []string{"b.go"}) {
		t.Fatalf("round 2: %v", got)
	}
}

// A worker that reports a send-back plan DONE without a new result cannot
// resurrect the old list.
func TestADoneReportWithoutANewRoundDoesNotResurrectTheOldList(t *testing.T) {
	at := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	it := sentBackRig(t, at)
	it.State = factory.StateRunning
	if err := ApplyReport(it, factory.RunReport{Stages: []factory.StageReport{{Name: "plan", State: factory.PhaseDone}}}, at); err != nil {
		t.Fatal(err)
	}
	if got := factory.PlanFilesOf(*it); got != nil {
		t.Fatalf("stale list: %v", got)
	}
}

// Out of attempts keeps the last draft, which is not a finished plan.
func TestAnOutOfAttemptsPlanDoesNotShowItsDraftFiles(t *testing.T) {
	at := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	it := factory.Item{ID: 10, State: factory.StateRunning,
		Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}},
		Stream: &factory.Stream{Started: at}}
	for _, r := range []factory.RunReport{
		{Stages: []factory.StageReport{{Name: "plan", State: factory.PhaseRunning, Round: 1,
			Result: &factory.StageResult{Output: "files: draft.go"}}}},
		{Question: &factory.Asked{Stage: "plan", Question: "plan is not done after 1 round"}, QuestionKind: factory.QKindOutOfAttempts},
	} {
		if err := ApplyReport(&it, r, at); err != nil {
			t.Fatal(err)
		}
	}
	if got := factory.PlanFilesOf(it); got != nil {
		t.Fatalf("draft files: %v", got)
	}
}
