package serve

import (
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

// wsOrg is a team factory with the org scene's people signed in.
type wsOrg struct {
	*peopleV2Fixture
	tokens map[string]string
}

func newWSOrg(t *testing.T) *wsOrg {
	t.Helper()
	f := peopleV2Setup(t, api.FactoryTeam)
	// The desk's org scene, kept the way the teams routes keep it: santosh is
	// in Platform and Growth, abir in Platform, mia in Growth and Design.
	if err := f.st.ChangeTeams(func(all *[]store.Team) error {
		*all = append(*all,
			store.Team{ID: "tm_platform", Name: "Platform", Members: []string{"abir", "santosh"}},
			store.Team{ID: "tm_growth", Name: "Growth", Members: []string{"mia", "santosh"}},
			store.Team{ID: "tm_design", Name: "Design", Members: []string{"mia"}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	g := &fakeGitHub{t: t}
	f.h.config.GitHubClientID, f.h.config.GitHubHTTPClient = "client", g.client()
	o := &wsOrg{peopleV2Fixture: f, tokens: map[string]string{}}
	for _, who := range []string{"owner", "santosh", "abir", "mia"} {
		row, err := tokens.Mint()
		if err != nil {
			t.Fatal(err)
		}
		if err := tokens.Add(f.cfg.TokensPath, row); err != nil {
			t.Fatal(err)
		}
		o.tokens[who] = signInHandle(t, f.h, g, &f.now, who, row.Token)
	}
	return o
}

// do sends one request as who, with If-Match when rev is not negative, and
// fails the test unless it answers status.
func (o *wsOrg) do(t *testing.T, who, method, path string, rev int, body any, status int, out any) {
	t.Helper()
	text := ""
	if body != nil {
		data, _ := json.Marshal(body)
		text = string(data)
	}
	r := httptest.NewRequest(method, path, strings.NewReader(text))
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Authorization", "Bearer "+o.tokens[who])
	if rev >= 0 {
		r.Header.Set("If-Match", strconv.Itoa(rev))
	}
	w := httptest.NewRecorder()
	o.h.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s %s: %d %s; want %d", who, method, path, w.Code, w.Body, status)
	}
	if out != nil {
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatal(err)
		}
	}
}

func (o *wsOrg) create(t *testing.T, who string, body api.WorkstreamNew) api.Workstream {
	t.Helper()
	var ws api.Workstream
	o.do(t, who, "POST", "/v1/workstreams", -1, body, 201, &ws)
	return ws
}

func (o *wsOrg) names(t *testing.T, who string, query string) []string {
	t.Helper()
	var list api.WorkstreamList
	o.do(t, who, "GET", "/v1/workstreams"+query, -1, nil, 200, &list)
	var out []string
	for _, ws := range list.Workstreams {
		out = append(out, ws.Name)
	}
	return out
}

// orgScene makes the four workstreams of the desk's fixture.
func (o *wsOrg) orgScene(t *testing.T) (webhook, launch, pricing, brand api.Workstream) {
	t.Helper()
	webhook = o.create(t, "abir", api.WorkstreamNew{Name: "Webhook reliability", Owner: &api.WorkstreamOwner{Kind: "team", ID: "tm_platform"}})
	var shared api.WorkstreamShared
	o.do(t, "abir", "PUT", "/v1/workstreams/"+webhook.ID+"/shares", -1, api.WorkstreamSharesPut{Teams: []string{"tm_growth"}}, 200, &shared)
	webhook = shared.Workstream
	o.now = o.now.Add(1)
	launch = o.create(t, "santosh", api.WorkstreamNew{Name: "Launch plan"})
	o.now = o.now.Add(1)
	pricing = o.create(t, "mia", api.WorkstreamNew{Name: "Pricing page", Owner: &api.WorkstreamOwner{Kind: "team", ID: "tm_growth"}})
	o.now = o.now.Add(1)
	brand = o.create(t, "mia", api.WorkstreamNew{Name: "Brand refresh", Owner: &api.WorkstreamOwner{Kind: "team", ID: "tm_design"}})
	return
}

// EACH PERSON SEES WHAT THEY OWN, WHAT THEIR TEAM OWNS AND WHAT IS SHARED WITH
// A TEAM THEY ARE IN, newest first; the operator sees all of it; and one they
// cannot see answers exactly as one that does not exist.
func TestOrgWorkstreamsAreSeenByOwnerTeamsAndShares(t *testing.T) {
	o := newWSOrg(t)
	webhook, launch, _, brand := o.orgScene(t)
	if webhook.Owner != (api.WorkstreamOwner{Kind: "team", ID: "tm_platform"}) || !slices.Equal(webhook.SharedWith, []string{"tm_growth"}) || webhook.CreatedBy != "abir" || !strings.HasPrefix(webhook.ID, "ws_") {
		t.Fatalf("webhook = %+v", webhook)
	}
	if launch.Owner != (api.WorkstreamOwner{Kind: "person", ID: "santosh"}) || launch.Stage != "shaping" || launch.Rev != 1 || launch.Pages == nil || launch.GoAheads == nil {
		t.Fatalf("launch = %+v", launch)
	}
	for who, want := range map[string][]string{
		"santosh": {"Pricing page", "Launch plan", "Webhook reliability"},
		"abir":    {"Webhook reliability"},
		"mia":     {"Brand refresh", "Pricing page", "Webhook reliability"},
		"owner":   {"Brand refresh", "Pricing page", "Launch plan", "Webhook reliability"},
	} {
		if got := o.names(t, who, ""); !slices.Equal(got, want) {
			t.Fatalf("%s sees %v, want %v", who, got, want)
		}
	}
	o.do(t, "santosh", "GET", "/v1/workstreams/"+brand.ID, -1, nil, 404, nil)
	o.do(t, "santosh", "GET", "/v1/workstreams/ws_nothing", -1, nil, 404, nil)
	o.do(t, "owner", "GET", "/v1/workstreams/"+brand.ID, -1, nil, 200, nil)
	// You may make one owned by a team you are in, never by one you are not.
	o.do(t, "abir", "POST", "/v1/workstreams", -1, api.WorkstreamNew{Name: "Nope", Owner: &api.WorkstreamOwner{Kind: "team", ID: "tm_design"}}, 403, nil)
	o.do(t, "abir", "POST", "/v1/workstreams", -1, api.WorkstreamNew{Name: "Nope", Owner: &api.WorkstreamOwner{Kind: "team", ID: "tm_nowhere"}}, 422, nil)
	o.do(t, "abir", "POST", "/v1/workstreams", -1, api.WorkstreamNew{Name: "  "}, 422, nil)
	// A team that owns a workstream cannot be deleted.
	o.do(t, "owner", "DELETE", "/v1/teams/tm_growth", -1, nil, 409, nil)
}

// A PATCH NEEDS THE REV IT READ: the right one saves and counts it up, a stale
// one is refused with 412 and changes nothing, and an array that is sent
// replaces the one kept. The operator sees every workstream but writes none it
// is not in.
func TestOrgWorkstreamPatchNeedsTheRevItRead(t *testing.T) {
	o := newWSOrg(t)
	webhook, launch, _, _ := o.orgScene(t)
	path := "/v1/workstreams/" + webhook.ID
	o.do(t, "mia", "PATCH", path, -1, map[string]any{"purpose": "x"}, 428, nil)
	var got api.Workstream
	pages := []api.WorkstreamPage{{Title: "Overview", Kind: "overview", BodyMD: "# Why"}, {Title: "Runbook", Kind: "runbook"}}
	o.do(t, "mia", "PATCH", path, webhook.Rev, map[string]any{"purpose": "Stop dropping webhooks", "stage": "building", "pages": pages}, 200, &got)
	if got.Rev != webhook.Rev+1 || got.Purpose != "Stop dropping webhooks" || got.Stage != "building" || len(got.Pages) != 2 || got.Pages[0].ID == "" || got.Pages[0].UpdatedBy != "mia" || got.Name != "Webhook reliability" {
		t.Fatalf("patched = %+v", got)
	}
	o.do(t, "santosh", "PATCH", path, webhook.Rev, map[string]any{"purpose": "lost"}, 412, nil)
	o.do(t, "santosh", "PATCH", path, got.Rev, map[string]any{"pages": []api.WorkstreamPage{got.Pages[1]}, "decisions": []api.WorkstreamDecision{{Text: "Retry for a day"}}}, 200, &got)
	if len(got.Pages) != 1 || got.Pages[0].Title != "Runbook" || got.Pages[0].UpdatedBy != "mia" || got.Purpose != "Stop dropping webhooks" || len(got.Decisions) != 1 || got.Decisions[0].State != "proposed" || got.Decisions[0].By != "santosh" {
		t.Fatalf("replaced = %+v", got)
	}
	o.do(t, "santosh", "PATCH", path, got.Rev, map[string]any{"stage": "sideways"}, 422, nil)
	o.do(t, "santosh", "PATCH", path, got.Rev, map[string]any{"links": []string{"ws_elsewhere"}}, 422, nil)
	o.do(t, "santosh", "PATCH", path, got.Rev, map[string]any{"pages": []api.WorkstreamPage{{Title: "Orphan", Parent: "pg_nobody"}}}, 422, nil)
	o.do(t, "owner", "PATCH", "/v1/workstreams/"+launch.ID, launch.Rev, map[string]any{"purpose": "x"}, 403, nil)
	o.do(t, "abir", "PATCH", "/v1/workstreams/"+launch.ID, launch.Rev, map[string]any{"purpose": "x"}, 404, nil)
}

// ONLY THE OWNER, THE OWNING TEAM OR THE OPERATOR CHANGES WHO OWNS OR SEES A
// WORKSTREAM; a team it is shared with writes it but cannot share it on.
func TestOrgWorkstreamOwnerAndSharesNeedTheOwner(t *testing.T) {
	o := newWSOrg(t)
	webhook, launch, _, _ := o.orgScene(t)
	o.do(t, "mia", "PUT", "/v1/workstreams/"+webhook.ID+"/shares", -1, api.WorkstreamSharesPut{Teams: []string{"tm_design"}}, 403, nil)
	o.do(t, "mia", "PUT", "/v1/workstreams/"+webhook.ID+"/owner", -1, api.WorkstreamOwner{Kind: "team", ID: "tm_growth"}, 403, nil)
	var ws api.Workstream
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+launch.ID+"/owner", -1, api.WorkstreamOwner{Kind: "team", ID: "tm_growth"}, 200, &ws)
	if ws.Owner.ID != "tm_growth" || ws.Rev != launch.Rev+1 {
		t.Fatalf("owner = %+v", ws)
	}
	if got := o.names(t, "mia", ""); !slices.Contains(got, "Launch plan") {
		t.Fatalf("mia does not see the workstream Growth now owns: %v", got)
	}
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+launch.ID+"/owner", -1, api.WorkstreamOwner{Kind: "team", ID: "tm_nowhere"}, 422, nil)
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+launch.ID+"/owner", -1, api.WorkstreamOwner{Kind: "robot", ID: "x"}, 422, nil)
	o.do(t, "owner", "PUT", "/v1/workstreams/"+launch.ID+"/owner", -1, api.WorkstreamOwner{Kind: "person", ID: "@Abir"}, 200, &ws)
	if ws.Owner != (api.WorkstreamOwner{Kind: "person", ID: "abir"}) {
		t.Fatalf("operator's owner change = %+v", ws.Owner)
	}
	// A share with the owning team is dropped: it already sees it.
	var shared api.WorkstreamShared
	o.do(t, "abir", "PUT", "/v1/workstreams/"+webhook.ID+"/shares", -1, api.WorkstreamSharesPut{Teams: []string{"tm_platform", "tm_design", "tm_design"}}, 200, &shared)
	if !slices.Equal(shared.Workstream.SharedWith, []string{"tm_design"}) || len(shared.AlsoShared) != 0 {
		t.Fatalf("shares = %+v", shared)
	}
	o.do(t, "abir", "PUT", "/v1/workstreams/"+webhook.ID+"/shares", -1, api.WorkstreamSharesPut{Teams: []string{}}, 200, &shared)
	if got := o.names(t, "mia", ""); slices.Contains(got, "Webhook reliability") || shared.Workstream.SharedWith == nil {
		t.Fatalf("mia still sees the workstream Growth was taken off: %v", got)
	}
}

// SHARING ASKS FIRST ABOUT THE LINKED ONES: the preview names the linked
// workstreams the caller may share that a chosen team does not see yet, and
// include_linked shares exactly those, adding to their shares.
func TestOrgWorkstreamSharesIncludeLinkedOnes(t *testing.T) {
	o := newWSOrg(t)
	webhook, launch, pricing, _ := o.orgScene(t)
	var ws api.Workstream
	o.do(t, "santosh", "PATCH", "/v1/workstreams/"+launch.ID, launch.Rev, map[string]any{"links": []string{webhook.ID, pricing.ID}}, 200, &ws)
	var preview api.WorkstreamSharePreview
	o.do(t, "santosh", "GET", "/v1/workstreams/"+launch.ID+"/shares/preview?teams=tm_platform", -1, nil, 200, &preview)
	// Webhook is Platform's already; Pricing is Growth's, and santosh is in Growth.
	if len(preview.Linked) != 1 || preview.Linked[0].ID != pricing.ID {
		t.Fatalf("preview = %+v", preview)
	}
	o.do(t, "santosh", "GET", "/v1/workstreams/"+launch.ID+"/shares/preview?teams=tm_nowhere", -1, nil, 422, nil)
	var shared api.WorkstreamShared
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+launch.ID+"/shares", -1, api.WorkstreamSharesPut{Teams: []string{"tm_platform"}}, 200, &shared)
	if len(shared.AlsoShared) != 0 {
		t.Fatalf("shared linked ones without being asked: %+v", shared)
	}
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+launch.ID+"/shares", -1, api.WorkstreamSharesPut{Teams: []string{"tm_platform"}, IncludeLinked: true}, 200, &shared)
	if !slices.Equal(shared.AlsoShared, []string{pricing.ID}) || !slices.Equal(shared.Workstream.SharedWith, []string{"tm_platform"}) {
		t.Fatalf("include_linked = %+v", shared)
	}
	got := o.names(t, "abir", "")
	slices.Sort(got)
	if !slices.Equal(got, []string{"Launch plan", "Pricing page", "Webhook reliability"}) {
		t.Fatalf("abir sees %v", got)
	}
	o.do(t, "santosh", "GET", "/v1/workstreams/"+launch.ID+"/shares/preview?teams=tm_platform", -1, nil, 200, &preview)
	if len(preview.Linked) != 0 {
		t.Fatalf("preview after sharing = %+v", preview)
	}
}

// DELETE ARCHIVES: the workstream leaves the list, keeps its words, refuses
// writes, and only the operator can remove it for good.
func TestOrgWorkstreamDeleteArchivesAndOnlyTheOperatorPurges(t *testing.T) {
	o := newWSOrg(t)
	_, launch, _, _ := o.orgScene(t)
	path := "/v1/workstreams/" + launch.ID
	o.do(t, "santosh", "DELETE", path, -1, nil, 204, nil)
	o.do(t, "santosh", "DELETE", path, -1, nil, 204, nil)
	if got := o.names(t, "santosh", ""); slices.Contains(got, "Launch plan") {
		t.Fatalf("archived workstream listed: %v", got)
	}
	if got := o.names(t, "santosh", "?archived=1"); !slices.Contains(got, "Launch plan") {
		t.Fatalf("archived workstream missing from ?archived=1: %v", got)
	}
	var ws api.Workstream
	o.do(t, "santosh", "GET", path, -1, nil, 200, &ws)
	if ws.ArchivedAt == "" || ws.Rev != launch.Rev+1 {
		t.Fatalf("archived = %+v", ws)
	}
	o.do(t, "santosh", "PATCH", path, ws.Rev, map[string]any{"purpose": "x"}, 409, nil)
	o.do(t, "santosh", "DELETE", path+"?purge=1", -1, nil, 403, nil)
	o.do(t, "owner", "DELETE", path+"?purge=1", -1, nil, 204, nil)
	o.do(t, "owner", "GET", path, -1, nil, 404, nil)
}

// A PERSONAL WORKSTREAM MOVES IN ONCE: the mover owns the copy, links to the
// laptop's other workstreams are dropped, and moving it again answers the copy
// already made.
func TestOrgWorkstreamImportIsOneShot(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	pkg := api.WorkstreamPackage{
		FromLocalID: "local-7", Name: "Moved in", Stage: "ongoing", Purpose: "p",
		Pages:     []api.WorkstreamPage{{ID: "p1", Title: "Overview", Kind: "overview", BodyMD: "x", UpdatedAt: "2026-11-01T00:00:00Z", UpdatedBy: "santosh"}},
		Decisions: []api.WorkstreamDecision{{ID: "d1", Text: "Ship it", State: "kept", At: "2026-11-01T00:00:00Z", By: "santosh"}},
		Sources:   []api.WorkstreamSource{{ID: "s1", Kind: "repo", Ref: "acme/api", Title: "API"}},
		Links:     []string{"local-8", webhook.ID},
	}
	var ws api.Workstream
	o.do(t, "santosh", "POST", "/v1/workstreams/import", -1, pkg, 201, &ws)
	if ws.Owner != (api.WorkstreamOwner{Kind: "person", ID: "santosh"}) || !slices.Equal(ws.Links, []string{webhook.ID}) || ws.Pages[0].UpdatedAt != "2026-11-01T00:00:00Z" || ws.Decisions[0].ID != "d1" || ws.Stage != "ongoing" || ws.CreatedBy != "santosh" {
		t.Fatalf("imported = %+v", ws)
	}
	var again api.Workstream
	o.do(t, "santosh", "POST", "/v1/workstreams/import", -1, pkg, 200, &again)
	if again.ID != ws.ID {
		t.Fatalf("second import made %s beside %s", again.ID, ws.ID)
	}
	o.do(t, "santosh", "POST", "/v1/workstreams/import", -1, api.WorkstreamPackage{Name: "No origin"}, 422, nil)
}

// EVERY CHANGE IS PUBLISHED, AND ONLY TO WHO COULD SEE IT: a team a workstream
// was just taken from still hears it changed, and someone who never saw it
// hears nothing. The stream carries kind and id only.
func TestOrgWorkstreamEventsReachOnlyItsPeople(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, brand := o.orgScene(t)
	o.do(t, "abir", "PUT", "/v1/workstreams/"+webhook.ID+"/shares", -1, api.WorkstreamSharesPut{Teams: []string{}}, 200, nil)
	o.do(t, "mia", "DELETE", "/v1/workstreams/"+brand.ID, -1, nil, 204, nil)
	events, _, err := live.Read(o.st.Root())
	if err != nil {
		t.Fatal(err)
	}
	var last, removed live.Event
	for _, e := range events {
		switch e.Kind {
		case api.EventWorkstreamChanged:
			last = e
		case api.EventWorkstreamRemoved:
			removed = e
		}
	}
	sees := func(who string, e live.Event) (string, bool) {
		r := httptest.NewRequest("GET", "/v1/events", nil)
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("Authorization", "Bearer "+o.tokens[who])
		out, ok := o.h.liveForViewer(r, e)
		return string(out.Data), ok
	}
	data, ok := sees("santosh", last)
	if !ok || data != `{"kind":"workstream.changed","id":"`+webhook.ID+`"}` {
		t.Fatalf("santosh, just taken off the share: %q %v", data, ok)
	}
	if _, ok := sees("santosh", removed); ok {
		t.Fatal("santosh heard about Design's workstream")
	}
	if data, ok := sees("mia", removed); !ok || data != `{"kind":"workstream.removed","id":"`+brand.ID+`"}` {
		t.Fatalf("mia's removal: %q %v", data, ok)
	}
	if _, ok := sees("owner", removed); !ok {
		t.Fatal("the operator missed a removal")
	}
}

// APPROVING A GO-AHEAD FOR AN ORGANIZATION WORKSTREAM NEEDS WRITE ACCESS TO IT,
// the operator's included; any other group keeps its old rule.
func TestGoAheadOnOrgWorkstreamNeedsWriteAccess(t *testing.T) {
	o := newWSOrg(t)
	webhook, launch, _, brand := o.orgScene(t)
	put := func(who, group string, status int) {
		t.Helper()
		o.do(t, who, "PUT", "/v1/policy", -1, api.PolicyPut{Group: group, Stopped: true, Allowances: []factory.Allowance{}}, status, nil)
	}
	put("mia", webhook.ID, 200)
	put("santosh", brand.ID, 403)
	put("owner", launch.ID, 403)
	put("abir", "some-desk-group", 200)
	var ws api.Workstream
	o.do(t, "abir", "GET", "/v1/workstreams/"+webhook.ID, -1, nil, 200, &ws)
	if ws.GoAheads == nil {
		t.Fatalf("go_aheads = %+v", ws.GoAheads)
	}
}
