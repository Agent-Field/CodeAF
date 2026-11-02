package serve_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func (f *v2Jobs) brief(t *testing.T, it factory.Item, query string, status int) api.JobBrief {
	t.Helper()
	w := verbRequest(f.h, "GET", fmt.Sprintf("/v1/jobs/%d/brief%s", it.ID, query), "")
	checkJSON(t, w, status)
	var b api.JobBrief
	if status == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
			t.Fatal(err)
		}
	}
	return b
}

// A JOB WITH NO BRIEF YET IS ANSWERED ITS STUB, made from the engine's data:
// the issue's line, the steps done of total and the facts its steps reported.
func TestJobBriefStubBeforeAnyVersion(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateRunning, Assignee: "sam", Title: "Retries double-send", Stream: &factory.Stream{Phases: []factory.Phase{
		{Name: "test", Kind: factory.StageCheck, State: factory.PhaseDone, Result: &factory.StageResult{Done: true, Tests: &factory.TestCounts{Passed: 3}}},
		{Name: "proof", Kind: factory.StageChat, State: factory.PhaseRunning},
	}}})
	b := f.brief(t, it, "", 200)
	if b.Job != it.ID || b.Status != factory.BriefStatusStub || !b.Brief.Stub || b.Brief.Issue != "Retries double-send" ||
		b.Brief.StepsDone != 1 || b.Brief.StepsTotal != 2 || len(b.Brief.Facts) != 1 || b.Brief.Facts[0].Value != "3 passed" ||
		b.Brief.Blocks == nil || b.ChangedSinceSeen == nil || b.Factory.Name == "" {
		t.Fatalf("stub brief = %+v", b)
	}
	f.brief(t, factory.Item{ID: 999}, "", 404)
	f.brief(t, it, "?since=x", 422)
}

// A KEPT VERSION IS ANSWERED WITH WHAT CHANGED since the rev the caller
// names, or since the version before when it names none; a write on its way
// or failed says so over the version that is there.
func TestJobBriefLatestVersionAndChangedSince(t *testing.T) {
	f := newV2Jobs(t)
	it := factory.Item{State: factory.StateRunning, Assignee: "sam"}
	it.KeepBrief(factory.BriefVersion{Verdict: "Started.", Model: "m", CostUSD: 0.01})
	it.KeepBrief(factory.BriefVersion{Verdict: "Fixed.", Facts: []factory.BriefFact{{Key: "test", Label: "Test", Value: "3 passed", OK: true}}})
	it.BriefWriting = &factory.BriefWriting{State: factory.BriefFailed, AfterStep: "proof", Failure: "the model answered no JSON"}
	it = f.create(t, it)

	b := f.brief(t, it, "", 200)
	if b.Status != factory.BriefStatusFailed || b.Failure != "the model answered no JSON" || b.FailedAfter != "proof" ||
		b.Brief.Rev != 2 || b.Brief.Verdict != "Fixed." || !slices.Equal(b.ChangedSinceSeen, []string{"verdict", "test"}) {
		t.Fatalf("brief = %+v", b)
	}
	if b := f.brief(t, it, "?since=2", 200); len(b.ChangedSinceSeen) != 0 || b.Since != 2 {
		t.Fatalf("seen latest = %+v", b)
	}
}

// THE JOB'S READ SAYS WHICH BRIEF IT HAS, so a page that polls the job reads
// the brief only when its rev or status moves; a job with only its stub
// says nothing.
func TestJobReadCarriesTheBriefMark(t *testing.T) {
	f := newV2Jobs(t)
	stub := f.create(t, factory.Item{State: factory.StateRunning, Assignee: "sam"})
	if j := f.read(t, stub); j.Brief != nil {
		t.Fatalf("stub job mark = %+v", j.Brief)
	}
	it := factory.Item{State: factory.StateRunning, Assignee: "sam"}
	it.KeepBrief(factory.BriefVersion{Verdict: "Fixed."})
	it.BriefWriting = &factory.BriefWriting{State: factory.BriefGenerating}
	it = f.create(t, it)
	if j := f.read(t, it); j.Brief == nil || *j.Brief != (api.BriefMark{Rev: 1, Status: factory.BriefStatusGenerating}) {
		t.Fatalf("mark = %+v", j.Brief)
	}
}
