package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// A WORKSTREAM'S PARTS: every method is one request on its own route, with the
// rev it was given as If-Match, the body it was given and nothing more, and
// the server's answer in its shape. The hello says so.
func TestOrgWorkstreamPartsForwardToTheServer(t *testing.T) {
	o, wire, id := attachOrg(t)
	if !strings.Contains(wire.hello, `"org_workstream_parts"`) {
		t.Fatalf("hello lacks org_workstream_parts: %s", wire.hello)
	}
	ws := map[string]any{"id": "ws_1", "name": "W", "rev": 3, "jobs": []int{7}, "budget_usd": 25, "spend_usd": 1.5,
		"pages": []map[string]any{{"id": "pg_1", "title": "One", "rev": 2}}}
	version := map[string]any{"version": 2, "by": "abir", "body_md": "hi", "state": []byte{1}}
	o.answer = func(method, uri string) (int, any) {
		switch {
		case strings.Contains(uri, "/versions") && method == "GET" && strings.HasSuffix(uri, "/versions"):
			return 200, map[string]any{"versions": []any{version}}
		case strings.Contains(uri, "/versions") || strings.HasSuffix(uri, "/restore"):
			return 200, version
		case strings.HasSuffix(uri, "/plan"):
			return 200, map[string]any{"plan": map[string]any{"items": []int{}}, "rev": 1}
		case strings.HasSuffix(uri, "/wakes") && method == "GET":
			return 200, map[string]any{}
		case strings.Contains(uri, "/suggestions") && method == "GET":
			return 200, map[string]any{"suggestions": []any{map[string]any{"id": "sg_1"}}}
		case strings.Contains(uri, "/wakes") || strings.Contains(uri, "/suggestions"):
			return 200, map[string]any{"id": "wk_1", "rev": 2}
		case strings.HasPrefix(uri, "/v1/workstreams/ws_1"):
			return 200, ws
		}
		return 0, nil
	}
	c := func(extra map[string]any) map[string]any {
		out := map[string]any{"connection": id, "id": "ws_1"}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	last := func() (string, string) {
		o.mu.Lock()
		defer o.mu.Unlock()
		return o.asks[len(o.asks)-1], o.ifMatch[len(o.ifMatch)-1]
	}
	for _, tc := range []struct {
		method  string
		params  map[string]any
		ask     string
		ifMatch string
		body    string
	}{
		{"factory.org_workstream_page_create", map[string]any{"title": " Two ", "index": 0}, "POST /v1/workstreams/ws_1/pages", "", `{"index":0,"title":"Two"}`},
		{"factory.org_workstream_page_update", map[string]any{"page": "pg_1", "rev": 2, "title": "Uno"}, "PATCH /v1/workstreams/ws_1/pages/pg_1", "2", `{"title":"Uno"}`},
		{"factory.org_workstream_page_delete", map[string]any{"page": "pg_1"}, "DELETE /v1/workstreams/ws_1/pages/pg_1", "", ``},
		{"factory.org_workstream_page_versions", map[string]any{"page": "pg_1"}, "GET /v1/workstreams/ws_1/pages/pg_1/versions", "", ``},
		{"factory.org_workstream_page_version", map[string]any{"page": "pg_1", "version": 2}, "GET /v1/workstreams/ws_1/pages/pg_1/versions/2", "", ``},
		{"factory.org_workstream_page_snapshot", map[string]any{"page": "pg_1", "body_md": "hi", "reason": "idle"}, "POST /v1/workstreams/ws_1/pages/pg_1/versions", "", `{"body_md":"hi","reason":"idle"}`},
		{"factory.org_workstream_page_restore", map[string]any{"page": "pg_1", "version": 1}, "POST /v1/workstreams/ws_1/pages/pg_1/restore", "", `{"version":1}`},
		{"factory.org_workstream_plan_get", nil, "GET /v1/workstreams/ws_1/plan", "", ``},
		{"factory.org_workstream_plan_set", map[string]any{"rev": 0, "plan": map[string]any{"items": []int{}}}, "PUT /v1/workstreams/ws_1/plan", "0", `{"plan":{"items":[]}}`},
		{"factory.org_workstream_wakes", nil, "GET /v1/workstreams/ws_1/wakes", "", ``},
		{"factory.org_workstream_wake_add", map[string]any{"wake": map[string]any{"state": "running"}}, "POST /v1/workstreams/ws_1/wakes", "", `{"state":"running"}`},
		{"factory.org_workstream_wake_update", map[string]any{"wake_id": "wk_1", "rev": 1, "wake": map[string]any{"state": nil}}, "PATCH /v1/workstreams/ws_1/wakes/wk_1", "1", `{"state":null}`},
		{"factory.org_workstream_suggestions", map[string]any{"page": "pg_1"}, "GET /v1/workstreams/ws_1/suggestions?page=pg_1", "", ``},
		{"factory.org_workstream_suggestion_add", map[string]any{"suggestion": map[string]any{"why": "x"}}, "POST /v1/workstreams/ws_1/suggestions", "", `{"why":"x"}`},
		{"factory.org_workstream_suggestion_update", map[string]any{"suggestion_id": "sg_1", "rev": 1, "suggestion": map[string]any{}}, "PATCH /v1/workstreams/ws_1/suggestions/sg_1", "1", `{}`},
		{"factory.org_workstream_job_add", map[string]any{"job": 7}, "PUT /v1/workstreams/ws_1/jobs/7", "", ``},
		{"factory.org_workstream_job_remove", map[string]any{"job": 7}, "DELETE /v1/workstreams/ws_1/jobs/7", "", ``},
		{"factory.org_workstream_unarchive", nil, "POST /v1/workstreams/ws_1/unarchive", "", ``},
	} {
		var out json.RawMessage
		wire.must(tc.method, c(tc.params), &out)
		ask, ifMatch := last()
		if ask != tc.ask || ifMatch != tc.ifMatch {
			t.Fatalf("%s sent %q If-Match %q; want %q %q", tc.method, ask, ifMatch, tc.ask, tc.ifMatch)
		}
		o.mu.Lock()
		sent := ""
		if o.lastBody != nil {
			raw, _ := json.Marshal(o.lastBody)
			sent = string(raw)
		}
		o.mu.Unlock()
		if sent != tc.body {
			t.Fatalf("%s sent body %s; want %s", tc.method, sent, tc.body)
		}
	}

	var got appwire.OrgWorkstream
	wire.must("factory.org_workstream_job_add", c(map[string]any{"job": 7}), &got)
	if len(got.Jobs) != 1 || got.BudgetUSD == nil || *got.BudgetUSD != 25 || got.SpendUSD != 1.5 || got.Pages[0].Rev != 2 {
		t.Fatalf("the workstream lost its parts: %+v", got)
	}
	var wakes appwire.OrgWorkstreamWakes
	wire.must("factory.org_workstream_wakes", c(nil), &wakes)
	if wakes.Wakes == nil {
		t.Fatal("an absent list came back null")
	}
	var v appwire.OrgPageVersion
	wire.must("factory.org_workstream_page_restore", c(map[string]any{"page": "pg_1", "version": 1}), &v)
	if v.BodyMD != "hi" || len(v.State) != 1 || v.By != "abir" {
		t.Fatalf("restore %+v", v)
	}

	// Refused before anything is sent.
	o.since()
	for _, bad := range []struct {
		method string
		params map[string]any
	}{
		{"factory.org_workstream_page_update", map[string]any{"page": "pg_1"}},
		{"factory.org_workstream_page_update", map[string]any{"page": "../x", "rev": 1}},
		{"factory.org_workstream_plan_set", map[string]any{"rev": 0, "plan": []int{1}}},
		{"factory.org_workstream_page_snapshot", map[string]any{"page": "pg_1", "reason": "whenever"}},
		{"factory.org_workstream_job_add", map[string]any{"job": 0}},
		{"factory.org_workstream_wake_update", map[string]any{"wake_id": "a/b", "rev": 1, "wake": map[string]any{}}},
	} {
		wire.mustFail(bad.method, c(bad.params), appwire.CodeBadParams)
	}
	if asks := o.since(); len(asks) != 0 {
		t.Fatalf("a refused call still reached the server: %v", asks)
	}
}
