package api

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE MANAGER SAYS WHAT THE JOB PAGE OFFERS. On the live walk the page showed
// "Merge pull request #177?" while the manager said merge was not offered yet.
// Both now read [factory.MergeOffered], so for every shape here the status
// offers Merge exactly when the page's merge block is not blocked, and gives
// the page's own reason when it is.
func TestTheManagersMergeFactIsTheJobPagesMergeGate(t *testing.T) {
	open := func(edit func(*factory.Item)) factory.Item {
		it := factory.Item{ID: 177, Kind: factory.KindIssue, State: factory.StateShipped, Publish: "pr", HeadCommit: "6212cfc", Assignee: "sam",
			PullRequest: &factory.PullRequest{Number: 177, State: factory.PROpen, HeadSHA: "6212cfc", Mergeable: factory.MergeableYes, ChecksRead: true, NoChecks: true, ReviewsRead: true},
			FollowUp:    &factory.PRFollowUp{Kind: "readiness", State: "healthy", Reason: "PR #177: No GitHub checks reported."}}
		if edit != nil {
			edit(&it)
		}
		return it
	}
	cases := map[string]factory.Item{
		"ready, nothing in the way": open(nil),
		"still a draft":             open(func(it *factory.Item) { it.PullRequest.Draft = true }),
		"checks running":            open(func(it *factory.Item) { it.PullRequest.NoChecks = false; it.PullRequest.Checks.Pending = 1 }),
		"unapproved commit":         open(func(it *factory.Item) { it.PullRequest.HeadSHA = "beef" }),
		"follow running":            open(func(it *factory.Item) { it.FollowUp.State = "running" }),
	}
	for name, it := range cases {
		page := FromItem(it)
		blocked := page.Merge != nil && page.Merge.Blocked != ""
		status := factory.StatusForManager(it, false, time.Now()).Text
		offered := strings.Contains(status, "Waiting on the person: Merge pull request #177?")
		if offered == blocked {
			t.Errorf("%s: page blocked=%v (%q) but the manager offered=%v:\n%s", name, blocked, page.Merge.Blocked, offered, status)
		}
		if blocked && !strings.Contains(status, "Merge is not offered yet: "+page.Merge.Blocked+".") {
			t.Errorf("%s: the manager's reason is not the page's %q:\n%s", name, page.Merge.Blocked, status)
		}
	}
}
