package run_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

func TestBelowCapReportKeepsNonCapQuestions(t *testing.T) {
	for _, kind := range []string{"", "scope"} {
		t.Run("kind="+kind, func(t *testing.T) {
			it := factory.Item{State: factory.StateRunning, Cap: 10, SpentBefore: .5,
				Stream: &factory.Stream{Spent: 5, Phases: []factory.Phase{{Name: "proof", State: factory.PhaseRunning}}}}
			err := run.ApplyReport(&it, factory.RunReport{Cost: .25, QuestionKind: kind,
				Question: &factory.Asked{Stage: "proof", Question: "Which API should this use?"}}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			wantKind := kind
			if wantKind == "" {
				wantKind = "step"
			}
			if it.State != factory.StateNeedsYou || it.QKind != wantKind || it.Question != "Which API should this use?" || it.Asking == nil || it.Asking.Stage != "proof" || it.Stream.Phases[0].State != factory.PhaseWaiting {
				t.Fatalf("non-cap question lost below cap: state=%s kind=%q question=%q asking=%+v phase=%+v", it.State, it.QKind, it.Question, it.Asking, it.Stream.Phases[0])
			}
		})
	}
}

func TestDroppedCapQuestionKeepsReportContents(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	it := factory.Item{State: factory.StateRunning, Cap: 10, SpentBefore: .5, Notes: []string{"earlier note"},
		Stream: &factory.Stream{Spent: 5, Phases: []factory.Phase{{Name: "proof", State: factory.PhaseRunning}}}}
	line := factory.LogLine{At: now, Tone: "said", Text: "late proof completed"}
	claim := factory.Claim{Text: "API is covered", OK: true, Medium: "test", Evidence: "go test ./api"}
	report := factory.RunReport{Cost: .25, QuestionKind: "cap", Question: &factory.Asked{Stage: "proof", Question: "budget of $5 reached"},
		Log: []factory.LogLine{line}, Notes: []string{"late note"}, Claims: []factory.Claim{claim}}
	if err := run.ApplyReport(&it, report, now); err != nil {
		t.Fatal(err)
	}
	if it.State != factory.StateRunning || it.Question != "" || it.QKind != "" || it.Asking != nil || it.Stream.Phases[0].State != factory.PhaseRunning {
		t.Fatalf("dropped question changed running job: state=%s kind=%q question=%q asking=%+v phase=%+v", it.State, it.QKind, it.Question, it.Asking, it.Stream.Phases[0])
	}
	if it.SpentBefore != .5 || it.Stream.Spent != 5.25 || !reflect.DeepEqual(it.Stream.Log, []factory.LogLine{line}) || !reflect.DeepEqual(it.Notes, []string{"earlier note", "late note"}) || !reflect.DeepEqual(it.Proof, []factory.Claim{claim}) {
		t.Fatalf("dropped question lost report contents: spentBefore=%g stream=%+v notes=%q proof=%+v", it.SpentBefore, it.Stream, it.Notes, it.Proof)
	}
}
