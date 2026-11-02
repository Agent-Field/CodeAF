package serve

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A REPOSITORY'S FACTS ARE READ OVER EVERY JOB THE FACTORY KEEPS, in its own
// zone, with the areas the repository declares, for a caller who can read
// the repository; one who cannot is told it is not found. The window and the
// months of spend are the caller's within their bounds, 30 days and 3 months
// by default.
func TestRepoFactsAnswerOneReadableRepositorysFactsOverItsWindow(t *testing.T) {
	h, clock, g := visibilityFloor(t, map[string]map[string]bool{"priya": {"acme/api": true}})
	priya := signViewer(t, h, g, clock, "priya")
	zone, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skipf("no zone database: %v", err)
	}
	now := time.Date(2026, time.November, 20, 9, 0, 0, 0, zone)
	h.config.Now = func() time.Time { return now }
	snapshot := factory.Snapshot{Repos: []factory.Repo{{Name: "acme/api", Areas: []string{"webhooks"}}, {Name: "acme/web"}}, Items: []factory.Item{
		{ID: 1, Repo: "acme/api", State: factory.StateShipped, Change: &factory.Change{Files: []factory.ChangedFile{{Path: "webhooks/a.go"}}},
			Stream: &factory.Stream{Started: time.Date(2026, time.October, 31, 23, 30, 0, 0, zone), Ended: now.AddDate(0, 0, -15), Spent: 2}},
		{ID: 2, Repo: "acme/web", State: factory.StateShipped, Stream: &factory.Stream{Started: now.AddDate(0, 0, -3), Spent: 5}},
	}}
	h.seam.Load = func() (factory.Snapshot, error) { return snapshot, nil }
	read := func(token, path string, status int) api.RepoFacts {
		t.Helper()
		w := identityRequest(h, "GET", path, "", token, nil)
		if w.Code != status {
			t.Fatalf("GET %s = %d %s", path, w.Code, w.Body)
		}
		var out api.RepoFacts
		if status == 200 && json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("GET %s answered %s", path, w.Body)
		}
		return out
	}
	value := func(facts api.RepoFacts, id string) (float64, bool) {
		for _, f := range facts.Facts {
			if f.ID == id {
				return f.Value, true
			}
		}
		return 0, false
	}

	got := read("floor-secret", "/v1/repos/acme/api/facts", 200)
	if got.Repo != "acme/api" || got.Days != 30 || got.Months != 3 || got.TimeZone != "America/Chicago" || !got.AsOf.Equal(now) {
		t.Fatalf("defaults = %+v", got)
	}
	// 23:30 on 31 October in the factory's zone is already November in UTC:
	// the spend is October's, as factory.spend_by_repo places it.
	if usd, _ := value(got, "usd.month:2026-10"); usd != 2 {
		t.Fatalf("October's spend = %v in %+v", usd, got.Facts)
	}
	if n, ok := value(got, "area:webhooks"); !ok || n != 1 {
		t.Fatalf("declared area fact = %v %v in %+v", n, ok, got.Facts)
	}
	if usd, _ := value(got, "usd.month:2026-11"); usd != 0 {
		t.Fatalf("another repository's spend leaked into November: %v", usd)
	}

	if got := read(priya, "/v1/repos/acme/api/facts?days=7&months=1", 200); got.Days != 7 || got.Months != 1 || len(got.Facts) == 0 {
		t.Fatalf("priya's week = %+v", got)
	} else if _, ok := value(got, "n.finished.7d"); !ok {
		t.Fatalf("a 7-day window has no n.finished.7d: %+v", got.Facts)
	}
	read(priya, "/v1/repos/acme/web/facts", 404)
	for _, bad := range []string{"?days=0", "?days=367", "?days=week", "?months=0", "?months=13"} {
		read("floor-secret", "/v1/repos/acme/api/facts"+bad, 422)
	}
	if w := identityRequest(h, "POST", "/v1/repos/acme/api/facts", "", "floor-secret", nil); w.Code != 405 {
		t.Fatalf("POST = %d", w.Code)
	}
}
