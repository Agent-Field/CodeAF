package run

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func diffJob() factory.Item {
	now := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	return factory.Item{ID: 3, State: factory.StateRunning,
		Stages: []factory.Stage{{Name: "plan", On: true}, {Name: factory.ApproveName, Kind: factory.StageGate, On: true}},
		Stream: &factory.Stream{Started: now, Spent: 0.5,
			Phases: []factory.Phase{{Name: "plan", Kind: factory.StageChat, State: factory.PhaseRunning}, {Name: factory.ApproveName, Kind: factory.StageGate, State: factory.PhasePending}},
			Log:    []factory.LogLine{{At: now, Tone: "said", Text: "queued"}}}}
}

// THE RUNNER'S WRITES, AS A REPORT: a step's result, the new log tail and
// cost go; the approve step and the landing line never do, and a report
// applied to the server's copy lands what the runner wrote.
func TestDiffReportsTheRunnersWritesOnly(t *testing.T) {
	prev := diffJob()
	cur := diffJob()
	cur.Stream = &factory.Stream{Started: prev.Stream.Started, Spent: 0.75, Log: append([]factory.LogLine(nil), prev.Stream.Log...),
		Phases: []factory.Phase{{Name: "plan", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1, Result: &factory.StageResult{Done: true, Output: "a plan"}},
			{Name: factory.ApproveName, Kind: factory.StageGate, State: factory.PhaseDone}}}
	cur.Stream.Log = append(cur.Stream.Log, factory.LogLine{Tone: "said", Text: "plan: a plan"},
		factory.LogLine{Tone: "said", Text: "steer: shorter"}, factory.LogLine{Tone: "said", Text: sayLanded})
	cur.State = factory.StateLanded
	r, ok := Diff(prev, cur)
	if !ok || len(r.Stages) != 1 || r.Stages[0].Name != "plan" || r.Stages[0].Result == nil {
		t.Fatalf("steps reported: %+v", r.Stages)
	}
	// `plan: a plan` is the server's own line for the result: not sent.
	if r.Cost != 0.25 || len(r.Log) != 1 || r.Log[0].Text != "steer: shorter" {
		t.Fatalf("cost %v, log %+v", r.Cost, r.Log)
	}
	if r.Pipeline != nil || r.Question != nil {
		t.Fatalf("an unchanged pipeline or no question was reported: %+v", r)
	}
	server := diffJob()
	if err := ApplyReport(&server, r, time.Now()); err != nil {
		t.Fatal(err)
	}
	if ph := server.Stream.Phases[0]; ph.State != factory.PhaseDone || ph.Result == nil || ph.Result.Output != "a plan" {
		t.Fatalf("the server's plan step: %+v", ph)
	}
	said := 0
	for _, l := range server.Stream.Log {
		if l.Text == "plan: a plan" {
			said++
		}
	}
	if said != 1 {
		t.Fatalf("the server's log says the result %d times: %+v", said, server.Stream.Log)
	}
	if _, ok := Diff(cur, cur); ok {
		t.Fatal("no change was reported as one")
	}
}

// A RESHAPED PIPELINE TRAVELS, but never without its approve step.
func TestApplyPipelineKeepsThePersonsGate(t *testing.T) {
	prev, cur := diffJob(), diffJob()
	cur.Stages = append(cur.Stages, factory.Stage{Name: "review", Kind: factory.StageChat, On: true})
	cur.Stream.Phases = append(cur.Stream.Phases, factory.Phase{Name: "review", Kind: factory.StageChat, State: factory.PhasePending})
	r, _ := Diff(prev, cur)
	server := diffJob()
	if err := ApplyReport(&server, r, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(server.Stream.Phases) != 3 || server.Stream.Phases[2].Name != "review" || factory.StageIndex(server.Stages, "review") < 0 {
		t.Fatalf("the reshaped job: %+v %+v", server.Stages, server.Stream.Phases)
	}
	cur.Stages = cur.Stages[:1]
	r, _ = Diff(prev, cur)
	server = diffJob()
	if err := ApplyReport(&server, r, time.Now()); err == nil || !strings.Contains(err.Error(), "approve step") {
		t.Fatalf("a pipeline without the approve step = %v", err)
	}
}
