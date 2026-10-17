package api

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/factory"
	"os"
	"testing"
)

func TestJobContractTwoUsesCanvasReferenceAndKeepsDocumentIdentity(t *testing.T) {
	if Contract != 2 {
		t.Fatalf("contract %d", Contract)
	}
	it := factory.Item{ID: 42, Repo: "acme/api", Num: 1498, State: factory.StateShipped, PullRequest: &factory.PullRequest{State: factory.PROpen}, Labels: []string{"P1", "size:XS"}, Cap: 3}
	raw, err := json.Marshal(FromItem(it))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["id"] != "api#1498" || fields["document_id"] != float64(42) || fields["state"] != "running" || fields["reason"] != "github" || fields["size"] != "XS" || fields["budget"] != float64(3) {
		t.Fatalf("job wire %s", raw)
	}
	var j Job
	if err := json.Unmarshal(raw, &j); err != nil {
		t.Fatal(err)
	}
	if back := ToItem(j); back.ID != 42 || back.State != factory.StateShipped {
		t.Fatalf("compatibility %+v", back)
	}
}

func TestV2JobFixtureNamesPersonBudgetAndMatchedStartApproval(t *testing.T) {
	raw, err := os.ReadFile("testdata/job-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var job Job
	if err := json.Unmarshal(raw, &job); err != nil {
		t.Fatal(err)
	}
	if job.JobID != "api#1498" || job.ID != 42 || job.Priority.SetBy != "sam" || job.Size != "M" || job.Budget != 3 || job.Approval.Kind != "start" || job.Approval.Matched != "touches auth/** · auth/token.go" {
		t.Fatalf("v2 fixture %+v", job)
	}
}

func TestJobProjectionInventsNoPriorityFromABugLabel(t *testing.T) {
	bug := FromItem(factory.Item{ID: 1, Repo: "acme/api", Num: 1, Labels: []string{"bug"}, Triage: factory.Triage{Type: "bug", Priority: 1}})
	if bug.Priority != nil {
		t.Fatalf("bug label invented %+v", bug.Priority)
	}
	labelled := FromItem(factory.Item{ID: 2, Repo: "acme/api", Num: 2, Labels: []string{"bug", "p1"}})
	if labelled.Priority == nil || labelled.Priority.Value != "high" {
		t.Fatalf("explicit label %+v", labelled.Priority)
	}
	marked := FromItem(factory.Item{ID: 3, Repo: "acme/api", Num: 3, Labels: []string{"bug", "priority:urgent"}, Priority: &factory.JobPriority{Value: "low", SetBy: "sam"}})
	if marked.Priority == nil || marked.Priority.Value != "low" || marked.Priority.SetBy != "sam" {
		t.Fatalf("human mark %+v", marked.Priority)
	}
}
