package serve

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A viewer who sees only some repositories gets today's spend on them, the
// same day the owner's floor counts, never what those jobs spent on earlier
// days.
func TestANarrowedViewSpendsTodayOnly(t *testing.T) {
	now := time.Date(2026, 11, 1, 15, 0, 0, 0, time.UTC)
	s := factory.Snapshot{Now: now, Items: []factory.Item{
		{ID: 1, Repo: "acme/api", Changed: now.Add(-time.Hour), Stream: &factory.Stream{Spent: 1.5}},
		{ID: 2, Repo: "acme/api", Changed: now.AddDate(0, 0, -1), Stream: &factory.Stream{Spent: 5}},
		{ID: 3, Repo: "acme/web", Changed: now.Add(-time.Hour), Stream: &factory.Stream{Spent: 7}},
	}}
	got := narrowView(s, map[string]bool{"acme/api": true})
	if len(got.Items) != 2 || got.Daily != 1.5 {
		t.Fatalf("narrowed daily = %v over %d jobs, want 1.5 (today's job in acme/api only)", got.Daily, len(got.Items))
	}
}
