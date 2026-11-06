package serve

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/route"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// /v1/routes: anyone signed in reads the routing table, the operator replaces
// it whole, a bad table or a stale expect is refused with the phase marker
// and changes nothing, and nobody unsigned gets either.
func TestFactoryRoutesRouteReadsReplacesAndRefusesWithoutSaving(t *testing.T) {
	f := peopleV2Setup(t, "team")
	var got store.KeptRoutes
	f.request(t, "GET", "/v1/routes", "operator-secret", nil, 200, &got)
	if got.Rules == nil || len(got.Rules) != 0 {
		t.Fatalf("a new factory has routes %#v", got)
	}
	table := []route.Rule{{Name: "jira", When: route.Match{Sources: []string{"jira"}}, Then: route.Action{Repo: "acme/api", Start: route.StartAuto}}}
	f.request(t, "PUT", "/v1/routes", "operator-secret", map[string]any{"rules": table}, 200, &got)
	if !reflect.DeepEqual(got.Rules, table) || got.ChangedBy == "" {
		t.Fatalf("saved %#v, want %#v with who changed it", got, table)
	}
	for _, bad := range []map[string]any{
		{},
		{"rules": []map[string]any{{"name": "x", "then": map[string]any{"start": "later"}}}},
		{"rules": []route.Rule{}, "expect": []route.Rule{}},
	} {
		w := identityRequest(f.h, "PUT", "/v1/routes", rulesBody(t, bad), "operator-secret", nil)
		if (w.Code != 422 && w.Code != 409) || w.Header().Get(api.PhaseHeader) != api.PhasePreMutation {
			t.Fatalf("bad body %v answered %d with phase %q: %s", bad, w.Code, w.Header().Get(api.PhaseHeader), w.Body)
		}
	}
	if saved, _ := f.st.Routes(); !reflect.DeepEqual(saved, table) {
		t.Fatalf("a refused write changed the table: %#v", saved)
	}
	f.request(t, "GET", "/v1/routes", "", nil, 401, nil)
	f.request(t, "PUT", "/v1/routes", "", map[string]any{"rules": table}, 401, nil)
}

// /v1/routes/test tries a table on the newest jobs without writing anything.
func TestFactoryRoutesTestTriesATableAndWritesNothing(t *testing.T) {
	f := peopleV2Setup(t, "team")
	made, err := f.st.Create(factory.Item{Repo: "acme/api", Num: 7, Kind: factory.KindIssue, Title: "Typo", Labels: []string{"bug"}, State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	table := []route.Rule{
		{Name: "small", When: route.Match{Labels: []string{"bug"}, Ask: "Is it small?"}, Then: route.Action{Start: route.StartAuto}},
	}
	var got routesTried
	f.request(t, "POST", "/v1/routes/test", "operator-secret", map[string]any{"rules": table, "limit": 5}, 200, &got)
	if len(got.Jobs) != 1 || got.Jobs[0].Job != made.ID || got.Jobs[0].Outcome != "pending" || got.Jobs[0].Rule != "small" {
		t.Fatalf("tried %+v", got)
	}
	if kept, _ := f.st.Routes(); len(kept) != 0 {
		t.Fatalf("a test kept the table: %+v", kept)
	}
	if after, _ := f.st.Get(made.ID); after.Judged != nil || after.Route != "" {
		t.Fatalf("a test wrote on the job: %+v", after)
	}
	f.request(t, "POST", "/v1/routes/test", "operator-secret", map[string]any{}, 422, nil)
	f.request(t, "POST", "/v1/routes/test", "", map[string]any{"rules": table}, 401, nil)
}
