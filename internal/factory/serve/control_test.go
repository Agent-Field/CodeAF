package serve_test

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"strings"
	"testing"
)

func TestTakeoverAndResumeKeepWorkAndSpend(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Kind: factory.KindIssue, State: factory.StateRunning, Assignee: "sam", Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}, {Name: "write", Kind: factory.StageChat, On: true}, {Name: "proof", Kind: factory.StageChat, On: true}}, Stream: recordedRun()})
	f.post(t, it, "control", `{"mode":"take_over"}`, 200)
	cur, _ := f.st.Get(it.ID)
	if !cur.Manual || cur.State != factory.StateNeedsYou || cur.Stream.Spent != 1.25 || cur.Stream.Phases[1].Chat != "old-chat" {
		t.Fatalf("takeover lost work: %+v", cur)
	}
	f.post(t, cur, "control", `{"mode":"resume_pipeline","words":"Verify my changes"}`, 200)
	cur, _ = f.st.Get(it.ID)
	if cur.Manual || cur.State != factory.StateQueued || cur.Stream.Spent != 1.25 || cur.Stream.Phases[1].Chat != "old-chat" || cur.Notes[len(cur.Notes)-1] != "Verify my changes" {
		t.Fatalf("resume lost context: %+v", cur)
	}
}

func TestResumeRetriesFinalReviewWithoutManufacturingARepairRequest(t *testing.T) {
	for _, mode := range []string{"retry final review", "manual changes", "explicit instruction", "CI pending"} {
		t.Run(mode, func(t *testing.T) {
			f := newV2Jobs(t)
			head := strings.Repeat("a", 40)
			it := factory.Item{Kind: factory.KindIssue, State: factory.StateNeedsYou, Publish: "pr", Assignee: "sam", Cap: 10, Autonomy: factory.AutonomyAuto, HeadCommit: head,
				Manual: mode == "manual changes", Stream: recordedRun(),
				PullRequest: &factory.PullRequest{Number: 7, State: factory.PROpen, Draft: true, HeadSHA: head, ChecksRead: true, ReviewsRead: true, Mergeable: factory.MergeableYes, CI: "ci ✓", Checks: factory.PRChecks{Passed: 1}, ReadinessKey: "snapshot", ReadinessContext: "complete diff and description"},
				FollowUp:    &factory.PRFollowUp{Kind: "readiness", State: "needs_you", Attempt: 3, Limit: 3, Readiness: &factory.PRReadiness{Key: "snapshot", Head: head, State: "reviewing"}},
			}
			if mode == "CI pending" {
				it.PullRequest.Checks.Pending = 1
				it.PullRequest.CI = "ci …"
			}
			cur := f.create(t, it)
			body := `{"mode":"resume_pipeline"}`
			if mode == "explicit instruction" {
				body = `{"mode":"resume_pipeline","words":"Verify my changes"}`
			}
			f.post(t, cur, "control", body, 200)
			got, _ := f.st.Get(cur.ID)
			if got.Manual || got.Stream.Spent != 1.25 {
				t.Fatal("resume lost control or spend")
			}
			if mode == "manual changes" || mode == "explicit instruction" {
				if got.FollowUp.Kind != "pr" || got.FollowUp.Request == "" {
					t.Fatal("manual work was not routed through verification")
				}
			} else if mode == "CI pending" {
				if got.State != factory.StateShipped || got.FollowUp.State != "watching" || got.FollowUp.Request != "" {
					t.Fatal("pending CI should continue monitoring")
				}
			} else if got.State != factory.StateQueued || got.FollowUp.Kind != "readiness" || got.FollowUp.Request != "" || got.FollowUp.Readiness == nil || got.FollowUp.Readiness.Key != "snapshot" || got.FollowUp.Attempt != 1 {
				t.Fatalf("readiness retry lost purpose: state=%s kind=%s request=%q", got.State, got.FollowUp.Kind, got.FollowUp.Request)
			}
		})
	}
}
