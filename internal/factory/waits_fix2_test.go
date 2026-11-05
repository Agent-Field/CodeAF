package factory

import (
	"strings"
	"testing"
)

// W1, W2, W4: only a merge or a published comment receipt satisfies children.
func TestWaitsClosedIssueBlocks(t *testing.T) {
	child := Item{ID: 2, Repo: "owner/api", WaitsOn: []int{1}}
	for _, parent := range []Item{
		{State: StateShipped, DoneHow: "closed"},
		{State: StateShipped},
		{State: StateShipped, Publish: "comment"},
		{State: StateShipped, DoneHow: "answered"},
		{State: StateShipped, CommentURL: " "},
	} {
		parent.ID, parent.Repo, parent.Num = 1, "owner/api", 1107
		got := EvaluateWaits(child, map[int]Item{1: parent})
		if WaitsSatisfied(parent) || WaitsEndedReason(parent) != "closed without merging" || got.Released || got.Blocked == nil || got.Blocked.Reason != "waits on api#1107, which closed without merging" {
			t.Fatalf("closed parent %+v: rule %+v", parent, got)
		}
	}
	for _, parent := range []Item{
		{State: StateShipped, DoneHow: "answered", Publish: "comment", CommentURL: "https://github.com/owner/api/issues/1107#issuecomment-7"},
		{State: StateShipped, CommentURL: "https://github.com/owner/api/issues/1107#issuecomment-7"},
		{State: StateShipped, DoneHow: "closed", PullRequest: &PullRequest{State: PRMerged}},
	} {
		if !WaitsSatisfied(parent) || WaitsEndedReason(parent) != "" {
			t.Fatalf("published parent did not satisfy: %+v", parent)
		}
	}
	open := Item{ID: 1, State: StateShipped, PullRequest: &PullRequest{State: PROpen}}
	if got := EvaluateWaits(child, map[int]Item{1: open}); WaitsSatisfied(open) || WaitsEndedReason(open) != "" || got.Blocked != nil || got.Released || len(got.WaitingOn) != 1 {
		t.Fatalf("open pull request must keep waiting: %+v", got)
	}
}

// W3, W4, W11: another repository contributes no reference or step label.
func TestWaitsParentWordsAcrossRepositories(t *testing.T) {
	child := Item{Repo: "owner/api", WaitsOn: []int{1}}
	parent := Item{ID: 1, Product: "owner", Repo: "other", Num: 1107, Failure: &Failure{Step: "check"}, Stages: []Stage{{Name: "check", Label: "Private check"}}}
	got := EvaluateWaits(child, map[int]Item{1: parent})
	if got.Blocked == nil || got.Blocked.Reason != "waits on job 1, which failed" {
		t.Fatalf("cross-repository failure = %+v", got.Blocked)
	}
	for _, ending := range []string{"dropped", "closed_locally", "closed"} {
		parent.DoneHow = ending
		got = EvaluateWaits(child, map[int]Item{1: parent})
		if got.Blocked == nil || got.Blocked.Reason != "waits on job 1, which closed without merging" {
			t.Fatalf("cross-repository ending %q = %+v", ending, got.Blocked)
		}
	}
	parent.DoneHow, parent.Product = "", ""
	got = EvaluateWaits(Item{Repo: "api", WaitsOn: []int{1}}, map[int]Item{1: parent})
	if got.Blocked == nil || got.Blocked.Reason != "waits on job 1, which failed" {
		t.Fatalf("different short repositories = %+v", got.Blocked)
	}
	parent.DoneHow, parent.Repo, parent.Product = "", "API", "OWNER"
	got = EvaluateWaits(child, map[int]Item{1: parent})
	if got.Blocked == nil || !strings.Contains(got.Blocked.Reason, "API#1107") || !strings.Contains(got.Blocked.Reason, "failed at Private check") {
		t.Fatalf("same-repository failure = %+v", got.Blocked)
	}
}
