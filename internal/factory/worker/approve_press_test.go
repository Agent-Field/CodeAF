package worker_test

import (
	"context"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// The desk's Approve on a Manual plan hold is factory.job_approve: POST
// /v1/jobs/{id}/approve with the head commit the person saw. The service
// keeps it approving for the undo window and answers the job when the window
// closes. The job goes past the approve step and runs write and test without
// another word: Manual holds no step before it runs any more (the owner's
// decision of 2026-11-17), so the next wait is the landing's own, the proof
// approval, never the plan hold the person just approved.
func TestADeskApproveOnAManualPlanHoldCarriesOnWhenUndoCloses(t *testing.T) {
	g := newRig(t)
	id := manualJob(t, g, append(planGateWrite(), named("test"))...)
	if err := g.st.Update(id, func(it *factory.Item) error { it.Assignee = "me"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	held := g.waitFor(t, id, factory.StateNeedsYou)
	seam := g.doors
	seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "me"}, nil }
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	handler := serve.New(seam, serve.Config{Store: g.st, Record: g.record, Now: func() time.Time { return time.Unix(0, clock.Load()) }})
	defer handler.Close()
	press := httptest.NewRequest("POST", "/v1/jobs/"+strconv.Itoa(id)+"/approve", strings.NewReader(`{"commit":"`+held.HeadCommit+`"}`))
	press.RemoteAddr = "127.0.0.1:1234"
	pressed := httptest.NewRecorder()
	handler.ServeHTTP(pressed, press)
	if pressed.Code != 200 {
		t.Fatalf("approve = %d %s", pressed.Code, pressed.Body)
	}
	if it, _ := g.st.Get(id); it.Approval == nil || it.Approval.State != "approving" {
		t.Fatalf("after the press: %+v", it.Approval)
	}
	clock.Add(int64(factory.ApprovalUndo))
	settle := httptest.NewRequest("GET", "/v1/queue", nil)
	settle.RemoteAddr = "127.0.0.1:1234"
	handler.ServeHTTP(httptest.NewRecorder(), settle)
	it, _ := g.st.Get(id)
	if it.Approval == nil || it.Approval.State != "approved" {
		t.Fatalf("the approval did not settle: %+v (state %s, %q)", it.Approval, it.State, it.Question)
	}
	next := g.waitFor(t, id, factory.StateLanded)
	if phaseState(next, factory.ApproveName) != factory.PhaseDone || phaseState(next, "write") != factory.PhaseDone || phaseState(next, "test") != factory.PhaseDone {
		t.Fatalf("steps at landing: %+v", next.Stream.Phases)
	}
	if kind := factory.NeedsKind(next); kind != factory.NeedsApproveProof {
		t.Fatalf("the landing reads as %s", kind)
	}
	if a := factory.JobApproval(next); a == nil || a.Kind != "proof" || a.State != "waiting" {
		t.Fatalf("the landing offers approval %+v", a)
	}
}
