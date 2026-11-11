package github

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

func TestFollowingRedCIQueuesThePublishedJobOnlyOnce(t *testing.T) {
	now := time.Now()
	m := newMemAPI(now)
	head := strings.Repeat("a", 40)
	m.followed = map[string]forge.Pull{"acme/api#7": {Number: 7, URL: "https://github.com/acme/api/pull/7", State: "open", HeadSHA: head, Base: "main"}}
	m.runs = map[string][]forge.CheckRun{head: {{Name: "test", Status: "completed", Conclusion: "failure", URL: "https://github.com/acme/api/actions/runs/1/job/2"}}}
	st := openStore(t)
	it, err := st.Create(factory.Item{Kind: factory.KindIssue, Repo: "api", Product: "acme", Num: 12, State: factory.StateShipped, Cap: 10, Stages: []factory.Stage{{Name: "proof", Kind: factory.StageChat, On: true}}, Publish: "pr", HeadCommit: head, PullRequest: &factory.PullRequest{Number: 7, State: factory.PROpen}, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "proof", State: factory.PhaseDone}}}})
	if err != nil {
		t.Fatal(err)
	}
	src := New(m, nil, nil)
	for n := 0; n < 2; n++ {
		if err := followOpened(context.Background(), src, st); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != factory.StateQueued || got.FollowUp == nil || got.FollowUp.Attempt != 1 || len(got.Stream.Phases) != 2 {
		t.Fatalf("not queued once: %+v", got)
	}
	if !strings.Contains(got.FollowUp.Details, "Log unavailable") {
		t.Fatal("missing logs were hidden")
	}
}

func TestLargeCILogRetainsEarlyAndLateDependencyFindings(t *testing.T) {
	body := "agents/agent/go.mod\nCVE-EARLY high x/net fixed in next version\n" + strings.Repeat("download log noise\n", 14000) + "package-lock.json\nCVE-LATE critical next\n"
	got := checkEvidence(body)
	for _, want := range []string{"go.mod", "CVE-EARLY", "package-lock.json", "CVE-LATE", "LOG TRUNCATED"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s", want)
		}
	}
}

func TestPausedFollowPRPollKeepsFactsAndQueuesOneValidatedRecovery(t *testing.T) {
	now := time.Now()
	m := newMemAPI(now)
	head := strings.Repeat("a", 40)
	m.followed = map[string]forge.Pull{"acme/api#11": {Number: 11, State: "open", HeadSHA: head, Base: "main"}}
	st := openStore(t)
	it, err := st.Create(factory.Item{Kind: factory.KindIssue, Repo: "api", Product: "acme", Num: 9, Autonomy: factory.AutonomyAuto, Cap: 30, Publish: "pr", HeadCommit: head, State: factory.StateNeedsYou, QKind: factory.QKindOutOfAttempts, Question: "verification blocked", Asking: &factory.Asked{Stage: factory.FollowPRStep}, FollowUp: &factory.PRFollowUp{Kind: "pr", State: "needs_you", Attempt: 2, Limit: 3, HeadSHA: head}, PullRequest: &factory.PullRequest{Number: 11, State: "open", HeadSHA: head}, Stream: &factory.Stream{Spent: 18.78, Phases: []factory.Phase{{Name: factory.FollowPRStep, State: factory.PhaseWaiting, Round: 34}}}})
	if err != nil {
		t.Fatal(err)
	}
	src := New(m, nil, nil)
	src.SetVerificationRecovery(func(item factory.Item) bool {
		return factory.ReviewPauseRecoverable(item) && item.FollowUp.RecoveryVersion == 0
	})
	for i := 0; i < 2; i++ {
		if err = followOpened(t.Context(), src, st); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := st.Get(it.ID)
	if got.State != factory.StateQueued || got.FollowUp.Attempt != 3 || got.FollowUp.RecoveryVersion != 1 || got.Stream.Spent != 18.78 {
		t.Fatalf("recovery lost limits or repeated: %+v", got.FollowUp)
	}
}
