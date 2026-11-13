package serve

import (
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// pageNamed is the page titled title, failing the test when there is none.
func pageNamed(t *testing.T, ws api.Workstream, title string) api.WorkstreamPage {
	t.Helper()
	for _, p := range ws.Pages {
		if p.Title == title {
			return p
		}
	}
	t.Fatalf("no page %q in %+v", title, ws.Pages)
	return api.WorkstreamPage{}
}

// TWO PEOPLE CHANGING TWO DIFFERENT PAGES BOTH SUCCEED; the same page from
// both refuses the second with 412, and someone who can only see the
// workstream from outside cannot change a page at all.
func TestOrgPagesHaveTheirOwnRevs(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	base := "/v1/workstreams/" + webhook.ID
	var ws api.Workstream
	o.do(t, "abir", "POST", base+"/pages", -1, api.PageNew{Title: "Retries"}, 201, &ws)
	o.do(t, "abir", "POST", base+"/pages", -1, api.PageNew{Title: "Alerts", Kind: "runbook"}, 201, &ws)
	retries, alerts := pageNamed(t, ws, "Retries"), pageNamed(t, ws, "Alerts")
	if retries.Rev != 1 || alerts.Kind != "runbook" || retries.UpdatedBy != "abir" {
		t.Fatalf("new pages %+v %+v", retries, alerts)
	}

	// Both read rev 1 of their own page, then both save.
	title := func(s string) *string { return &s }
	o.do(t, "santosh", "PATCH", base+"/pages/"+retries.ID, retries.Rev, api.PagePatch{Title: title("Retries v2")}, 200, nil)
	o.do(t, "abir", "PATCH", base+"/pages/"+alerts.ID, alerts.Rev, api.PagePatch{Title: title("Alerts v2")}, 200, &ws)
	if pageNamed(t, ws, "Retries v2").UpdatedBy != "santosh" || pageNamed(t, ws, "Alerts v2").Rev != 2 {
		t.Fatalf("both saves kept: %+v", ws.Pages)
	}
	// The same page from a stale read is refused.
	o.do(t, "abir", "PATCH", base+"/pages/"+retries.ID, retries.Rev, api.PagePatch{Title: title("mine")}, 412, nil)
	// Without If-Match nothing is changed.
	o.do(t, "abir", "PATCH", base+"/pages/"+retries.ID, -1, api.PagePatch{Title: title("mine")}, 428, nil)

	// Move Alerts under Retries and to the top; a page under itself is refused.
	parent, zero := retries.ID, 0
	o.do(t, "abir", "PATCH", base+"/pages/"+alerts.ID, 2, api.PagePatch{Parent: &parent, Index: &zero}, 200, &ws)
	if ws.Pages[0].ID != alerts.ID || ws.Pages[0].Parent != retries.ID {
		t.Fatalf("moved %+v", ws.Pages)
	}
	child := alerts.ID
	o.do(t, "abir", "PATCH", base+"/pages/"+retries.ID, 2, api.PagePatch{Parent: &child}, 422, nil)

	// Deleting Retries keeps Alerts, moved up to the top.
	// Decoded into a fresh value: an omitted parent must not keep the old one.
	ws = api.Workstream{}
	o.do(t, "santosh", "DELETE", base+"/pages/"+retries.ID, -1, nil, 200, &ws)
	if len(ws.Pages) != 1 || ws.Pages[0].ID != alerts.ID || ws.Pages[0].Parent != "" {
		t.Fatalf("after delete %+v", ws.Pages)
	}
	o.do(t, "santosh", "DELETE", base+"/pages/"+retries.ID, -1, nil, 404, nil)

	// mia (Growth, shared) writes; the owner of nothing here cannot see it.
	var launch api.Workstream
	o.do(t, "santosh", "GET", "/v1/workstreams", -1, nil, 200, nil)
	launch = o.create(t, "santosh", api.WorkstreamNew{Name: "Private launch"})
	o.do(t, "abir", "POST", "/v1/workstreams/"+launch.ID+"/pages", -1, api.PageNew{Title: "x"}, 404, nil)
}

// VERSIONS NAME WHO SAVED THEM; the same words twice are kept once, and a
// restore is a new version by the restorer that answers the words to apply.
func TestOrgPageVersionsAndRestore(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	base := "/v1/workstreams/" + webhook.ID
	var ws api.Workstream
	o.do(t, "abir", "POST", base+"/pages", -1, api.PageNew{Title: "Retries"}, 201, &ws)
	page := pageNamed(t, ws, "Retries")
	pbase := base + "/pages/" + page.ID

	var v api.PageVersion
	o.do(t, "santosh", "POST", pbase+"/versions", -1, api.PageVersionNew{BodyMD: "first", State: []byte{1, 2}, Reason: "idle"}, 201, &v)
	if v.Version != 1 || v.By != "santosh" {
		t.Fatalf("v1 %+v", v)
	}
	o.do(t, "abir", "POST", pbase+"/versions", -1, api.PageVersionNew{BodyMD: "first", State: []byte{1, 2}, Reason: "idle"}, 200, &v)
	if v.Version != 1 || v.By != "santosh" {
		t.Fatalf("the same words were kept twice: %+v", v)
	}
	o.do(t, "abir", "POST", pbase+"/versions", -1, api.PageVersionNew{BodyMD: "second", Reason: "manual"}, 201, &v)
	o.do(t, "abir", "POST", pbase+"/versions", -1, api.PageVersionNew{BodyMD: "x", Reason: "whenever"}, 422, nil)

	// The page's mirror follows the newest words without moving its rev.
	var after api.Workstream
	o.do(t, "mia", "GET", base, -1, nil, 200, &after)
	if p := pageNamed(t, after, "Retries"); p.BodyMD != "second" || p.Rev != page.Rev || after.Rev != ws.Rev {
		t.Fatalf("mirror %+v rev %d, was %d", p, after.Rev, ws.Rev)
	}

	o.do(t, "mia", "POST", pbase+"/restore", -1, api.PageRestore{Version: 1}, 201, &v)
	if v.Version != 3 || v.By != "mia" || v.RestoredFrom != 1 || v.BodyMD != "first" || len(v.State) != 2 || v.Reason != "restore" {
		t.Fatalf("restore %+v", v)
	}
	var list api.PageVersions
	o.do(t, "abir", "GET", pbase+"/versions", -1, nil, 200, &list)
	if len(list.Versions) != 3 {
		t.Fatalf("versions %+v", list)
	}
	for i, want := range []string{"mia", "abir", "santosh"} {
		if list.Versions[i].By != want || list.Versions[i].BodyMD != "" {
			t.Fatalf("version %d %+v", i, list.Versions[i])
		}
	}
	o.do(t, "abir", "GET", pbase+"/versions/2", -1, nil, 200, &v)
	if v.BodyMD != "second" {
		t.Fatal(v)
	}
	o.do(t, "abir", "POST", pbase+"/restore", -1, api.PageRestore{Version: 9}, 404, nil)
}

// THE PLAN, WAKES AND SUGGESTIONS keep their own revs and the server's
// authors, whatever the body says.
func TestOrgPlanWakesAndSuggestions(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	base := "/v1/workstreams/" + webhook.ID

	var plan api.WorkstreamPlan
	o.do(t, "abir", "GET", base+"/plan", -1, nil, 200, &plan)
	if string(plan.Plan) != "null" || plan.Rev != 0 {
		t.Fatalf("no plan yet %+v", plan)
	}
	o.do(t, "santosh", "PUT", base+"/plan", 0, map[string]any{"plan": map[string]any{"items": []int{1}}}, 200, &plan)
	if plan.Rev != 1 || plan.UpdatedBy != "santosh" {
		t.Fatal(plan)
	}
	o.do(t, "abir", "PUT", base+"/plan", 0, map[string]any{"plan": map[string]any{}}, 412, nil)
	o.do(t, "abir", "PUT", base+"/plan", 1, map[string]any{"plan": []int{1}}, 422, nil)

	var wake map[string]any
	o.do(t, "abir", "POST", base+"/wakes", -1, map[string]any{"by": "santosh", "state": "running", "items": []any{}}, 201, &wake)
	if wake["by"] != "abir" || wake["state"] != "running" || wake["rev"] != float64(1) || wake["workstream_id"] != webhook.ID {
		t.Fatalf("wake %+v", wake)
	}
	id := wake["id"].(string)
	wake = nil
	o.do(t, "santosh", "PATCH", base+"/wakes/"+id, 1, map[string]any{"state": "settled", "items": nil}, 200, &wake)
	if wake["state"] != "settled" || wake["updated_by"] != "santosh" || wake["by"] != "abir" || wake["items"] != nil {
		t.Fatalf("patched %+v", wake)
	}
	o.do(t, "abir", "PATCH", base+"/wakes/"+id, 1, map[string]any{"state": "failed"}, 412, nil)

	o.do(t, "abir", "POST", base+"/suggestions", -1, map[string]any{"page_id": "pg_a", "why": "x"}, 201, nil)
	o.do(t, "abir", "POST", base+"/suggestions", -1, map[string]any{"page_id": "pg_b", "why": "y"}, 201, nil)
	var list api.WorkstreamSuggestions
	o.do(t, "mia", "GET", base+"/suggestions?page=pg_b", -1, nil, 200, &list)
	if len(list.Suggestions) != 1 {
		t.Fatal(list)
	}
	var wakes api.WorkstreamWakes
	o.do(t, "mia", "GET", base+"/wakes", -1, nil, 200, &wakes)
	if len(wakes.Wakes) != 1 {
		t.Fatal(wakes)
	}
}

// A JOB ADDED BY SANTOSH SHOWS IN ABIR'S WORKSTREAM, and a job is in one
// workstream at a time.
func TestOrgWorkstreamJobsAreMembership(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, pricing, _ := o.orgScene(t)
	it, err := o.st.Create(factory.Item{Repo: "acme/api", Title: "fix retries", State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	job := strconv.Itoa(it.ID)
	var ws api.Workstream
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+pricing.ID+"/jobs/"+job, -1, nil, 200, &ws)
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+webhook.ID+"/jobs/"+job, -1, nil, 200, &ws)
	if len(ws.Jobs) != 1 || ws.Jobs[0] != it.ID || ws.Total != 1 {
		t.Fatalf("jobs %+v total %d", ws.Jobs, ws.Total)
	}
	o.do(t, "abir", "GET", "/v1/workstreams/"+webhook.ID, -1, nil, 200, &ws)
	if len(ws.Jobs) != 1 {
		t.Fatalf("abir sees %+v", ws.Jobs)
	}
	o.do(t, "mia", "GET", "/v1/workstreams/"+pricing.ID, -1, nil, 200, &ws)
	if len(ws.Jobs) != 0 {
		t.Fatalf("the job stayed in pricing too: %+v", ws.Jobs)
	}
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+webhook.ID+"/jobs/99999", -1, nil, 404, nil)
	o.do(t, "abir", "DELETE", "/v1/workstreams/"+webhook.ID+"/jobs/"+job, -1, nil, 200, &ws)
	if len(ws.Jobs) != 0 {
		t.Fatal(ws.Jobs)
	}
}

// UNARCHIVE BRINGS A WORKSTREAM BACK; a budget is set and cleared; spend is
// posted as the caller and counted once per key; dropped decisions stay as
// superseded.
func TestOrgUnarchiveBudgetSpendAndSupersede(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	base := "/v1/workstreams/" + webhook.ID
	o.do(t, "abir", "DELETE", base, -1, nil, 204, nil)
	o.do(t, "abir", "POST", base+"/pages", -1, api.PageNew{Title: "x"}, 409, nil)
	var ws api.Workstream
	o.do(t, "abir", "POST", base+"/unarchive", -1, nil, 200, &ws)
	if ws.ArchivedAt != "" {
		t.Fatal("still archived")
	}
	budget := 25.0
	o.do(t, "abir", "PATCH", base, ws.Rev, api.WorkstreamPatch{
		BudgetUSD: &budget,
		Decisions: &[]api.WorkstreamDecision{{Text: "use retries"}, {Text: "page on-call"}},
	}, 200, &ws)
	if ws.BudgetUSD == nil || *ws.BudgetUSD != 25 || len(ws.Decisions) != 2 {
		t.Fatalf("budget %+v decisions %+v", ws.BudgetUSD, ws.Decisions)
	}
	keep := ws.Decisions[:1]
	o.do(t, "abir", "PATCH", base, ws.Rev, api.WorkstreamPatch{Decisions: &keep}, 200, &ws)
	if len(ws.Decisions) != 2 || ws.Decisions[1].State != "superseded" {
		t.Fatalf("a dropped decision was deleted: %+v", ws.Decisions)
	}

	post := func(who, key string, usd float64, status int) api.WorkstreamSpend {
		t.Helper()
		var out api.WorkstreamSpend
		body, _ := json.Marshal(map[string]any{"usd": usd, "chat": "c1"})
		var raw json.RawMessage = body
		o.doKey(t, who, base+"/spend", key, raw, status, &out)
		return out
	}
	if s := post("santosh", "k1", 1.25, 200); s.SpendUSD != 1.25 || *s.BudgetUSD != 25 {
		t.Fatal(s)
	}
	if s := post("santosh", "k1", 1.25, 200); s.SpendUSD != 1.25 {
		t.Fatal("one key counted twice", s)
	}
	post("abir", "", -1, 422)
	o.do(t, "mia", "GET", base, -1, nil, 200, &ws)
	if ws.SpendUSD != 1.25 {
		t.Fatal(ws.SpendUSD)
	}
}

// doKey posts body as who with an Idempotency-Key.
func (o *wsOrg) doKey(t *testing.T, who, path, key string, body json.RawMessage, status int, out any) {
	t.Helper()
	r := httptest.NewRequest("POST", path, strings.NewReader(string(body)))
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Authorization", "Bearer "+o.tokens[who])
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	o.h.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s POST %s: %d %s; want %d", who, path, w.Code, w.Body, status)
	}
	if out != nil && status < 300 {
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatal(err)
		}
	}
}

// A VIEW SHARE READS EVERY PART AND CHANGES NONE.
func TestOrgPartsFollowTheRole(t *testing.T) {
	o := newWSOrg(t)
	launch := o.create(t, "santosh", api.WorkstreamNew{Name: "Launch plan"})
	if _, err := o.st.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		w := all[launch.ID]
		w.Shares = []factory.Share{{Kind: "person", ID: "abir", Role: "view"}}
		all[launch.ID] = w
		return []string{launch.ID}, nil
	}); err != nil {
		t.Fatal(err)
	}
	base := "/v1/workstreams/" + launch.ID
	var ws api.Workstream
	o.do(t, "santosh", "POST", base+"/pages", -1, api.PageNew{Title: "Brief"}, 201, &ws)
	o.do(t, "abir", "GET", base+"/plan", -1, nil, 200, nil)
	o.do(t, "abir", "GET", base+"/pages/"+ws.Pages[0].ID+"/versions", -1, nil, 200, nil)
	o.do(t, "abir", "POST", base+"/pages", -1, api.PageNew{Title: "mine"}, 403, nil)
	o.do(t, "abir", "PUT", base+"/plan", 0, map[string]any{"plan": map[string]any{}}, 403, nil)
	o.do(t, "abir", "POST", base+"/wakes", -1, map[string]any{}, 403, nil)
	o.do(t, "abir", "POST", base+"/pages/"+ws.Pages[0].ID+"/versions", -1, api.PageVersionNew{BodyMD: "x", Reason: "idle"}, 403, nil)
	o.do(t, "mia", "GET", base+"/plan", -1, nil, 404, nil)
}
