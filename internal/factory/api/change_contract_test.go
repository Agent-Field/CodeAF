package api

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestChangeFactsAreAdditiveContractOne(t *testing.T) {
	if Contract != 2 {
		t.Fatalf("contract = %d", Contract)
	}
	change := &factory.Change{Branch: "factory/issue-7", Files: []factory.ChangedFile{{Path: "fix.go", Added: 2, Removed: 1, Status: "modified"}}, Stat: &factory.ChangeStat{Files: 1, Added: 2, Removed: 1}, Tests: []factory.StepTests{{Step: "test", TestCounts: factory.TestCounts{Passed: 4, Failed: 1, Skipped: 2}}}}
	it := factory.Item{ID: 7, Kind: factory.KindIssue, State: factory.StateRunning, Change: change, ChangeBase: "base-sha", PullRequest: &factory.PullRequest{Number: 8, URL: "https://github.com/acme/api/pull/8", Draft: true}}
	raw, err := json.Marshal(FromItem(it))
	if err != nil {
		t.Fatal(err)
	}
	var job Job
	if err := json.Unmarshal(raw, &job); err != nil {
		t.Fatal(err)
	}
	back := ToItem(job)
	if !reflect.DeepEqual(back.Change, change) || back.ChangeBase != it.ChangeBase || !back.PullRequest.Draft {
		t.Fatalf("lost change facts: %+v", back)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	var changeFields map[string]json.RawMessage
	if err := json.Unmarshal(fields["change"], &changeFields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"files", "stat", "branch", "tests"} {
		if len(changeFields[key]) == 0 {
			t.Fatalf("missing change.%s: %s", key, raw)
		}
	}
	var oldClient struct {
		ID    int    `json:"id"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(raw, &oldClient); err != nil || oldClient.ID != it.ID || oldClient.State != StateRunning {
		t.Fatalf("old contract reader = %+v, %v", oldClient, err)
	}
	report := factory.RunReport{Change: change, ChangeBase: "base-sha", PR: it.PullRequest, Stages: []factory.StageReport{{Name: "test", Result: &factory.StageResult{Tests: &factory.TestCounts{Passed: 4}}}}}
	raw, err = json.Marshal(FromRunReport(report))
	if err != nil {
		t.Fatal(err)
	}
	var worker WorkerReport
	if err := json.Unmarshal(raw, &worker); err != nil {
		t.Fatal(err)
	}
	if back := ToWorkerReport(worker); !reflect.DeepEqual(back, report) {
		t.Fatalf("worker lost facts: %+v", back)
	}
}

func TestChangeOmitsUnknownAndZeroFacts(t *testing.T) {
	for _, it := range []factory.Item{{}, {Change: &factory.Change{Branch: "factory/7"}}} {
		raw, _ := json.Marshal(FromItem(it))
		var job map[string]any
		if err := json.Unmarshal(raw, &job); err != nil {
			t.Fatal(err)
		}
		if it.Change == nil {
			if _, ok := job["change"]; ok {
				t.Fatalf("unknown change emitted: %s", raw)
			}
		} else {
			change := job["change"].(map[string]any)
			if len(change) != 1 || change["branch"] != "factory/7" {
				t.Fatalf("empty facts emitted: %s", raw)
			}
		}
		if _, ok := job["draft"]; ok {
			t.Fatalf("issue has unknown PR draft: %s", raw)
		}
	}
	for _, draft := range []bool{false, true} {
		job := FromItem(factory.Item{Kind: factory.KindPR, Draft: draft})
		if job.Draft == nil || *job.Draft != draft || ToItem(job).Draft != draft {
			t.Fatalf("source PR draft lost: %+v", job)
		}
	}
}
