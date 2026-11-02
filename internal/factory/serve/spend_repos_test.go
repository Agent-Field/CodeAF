package serve

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A REPOSITORY'S SPEND IS PLACED ON THE DAY ITS RUN STARTED, in the factory's
// own zone, and a caller sees only the repositories they can read. Job 2
// started at 23:30 on 30 September in the factory's zone, which is already
// 1 October in UTC: it is September's. Job 4 never ran, so its chat's spend is
// placed on the day it was created. A job that spent nothing is not counted.
func TestSpendByRepoPlacesEachJobOnItsRunsStartDayForWhatTheCallerCanSee(t *testing.T) {
	h, clock, g := visibilityFloor(t, map[string]map[string]bool{"priya": {"acme/api": true}})
	priya := signViewer(t, h, g, clock, "priya")
	zone := time.FixedZone("factory", -5*3600)
	at := func(month time.Month, day, hour, minute int) time.Time {
		return time.Date(2026, month, day, hour, minute, 0, 0, zone)
	}
	h.config.Now = func() time.Time { return at(time.November, 1, 9, 0) }
	snapshot := factory.Snapshot{Repos: []factory.Repo{{Name: "acme/api"}, {Name: "acme/web"}}, Items: []factory.Item{
		{ID: 1, Repo: "acme/api", State: factory.StateShipped, Stream: &factory.Stream{Started: at(time.October, 3, 10, 0), Spent: 1.25}},
		{ID: 2, Repo: "acme/api", State: factory.StateShipped, Stream: &factory.Stream{Started: at(time.September, 30, 23, 30), Spent: 2}},
		{ID: 3, Repo: "acme/web", State: factory.StateShipped, Stream: &factory.Stream{Started: at(time.October, 5, 8, 0), Spent: 0.75}},
		{ID: 4, Repo: "acme/api", State: factory.StateNew, Created: at(time.October, 7, 12, 0), ChatSpent: 0.125},
		{ID: 5, Repo: "acme/api", State: factory.StateRunning, Stream: &factory.Stream{Started: at(time.October, 8, 12, 0)}},
		{ID: 6, Repo: "acme/web", State: factory.StateRunning, Stream: &factory.Stream{Started: at(time.November, 1, 1, 0), Spent: 3}},
	}}
	h.seam.Load = func() (factory.Snapshot, error) { return snapshot, nil }
	read := func(token, query string, status int) api.SpendByRepo {
		t.Helper()
		w := identityRequest(h, "GET", "/v1/spend/repos"+query, "", token, nil)
		var out api.SpendByRepo
		if w.Code != status {
			t.Fatalf("GET %s = %d %s", query, w.Code, w.Body)
		}
		if status == 200 && json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("GET %s answered %s", query, w.Body)
		}
		return out
	}
	october := "?from=2026-10-01&to=2026-10-31"
	want := api.SpendByRepo{From: "2026-10-01", To: "2026-10-31", TotalUSD: 2.125, Repos: []api.RepoSpend{
		{Repo: "acme/api", SpentUSD: 1.375, Jobs: 2},
		{Repo: "acme/web", SpentUSD: 0.75, Jobs: 1},
	}}
	if got := read("floor-secret", october, 200); !reflect.DeepEqual(got, want) {
		t.Fatalf("owner's October = %+v, want %+v", got, want)
	}
	want = api.SpendByRepo{From: "2026-10-01", To: "2026-10-31", TotalUSD: 1.375, Repos: []api.RepoSpend{{Repo: "acme/api", SpentUSD: 1.375, Jobs: 2}}}
	if got := read(priya, october, 200); !reflect.DeepEqual(got, want) {
		t.Fatalf("priya's October = %+v, want %+v", got, want)
	}
	want = api.SpendByRepo{From: "2026-09-01", To: "2026-09-30", TotalUSD: 2, Repos: []api.RepoSpend{{Repo: "acme/api", SpentUSD: 2, Jobs: 1}}}
	if got := read("floor-secret", "?from=2026-09-01&to=2026-09-30", 200); !reflect.DeepEqual(got, want) {
		t.Fatalf("September = %+v, want %+v", got, want)
	}
	if got := read("floor-secret", "?from=2025-11-02&to=2026-11-02", 200); got.TotalUSD != 7.125 {
		t.Fatalf("366 days = %+v", got)
	}
	if got := read("floor-secret", "?from=2026-01-01&to=2026-01-31", 200); got.Repos == nil || len(got.Repos) != 0 || got.TotalUSD != 0 {
		t.Fatalf("an empty month = %+v, want repos []", got)
	}
	for _, bad := range []string{"", "?from=2026-10-01", "?to=2026-10-31", "?from=2026-10-31&to=2026-10-01", "?from=2025-11-01&to=2026-11-02", "?from=10/01/2026&to=2026-10-31"} {
		read("floor-secret", bad, 422)
	}
	if w := identityRequest(h, "POST", "/v1/spend/repos"+october, "", "floor-secret", nil); w.Code != 405 {
		t.Fatalf("POST = %d", w.Code)
	}
}
