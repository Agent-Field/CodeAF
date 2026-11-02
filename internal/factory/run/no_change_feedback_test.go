package run

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

func TestFollowPRNoChangeRefusalDoesNotPayForSameSnapshot(t *testing.T) {
	dir := gitRig(t)
	head := mustGit(t, dir, "rev-parse", "HEAD")
	var calls atomic.Int32
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
		calls.Add(1)
		return factory.StageResult{Done: true, Spent: .03, Repair: &factory.CIRepairResult{Decision: "no_change", Reason: "Nothing to change"}}, nil
	})}, func(o *Options) { o.Workdir = func(factory.Item) (string, error) { return dir, nil } })
	id := g.add("incomplete feedback review", chat("proof"))
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.Cap = 10
		it.State, it.Publish, it.HeadCommit = factory.StateRunning, "pr", head
		it.PullRequest = &factory.PullRequest{Number: 11, State: factory.PROpen, HeadSHA: head, FeedbackTracked: true, Feedback: "Please fix the error", FeedbackItems: []factory.PRFeedback{{ID: "conversation:1", Revision: "v1"}}}
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
	it := g.waitState(id, factory.StateNeedsYou)
	before := calls.Load()
	if err := g.r.Answer(id, true, factory.ChoiceTryOnceMore); err == nil || !strings.Contains(err.Error(), "new push or comment") {
		t.Fatalf("unchanged refusal started paid retry instead of explaining recovery: %v", err)
	}
	for _, action := range []struct {
		name string
		run  func(*factory.Item) error
	}{
		{"held answer", func(i *factory.Item) error { return AnswerHeld(i, true, time.Now(), factory.ChoiceTryOnceMore) }},
		{"cold answer", func(i *factory.Item) error { return AnswerUnheld(i, true, factory.ChoiceTryOnceMore, time.Now()) }},
		{"step rerun", func(i *factory.Item) error { return StepControl(i, factory.FollowPRStep, true, "", time.Now()) }},
	} {
		t.Run(action.name, func(t *testing.T) {
			current, _ := g.st.Get(id)
			if err := action.run(&current); err == nil {
				t.Fatal("unchanged deterministic refusal allowed retry")
			}
			for _, change := range []string{"head", "comment"} {
				fresh, _ := g.st.Get(id)
				if change == "head" {
					fresh.PullRequest.HeadSHA = strings.Repeat("f", 40)
				} else {
					fresh.PullRequest.FeedbackItems = append(fresh.PullRequest.FeedbackItems, factory.PRFeedback{ID: "conversation:2", Revision: "v1"})
					fresh.PullRequest.Feedback += "\nNew comment"
				}
				if err := action.run(&fresh); err != nil {
					t.Fatalf("%s change still refused: %v", change, err)
				}
			}
		})
	}
	current, _ := g.st.Get(id)
	if calls.Load() != before || current.Stream.Spent != it.Stream.Spent || current.Question != it.Question {
		t.Fatal("refused retry changed spend or question")
	}
	if v := factory.MergeGate(current, factory.MergeAsk{By: "sam"}); v.OK {
		t.Fatal("attempt exhaustion opened Merge with unassessed requested change")
	}
	if err := AnswerUnheld(&current, false, factory.ChoiceCancel, time.Now()); err != nil {
		t.Fatalf("Stop refused: %v", err)
	}
}

func TestFollowPRCheckoutRefusalRetriesWithoutPayingUntilCheckoutChanges(t *testing.T) {
	dir := gitRig(t)
	head := mustGit(t, dir, "rev-parse", "HEAD")
	dirty := filepath.Join(dir, "untracked.txt")
	if err := os.WriteFile(dirty, []byte("unfinished work"), 0600); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
		calls.Add(1)
		return factory.StageResult{Done: true, Repair: &factory.CIRepairResult{Decision: "no_change", Reason: "Comment requests no change", Readiness: &factory.ReadinessReview{Head: head, DiffReviewed: true, DescriptionMatches: true, FeedbackAddressed: true, NoUnfinishedWork: true}}}, nil
	})}, func(o *Options) { o.Workdir = func(factory.Item) (string, error) { return dir, nil } })
	id := g.add("checkout refusal", chat("proof"))
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.Cap = 10
		it.State, it.Publish, it.HeadCommit = factory.StateRunning, "pr", head
		it.PullRequest = &factory.PullRequest{Number: 11, State: factory.PROpen, HeadSHA: head, FeedbackTracked: true, Feedback: "Nothing to change", FeedbackItems: []factory.PRFeedback{{ID: "conversation:1", Revision: "v1"}}}
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
	it := g.waitState(id, factory.StateNeedsYou)
	if it.FollowUp.Reviewed["conversation:1"] != "" {
		t.Fatal("dirty checkout acknowledged feedback")
	}
	before := calls.Load()
	if err := g.r.Answer(id, true, factory.ChoiceTryOnceMore); err != nil {
		t.Fatalf("checkout retry must reach unpaid checkout validation: %v", err)
	}
	g.wait(id, "new unpaid refusal", func(i factory.Item) bool {
		return i.State == factory.StateNeedsYou && i.Stream.Phases[len(i.Stream.Phases)-1].Round > it.Stream.Phases[len(it.Stream.Phases)-1].Round
	})
	if calls.Load() != before {
		t.Fatal("unchanged dirty checkout started another paid model round")
	}
	if err := os.Remove(dirty); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Answer(id, true, factory.ChoiceTryOnceMore); err != nil {
		t.Fatalf("restored checkout still refused: %v", err)
	}
	got := g.waitState(id, factory.StateShipped)
	if calls.Load() != before+1 || got.FollowUp.Reviewed["conversation:1"] != "v1" {
		t.Fatal("restored checkout did not receive one fresh review")
	}
}
