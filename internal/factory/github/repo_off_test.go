package github

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// The poll leaves the pull requests of a repository that is off alone: GitHub
// is not asked about them and nothing is written onto their jobs. Turned back
// on, the pull request is followed from where it stands.
func TestPollDoesNotFollowThePullRequestsOfARepoThatIsOff(t *testing.T) {
	when := time.Date(2026, 10, 7, 9, 41, 0, 0, time.UTC)
	m := newMemAPI(when)
	m.followed = map[string]forge.Pull{
		"acme/web#50": {Number: 50, URL: "https://github.com/acme/web/pull/50", State: "closed", Merged: true, MergedBy: "mei", MergedAt: when, HeadSHA: "sha50", Base: "main"},
	}
	st := openStore(t)
	if err := st.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	job, err := st.Create(factory.Item{
		Repo: "web", Product: "acme", Num: 20, Kind: factory.KindIssue, Title: "work",
		State: factory.StateShipped, Origin: factory.OriginForge,
		PullRequest: &factory.PullRequest{Number: 50, URL: "https://github.com/acme/web/pull/50", State: factory.PROpen},
	})
	if err != nil {
		t.Fatal(err)
	}
	src := New(m, nil, nil)
	poll := func() factory.Item {
		t.Helper()
		if _, err := PollOnce(context.Background(), src, st, "", func() time.Time { return when }); err != nil {
			t.Fatal(err)
		}
		it, err := st.Get(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		return it
	}
	if it := poll(); m.details != 0 || it.PullRequest.State != factory.PROpen {
		t.Fatalf("an off repository's pull request was followed: %d reads, pr %+v", m.details, it.PullRequest)
	}
	if err := st.SetRepos([]string{"acme/api", "acme/web"}); err != nil {
		t.Fatal(err)
	}
	if it := poll(); it.PullRequest.State != factory.PRMerged {
		t.Fatalf("turned back on, the pull request was not followed: %+v", it.PullRequest)
	}
}
