package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

func reopenJobs(t *testing.T, f *v2Jobs) {
	t.Helper()
	f.h.Close()
	f.h = serve.New(f.seam, f.cfg)
}

func recordedRun() *factory.Stream {
	return &factory.Stream{Cur: 1, Spent: 1.25, Started: now, Phases: []factory.Phase{
		{Name: "plan", Kind: factory.StageChat, State: factory.PhaseDone, Result: &factory.StageResult{Output: "keep this plan", Done: true}},
		{Name: "write", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 2, Chat: "old-chat"},
		{Name: "proof", Kind: factory.StageChat, State: factory.PhasePending},
	}}
}

// A STOPPED JOB SAYS SO: the job's Stop leaves the same fact a step's Stop
// does, so it never reads as a fresh backlog job with a step still running.
// Undo puts back the run exactly as it stood, and a start takes the stop down
// and begins the run again at its first step with nothing spent.
func TestJobScreensAStoppedJobSaysSoUntilUndoOrStart(t *testing.T) {
	f := newV2Jobs(t)
	stages := []factory.Stage{
		{Name: "plan", Kind: factory.StageChat, Ask: "plan it", On: true},
		{Name: "write", Kind: factory.StageChat, Ask: "write it", On: true},
		{Name: "proof", Kind: factory.StageChat, Ask: "prove it", On: true},
	}
	it := f.create(t, factory.Item{State: factory.StateRunning, Assignee: "sam", Stages: stages, Stream: recordedRun()})
	f.post(t, it, "stop", "{}", 200)
	j := f.read(t, it)
	if j.State != "needs_you" || j.Failure == nil || j.Failure.Code != factory.CodeStopped || j.Failure.Next != factory.FailureStopped || j.Failure.Step != "write" || j.Failure.Detail != "Stopped by you" {
		t.Fatalf("the stopped job does not say so: %s %+v", j.State, j.Failure)
	}
	if j.StatePresentation.Kind != "failed" || j.StatePresentation.Title != "Step stopped" || j.Cost != 1.25 {
		t.Fatalf("stopped presentation: %+v cost %v", j.StatePresentation, j.Cost)
	}
	// NOTHING IS LEFT TO STOP, and the stop is the person's to take back.
	if j.Can.Stop || j.Controls.StopStep || j.Can.Run || !j.Can.RunAgain || !j.Can.Undo {
		t.Fatalf("stopped doors: %+v %+v", j.Can, j.Controls)
	}
	for _, s := range j.Steps {
		if s.State == api.StepRunning || s.State == api.StepWaiting {
			t.Fatalf("step %s still reads %s", s.Name, s.State)
		}
	}
	if s := j.Steps[1]; s.State != api.StepFailed || s.Failure == nil || s.Failure.Code != factory.CodeStopped {
		t.Fatalf("the cut step = %+v", s)
	}
	f.post(t, it, "stop", "{}", 409)

	f.post(t, it, "undo", "{}", 200)
	cur, _ := f.st.Get(it.ID)
	if cur.State != factory.StateQueued || cur.Failure != nil || cur.Stream.Phases[1].State != factory.PhaseRunning || cur.Stream.Phases[1].Failure != nil || cur.Stream.Phases[1].Chat != "old-chat" || !cur.Stream.Ended.IsZero() || cur.Stream.Spent != 1.25 {
		t.Fatalf("undo did not put the run back: %s %+v %+v", cur.State, cur.Failure, cur.Stream)
	}

	f.post(t, it, "stop", "{}", 200)
	f.clock.Add(int64(factory.ApprovalUndo))
	f.post(t, it, "run_again", "{}", 200)
	j = f.read(t, it)
	if j.State != "queued" || j.Failure != nil || j.Cost != 0 || len(j.Steps) != 3 || j.StatePresentation.Title != "Waiting to start" {
		t.Fatalf("start after the stop: %s %+v cost %v", j.State, j.Failure, j.Cost)
	}
	for _, s := range j.Steps {
		if s.State != api.StepPending || s.Failure != nil {
			t.Fatalf("the started run kept the stopped step: %+v", s)
		}
	}
}

func TestJobScreensStopRerunKeepsEarlierWorkAndRevokesLease(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateQueued, Assignee: "sam", Stream: recordedRun()})
	desk, err := lease.Open(f.st, f.cfg.LeaseOptions)
	if err != nil {
		t.Fatal(err)
	}
	worker := lease.Worker{ID: "worker", Machine: "laptop", Handle: "sam", Team: true, Slots: 1}
	held, err := desk.Pull(worker)
	if err != nil {
		t.Fatal(err)
	}
	f.post(t, it, "steps/write/stop", `{"expect":{"step":"plan"}}`, 409)
	f.person = "mei"
	f.post(t, it, "steps/write/stop", "{}", 403)
	f.person = "sam"
	f.post(t, it, "steps/missing/stop", "{}", 404)
	f.post(t, it, "steps/plan/stop", "{}", 409)
	f.post(t, it, "steps/write/stop", "{}", 200)
	if _, err := desk.Heartbeat(held.Job, worker.ID); err == nil {
		t.Fatal("old lease still accepts a heartbeat")
	}
	j := f.read(t, it)
	if j.State != "needs_you" || j.StatePresentation.Title != "Step stopped" || !j.Controls.RerunStep || j.Controls.StopStep || j.Cost != 1.25 {
		t.Fatalf("stop: %+v", j)
	}
	f.post(t, it, "steps/proof/rerun", "{}", 409)
	f.post(t, it, "steps/write/rerun", "{}", 200)
	cur, _ := f.st.Get(it.ID)
	if cur.State != factory.StateQueued || cur.Stream.Phases[0].Result.Output != "keep this plan" || cur.Stream.Phases[1].Chat != "" || cur.Stream.Phases[1].Round != recordedRun().Phases[1].Round+1 || cur.Stream.Spent != 1.25 || cur.Failure != nil {
		t.Fatalf("rerun: %+v", cur)
	}
	f.post(t, it, "steps/plan/rerun", `{"words":"  use the other file  "}`, 200)
	cur, _ = f.st.Get(it.ID)
	if cur.Stream.Cur != 0 || (cur.Stream.Phases[0].State != factory.PhasePending || cur.Stream.Phases[0].Round < 1) || cur.Stream.Spent != 1.25 {
		t.Fatalf("first step rerun lost revival state: %+v", cur.Stream)
	}
	// THE WORDS ARE KEPT WHERE EVERY STEP READS THEM, and a stop takes none.
	if len(cur.Notes) != 1 || cur.Notes[0] != "use the other file" {
		t.Fatalf("rerun words = %q", cur.Notes)
	}
	f.post(t, it, "steps/plan/stop", `{"words":"why"}`, 422)
	events, err := f.rec.Since(context.Background(), time.Time{})
	if err != nil || len(events) < 3 || events[len(events)-1].Kind != record.StepRerun {
		t.Fatalf("events: %+v %v", events, err)
	}
}

func TestJobScreensEditsAndAssignmentRespectBounds(t *testing.T) {
	f := newV2Jobs(t)
	f.seam.Edit = factory.LocalSeam(f.st, now).Edit
	reopenJobs(t, f)
	it := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam", Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, Ask: "old", On: true}, {Name: "proof", Kind: factory.StageChat, Ask: "prove", Fixed: true, On: true}}})
	w := verbRequest(f.h, "GET", fmt.Sprintf("/v1/jobs/%d/step-edits", it.ID), "")
	checkJSON(t, w, 200)
	var before api.StepEdits
	if err := json.Unmarshal(w.Body.Bytes(), &before); err != nil {
		t.Fatal(err)
	}
	f.post(t, it, "step-edits", `{"skip":["proof"]}`, 409)
	f.post(t, it, "step-edits", `{"ask":{"write":"new ask"},"why":"make the scope clear"}`, 200)
	cur, _ := f.st.Get(it.ID)
	if cur.Stages[0].Ask != "new ask" || cur.Stages[0].By != factory.ByYou {
		t.Fatal(cur.Stages)
	}
	f.post(t, it, "step-edits", fmt.Sprintf(`{"ask":{"write":"stale"},"expect":{"version":%d}}`, before.Version), 409)
	f.person = "mei"
	f.post(t, it, "step-edits", `{"ask":{"write":"wrong person"}}`, 403)
	f.person = "sam"
	f.post(t, it, "assignee", `{"assignee":"@Mei"}`, 200)
	cur, _ = f.st.Get(it.ID)
	if cur.Person() != "mei" {
		t.Fatal(cur.Person())
	}
	f.post(t, it, "assignee", `{"assignee":"sam"}`, 403)
}

type jobsTransport func(*http.Request) (*http.Response, error)

func (f jobsTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestJobScreensIssuesPipelineAndSyncUseRealGitHubShapes(t *testing.T) {
	f := newV2Jobs(t)
	f.cfg.Mode = api.FactorySolo
	f.cfg.GitHubToken = func(context.Context) string { return "private-token" }
	var creates, refreshes int
	f.cfg.GitHubHTTPClient = &http.Client{Transport: jobsTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer private-token" {
			t.Fatal("wrong GitHub credential")
		}
		body, status := "", 200
		switch r.URL.Path {
		case "/repos/acme/api/issues":
			creates++
			var fields struct {
				Title     string
				Body      string
				Assignees []string
			}
			if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
				t.Fatal(err)
			}
			if r.Method != "POST" || fields.Title != "Fix it" || fields.Body != "the brief" || len(fields.Assignees) != 1 || fields.Assignees[0] != "sam" {
				t.Fatalf("GitHub issue body: %+v", fields)
			}
			status = 201
			body = fmt.Sprintf(`{"number":%d,"title":"Fix it","html_url":"https://github.com/acme/api/issues/%d"}`, creates+10, creates+10)
		case "/search/issues":
			if r.URL.Query().Get("q") != "repo:acme/api is:issue is:open fix repo:private/hidden" {
				t.Fatal(r.URL.Query())
			}
			body = `{"items":[{"number":11,"title":"Fix it","html_url":"https://github.com/acme/api/issues/11","repository_url":"https://api.github.com/repos/acme/api"},{"number":7,"title":"Secret","repository_url":"https://api.github.com/repos/private/hidden"}]}`
		default:
			t.Fatalf("unexpected GitHub request: %s", r.URL)
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	p := pipeline.Defaults()
	p.Pipeline[factory.Kind("repair")] = []string{"write", "proof"}
	f.cfg.Pipeline = func(context.Context, string, string) (pipeline.File, error) { return p, nil }
	f.seam.RecipeAt = func(string) (factory.Recipe, []factory.Problem, string, error) { return p.Recipe(), nil, "", nil }
	f.seam.New = func(repo, words string) (int, error) {
		it, err := f.st.Create(factory.Item{State: factory.StateNew, Repo: repo, Title: "Fix it", Body: "the brief", Kind: factory.KindIssue, Origin: factory.OriginTerminal, Assignee: "sam"})
		return it.ID, err
	}
	f.seam.Refresh = func(context.Context, int) error { refreshes++; return nil }
	reopenJobs(t, f)
	body := `{"repo":"acme/api","title":"Fix it","text":"the brief","assignee":"sam","pipeline":"repair"}`
	w := verbRequest(f.h, "POST", "/v1/issues", body)
	checkJSON(t, w, 201)
	var created api.JobCreated
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	it, _ := f.st.Get(created.ID)
	if it.Num != 11 || !it.Synced || it.Origin != factory.OriginForge || len(it.Stages) != 2 || it.Stages[0].Name != "write" {
		t.Fatalf("issue job: %+v", it)
	}
	f.post(t, it, "sync", "{}", 200)
	if refreshes != 1 || creates != 1 {
		t.Fatal(refreshes, creates)
	}
	w = verbRequest(f.h, "GET", "/v1/issues?repo=acme/api&q=fix%20repo:private/hidden", "")
	checkJSON(t, w, 200)
	if strings.Contains(w.Body.String(), "Secret") || !strings.Contains(w.Body.String(), "Fix it") {
		t.Fatal(w.Body.String())
	}
	w = verbRequest(f.h, "POST", "/v1/issues", strings.ReplaceAll(body, "repair", "missing"))
	checkJSON(t, w, 422)
	if creates != 1 {
		t.Fatal("invalid pipeline opened an issue")
	}
	local := f.create(t, factory.Item{State: factory.StateNew, Origin: factory.OriginTerminal, Title: "Fix it", Body: "the brief", Assignee: "sam"})
	f.post(t, local, "sync", "{}", 200)
	f.post(t, local, "sync", "{}", 200)
	if creates != 2 || refreshes != 2 {
		t.Fatal("sync duplicated issue", creates, refreshes)
	}
}

func TestJobScreensHandOffNotNowSurvivesRestartAndIsPersonal(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateRunning, Assignee: "sam", Ask: &factory.JobAsk{Text: "fix the brief", Source: "hand-off", FromRef: &factory.SourceRef{Kind: "agent", ID: "a1", Title: "Source"}}})
	path := fmt.Sprintf("/v1/jobs/%d/hand-off/a1", it.ID)
	checkJSON(t, verbRequest(f.h, "GET", path, ""), 200)
	f.post(t, it, "hand-off/a1/not-now", "{}", 200)
	reopenJobs(t, f)
	w := verbRequest(f.h, "GET", path, "")
	checkJSON(t, w, 200)
	if !strings.Contains(w.Body.String(), `"not_now":true`) {
		t.Fatal(w.Body.String())
	}
	cur, _ := f.st.Get(it.ID)
	if cur.State != factory.StateRunning {
		t.Fatal("Not now stopped the job")
	}
	f.person = "mei"
	w = verbRequest(f.h, "GET", path, "")
	checkJSON(t, w, 200)
	if !strings.Contains(w.Body.String(), `"not_now":false`) {
		t.Fatal("dismissal crossed people")
	}
	checkJSON(t, verbRequest(f.h, "GET", path+"missing", ""), 404)
}

func TestJobScreensStatePresentationOnQueueAndJobReads(t *testing.T) {
	for _, tc := range []struct {
		name, state, qkind, kind, title string
		failure                         *factory.Failure
	}{
		{"failed", string(factory.StateNew), "", "failed", "Step failed", &factory.Failure{Code: factory.CodeToolError, Step: "write", Detail: "command failed"}},
		{"rounds", string(factory.StateNeedsYou), factory.QKindOutOfAttempts, "out_of_rounds", "Stuck", nil},
		{"queued", string(factory.StateQueued), "", "waiting_for_runner", "Waiting to start", nil},
		{"spending", string(factory.StateNeedsYou), "cap", "spending", "Spending limit reached", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newV2Jobs(t)
			it := f.create(t, factory.Item{State: factory.State(tc.state), QKind: tc.qkind, Question: "continue?", Failure: tc.failure, Stream: recordedRun(), Assignee: "sam", Cap: 2})
			j := f.read(t, it)
			if j.StatePresentation.Kind != tc.kind || j.StatePresentation.Title != tc.title || !j.Controls.Timeline {
				t.Fatalf("presentation %+v", j)
			}
			w := verbRequest(f.h, "GET", "/v1/queue", "")
			checkJSON(t, w, 200)
			if !strings.Contains(w.Body.String(), `"state_presentation"`) || !strings.Contains(w.Body.String(), `"controls"`) {
				t.Fatal(w.Body.String())
			}
		})
	}
}

func TestJobScreensPublishedRoutesAreRegistered(t *testing.T) {
	f := newV2Jobs(t)
	for _, route := range api.ServiceRoutes {
		path := strings.NewReplacer("{id}", "999", "{sid}", "write", "{card}", "missing", "{owner}", "acme", "{repo}", "api").Replace(route.Path)
		w := verbRequest(f.h, route.Method, path, "{}")
		if w.Code == 405 || strings.Contains(w.Body.String(), "factory endpoint not found") {
			t.Fatalf("unregistered %s %s: %s", route.Method, route.Path, w.Body.String())
		}
	}
}

func TestJobScreensGitHubFailureCreatesNoJob(t *testing.T) {
	f := newV2Jobs(t)
	f.cfg.Mode = api.FactorySolo
	f.cfg.GitHubToken = func(context.Context) string { return "private-token" }
	f.cfg.GitHubHTTPClient = &http.Client{Transport: jobsTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"message":"private response"}`))}, nil
	})}
	f.seam.New = func(repo, words string) (int, error) { t.Fatal("failed GitHub issue created a job"); return 0, nil }
	reopenJobs(t, f)
	w := verbRequest(f.h, "POST", "/v1/issues", `{"repo":"acme/api","title":"Fix it","text":"brief"}`)
	checkJSON(t, w, 502)
	if strings.Contains(w.Body.String(), "private response") || strings.Contains(w.Body.String(), "private-token") {
		t.Fatal("GitHub response leaked")
	}
	items, err := f.st.List()
	if err != nil || len(items) != 0 {
		t.Fatal(items, err)
	}
}

func TestJobScreensLiveEditsRecompileTailAndRevokeOldWorker(t *testing.T) {
	f := newV2Jobs(t)
	f.seam.Edit = factory.LocalSeam(f.st, now).Edit
	reopenJobs(t, f)
	it := f.create(t, factory.Item{State: factory.StateQueued, Assignee: "sam", Stream: recordedRun(), Stages: []factory.Stage{
		{Name: "plan", Kind: factory.StageChat, On: true}, {Name: "write", Kind: factory.StageChat, On: true}, {Name: "proof", Kind: factory.StageChat, On: true},
	}})
	desk, err := lease.Open(f.st, f.cfg.LeaseOptions)
	if err != nil {
		t.Fatal(err)
	}
	worker := lease.Worker{ID: "worker", Handle: "sam", Machine: "laptop", Slots: 1}
	held, err := desk.Pull(worker)
	if err != nil {
		t.Fatal(err)
	}
	f.post(t, it, "step-edits", `{"add":[{"step":{"name":"security","ask":"Check permissions"},"after":"write"}]}`, 200)
	if _, err := desk.Heartbeat(held.Job, worker.ID); err == nil {
		t.Fatal("old worker can restore previous pipeline")
	}
	cur, _ := f.st.Get(it.ID)
	if cur.State != factory.StateQueued || len(cur.Stream.Phases) != 4 || cur.Stream.Phases[2].Name != "security" || cur.Stream.Phases[1].Chat != "old-chat" || cur.Stream.Spent != 1.25 {
		t.Fatalf("live edit: %+v", cur)
	}
	f.post(t, it, "step-edits", `{"ask":{"write":"too late"}}`, 409)
}

// A JOB'S PERSON REMOVES AND MOVES ITS STEPS through the same door and bounds
// as every other step edit: a move says where it goes, proof stays, and only
// the job's person edits.
func TestJobScreensRemoveAndMoveAStepOfOneJob(t *testing.T) {
	f := newV2Jobs(t)
	f.seam.Edit = factory.LocalSeam(f.st, now).Edit
	reopenJobs(t, f)
	it := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam", Stages: []factory.Stage{
		{Name: "write", Kind: factory.StageChat, Ask: "write it", On: true},
		{Name: "pretest", Kind: factory.StageChat, Ask: "run the old tests", On: false},
		{Name: "docs", Kind: factory.StageChat, Ask: "say it", On: true},
		{Name: "proof", Kind: factory.StageChat, Ask: "prove", Fixed: true, On: true},
	}})
	f.post(t, it, "step-edits", `{"move":[{"step":"docs"}]}`, 422)
	f.post(t, it, "step-edits", `{"remove":["proof"]}`, 409)
	f.person = "mei"
	f.post(t, it, "step-edits", `{"remove":["pretest"]}`, 403)
	f.person = "sam"
	f.post(t, it, "step-edits", `{"remove":["pretest"],"move":[{"step":"docs","after":""}],"why":"docs first"}`, 200)
	cur, _ := f.st.Get(it.ID)
	if len(cur.Stages) != 3 || cur.Stages[0].Name != "docs" || cur.Stages[1].Name != "write" || cur.Stages[0].By != factory.ByYou {
		t.Fatalf("stages = %+v", cur.Stages)
	}
	if strings.Join(cur.Adapted, " · ") != "you removed pretest · you moved docs first · why: docs first" {
		t.Fatalf("record = %q", cur.Adapted)
	}
}
