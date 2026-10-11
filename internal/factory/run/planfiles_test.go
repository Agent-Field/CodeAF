package run

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestPlanFilesReadsTheResultListOrTheClaims(t *testing.T) {
	cases := []struct {
		name string
		res  factory.StageResult
		want []string
	}{
		{
			name: "headed list",
			res:  factory.StageResult{Output: "Wrap the sender.\n\n## Files\n- internal/a.go\n- `internal/b.go`\n\nThen stop."},
			want: []string{"internal/a.go", "internal/b.go"},
		},
		{
			name: "files colon",
			res:  factory.StageResult{Output: "files: internal/a.go, ./internal/b.go"},
			want: []string{"internal/a.go", "internal/b.go"},
		},
		{
			name: "path bullets",
			res:  factory.StageResult{Output: "plan\n- deliver.go\n- api/retry.go\n- wrap the sender"},
			want: []string{"deliver.go", "api/retry.go"},
		},
		{
			name: "claims when the result names none",
			res: factory.StageResult{
				Output: "backoff in the deliverer",
				Claims: []factory.Claim{
					{Text: "will change api/webhooks/deliver.go"},
					{Text: "and api/webhooks/deliver.go again", Evidence: "api/webhooks/retry.go:12"},
					{Text: "see https://github.com/acme/api/blob/main/README.md"},
				},
			},
			want: []string{"api/webhooks/deliver.go", "api/webhooks/retry.go"},
		},
		{
			name: "a list wins over claims",
			res: factory.StageResult{
				Output: "files:\n- internal/a.go\n",
				Claims: []factory.Claim{{Text: "also internal/b.go"}},
			},
			want: []string{"internal/a.go"},
		},
		{
			name: "prose is not a list",
			res:  factory.StageResult{Output: "see docs/design/factory/DESIGN.md and stop"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := planFiles(c.res); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("plan files %v, want %v", got, c.want)
			}
		})
	}
}

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
	if !it.Stream.Phases[1].At.Equal(at.Add(time.Minute)) {
		t.Fatalf("write finished at %v", it.Stream.Phases[1].At)
	}
}

func TestApplyReportReadsPlanClaimsWhenTheResultNamesNone(t *testing.T) {
	at := time.Date(2026, 10, 11, 15, 4, 0, 0, time.UTC)
	it := factory.Item{ID: 8, State: factory.StateRunning,
		Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}},
		Stream: &factory.Stream{Started: at, Phases: []factory.Phase{{Name: "plan", Kind: factory.StageChat, State: factory.PhaseRunning}}}}
	err := ApplyReport(&it, factory.RunReport{Stages: []factory.StageReport{{
		Name: "plan", State: factory.PhaseDone,
		Result: &factory.StageResult{Done: true, Output: "backoff in the deliverer", Claims: []factory.Claim{
			{Text: "api/webhooks/deliver.go"},
			{Text: "touch api/webhooks/retry.go"},
		}},
	}}}, at)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"api/webhooks/deliver.go", "api/webhooks/retry.go"}
	if !reflect.DeepEqual(it.PlannedFiles, want) {
		t.Fatalf("planned %v, want %v", it.PlannedFiles, want)
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
