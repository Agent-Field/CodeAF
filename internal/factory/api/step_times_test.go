package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A STEP AND EACH OF ITS FINISHED ROUNDS SAY WHEN THEY BEGAN, so a surface
// that draws a frame per round places each one from the job's own facts, not
// from the first line of its chat. A running step has a start and no end; a
// finished one has both; a pending one has neither. Each finished round
// carries its start and its chat.
func TestTheReadSaysWhenEachStepAndRoundBegan(t *testing.T) {
	t0 := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	it := factory.Item{ID: 7, Repo: "acme/api", State: factory.StateRunning,
		Stages: []factory.Stage{{Name: "review", On: true}, {Name: "test", On: true}, {Name: "proof", On: true}},
		Stream: &factory.Stream{Cur: 1, Started: t0, Phases: []factory.Phase{
			{Name: "review", Kind: factory.StageChat, State: factory.PhaseDone, Round: 2, Chat: "review-2",
				Started: t0.Add(5 * time.Minute), At: t0.Add(9 * time.Minute),
				Rounds: []factory.RoundLine{
					{Round: 1, Summary: "2 findings", Started: t0, At: t0.Add(4 * time.Minute), Chat: "review-1"},
					{Round: 2, Summary: "clean", Started: t0.Add(5 * time.Minute), At: t0.Add(9 * time.Minute), Chat: "review-2"},
				}},
			{Name: "test", Kind: factory.StageCheck, State: factory.PhaseRunning, Round: 1, Started: t0.Add(10 * time.Minute)},
			{Name: "proof", Kind: factory.StageChat, State: factory.PhasePending},
		}}}
	raw, err := json.Marshal(FromItem(it))
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Steps []map[string]any `json:"steps"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Steps) != 3 {
		t.Fatalf("steps %s", raw)
	}
	stamp := func(d time.Duration) string { return t0.Add(d).Format(time.RFC3339) }
	review, test, proof := wire.Steps[0], wire.Steps[1], wire.Steps[2]
	if review["started_at"] != stamp(5*time.Minute) || review["at"] != stamp(9*time.Minute) {
		t.Fatalf("the finished step reads %v", review)
	}
	rounds, _ := review["rounds"].([]any)
	if len(rounds) != 2 {
		t.Fatalf("the finished step's rounds read %v", review["rounds"])
	}
	first, _ := rounds[0].(map[string]any)
	if first["started_at"] != stamp(0) || first["at"] != stamp(4*time.Minute) || first["chat"] != "review-1" {
		t.Fatalf("the earlier round reads %v", first)
	}
	if test["started_at"] != stamp(10*time.Minute) {
		t.Fatalf("the running step reads %v", test)
	}
	if _, ok := test["at"]; ok {
		t.Fatalf("a running step says it ended: %v", test)
	}
	if _, ok := proof["started_at"]; ok {
		t.Fatalf("a pending step says it began: %v", proof)
	}

	// And the job read back is the job: a worker resumes from this.
	var job Job
	if err := json.Unmarshal(raw, &job); err != nil {
		t.Fatal(err)
	}
	back := ToItem(job).Stream.Phases
	if !back[0].Started.Equal(it.Stream.Phases[0].Started) || !back[1].Started.Equal(it.Stream.Phases[1].Started) || !back[2].Started.IsZero() {
		t.Fatalf("starts read back as %v, %v, %v", back[0].Started, back[1].Started, back[2].Started)
	}
	for i, r := range back[0].Rounds {
		want := it.Stream.Phases[0].Rounds[i]
		if r.Round != want.Round || !r.Started.Equal(want.Started) || !r.At.Equal(want.At) || r.Chat != want.Chat || r.Summary != want.Summary {
			t.Fatalf("round %d read back as %+v, want %+v", i+1, r, want)
		}
	}
}

// A JOB RECORDED BEFORE STEP TIMES WERE KEPT STILL READS: its steps and rounds
// simply have no start and no round chat, and the read says nothing about
// either rather than a zero time.
func TestAJobFromBeforeStepTimesStillReads(t *testing.T) {
	old := `{"document_id":7,"repo":"acme/api","number":0,"kind":"","title":"t","state":"running","gate":"","cost":0,"created_at":"0001-01-01T00:00:00Z","pr":null,"can":{},
		"steps":[{"name":"review","state":"done","round":2,"at":"2026-10-19T09:09:00Z","in_pipeline":true,"in_run":true,
			"rounds":[{"round":1,"summary":"2 findings","at":"2026-10-19T09:04:00Z"},{"round":2,"summary":"clean","at":"2026-10-19T09:09:00Z"}]}],
		"desk":{"changed_at":"0001-01-01T00:00:00Z","heard":"0001-01-01T00:00:00Z","triage":{},"comments":null,"run":{"started":"2026-10-19T09:00:00Z","ended":"0001-01-01T00:00:00Z"}}}`
	var job Job
	if err := json.Unmarshal([]byte(old), &job); err != nil {
		t.Fatal(err)
	}
	it := ToItem(job)
	if it.Stream == nil || len(it.Stream.Phases) != 1 {
		t.Fatalf("the old job read as %+v", it.Stream)
	}
	ph := it.Stream.Phases[0]
	if !ph.Started.IsZero() || len(ph.Rounds) != 2 || !ph.Rounds[0].Started.IsZero() || ph.Rounds[0].Chat != "" || ph.Rounds[1].Summary != "clean" {
		t.Fatalf("the old step read as %+v", ph)
	}
	raw, err := json.Marshal(FromItem(it))
	if err != nil {
		t.Fatal(err)
	}
	var again struct {
		Steps []struct {
			StartedAt *string          `json:"started_at"`
			Rounds    []map[string]any `json:"rounds"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(raw, &again); err != nil {
		t.Fatal(err)
	}
	if len(again.Steps) != 1 || again.Steps[0].StartedAt != nil || len(again.Steps[0].Rounds) != 2 {
		t.Fatalf("the old job reads again as %s", raw)
	}
	for _, r := range again.Steps[0].Rounds {
		if _, ok := r["started_at"]; ok {
			t.Fatalf("an old round says it began: %s", raw)
		}
		if _, ok := r["chat"]; ok {
			t.Fatalf("an old round names a chat: %s", raw)
		}
	}
}

// A WORKER'S REPORT CARRIES WHEN ITS ROUND BEGAN, on its own clock, and each
// finished round's start and chat, through the wire and back unchanged.
func TestAWorkerReportCarriesWhenItsRoundBeganAndItsChat(t *testing.T) {
	t0 := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	report := factory.RunReport{Stages: []factory.StageReport{{
		Name: "review", State: factory.PhaseRunning, Round: 2, Started: t0.Add(5 * time.Minute),
		Rounds: []factory.RoundLine{{Round: 1, Summary: "2 findings", Started: t0, At: t0.Add(4 * time.Minute), Chat: "/home/w/review-1.jsonl"}},
	}, {
		Name: "test", State: factory.PhasePending,
	}}}
	raw, err := json.Marshal(FromRunReport(report))
	if err != nil {
		t.Fatal(err)
	}
	var wire WorkerReport
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	back := ToWorkerReport(wire)
	if len(back.Stages) != 2 {
		t.Fatalf("stages %+v", back.Stages)
	}
	got, want := back.Stages[0], report.Stages[0]
	if !got.Started.Equal(want.Started) || len(got.Rounds) != 1 {
		t.Fatalf("the running step came back as %+v", got)
	}
	if r, w := got.Rounds[0], want.Rounds[0]; r.Round != w.Round || r.Summary != w.Summary || !r.Started.Equal(w.Started) || !r.At.Equal(w.At) || r.Chat != w.Chat {
		t.Fatalf("its finished round came back as %+v, want %+v", r, w)
	}
	if !back.Stages[1].Started.IsZero() {
		t.Fatalf("a step that has not begun came back begun: %+v", back.Stages[1])
	}
}
