package main

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
)

// A scripted worker asks successive gates through the real report door. The
// app reads the decisions through factory.job, including after a fresh launch.
func TestAppFactoryApprovalSurvivesLaterGatesAndRestart(t *testing.T) {
	s := newLocalSetup(t)
	st := s.start()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	worker := client.Open(st.URL, s.backend.factoryHub().tokenFor(st.Factory)).Worker("")
	if _, err := worker.Attach(ctx, api.WorkerAttach{Machine: "approval-fixture", Name: "fixture", Handle: "owner", Slots: 1}); err != nil {
		t.Fatal(err)
	}
	it, err := s.backend.local.host.floor.Create(factory.Item{
		Repo: "acme/api", Num: 54, Kind: factory.KindIssue, Title: "Keep my plan approval", State: factory.StateQueued,
		Autonomy: factory.AutonomyManual, Starter: "owner", Cap: 10,
		Stages: []factory.Stage{
			{Name: "plan", Kind: factory.StageCheck, On: true},
			{Name: "approve", Kind: factory.StageGate, On: true},
			{Name: "write", Kind: factory.StageCheck, On: true},
			{Name: "approve-test", Kind: factory.StageGate, On: true},
			{Name: "test", Kind: factory.StageCheck, On: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	pulled, ok, err := worker.Pull(ctx)
	if err != nil || !ok || pulled.Job.ID != it.ID {
		t.Fatalf("pull: ok=%v err=%v job=%+v", ok, err, pulled.Job)
	}
	params := map[string]any{"factory": st.Factory, "job": it.ID}
	read := func(w *factoryWire) api.Job {
		t.Helper()
		var j api.Job
		w.must("factory.job", params, &j)
		return j
	}
	report := func(r api.WorkerReport) {
		t.Helper()
		if err := worker.Report(ctx, pulled.Lease.ID, r); err != nil {
			t.Fatal(err)
		}
	}
	approve := func() factory.Approval {
		t.Helper()
		j := read(s.factoryWire)
		if !j.Can.Approve {
			t.Fatalf("current gate cannot be approved: %+v", j)
		}
		var acted appwire.FactoryJobActed
		s.must("factory.job_approve", map[string]any{"factory": st.Factory, "job": it.ID, "expect": map[string]any{"version": j.Version}}, &acted)
		if acted.Approval == nil || acted.Approval.State != "approving" || acted.Approval.DecidedBy != "owner" {
			t.Fatalf("personal decision = %+v", acted.Approval)
		}
		time.Sleep(factory.ApprovalUndo + 20*time.Millisecond)
		settled := read(s.factoryWire)
		if settled.Approval == nil || settled.Approval.State != "approved" || settled.Approval.DecidedBy != "owner" {
			t.Fatalf("settled decision = %+v", settled.Approval)
		}
		return *settled.Approval
	}
	assertDecision := func(where string, j api.Job, want factory.Approval) {
		t.Helper()
		if j.Approval == nil || !reflect.DeepEqual(*j.Approval, want) {
			t.Fatalf("%s: decided approval lost: got %+v, want %+v", where, j.Approval, want)
		}
	}
	report(api.WorkerReport{Steps: []api.StepReport{{Name: "plan", State: api.StepDone}}, Question: &api.Question{Text: factory.ApproveQuestion("plan"), FromStep: "approve", Kind: factory.QKindApprove}})
	first := approve()
	report(api.WorkerReport{Steps: []api.StepReport{{Name: "write", State: api.StepRunning}}})
	assertDecision("later work", read(s.factoryWire), first)
	report(api.WorkerReport{Steps: []api.StepReport{{Name: "write", State: api.StepDone}}, Question: &api.Question{Text: factory.NextStepQuestion("test"), FromStep: "approve-test", Kind: factory.QKindApprove}})
	waiting := read(s.factoryWire)
	assertDecision("Test gate", waiting, first)
	if waiting.NeedsYou == nil || !waiting.Can.Approve || waiting.Steps[3].State != "waiting" {
		t.Fatalf("Test must retain its own waiting gate: %+v", waiting)
	}
	second := approve()
	if !second.At.After(first.At) {
		t.Fatal("later gate reused the earlier consent")
	}
	report(api.WorkerReport{Steps: []api.StepReport{{Name: "test", State: api.StepDone}}})
	assertDecision("later decision", read(s.factoryWire), second)
	if err := worker.Done(ctx, pulled.Lease.ID, api.WorkerDone{State: api.StateNeedsYou, Ready: true}); err != nil {
		t.Fatal(err)
	}
	s.backend.Close()
	w, _ := reopened(t, s.backend.factoryHub())
	var list appwire.FactoryList
	w.must("factory.list", struct{}{}, &list)
	assertDecision("restart and reopen", read(w), second)
}
