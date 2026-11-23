package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A followed pull request is additive on Contract 1. done stays the published
// job. pr.state carries open, merged and closed, which is what the design
// calls PR open and Merged.
func TestFollowedPullRequestStaysContract1(t *testing.T) {
	if Contract != 2 {
		t.Fatalf("contract %d", Contract)
	}
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	snap := factory.Fixture(now)
	var shipped factory.Item
	for _, it := range snap.Items {
		if it.PullRequest != nil {
			shipped = it
			break
		}
	}
	j := FromItem(shipped)
	if j.State != StateDone {
		t.Fatalf("job state %q", j.State)
	}
	raw, err := json.Marshal(j.PR)
	if err != nil {
		t.Fatal(err)
	}
	var pr map[string]any
	if err := json.Unmarshal(raw, &pr); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"number", "url", "state", "draft", "merged_by", "merged_at", "review_decision", "reviews", "required_reviews", "checks"} {
		if _, ok := pr[key]; !ok {
			t.Errorf("pr.%s missing from %s", key, raw)
		}
	}
	if pr["state"] != factory.PRMerged {
		t.Fatalf("pr.state %v", pr["state"])
	}
	checks, _ := pr["checks"].(map[string]any)
	if checks["passed"] != float64(12) {
		t.Fatalf("checks %v", pr["checks"])
	}
	reviews, _ := pr["reviews"].([]any)
	if len(reviews) != 1 {
		t.Fatalf("reviews %v", pr["reviews"])
	}
	back := ToItem(j)
	if back.State != factory.StateShipped || back.PullRequest == nil || back.PullRequest.State != factory.PRMerged || back.PullRequest.MergedBy != "mei" {
		t.Fatalf("round trip %+v", back.PullRequest)
	}
	q := FromSnapshot(snap, "santosh")
	if q.Since.MergedToday != 1 || q.Since.MergedWeek != 1 {
		t.Fatalf("since %+v", q.Since)
	}
}
