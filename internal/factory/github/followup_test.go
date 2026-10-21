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
	it, err := st.Create(factory.Item{Kind: factory.KindIssue, Repo: "api", Product: "acme", Num: 12, State: factory.StateShipped, Publish: "pr", HeadCommit: head, PullRequest: &factory.PullRequest{Number: 7, State: factory.PROpen}, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "proof", State: factory.PhaseDone}}}})
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
