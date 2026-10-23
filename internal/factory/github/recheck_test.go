package github

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// standingAPI answers one issue and one pull request and counts every
// request, so a test can hold the recheck to ONE. NOTHING HERE TOUCHES THE
// NETWORK; any other read panics through the nil API.
type standingAPI struct {
	API
	issue    forge.Issue
	pull     forge.Pull
	err      error
	requests int
}

func (s *standingAPI) Issue(context.Context, string, string, int) (forge.Issue, error) {
	s.requests++
	return s.issue, s.err
}

func (s *standingAPI) Pull(context.Context, string, string, int) (forge.Pull, error) {
	s.requests++
	return s.pull, s.err
}

func TestStandingReadsOneItemInOneRequest(t *testing.T) {
	issue := factory.Item{Origin: factory.OriginForge, Num: 12, Kind: factory.KindIssue, Repo: "acme/api"}
	pr := factory.Item{Origin: factory.OriginForge, Num: 13, Kind: factory.KindPR, Repo: "acme/api"}
	cases := []struct {
		name string
		api  standingAPI
		it   factory.Item
		want string
	}{
		{"open issue", standingAPI{issue: forge.Issue{State: "open"}}, issue, ""},
		{"closed issue", standingAPI{issue: forge.Issue{State: "closed"}}, issue, StandingClosed},
		{"open pull request", standingAPI{pull: forge.Pull{State: "open"}}, pr, ""},
		{"closed pull request", standingAPI{pull: forge.Pull{State: "closed"}}, pr, StandingClosed},
		{"merged pull request", standingAPI{pull: forge.Pull{State: "closed", Merged: true}}, pr, StandingMerged},
	}
	for _, c := range cases {
		got, err := Standing(context.Background(), &c.api, c.it)
		if err != nil || got != c.want || c.api.requests != 1 {
			t.Errorf("%s: %q %v after %d requests", c.name, got, err, c.api.requests)
		}
	}
	down := &standingAPI{err: errors.New("dial tcp: no route")}
	if _, err := Standing(context.Background(), down, issue); err == nil {
		t.Fatal("a GitHub that cannot be reached answered a standing")
	}
	typed := factory.Item{Origin: factory.OriginTerminal, Num: 0, Repo: "acme/api"}
	if _, err := Standing(context.Background(), down, typed); !errors.Is(err, factory.ErrNotOnSource) {
		t.Fatalf("an item that is not on GitHub: %v", err)
	}
	if _, err := Rechecker(func(context.Context) string { return "" })(context.Background(), issue); err == nil {
		t.Fatal("a recheck with no token read something")
	}
}
