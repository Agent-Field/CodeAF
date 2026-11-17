package api_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
)

// oldFailure is api.Failure as a factory older than the one-line summary
// spells it. serve.decode sets DisallowUnknownFields, so a "summary" key in a
// worker's report is a whole report that factory refuses.
type oldFailure struct {
	Rounds   int       `json:"rounds,omitempty"`
	Runner   string    `json:"runner,omitempty"`
	Step     string    `json:"step,omitempty"`
	Code     string    `json:"code"`
	Detail   string    `json:"detail,omitempty"`
	Attempts int       `json:"attempts,omitempty"`
	Machine  string    `json:"machine,omitempty"`
	Next     string    `json:"next"`
	At       time.Time `json:"at"`
}

type oldFailedStepReport struct {
	Name    string      `json:"name"`
	State   string      `json:"state,omitempty"`
	Round   int         `json:"round,omitempty"`
	Failure *oldFailure `json:"failure,omitempty"`
}

// failedWithSummary is a worker runner's report after its manager wrote the
// failure's headline onto the step's failure (factory.SetFailureSummary).
func failedWithSummary() factory.RunReport {
	at := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	failure := &factory.Failure{Step: "proof", Code: "step_failed", Detail: "nine of ten checks passed; the last found no pull request", Next: "run_again", At: at, Summary: "9 of 10 checks passed · failed: no pull request when checked"}
	return factory.RunReport{
		Stages:   []factory.StageReport{{Name: "proof", Round: 1, State: factory.PhaseFailed, Failure: failure}},
		Pipeline: &factory.PipelineChange{Stages: []factory.Stage{{Name: "proof"}}, Phases: []factory.Phase{{Name: "proof", State: factory.PhaseFailed, Failure: failure}}},
	}
}

func TestANewWorkersFailedReportIsAcceptedByAnOlderFactory(t *testing.T) {
	// What an older factory's pulled job admits: nothing beyond the established body.
	var olderJob api.Job
	report := failedWithSummary()
	request, err := client.PrepareWorkerReport(api.FromRunReport(report, olderJob.ReportAdmits()), "compat-test")
	if err != nil {
		t.Fatal(err)
	}
	var old struct {
		Steps    []oldFailedStepReport `json:"steps,omitempty"`
		Pipeline []json.RawMessage     `json:"pipeline,omitempty"`
	}
	decoder := json.NewDecoder(bytes.NewReader(request.Body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&old); err != nil {
		t.Fatalf("an older factory refused the new worker's failed report: %v\n%s", err, request.Body)
	}
	if len(old.Steps) != 1 || old.Steps[0].Failure == nil || old.Steps[0].Failure.Detail != report.Stages[0].Failure.Detail {
		t.Fatalf("the failure itself must still cross whole: %+v", old.Steps)
	}
	for _, raw := range old.Pipeline {
		var step struct {
			Failure *json.RawMessage `json:"failure"`
		}
		if err := json.Unmarshal(raw, &step); err != nil {
			t.Fatal(err)
		}
		if step.Failure == nil {
			t.Fatalf("the pipeline step lost its failure: %s", raw)
		}
		var f oldFailure
		strict := json.NewDecoder(bytes.NewReader(*step.Failure))
		strict.DisallowUnknownFields()
		if err := strict.Decode(&f); err != nil {
			t.Fatalf("an older factory refused the pipeline step's failure: %v", err)
		}
	}
	if report.Stages[0].Failure.Summary == "" || report.Pipeline.Phases[0].Failure.Summary == "" {
		t.Fatal("leaving the summary off the wire must not change the runner's own failure")
	}
}

func TestAFactoryThatAdmitsSummariesReceivesThem(t *testing.T) {
	job := api.WorkerJob(factory.Item{})
	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if got, _ := body["failure_summaries"].(bool); !got {
		t.Fatalf("worker admission does not advertise failure_summaries: %s", raw)
	}
	if reader, _ := json.Marshal(api.FromItem(factory.Item{})); bytes.Contains(reader, []byte("failure_summaries")) {
		t.Fatalf("a reader's job advertises a worker admission: %s", reader)
	}
	want := failedWithSummary().Stages[0].Failure.Summary
	got := api.ToWorkerReport(api.FromRunReport(failedWithSummary(), job.ReportAdmits()))
	if len(got.Stages) != 1 || got.Stages[0].Failure == nil || got.Stages[0].Failure.Summary != want {
		t.Fatalf("an admitting factory lost the step failure's summary: %+v", got.Stages)
	}
	if got.Pipeline == nil || len(got.Pipeline.Phases) != 1 || got.Pipeline.Phases[0].Failure == nil || got.Pipeline.Phases[0].Failure.Summary != want {
		t.Fatalf("an admitting factory lost the pipeline failure's summary: %+v", got.Pipeline)
	}
}
