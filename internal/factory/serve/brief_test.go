package serve_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
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

func (f *v2Jobs) briefPost(t *testing.T, it factory.Item, verb, body string, status int, out any) {
	t.Helper()
	w := f.post(t, it, "brief/"+verb, body, status)
	if out != nil {
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatal(err)
		}
	}
}

// A WRITE OF THE BRIEF, THROUGH THE FACTORY: begin hands the writer its
// prompt and the job's input and marks the brief generating; the same
// inputs begin nothing; keep checks the draft against the job, keeps what
// passed, says each drop, and books the call's cost to the job.
func TestJobBriefBeginAndKeep(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateRunning, Assignee: "sam", Title: "Fix retries", Stream: &factory.Stream{Spent: 1, Phases: []factory.Phase{
		{Name: "test", Kind: factory.StageCheck, State: factory.PhaseDone, Round: 1, Result: &factory.StageResult{Done: true, Tests: &factory.TestCounts{Passed: 3}}},
	}}})
	var begin api.BriefBegin
	f.briefPost(t, it, "begin", `{"trigger":"step"}`, 200, &begin)
	if begin.Skip != "" || begin.Hash == "" || !strings.Contains(begin.System, "ONE JSON object") || !strings.Contains(begin.Input, `"title":"Fix retries"`) {
		t.Fatalf("begin = %+v", begin)
	}
	if b := f.brief(t, it, "", 200); b.Status != factory.BriefStatusGenerating {
		t.Fatalf("status = %s", b.Status)
	}
	draft := `{"verdict":"Fixed; 3 tests pass.","verdict_evidence":[{"step":"test","try":1,"kind":"tests"}],"blocks":[{"kind":"bullets","heading":"What happened","bullets":[{"text":"4 tests pass","evidence":[{"step":"test","try":1,"kind":"tests"}]},{"text":"a cause","evidence":[{"step":"test","try":1,"kind":"tests"}]}]}]}`
	keep, _ := json.Marshal(api.BriefKeepRequest{Hash: begin.Hash, Model: "deepseek/deepseek-v4.1-flash", CostUSD: 0.002, Draft: draft})
	var kept api.BriefKept
	f.briefPost(t, it, "keep", string(keep), 200, &kept)
	if !kept.Kept || kept.Status != factory.BriefStatusReady || kept.Brief.Rev != 1 || kept.Brief.Model != "deepseek/deepseek-v4.1-flash" ||
		len(kept.Brief.Blocks) != 1 || len(kept.Brief.Blocks[0].Bullets) != 1 || len(kept.Drops) != 1 || kept.Brief.Facts[0].Value != "3 passed" {
		t.Fatalf("kept = %+v", kept)
	}
	if cur, _ := f.st.Get(it.ID); cur.Stream.Spent != 1.002 {
		t.Fatalf("job spend = %v", cur.Stream.Spent)
	}
	begin = api.BriefBegin{}
	f.briefPost(t, it, "begin", `{"trigger":"ask"}`, 200, &begin)
	if begin.Skip != factory.BriefSkipUnchanged || begin.Input != "" {
		t.Fatalf("same inputs = %+v", begin)
	}
	f.briefPost(t, it, "begin", `{"trigger":"retry"}`, 200, &begin)
	kept = api.BriefKept{}
	f.briefPost(t, it, "keep", `{"hash":"x","failure":"model_failed"}`, 200, &kept)
	if kept.Kept || kept.Status != factory.BriefStatusFailed || kept.Failure != "model_failed" || kept.Brief.Rev != 1 {
		t.Fatalf("failed write = %+v", kept)
	}
	f.briefPost(t, it, "begin", `{"trigger":"whenever"}`, 422, nil)
	f.briefPost(t, it, "keep", `{"hash":"x"}`, 422, nil)
	f.person = "mei"
	f.briefPost(t, it, "begin", `{"trigger":"retry"}`, 403, nil)
}

// A SPOT OF THE BRIEF OPENS ON ITS HUNKS from the job's change, with the
// writer's notes; a spot the newest version lacks is not found.
func TestJobBriefSpotReadsItsHunks(t *testing.T) {
	f := newV2Jobs(t)
	it := factory.Item{State: factory.StateRunning, Assignee: "sam", Change: &factory.Change{
		Patch: "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,2 +1,3 @@ func Send() {\n x\n+y\n",
	}}
	it.KeepBrief(factory.BriefVersion{Verdict: "Fixed.", Blocks: []factory.BriefBlock{{Kind: factory.BlockWhereToLook, Heading: "Where to look", Rest: "nothing else",
		Spots: []factory.BriefSpot{{ID: "1", File: "a.go", Symbol: "Send", Lines: 1, Why: "the send", Notes: []factory.BriefNote{{Hunk: "a.go:1", Text: "adds y"}}}}}}})
	it = f.create(t, it)
	w := verbRequest(f.h, "GET", fmt.Sprintf("/v1/jobs/%d/brief/spots/1", it.ID), "")
	checkJSON(t, w, 200)
	var got api.JobBriefSpot
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Rev != 1 || got.File != "a.go" || len(got.Hunks) != 1 || got.Hunks[0].Added != 1 || len(got.Annotations) != 1 || got.Annotations[0].Text != "adds y" {
		t.Fatalf("spot = %+v", got)
	}
	checkJSON(t, verbRequest(f.h, "GET", fmt.Sprintf("/v1/jobs/%d/brief/spots/2", it.ID), ""), 404)
}
