package serve_test

import (
	"context"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"strings"
	"testing"
	"time"
)

func autoProof() factory.Item {
	return factory.Item{Autonomy: factory.AutonomyAuto, State: factory.StateLanded, Assignee: "sam", Cap: 10, HeadCommit: strings.Repeat("a", 40), Stages: []factory.Stage{{Name: "approve", Kind: factory.StageGate, On: true}}, Proof: []factory.Claim{{Text: "regression passes", OK: true, Evidence: "test exit 0"}}, Stream: &factory.Stream{Spent: 1}}
}

func TestAutoProofUsesRecordedHeadApprovalWithoutUIApprove(t *testing.T) {
	f := newV2Jobs(t)
	in := autoProof()
	in.MergePolicy = &factory.MergePolicy{Auto: true}
	it := f.create(t, in)
	j := f.read(t, it)
	saved, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if j.Autonomy != "auto" || !saved.Publishing || saved.MergePolicy != nil || saved.Approval == nil || !saved.Approval.Automatic || saved.Approval.State != "approved" {
		t.Fatalf("automatic publish not queued: mode=%s publishing=%v approval=%+v merge=%+v", j.Autonomy, saved.Publishing, saved.Approval, saved.MergePolicy)
	}
	events, err := f.rec.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	approved := 0
	for _, e := range events {
		if e.Kind == record.Approved {
			approved++
			if e.Handle != "sam" || e.Commit != in.HeadCommit || !strings.Contains(e.Detail, "Auto mode") {
				t.Fatalf("bad recorded authority: %+v", e)
			}
		}
	}
	if approved != 1 {
		t.Fatalf("approval count=%d", approved)
	}
	f.read(t, it)
	events, _ = f.rec.Since(context.Background(), time.Time{})
	n := 0
	for _, e := range events {
		if e.Kind == record.Approved {
			n++
		}
	}
	if n != 1 {
		t.Fatal("repeated read repeated automatic publication")
	}
}

func TestAutoFinalReadinessUsesRecordedApprovalWithoutHumanReview(t *testing.T) {
	f := newV2Jobs(t)
	in := autoProof()
	in.Publish = "pr"
	in.PullRequest = &factory.PullRequest{Number: 7, State: factory.PROpen, Draft: true, HeadSHA: in.HeadCommit, ChecksRead: true, ReviewsRead: true, Mergeable: factory.MergeableYes, CI: "ci ✓", Checks: factory.PRChecks{Passed: 1}, Required: 2, ReviewDecision: "review_required", ReadinessKey: "snapshot"}
	in.FollowUp = &factory.PRFollowUp{Kind: "readiness", State: "ready_approval", Readiness: &factory.PRReadiness{Key: "snapshot", Head: in.HeadCommit, State: "reviewed"}}
	it := f.create(t, in)
	f.read(t, it)
	got, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Publishing || got.Approval == nil || !got.Approval.Automatic || got.Approval.State != "approved" || !factory.ReadinessPending(got) {
		t.Fatalf("routine readiness still needs a click: publishing=%v approval=%+v", got.Publishing, got.Approval)
	}
	if got.Merging != nil || got.PullRequest.State != factory.PROpen {
		t.Fatal("readiness approval granted merge authority")
	}
	events, err := f.rec.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	approved := 0
	for _, event := range events {
		if event.Kind == record.Approved && event.Commit == in.HeadCommit {
			approved++
		}
	}
	if approved != 1 {
		t.Fatalf("recorded readiness approvals=%d", approved)
	}
}

func TestAutoLeavesCriticalProblemsAndManualJobsWaiting(t *testing.T) {
	cases := map[string]func(*factory.Item){
		"manual":                 func(it *factory.Item) { it.Autonomy = factory.AutonomyManual },
		"old mode absent":        func(it *factory.Item) { it.Autonomy = "" },
		"take over":              func(it *factory.Item) { it.Manual = true },
		"fixed publication gate": func(it *factory.Item) { it.Stages[0].Fixed = true },
		"cap reached":            func(it *factory.Item) { it.Stream.Spent = 10 },
		"no proof":               func(it *factory.Item) { it.Proof = nil },
		"proof fails":            func(it *factory.Item) { it.Proof[0].OK = false },
		"policy fails":           func(it *factory.Item) { it.Policy = []factory.Claim{{Text: "policy needs a decision"}} },
		"publish failed":         func(it *factory.Item) { it.PublishError = "GitHub access expired" },
		"unanswered choice": func(it *factory.Item) {
			it.State = factory.StateNeedsYou
			it.QKind = "step"
			it.Question = "Which account?"
		},
		"prior automatic refusal": func(it *factory.Item) {
			it.Approval = &factory.Approval{Kind: "proof", State: "waiting", Error: "head changed", Automatic: true}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := newV2Jobs(t)
			in := autoProof()
			change(&in)
			it := f.create(t, in)
			j := f.read(t, it)
			if factory.Automatic(in) && j.AutonomyWhy == "" {
				t.Fatal("Auto exception has no explanation")
			}
			saved, _ := f.st.Get(it.ID)
			if saved.Publishing {
				t.Fatalf("%s was automatically published", name)
			}
			events, _ := f.rec.Since(context.Background(), time.Time{})
			for _, e := range events {
				if e.Kind == record.Approved {
					t.Fatalf("%s recorded approval", name)
				}
			}
		})
	}
}

func TestAutoModeControlPersistsAndDoesNotResumeTakeOver(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam", Autonomy: factory.AutonomyManual, Manual: true})
	j := f.read(t, it)
	f.post(t, it, "control", fmt.Sprintf(`{"mode":"auto","expect":{"version":%d}}`, j.Version), 200)
	j = f.read(t, it)
	if j.Autonomy != "auto" || !j.Manual {
		t.Fatal("Auto did not persist or resumed Take over")
	}
	f.post(t, it, "control", fmt.Sprintf(`{"mode":"manual","expect":{"version":%d}}`, j.Version), 200)
	j = f.read(t, it)
	if j.Autonomy != "manual" || !j.Manual {
		t.Fatal("Manual did not persist")
	}
	// A changed control decision must not be replayed from an older view.
	f.post(t, it, "control", fmt.Sprintf(`{"mode":"auto","expect":{"version":%d}}`, j.Version-1), 409)
}

func TestAutoRoutinePlanGateRecordsDecisionButFixedGateWaits(t *testing.T) {
	for _, fixed := range []bool{false, true} {
		t.Run(fmt.Sprint(fixed), func(t *testing.T) {
			f := newV2Jobs(t)
			it := f.create(t, factory.Item{Autonomy: "auto", State: factory.StateNeedsYou, Assignee: "sam", Cap: 10, QKind: factory.QKindApprove, Question: "plan is ready", Stages: []factory.Stage{{Name: "plan", On: true}, {Name: "approve", Kind: factory.StageGate, On: true, Fixed: fixed}, {Name: "write", On: true}}, Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: "approve", Kind: factory.StageGate, State: factory.PhaseWaiting}, {Name: "write", State: factory.PhasePending}}}})
			j := f.read(t, it)
			if fixed {
				if j.State != api.StateNeedsYou {
					t.Fatal("fixed gate was bypassed")
				}
			} else if j.State == api.StateNeedsYou {
				t.Fatal("routine gate still needs approval")
			}
		})
	}
}

func TestManualWithoutInternalGatesStillRequiresPublicationApproval(t *testing.T) {
	f := newV2Jobs(t)
	in := autoProof()
	in.Autonomy = factory.AutonomyManual
	in.Stages = []factory.Stage{{Name: "proof", On: true}}
	it := f.create(t, in)
	j := f.read(t, it)
	if j.Approval == nil || j.Approval.Kind != "proof" || j.Approval.State != "waiting" || !j.Can.Approve {
		t.Fatal("Manual without internal gates lost publication approval")
	}
	saved, _ := f.st.Get(it.ID)
	if saved.Publishing {
		t.Fatal("Manual published automatically")
	}
}
