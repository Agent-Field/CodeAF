package serve_test

import (
	"fmt"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

// THE WORKER THAT TAKES A JOB ON IS HANDED EVERY EARLIER STEP'S WHOLE REPORT,
// and nobody else is. A job whose plan already reported (on another machine,
// before its worker went away) is pulled: the plan step carries its done, its
// notes, its claims and its chat, so the runner there briefs write with all of
// it. The same job read as a reader has none of it, its chat path least of all.
func TestThePullCarriesEachStepsWholeReportAndAReaderNone(t *testing.T) {
	f := workerSetup(t)
	report := &factory.StageResult{Done: true, Findings: 1, Output: "the plan: add RefundOnce", Notes: []string{"keep the old flag"}, Chat: "/home/sam/.codeaf/plan.jsonl",
		Claims: []factory.Claim{{Text: "the plan adds one function", OK: true, Evidence: "files: ledger.go", Medium: "transcript"}}}
	it, err := f.st.Create(factory.Item{Title: "fix the job", Repo: "owner/repo", State: factory.StateQueued, Assignee: "sam",
		Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}, {Name: "write", Kind: factory.StageChat, On: true}},
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{
			{Name: "plan", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1, Result: report},
			{Name: "write", Kind: factory.StageChat, State: factory.PhasePending},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	pulled := f.pull(t, f.attach(t, "sam"))
	if len(pulled.Job.Steps) < 2 {
		t.Fatalf("the pull's steps are %+v", pulled.Job.Steps)
	}
	got := pulled.Job.Steps[0].Report
	if got == nil || !got.Done || got.Findings != 1 || got.Output != report.Output || got.Chat != report.Chat ||
		len(got.Notes) != 1 || got.Notes[0] != "keep the old flag" || len(got.Claims) != 1 || !got.Claims[0].Proven {
		t.Fatalf("the plan step's report on the pull is %+v", got)
	}
	if pulled.Job.Steps[0].Result != report.Output {
		t.Fatalf("the pull no longer carries the result line an older worker reads: %q", pulled.Job.Steps[0].Result)
	}
	if pulled.Job.Steps[1].Report != nil {
		t.Fatalf("a step that has not run carries a report: %+v", pulled.Job.Steps[1].Report)
	}

	var job api.Job
	f.request(t, "GET", fmt.Sprintf("/v1/jobs/%d", it.ID), "", nil, 200, &job)
	for _, s := range job.Steps {
		if s.Report != nil {
			t.Fatalf("a reader's job carries %s's report: %+v", s.Name, s.Report)
		}
	}
	var queue api.Queue
	f.request(t, "GET", "/v1/queue", "", nil, 200, &queue)
	for _, j := range queue.Jobs {
		for _, s := range j.Steps {
			if s.Report != nil {
				t.Fatalf("the queue carries %s's report: %+v", s.Name, s.Report)
			}
		}
	}
}

func TestWorkerRoundTripKeepsFloorReconciliationFields(t *testing.T) {
	f := workerSetup(t)
	job := f.add(t, "sam")
	if err := f.st.Update(job.ID, func(it *factory.Item) error { it.GitHubIssueID = 1001; factory.TouchIssue(it); return nil }); err != nil {
		t.Fatal(err)
	}
	worker := f.attach(t, "sam")
	pulled := f.pull(t, worker)
	before, local := api.ToItem(pulled.Job), api.ToItem(pulled.Job)
	// These service-owned fields are deliberately absent from the worker's copy.
	if local.GitHubIssueID != 0 || local.SourceClosed || local.PersonTouched {
		t.Fatal("worker unexpectedly owns reconciliation fields")
	}
	local.Stream = &factory.Stream{Spent: .01}
	report, moved := run.Diff(before, local)
	if !moved {
		t.Fatal("worker spending did not produce a report")
	}
	endpoint := "/v1/workers/leases/" + pulled.Lease.ID + "/report"
	f.request(t, "POST", endpoint, worker.Credential, api.FromRunReport(report), 200, nil)
	floor, err := f.st.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if floor.GitHubIssueID != 1001 || !floor.PersonTouched || floor.SourceClosed {
		t.Fatalf("worker round trip changed floor reconciliation fields: id=%d touched=%t closed=%t", floor.GitHubIssueID, floor.PersonTouched, floor.SourceClosed)
	}
}
