package run

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/factory"
	"strings"
	"testing"
	"time"
)

func completedReportFixture() (factory.Item, factory.RunReport) {
	head := strings.Repeat("a", 40)
	now := time.Date(2026, 10, 23, 13, 46, 0, 0, time.UTC)
	result := factory.StageResult{Done: true, Repair: &factory.CIRepairResult{Decision: "no_change", Reason: "Current published head already addresses the finding; checks passed", Readiness: &factory.ReadinessReview{Head: head, DiffReviewed: true, DescriptionMatches: true, FeedbackAddressed: true, NoUnfinishedWork: true}}}
	it := factory.Item{ID: 12, State: factory.StateRunning, Kind: factory.KindIssue, Publish: "pr", HeadCommit: head, Stages: []factory.Stage{{Name: factory.FollowPRStep, Kind: factory.StageChat, Until: factory.UntilProven}}, Stream: &factory.Stream{Phases: []factory.Phase{{Name: factory.FollowPRStep, State: factory.PhaseRunning, Round: 37, Started: now}}}, PullRequest: &factory.PullRequest{Number: 11, URL: "https://example.test/pull/11", HeadSHA: head, State: factory.PROpen, FeedbackTracked: true, FeedbackItems: []factory.PRFeedback{{ID: "inline:1", Revision: "v1"}}}, FollowUp: &factory.PRFollowUp{Kind: "pr", State: "running", HeadSHA: head, Attempt: 1, Limit: 3, ReviewKey: "review", EventKey: "event", Inspecting: map[string]string{"inline:1": "v1"}, Request: "Inspect this review"}}
	next := *it.FollowUp
	next.Attempt = 0
	next.State = "watching"
	next.Inspecting = nil
	report := factory.RunReport{FollowUp: &next, Cost: .25, Stages: []factory.StageReport{{Name: factory.FollowPRStep, State: factory.PhaseWaiting, Round: 37, Started: now, Result: &result}}}
	return it, report
}
func cloneReportFixture(it factory.Item, r factory.RunReport) (factory.Item, factory.RunReport) {
	a, _ := json.Marshal(it)
	b, _ := json.Marshal(r)
	var x factory.Item
	var y factory.RunReport
	_ = json.Unmarshal(a, &x)
	_ = json.Unmarshal(b, &y)
	return x, y
}

func TestCompletedNoChangeReportAcceptsLegacyResetAndAcknowledgesCapturedReview(t *testing.T) {
	it, r := completedReportFixture()
	if err := ApplyReport(&it, r, time.Now()); err != nil {
		t.Fatal(err)
	}
	if it.FollowUp.Attempt != 0 || it.FollowUp.State != "watching" || it.FollowUp.Request != "" || it.FollowUp.Reviewed["inline:1"] != "v1" || it.FollowUp.Inspecting != nil || it.Stream.Phases[0].State != factory.PhaseWaiting || it.Stream.Spent != .25 {
		t.Fatalf("completion not accepted: follow=%+v spent=%v", it.FollowUp, it.Stream.Spent)
	}
}
func TestCompletedNoChangeReportKeepsStaleAttemptAndReadinessFences(t *testing.T) {
	base, r := completedReportFixture()
	for name, change := range map[string]func(*factory.Item, *factory.RunReport){
		"old round": func(_ *factory.Item, r *factory.RunReport) { r.Stages[0].Round-- },
		"other start": func(_ *factory.Item, r *factory.RunReport) {
			r.Stages[0].Started = r.Stages[0].Started.Add(-time.Second)
		},
		"new head":             func(i *factory.Item, _ *factory.RunReport) { i.FollowUp.HeadSHA = strings.Repeat("b", 40) },
		"new feedback":         func(i *factory.Item, _ *factory.RunReport) { i.FollowUp.ReviewKey = "new" },
		"new request":          func(i *factory.Item, _ *factory.RunReport) { i.FollowUp.EventKey = "new" },
		"findings":             func(_ *factory.Item, r *factory.RunReport) { r.Stages[0].Result.Findings = 1 },
		"incomplete checklist": func(_ *factory.Item, r *factory.RunReport) { r.Stages[0].Result.Repair.Readiness = nil },
		"real decision":        func(i *factory.Item, _ *factory.RunReport) { i.QKind = "scope"; i.Question = "Choose scope" },
		"checkpoint":           func(_ *factory.Item, r *factory.RunReport) { r.Stages[0].Result.Checkpoint = true },
		"execution blocked":    func(_ *factory.Item, r *factory.RunReport) { r.Stages[0].Result.ExecutionBlocked = "verification" },
		"budget stopped":       func(_ *factory.Item, r *factory.RunReport) { r.Stages[0].Result.JobBudgetStopped = true },
		"counter rewrite":      func(_ *factory.Item, r *factory.RunReport) { r.FollowUp.Attempt = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			i, r := cloneReportFixture(base, r)
			change(&i, &r)
			if err := ApplyReport(&i, r, time.Now()); err == nil {
				t.Fatal("unsafe completion accepted")
			}
		})
	}
}
func TestDiffKeepsHeldAttemptWhileLocalCompletionResetsRetryCounter(t *testing.T) {
	before, r := completedReportFixture()
	after, _ := cloneReportFixture(before, r)
	if err := ApplyReport(&after, r, time.Now()); err != nil {
		t.Fatal(err)
	}
	delta, ok := Diff(before, after)
	if !ok || delta.FollowUp == nil || delta.FollowUp.Attempt != 1 {
		t.Fatalf("held attempt lost: %+v", delta.FollowUp)
	}
	if err := ApplyReport(&before, delta, time.Now()); err != nil {
		t.Fatal(err)
	}
	if before.FollowUp.Reviewed["inline:1"] != "v1" {
		t.Fatal("captured review was not acknowledged")
	}
}
func TestCompletedReportDoesNotAcknowledgeFeedbackChangedInFlight(t *testing.T) {
	i, r := completedReportFixture()
	i.PullRequest.FeedbackItems[0].Revision = "v2"
	if err := ApplyReport(&i, r, time.Now()); err != nil {
		t.Fatal(err)
	}
	if i.FollowUp.Reviewed["inline:1"] != "" {
		t.Fatal("new unseen revision acknowledged")
	}
}

func TestCompletedReportPreservesNewerServiceFeedbackOverWorkerSnapshot(t *testing.T) {
	i, r := completedReportFixture()
	old := *i.PullRequest
	old.FeedbackItems = append([]factory.PRFeedback(nil), i.PullRequest.FeedbackItems...)
	r.PR = &old
	i.PullRequest.FeedbackItems[0].Revision = "v2"
	if err := ApplyReport(&i, r, time.Now()); err != nil {
		t.Fatal(err)
	}
	if i.PullRequest.FeedbackItems[0].Revision != "v2" || i.FollowUp.Reviewed["inline:1"] != "" {
		t.Fatal("completion overwrote or acknowledged unseen feedback")
	}
}

func TestDiffPreservesHeldAttemptAcrossPublishedRepairAndMovedHead(t *testing.T) {
	for _, fixed := range []bool{false, true} {
		before, r := completedReportFixture()
		after, _ := cloneReportFixture(before, r)
		after.FollowUp = r.FollowUp
		after.FollowUp.ObservedFrom = before.FollowUp.HeadSHA
		nextHead := strings.Repeat("b", 40)
		after.FollowUp.HeadSHA = nextHead
		after.HeadCommit = nextHead
		after.PullRequest.HeadSHA = nextHead
		after.Stream.Phases[0].State = factory.PhaseWaiting
		after.Stream.Phases[0].Result = r.Stages[0].Result
		after.Stream.Phases[0].Result.Repair.Readiness.Head = nextHead
		if fixed {
			after.Stream.Phases[0].Result.Repair.Decision = "fixed"
		}
		delta, _ := Diff(before, after)
		if delta.FollowUp.Attempt != 1 {
			t.Fatal("publication reset escaped transport identity")
		}
		if err := ApplyReport(&before, delta, time.Now()); err != nil {
			t.Fatalf("fixed=%v: %v", fixed, err)
		}
	}
}
func TestLegacyResetAfterObservedHeadMoveUsesCapturedTransitionFence(t *testing.T) {
	i, r := completedReportFixture()
	head := strings.Repeat("b", 40)
	pr := *i.PullRequest
	pr.HeadSHA = head
	r.PR = &pr
	r.FollowUp.ObservedFrom = i.FollowUp.HeadSHA
	r.FollowUp.HeadSHA = head
	r.Stages[0].Result.Repair.Readiness.Head = head
	if err := ApplyReport(&i, r, time.Now()); err != nil {
		t.Fatal(err)
	}
	if i.FollowUp.HeadSHA != head || i.FollowUp.Attempt != 0 {
		t.Fatal("observed-head completion not folded")
	}
}
