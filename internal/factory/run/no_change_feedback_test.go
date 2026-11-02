package run

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestFollowPRNoChangeFeedbackCorrectsReportWithoutAnotherRound(t *testing.T) {
	dir := gitRig(t)
	head := mustGit(t, dir, "rev-parse", "HEAD")
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		r := factory.StageResult{Done: true, Repair: &factory.CIRepairResult{Decision: "no_change", Reason: "Read the current comment: Nothing to change. It requests no edit."}}
		if err := c.Spec.Stage.Report(ctx, r); err == nil || !strings.Contains(err.Error(), "repair.readiness") {
			t.Errorf("incomplete feedback review was accepted instead of corrected in the same round: %v", err)
		}
		r.Repair.Readiness = &factory.ReadinessReview{Head: head, DiffReviewed: true, DescriptionMatches: true, FeedbackAddressed: true, NoUnfinishedWork: true}
		if err := c.Spec.Stage.Report(ctx, r); err != nil {
			t.Errorf("complete no-change assessment refused: %v", err)
		}
	}}
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: NewChatExecutor(maker)}, func(o *Options) {
		o.Workdir = func(factory.Item) (string, error) { return dir, nil }
		o.Push = func(string, string) error { t.Error("no-change review pushed a commit"); return nil }
	})
	id := g.add("read harmless comment", chat("proof"))
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.Cap = 10
		it.State, it.Publish, it.HeadCommit = factory.StateRunning, "pr", head
		it.PullRequest = &factory.PullRequest{Number: 11, State: factory.PROpen, HeadSHA: head, ChecksRead: true, ReviewsRead: true, Mergeable: factory.MergeableYes, FeedbackTracked: true, Feedback: "Nothing to change", FeedbackItems: []factory.PRFeedback{{ID: "conversation:1", Revision: "v1"}}}
		it.FollowUp = &factory.PRFollowUp{Kind: "pr", State: "queued", HeadSHA: head, Attempt: 1, Limit: 3, Inspecting: map[string]string{"conversation:1": "v1"}}
		it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "proof", Kind: factory.StageChat, State: factory.PhaseDone}}, Paused: true}
		factory.EnsureFollowPR(it)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Hold(id, false); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateShipped)
	if it.FollowUp.State != "watching" || it.FollowUp.Reviewed["conversation:1"] != "v1" || !strings.Contains(it.FollowUp.Reason, "requests no edit") || len(maker.Opened()) != 1 {
		t.Fatalf("feedback not considered in one round: %+v", it.FollowUp)
	}
	if v := factory.MergeGate(it, factory.MergeAsk{By: "sam"}); !v.OK {
		t.Fatalf("harmless feedback still blocks Merge: %s", v.Why)
	}
	for _, ci := range []factory.PRChecks{{Pending: 1}, {Failed: 1}} {
		it.PullRequest.Checks = ci
		if v := factory.MergeGate(it, factory.MergeAsk{By: "sam"}); v.OK {
			t.Fatal("CI blocker opened Merge")
		}
	}
	if got := mustGit(t, dir, "rev-parse", "HEAD"); got != head {
		t.Fatal("no-change review changed HEAD")
	}
}
