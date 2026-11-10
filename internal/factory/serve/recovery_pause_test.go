package serve_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

func TestDismissRecoveryDecisionPausesUntilManagerRetry(t *testing.T) {
	f := newAppRecoveryJobs(t)
	now := time.Unix(0, f.clock.Load())
	it := staleWorkerJob(now)
	it.State, it.Talk = factory.StateQueued, "manager"
	it = f.create(t, it)
	desk, err := lease.Open(f.st, f.cfg.LeaseOptions)
	if err != nil {
		t.Fatal(err)
	}
	worker := lease.Worker{ID: "worker", Machine: it.WorktreeMachine, Handle: it.Assignee}
	held, err := desk.Pull(worker)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.st.Update(it.ID, func(cur *factory.Item) error {
		cur.State, cur.QKind, cur.Question = factory.StateNeedsYou, factory.QKindOutOfAttempts, "The same check failed again."
		cur.Stream.Phases[1].State = factory.PhaseWaiting
		cur.Asking = &factory.Asked{At: now, Stage: "test", Question: cur.Question, With: factory.AskedYou}
		cur.Approval = &factory.Approval{Kind: "plan", State: "approved", Commit: cur.HeadCommit}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	job := readRecoveryJob(t, f, it)
	if job.NeedsYou == nil || !job.Can.Hold {
		t.Fatal("recovery decision offers no pause")
	}
	postRecoveryJob(t, f, it, "pause", fmt.Sprintf(`{"expect":{"version":%d,"state":"needs_you","step":"test"}}`, job.Version), 200)
	if _, err := desk.Renew(held.ID, worker.ID); err != lease.ErrWithdrawn {
		t.Fatalf("old worker can renew the paused attempt: %v", err)
	}
	f.h.Close()
	f.h = serve.New(f.seam, f.cfg)
	job = readRecoveryJob(t, f, it)
	if job.State != api.StateNeedsYou || job.StatePresentation.Kind != "paused" || job.NeedsYou != nil || len(job.Questions) != 0 ||
		job.Can.Answer || job.Can.Hold || job.Can.Resume || job.Controls.RerunStep {
		t.Fatalf("saved pause read back incorrectly: state=%s presentation=%+v needs=%+v", job.State, job.StatePresentation, job.NeedsYou)
	}
	postRecoveryJob(t, f, it, "answer", `{"yes":true}`, 409)
	postRecoveryJob(t, f, it, "steps/test/rerun", fmt.Sprintf(`{"expect":{"version":%d,"state":"needs_you","step":"test"}}`, job.Version), 409)
	cur, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cur.Question != "The same check failed again." || cur.Asking == nil || cur.Stream.Phases[1].Round != 4 ||
		cur.Approval == nil || cur.Approval.State != "approved" {
		t.Fatal("pause changed the pending decision or work")
	}
	if _, err := desk.Pull(worker); err != lease.ErrNoJob {
		t.Fatalf("a worker took a paused step: %v", err)
	}
	if err := f.st.Update(it.ID, func(cur *factory.Item) error {
		cur.Recovery = &factory.StepRecovery{Step: "test", State: "waiting", NextAt: now.Add(-time.Minute), Limit: 3}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if job := readRecoveryJob(t, f, it); job.StatePresentation.Kind != "paused" {
		t.Fatal("a due automatic recovery removed the pause")
	}
	doors := lease.NewDoors(desk)
	qid := factory.PendingQuestionID(cur)
	words := "Check the same test with the corrected fixture."
	retry := func(manager, question string) error {
		return doors.RetryPausedRecovery(it.ID, manager, question, words, "chat-retry-1")
	}
	if err := retry("other-manager", qid); err == nil {
		t.Fatal("the wrong manager resumed the job")
	}
	if err := retry(it.Talk, "wrong-decision"); err == nil {
		t.Fatal("a stale chat action resumed the job")
	}
	if err := f.st.SetRepos(nil); err != nil {
		t.Fatal(err)
	}
	if err := retry(it.Talk, qid); err == nil {
		t.Fatal("a turned-off repository resumed the job")
	}
	if err := f.st.SetRepos([]string{it.Repo}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.Update(it.ID, func(cur *factory.Item) error { cur.Cap = cur.Stream.Spent; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := retry(it.Talk, qid); err == nil {
		t.Fatal("a spent budget resumed the job")
	}
	if err := f.st.Update(it.ID, func(cur *factory.Item) error { cur.Cap = it.Cap; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := retry(it.Talk, qid); err != nil {
		t.Fatal(err)
	}
	cur, err = f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cur.State != factory.StateQueued || factory.RecoveryPaused(cur) || cur.Question != "" || cur.Asking != nil ||
		cur.LastRecoveryResume != "chat-retry-1" || cur.Stream.Phases[0].State != factory.PhaseDone ||
		cur.Stream.Phases[1].Round != 5 || cur.Stream.Spent != it.Stream.Spent || !slices.Contains(cur.Notes, words) {
		t.Fatalf("the chat retry did not queue the saved step: state=%s round=%d", cur.State, cur.Stream.Phases[1].Round)
	}
	revision, err := f.st.Revision(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := retry(it.Talk, ""); err != nil {
		t.Fatalf("retry receipt lost after the pause cleared: %v", err)
	}
	if after, err := f.st.Revision(it.ID); err != nil || revision != after {
		t.Fatalf("replaying the message changed the job revision: %d → %d (%v)", revision, after, err)
	}
	if _, err := desk.Pull(worker); err != nil {
		t.Fatalf("the matching runner cannot pick up the explicit retry: %v", err)
	}
}

func TestDismissingAnOldRecoveryDecisionDoesNotPauseRunningWork(t *testing.T) {
	f := newAppRecoveryJobs(t)
	now := time.Unix(0, f.clock.Load())
	it := staleWorkerJob(now)
	it.State, it.QKind, it.Question, it.Talk = factory.StateNeedsYou, factory.QKindOutOfAttempts, "Try again?", "manager"
	it.Stream.Phases[1].State = factory.PhaseWaiting
	it = f.create(t, it)
	job := readRecoveryJob(t, f, it)
	if err := f.st.Update(it.ID, func(cur *factory.Item) error {
		cur.State, cur.QKind, cur.Question = factory.StateRunning, "", ""
		cur.Stream.Phases[1].State = factory.PhaseRunning
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	postRecoveryJob(t, f, it, "pause", fmt.Sprintf(`{"expect":{"version":%d,"state":"needs_you","step":"test"}}`, job.Version), 409)
	cur, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cur.State != factory.StateRunning || cur.Stream.Paused || factory.RecoveryPaused(cur) {
		t.Fatal("a stale card paused new running work")
	}
}

func TestAHostedFactoryDoesNotOfferAChatOnlyRecoveryPause(t *testing.T) {
	f := newV2Jobs(t)
	now := time.Unix(0, f.clock.Load())
	it := staleWorkerJob(now)
	it.State, it.QKind, it.Question, it.Talk = factory.StateNeedsYou, factory.QKindOutOfAttempts, "Try again?", "manager"
	it.Stream.Phases[1].State = factory.PhaseWaiting
	it = f.create(t, it)
	job := f.read(t, it)
	if job.Can.Hold || job.NeedsYou == nil {
		t.Fatal("hosted job lost its decision or offered a pause it cannot resume")
	}
	f.post(t, it, "pause", fmt.Sprintf(`{"expect":{"version":%d,"state":"needs_you","step":"test"}}`, job.Version), 501)
}

func TestDismissMergeDecisionPausesLiveFollowPR(t *testing.T) {
	f := newAppRecoveryJobs(t)
	now := time.Unix(0, f.clock.Load())
	it := staleWorkerJob(now)
	it.State, it.Talk, it.Publish = factory.StateQueued, "manager", "pr"
	it.PullRequest = &factory.PullRequest{Number: 2550, State: factory.PROpen, HeadSHA: it.HeadCommit}
	it.FollowUp = &factory.PRFollowUp{Kind: "pr", State: "running", HeadSHA: it.HeadCommit, Attempt: 1, Limit: 2}
	it.Stages[1].Name, it.Stream.Phases[1].Name = factory.FollowPRStep, factory.FollowPRStep
	it = f.create(t, it)
	desk, err := lease.Open(f.st, f.cfg.LeaseOptions)
	if err != nil {
		t.Fatal(err)
	}
	worker := lease.Worker{ID: "worker", Machine: it.WorktreeMachine, Handle: it.Assignee}
	held, err := desk.Pull(worker)
	if err != nil {
		t.Fatal(err)
	}
	job := readRecoveryJob(t, f, it)
	if job.State != api.StateRunning || !job.Can.Hold || job.Can.Merge {
		t.Fatalf("the blocked merge decision should offer pause: state=%s hold=%t merge=%t", job.State, job.Can.Hold, job.Can.Merge)
	}
	stale := fmt.Sprintf(`{"expect":{"version":%d,"state":"running","step":"followpr"}}`, job.Version)
	if err := f.st.Update(it.ID, func(cur *factory.Item) error {
		cur.PullRequest.HeadSHA = "updated-head"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	postRecoveryJob(t, f, it, "pause", stale, 409)
	if _, err := desk.Renew(held.ID, worker.ID); err != nil {
		t.Fatalf("a stale decision withdrew the Follow PR worker: %v", err)
	}
	if err := f.st.Update(it.ID, func(cur *factory.Item) error {
		cur.PullRequest.HeadSHA = it.HeadCommit
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	job = readRecoveryJob(t, f, it)
	postRecoveryJob(t, f, it, "pause", fmt.Sprintf(`{"expect":{"version":%d,"state":"running","step":"followpr"}}`, job.Version), 200)
	if _, err := desk.Renew(held.ID, worker.ID); err != lease.ErrWithdrawn {
		t.Fatalf("old Follow PR worker can still renew: %v", err)
	}
	f.h.Close()
	f.h = serve.New(f.seam, f.cfg)
	job = readRecoveryJob(t, f, it)
	if job.State != api.StateNeedsYou || job.StatePresentation.Kind != "paused" || job.Can.Resume || job.Can.Merge || job.Can.Hold || job.NeedsYou != nil {
		t.Fatalf("live PR decision did not stay paused after reopen: %+v", job.StatePresentation)
	}
	postRecoveryJob(t, f, it, "merge", `{}`, 409)
	if _, err := desk.Pull(worker); err != lease.ErrNoJob {
		t.Fatalf("another worker took the paused Follow PR: %v", err)
	}
	cur, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.NewDoors(desk).RetryPausedRecovery(it.ID, it.Talk, cur.PausedRecoveryQuestion, "", "resume-pr"); err != nil {
		t.Fatal(err)
	}
	got, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != factory.StateQueued || factory.RecoveryPaused(got) || got.Merging != nil || got.PullRequest.HeadSHA != it.HeadCommit ||
		got.Stream.Phases[1].Round != 4 || got.FollowUp.Attempt != 1 || got.Stream.Spent != it.Stream.Spent {
		t.Fatalf("Resume did not keep the original Follow PR work: state=%s follow_up=%+v", got.State, got.FollowUp)
	}
	if _, err := desk.Pull(worker); err != nil {
		t.Fatalf("the runner could not resume its Follow PR: %v", err)
	}
}

func TestDismissApprovalDecisionPreservesItsConsentGate(t *testing.T) {
	f := newAppRecoveryJobs(t)
	it := f.create(t, factory.Item{State: factory.StateNeedsYou, Talk: "manager", Question: "Approve the plan?",
		QKind: factory.QKindApprove, Autonomy: factory.AutonomyManual, Assignee: "sam",
		Approval: &factory.Approval{Kind: "plan", State: "waiting"}})
	job := readRecoveryJob(t, f, it)
	if !job.Can.Approve || !job.Can.Hold {
		t.Fatal("the plan decision should offer both approval and pause")
	}
	postRecoveryJob(t, f, it, "pause", fmt.Sprintf(`{"expect":{"version":%d,"state":"needs_you"}}`, job.Version), 200)
	job = readRecoveryJob(t, f, it)
	if job.Can.Approve || job.NeedsYou != nil || job.StatePresentation.Kind != "paused" {
		t.Fatal("the paused approval is still actionable")
	}
	postRecoveryJob(t, f, it, "approve", `{}`, 409)
	desk, err := lease.Open(f.st, f.cfg.LeaseOptions)
	if err != nil {
		t.Fatal(err)
	}
	cur, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.NewDoors(desk).RetryPausedRecovery(it.ID, it.Talk, cur.PausedRecoveryQuestion, "", "resume-plan"); err != nil {
		t.Fatal(err)
	}
	job = readRecoveryJob(t, f, it)
	if job.State != api.StateNeedsYou || !job.Can.Approve || job.Approval == nil || job.Approval.State != "waiting" || job.NeedsYou == nil {
		t.Fatal("Resume accepted or lost the plan decision")
	}
}

func newAppRecoveryJobs(t *testing.T) *v2Jobs {
	t.Helper()
	f := newV2Jobs(t)
	f.h.Close()
	f.cfg.Mode, f.cfg.ServedBy, f.cfg.AccessToken = api.FactorySolo, "app", "test-app-token"
	f.h = serve.New(f.seam, f.cfg)
	return f
}

func readRecoveryJob(t *testing.T, f *v2Jobs, it factory.Item) api.Job {
	t.Helper()
	w := appRecoveryRequest(f, "GET", fmt.Sprintf("/v1/jobs/%d", it.ID), "")
	checkJSON(t, w, 200)
	var job api.Job
	if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	return job
}

func postRecoveryJob(t *testing.T, f *v2Jobs, it factory.Item, verb, body string, status int) {
	t.Helper()
	checkJSON(t, appRecoveryRequest(f, "POST", fmt.Sprintf("/v1/jobs/%d/%s", it.ID, verb), body), status)
}

func appRecoveryRequest(f *v2Jobs, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Authorization", "Bearer "+f.cfg.AccessToken)
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}
