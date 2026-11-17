package factory

import (
	"testing"
	"time"
)

func TestMergedCountsTodayAndThisWeek(t *testing.T) {
	now := time.Date(2026, 10, 7, 15, 0, 0, 0, time.UTC) // Wednesday
	items := []Item{
		{PullRequest: &PullRequest{State: PRMerged, MergedAt: time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)}},
		{PullRequest: &PullRequest{State: PRMerged, MergedAt: time.Date(2026, 10, 5, 11, 0, 0, 0, time.UTC)}},
		{PullRequest: &PullRequest{State: PRMerged, MergedAt: time.Date(2026, 9, 30, 11, 0, 0, 0, time.UTC)}},
		{PullRequest: &PullRequest{State: PROpen, MergedAt: now}},
		{PullRequest: &PullRequest{State: PRMerged}},
	}
	today, week := MergedCounts(items, now)
	if today != 1 || week != 2 {
		t.Fatalf("merged today %d week %d", today, week)
	}
}
