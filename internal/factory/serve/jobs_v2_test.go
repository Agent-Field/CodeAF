package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

type v2Jobs struct {
	st      *store.Store
	rec     *record.Record
	h       *serve.Handler
	seam    factory.Seam
	cfg     serve.Config
	clock   atomic.Int64
	person  string
	deletes atomic.Int64
}

func newV2Jobs(t *testing.T) *v2Jobs {
	t.Helper()
	f := &v2Jobs{person: "sam"}
	f.clock.Store(now.UnixNano())
	var err error
	f.st, err = store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clock := func() time.Time { return time.Unix(0, f.clock.Load()).UTC() }
	f.rec, err = record.Open(f.st, record.Options{Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	desk, err := lease.Open(f.st, lease.Options{Now: clock, Record: f.rec})
	if err != nil {
		t.Fatal(err)
	}
	doors := lease.NewDoors(desk)
	f.seam = factory.Seam{
		Load: func() (factory.Snapshot, error) {
			items, err := f.st.List()
			return factory.Snapshot{Items: items}, err
		},
		GitHub: func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: f.person}, nil },
		Launch: doors.Launch, Stop: doors.Stop, Answer: doors.Answer, SignOff: doors.SignOff,
	}
	f.cfg = serve.Config{Mode: api.FactoryTeam, Address: config.Address, Version: config.Version, Store: f.st, Record: f.rec, Now: clock, LeaseOptions: lease.Options{Now: clock, Record: f.rec},
		Pipeline: func(context.Context, string, string) (pipeline.File, error) {
			p := pipeline.Defaults()
			p.DefaultBudget = 4
			return p, nil
		},
		DeleteBranch: func(context.Context, factory.Item) error { f.deletes.Add(1); return nil },
	}
	f.h = serve.New(f.seam, f.cfg)
	t.Cleanup(func() { f.h.Close() })
	return f
}

func (f *v2Jobs) create(t *testing.T, it factory.Item) factory.Item {
	t.Helper()
	if it.Repo == "" {
		it.Repo = "acme/api"
	}
	if it.Title == "" {
		it.Title = "test job"
	}
	got, err := f.st.Create(it)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func (f *v2Jobs) post(t *testing.T, it factory.Item, verb, body string, status int) *httptest.ResponseRecorder {
	t.Helper()
	w := verbRequest(f.h, "POST", fmt.Sprintf("/v1/jobs/%d/%s", it.ID, verb), body)
	checkJSON(t, w, status)
	return w
}
func (f *v2Jobs) read(t *testing.T, it factory.Item) api.Job {
	t.Helper()
	w := verbRequest(f.h, "GET", fmt.Sprintf("/v1/jobs/%d", it.ID), "")
	checkJSON(t, w, 200)
	var j api.Job
	if err := json.Unmarshal(w.Body.Bytes(), &j); err != nil {
		t.Fatal(err)
	}
	return j
}

func TestV2RunStopUndoAndStartNow(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam"})
	f.post(t, it, "run", "", 200)
	if j := f.read(t, it); j.State != "queued" || j.Budget != 4 {
		t.Fatalf("run %+v", j)
	}
	f.post(t, it, "stop", "", 200)
	if j := f.read(t, it); j.State != "backlog" {
		t.Fatal(j.State)
	}
	f.post(t, it, "undo", "", 200)
	if j := f.read(t, it); j.State != "queued" {
		t.Fatal(j.State)
	}
	f.post(t, it, "stop", "", 200)
	f.clock.Add(int64(factory.ApprovalUndo))
	f.post(t, it, "undo", "", 409)
	f.post(t, it, "start_now", "", 200)
	cur, _ := f.st.Get(it.ID)
	if !cur.Marked || cur.State != factory.StateQueued {
		t.Fatalf("start now %+v", cur)
	}
}

func TestV2DismissIsPersonalAndGitHubClearsIt(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Num: 1498, Origin: factory.OriginForge, State: factory.StateRunning, Assignee: "mei"})
	f.post(t, it, "dismiss", "", 200)
	if j := f.read(t, it); j.State != "running" || !j.Dismissed || j.Ref != "api#1498" {
		t.Fatalf("dismiss %+v", j)
	}
	f.person = "mei"
	if f.read(t, it).Dismissed {
		t.Fatal("dismiss crossed people")
	}
	fresh := it
	fresh.Labels = []string{"bug"}
	if err := github.Merge(f.st, "github", []factory.Item{fresh}); err != nil {
		t.Fatal(err)
	}
	f.person = "sam"
	if f.read(t, it).Dismissed {
		t.Fatal("GitHub activity did not clear dismissal")
	}
}

func TestV2DropIsFinalAndDeletesOnlyAfterSevenDays(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Num: 7, Origin: factory.OriginForge, State: factory.StateRunning, Assignee: "sam"})
	f.post(t, it, "drop", `{"words":"not this one"}`, 200)
	if j := f.read(t, it); j.State != "done" || j.DoneHow != "dropped" || j.BranchDeleteAt.Sub(j.DoneAt) != factory.DroppedBranchRetention {
		t.Fatalf("drop %+v", j)
	}
	f.post(t, it, "run", "", 409)
	fresh := it
	fresh.Labels = []string{"bug"}
	if err := github.Merge(f.st, "github", []factory.Item{fresh}); err != nil {
		t.Fatal(err)
	}
	if f.read(t, it).DoneHow != "dropped" {
		t.Fatal("GitHub revived Drop")
	}
	f.clock.Add(int64(factory.DroppedBranchRetention - time.Nanosecond))
	f.read(t, it)
	if f.deletes.Load() != 0 {
		t.Fatal("early delete")
	}
	f.clock.Add(1)
	f.read(t, it)
	f.read(t, it)
	if f.deletes.Load() != 1 {
		t.Fatal("delete repeated or missing")
	}
	events, _ := f.rec.Since(context.Background(), time.Time{})
	if len(events) != 2 || events[0].Kind != record.Dropped || events[1].Kind != record.BranchDeleted {
		t.Fatalf("events %+v", events)
	}
}

func TestV2PriorityBudgetAndAssignmentTransitions(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateNew, Labels: []string{"bug", "size:XL"}})
	f.post(t, it, "assign_to_me", "", 200)
	f.post(t, it, "priority", `{"priority":"urgent"}`, 200)
	f.post(t, it, "raise_budget", `{"budget":10}`, 200)
	if j := f.read(t, it); j.Priority.Value != "urgent" || j.Priority.SetBy != "sam" || j.Size != "XL" || j.Budget != 10 || j.State != "backlog" {
		t.Fatalf("metadata %+v", j)
	}
	f.post(t, it, "run", "", 200)
	f.post(t, it, "unassign", "", 200)
	if j := f.read(t, it); j.State != "backlog" || j.AssignedTo != "" {
		t.Fatalf("unassign %+v", j)
	}
	f.post(t, it, "change_assignee", `{"assignee":"mei"}`, 403)
	f.post(t, it, "assign_to_me", "", 200)
	f.post(t, it, "change_assignee", `{"assignee":"mei"}`, 200)
	f.post(t, it, "nudge", `{"words":"please look"}`, 200)
	events, _ := f.rec.Since(context.Background(), time.Time{})
	last := events[len(events)-1]
	if last.Kind != record.Nudged || last.Detail != "@mei please look" {
		t.Fatalf("nudge %+v", last)
	}
	f.post(t, it, "raise_budget", `{"budget":20}`, 403)
}

func TestV2ApprovalsWaitUndoAndRecoverOnRestart(t *testing.T) {
	for _, kind := range []string{"plan", "proof", "start"} {
		t.Run(kind, func(t *testing.T) {
			f := newV2Jobs(t)
			it := factory.Item{State: factory.StateNeedsYou, Assignee: "sam", Question: "approve?", QKind: factory.QKindApprove}
			if kind == "start" {
				it.QKind = factory.QKindAskFirst
				it.AskedFirstReason = "touches auth/"
			}
			if kind == "proof" {
				it.State = factory.StateLanded
				it.Question = ""
				it.QKind = ""
				it.HeadCommit = approvedHead
				it.Proof = []factory.Claim{{Text: "works", OK: true}}
			}
			it = f.create(t, it)
			body := "{}"
			if kind == "proof" {
				body = `{"commit":"` + approvedHead + `"}`
			}
			f.post(t, it, "approve", body, 200)
			j := f.read(t, it)
			if j.Approval.Kind != kind || j.Approval.State != "approving" || j.Approval.UndoUntil.Sub(j.Approval.At) != 5*time.Second {
				t.Fatalf("approval %+v", j.Approval)
			}
			if events, _ := f.rec.Since(context.Background(), time.Time{}); len(events) != 0 {
				t.Fatal("approval ran before Undo")
			}
			f.post(t, it, "undo", "", 200)
			f.clock.Add(int64(factory.ApprovalUndo))
			f.read(t, it)
			if events, _ := f.rec.Since(context.Background(), time.Time{}); len(events) != 1 || events[0].Kind != record.Undone {
				t.Fatal("Undo let approval through")
			}
			f.post(t, it, "approve", body, 200)
			f.h.Close()
			f.clock.Add(int64(factory.ApprovalUndo))
			f.h = serve.New(f.seam, f.cfg)
			j = f.read(t, it)
			if j.Approval == nil || j.Approval.State != "approved" {
				t.Fatalf("restart approval %+v", j.Approval)
			}
			events, _ := f.rec.Since(context.Background(), time.Time{})
			if len(events) != 2 || events[1].Kind != record.Approved || events[1].Handle != "sam" {
				t.Fatalf("receipts %+v", events)
			}
			f.read(t, it)
			events, _ = f.rec.Since(context.Background(), time.Time{})
			if len(events) != 2 {
				t.Fatal("approval repeated")
			}
		})
	}
}

func TestV2PlainQuestionFirstAnswerWinsExactCopy(t *testing.T) {
	f := newV2Jobs(t)
	f.seam.Answer = func(id int, yes bool, words string) error {
		return f.st.Update(id, func(it *factory.Item) error {
			it.State = factory.StateQueued
			it.Question = ""
			it.QKind = ""
			return nil
		})
	}
	f.h.Close()
	f.h = serve.New(f.seam, f.cfg)
	it := f.create(t, factory.Item{State: factory.StateNeedsYou, Assignee: "raj", Question: "which API?", QKind: "scope"})
	if j := f.read(t, it); j.WaitingFor != "@raj" {
		t.Fatalf("waiting %+v", j)
	}
	f.person = "jm"
	f.post(t, it, "answer", `{"words":"v2"}`, 200)
	f.clock.Add(int64(10 * time.Second))
	f.person = "sam"
	w := f.post(t, it, "answer", `{"words":"v1"}`, 409)
	var got struct{ Error string }
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Error != "@jm answered this 10s ago. Your answer wasn't sent." {
		t.Fatal(got.Error)
	}
}

func TestV2ClosedPRRunAgainAndMergedRefusal(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateShipped, Assignee: "sam", PullRequest: &factory.PullRequest{State: factory.PRClosed}})
	if j := f.read(t, it); j.DoneHow != "closed" || j.State != "done" {
		t.Fatalf("closed %+v", j)
	}
	f.post(t, it, "run_again", "", 200)
	if f.read(t, it).State != "queued" {
		t.Fatal("Run again did not queue")
	}
	merged := f.create(t, factory.Item{State: factory.StateShipped, Assignee: "sam", PullRequest: &factory.PullRequest{State: factory.PRMerged}})
	f.post(t, merged, "run_again", "", 409)
}

func TestSoloUnassignedPlanApprovalKeepsUndoAndNoAssignment(t *testing.T) {
	f := newV2Jobs(t)
	f.h.Close()
	f.cfg.Mode = api.FactorySolo
	f.h = serve.New(f.seam, f.cfg)
	it := f.create(t, factory.Item{State: factory.StateNeedsYou, Question: "plan ready?", QKind: factory.QKindApprove})
	j := f.read(t, it)
	w := verbRequest(f.h, "GET", "/v1/queue", "")
	checkJSON(t, w, 200)
	var q api.Queue
	if err := json.Unmarshal(w.Body.Bytes(), &q); err != nil || q.Mode != api.FactorySolo || len(q.Jobs) != 1 || !q.Jobs[0].Can.Approve || q.Jobs[0].AssignedTo != "" {
		t.Fatalf("solo queue approval: %+v %v", q, err)
	}
	if !j.Can.Approve || j.AssignedTo != "" {
		t.Fatalf("solo plan cannot be approved privately: %+v", j.Can)
	}
	f.post(t, it, "approve", "{}", 200)
	j = f.read(t, it)
	if !j.Can.Undo || j.Can.Approve || j.Approval.State != "approving" {
		t.Fatalf("missing Undo: %+v %+v", j.Can, j.Approval)
	}
	f.post(t, it, "undo", "", 200)
	if !f.read(t, it).Can.Approve {
		t.Fatal("Undo did not restore approval")
	}
	f.post(t, it, "approve", "{}", 200)
	f.clock.Add(int64(factory.ApprovalUndo))
	if j = f.read(t, it); j.Approval == nil || j.Approval.State != "approved" {
		t.Fatalf("approval did not settle: %+v", j.Approval)
	}
	stored, err := f.st.Get(it.ID)
	if err != nil || stored.Person() != "" {
		t.Fatalf("solo approval invented an owner: %+v %v", stored, err)
	}
}

func TestTeamUnassignedPlanCannotBeApproved(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateNeedsYou, Question: "plan ready?", QKind: factory.QKindApprove})
	if f.read(t, it).Can.Approve {
		t.Fatal("team unassigned job gained approval")
	}
	f.post(t, it, "approve", "{}", 422)
	stored, _ := f.st.Get(it.ID)
	if stored.Approval != nil {
		t.Fatal("refused approval left a decision")
	}
}

func TestSoloCallerKeepsPersonalDismissAndStopUndo(t *testing.T) {
	f := newV2Jobs(t)
	f.h.Close()
	f.cfg.Mode = api.FactorySolo
	f.h = serve.New(f.seam, f.cfg)
	it := f.create(t, factory.Item{State: factory.StateNew})
	if !f.read(t, it).Can.Dismiss {
		t.Fatal("signed-in solo caller cannot dismiss")
	}
	f.post(t, it, "dismiss", "", 200)
	if !f.read(t, it).Dismissed {
		t.Fatal("personal dismissal was lost")
	}
	active := f.create(t, factory.Item{State: factory.StateQueued})
	f.post(t, active, "stop", "", 200)
	if !f.read(t, active).Can.Undo {
		t.Fatal("solo caller cannot undo their stop")
	}
	f.post(t, active, "undo", "", 200)
	if f.read(t, active).State != api.StateQueued {
		t.Fatal("stop Undo failed")
	}
}

func TestSoloApprovalRequiresGitHubIdentityWithoutInventingAnAssignee(t *testing.T) {
	f := newV2Jobs(t)
	f.h.Close()
	f.cfg.Mode = api.FactorySolo
	f.person = ""
	f.h = serve.New(f.seam, f.cfg)
	it := f.create(t, factory.Item{State: factory.StateNeedsYou, Question: "plan ready?", QKind: factory.QKindApprove})
	if f.read(t, it).Can.Approve {
		t.Fatal("unknown caller gained approval")
	}
	w := f.post(t, it, "approve", "{}", 422)
	if !strings.Contains(w.Body.String(), "Sign in to GitHub") {
		t.Fatalf("not actionable: %s", w.Body.String())
	}
}
