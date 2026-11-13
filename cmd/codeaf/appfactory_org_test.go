package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// orgServer is a stub organization server that speaks the Teams wave's
// contract (teams, /v1/me, workstreams) from memory. It is NOT the real server
// (lanes G1 and G2 build that); it is the contract as the engine reads it, so
// the engine's half is proved without waiting on theirs.
type orgServer struct {
	mu       sync.Mutex
	url      string
	operator bool
	asks     []string
	bearers  []string
	ifMatch  []string
	teams    map[string]*appwire.OrgTeam
	streams  map[string]*appwire.OrgWorkstream
	next     int
	lastBody map[string]any
}

func startOrgServer(t *testing.T) *orgServer {
	t.Helper()
	o := &orgServer{operator: true, teams: map[string]*appwire.OrgTeam{}, streams: map[string]*appwire.OrgWorkstream{}}
	srv := httptest.NewServer(http.HandlerFunc(o.serve))
	t.Cleanup(srv.Close)
	o.url = srv.URL
	return o
}

func (o *orgServer) seen() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.asks...)
}

func (o *orgServer) since() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := o.asks
	o.asks = nil
	return out
}

func orgJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func orgRefuse(w http.ResponseWriter, status int, words string) {
	orgJSON(w, status, map[string]string{"error": words})
}

func (o *orgServer) serve(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if r.URL.Path == "/v1/about" || r.URL.Path == "/v1/me" && r.Header.Get("Authorization") == "" {
		orgJSON(w, 200, api.Me{Contract: api.Contract, Factory: api.Factory{Name: "Acme"}, Mode: api.FactoryTeam})
		return
	}
	o.asks = append(o.asks, r.Method+" "+r.URL.RequestURI())
	o.bearers = append(o.bearers, r.Header.Get("Authorization"))
	o.ifMatch = append(o.ifMatch, r.Header.Get("If-Match"))
	o.lastBody = nil
	if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
		_ = json.Unmarshal(raw, &o.lastBody)
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case r.URL.Path == "/v1/me":
		mine := []string{}
		for id, t := range o.teams {
			for _, m := range t.Members {
				if m == "santosh" {
					mine = append(mine, id)
				}
			}
		}
		orgJSON(w, 200, map[string]any{"handle": "santosh", "name": "Santosh", "avatar_url": "", "operator": o.operator,
			"mode": api.FactoryTeam, "contract": api.Contract, "teams": mine})
	case len(parts) >= 2 && parts[1] == "teams":
		o.serveTeams(w, r, parts[2:])
	case len(parts) >= 2 && parts[1] == "workstreams":
		o.serveStreams(w, r, parts[2:])
	default:
		http.NotFound(w, r)
	}
}

func (o *orgServer) serveTeams(w http.ResponseWriter, r *http.Request, rest []string) {
	if r.Method != http.MethodGet && !o.operator {
		orgRefuse(w, 403, "only the organization's operator manages teams")
		return
	}
	name, _ := o.lastBody["name"].(string)
	switch {
	case len(rest) == 0 && r.Method == http.MethodGet:
		list := []appwire.OrgTeam{}
		for _, t := range o.teams {
			list = append(list, *t)
		}
		orgJSON(w, 200, map[string]any{"teams": list})
	case len(rest) == 0 && r.Method == http.MethodPost:
		for _, t := range o.teams {
			if t.Name == name {
				orgRefuse(w, 409, "a team named "+name+" already exists")
				return
			}
		}
		o.next++
		t := &appwire.OrgTeam{ID: "tm_" + strconv.Itoa(o.next), Name: name, Members: []string{}}
		o.teams[t.ID] = t
		orgJSON(w, 200, t)
	default:
		t, ok := o.teams[rest[0]]
		if !ok {
			orgRefuse(w, 404, "no such team")
			return
		}
		switch {
		case len(rest) == 1 && r.Method == http.MethodPatch:
			t.Name = name
			orgJSON(w, 200, t)
		case len(rest) == 1 && r.Method == http.MethodDelete:
			for _, s := range o.streams {
				if s.Owner.Kind == "team" && s.Owner.ID == t.ID {
					orgRefuse(w, 409, t.Name+" owns "+s.Name+"; give it to someone else first")
					return
				}
			}
			delete(o.teams, t.ID)
			w.WriteHeader(204)
		case len(rest) == 3 && rest[1] == "members" && r.Method == http.MethodPut:
			t.Members = append(t.Members, rest[2])
			orgJSON(w, 200, t)
		case len(rest) == 3 && rest[1] == "members" && r.Method == http.MethodDelete:
			kept := []string{}
			for _, m := range t.Members {
				if m != rest[2] {
					kept = append(kept, m)
				}
			}
			t.Members = kept
			orgJSON(w, 200, t)
		default:
			http.NotFound(w, r)
		}
	}
}

func (o *orgServer) serveStreams(w http.ResponseWriter, r *http.Request, rest []string) {
	switch {
	case len(rest) == 0 && r.Method == http.MethodGet:
		list := []appwire.OrgWorkstreamSummary{}
		for _, s := range o.streams {
			list = append(list, s.OrgWorkstreamSummary)
		}
		orgJSON(w, 200, map[string]any{"workstreams": list})
	case len(rest) == 0 && r.Method == http.MethodPost, len(rest) == 1 && rest[0] == "import" && r.Method == http.MethodPost:
		o.next++
		s := &appwire.OrgWorkstream{OrgWorkstreamSummary: appwire.OrgWorkstreamSummary{
			ID: "ws_" + strconv.Itoa(o.next), Stage: "shaping", Owner: appwire.OrgOwner{Kind: "person", ID: "santosh"}, Rev: 1,
		}, CreatedBy: "santosh"}
		s.Name, _ = o.lastBody["name"].(string)
		s.Purpose, _ = o.lastBody["purpose"].(string)
		if owner, ok := o.lastBody["owner"].(map[string]any); ok {
			s.Owner = appwire.OrgOwner{Kind: owner["kind"].(string), ID: owner["id"].(string)}
		}
		if links, ok := o.lastBody["links"].([]any); ok {
			for _, l := range links {
				s.Links = append(s.Links, l.(string))
			}
		}
		o.streams[s.ID] = s
		orgJSON(w, 200, s)
	default:
		s, ok := o.streams[rest[0]]
		if !ok {
			orgRefuse(w, 404, "no such workstream")
			return
		}
		switch {
		case len(rest) == 1 && r.Method == http.MethodGet:
			orgJSON(w, 200, s)
		case len(rest) == 1 && r.Method == http.MethodPatch:
			if r.Header.Get("If-Match") != strconv.Itoa(s.Rev) {
				orgRefuse(w, 412, "this workstream changed since rev "+r.Header.Get("If-Match"))
				return
			}
			if name, ok := o.lastBody["name"].(string); ok {
				s.Name = name
			}
			if links, ok := o.lastBody["links"].([]any); ok {
				s.Links = []string{}
				for _, l := range links {
					s.Links = append(s.Links, l.(string))
				}
			}
			s.Rev++
			orgJSON(w, 200, s)
		case len(rest) == 1 && r.Method == http.MethodDelete:
			s.ArchivedAt = "2026-11-13T00:00:00Z"
			w.WriteHeader(204)
		case len(rest) == 2 && rest[1] == "owner" && r.Method == http.MethodPut:
			s.Owner = appwire.OrgOwner{Kind: o.lastBody["kind"].(string), ID: o.lastBody["id"].(string)}
			s.Rev++
			orgJSON(w, 200, s)
		case len(rest) == 2 && rest[1] == "shares" && r.Method == http.MethodPut:
			s.SharedWith = []string{}
			for _, t := range o.lastBody["teams"].([]any) {
				s.SharedWith = append(s.SharedWith, t.(string))
			}
			also := []string{}
			if o.lastBody["include_linked"] == true {
				for _, l := range s.Links {
					o.streams[l].SharedWith = s.SharedWith
					also = append(also, l)
				}
			}
			s.Rev++
			orgJSON(w, 200, map[string]any{"workstream": s, "also_shared": also})
		case len(rest) == 3 && rest[1] == "shares" && rest[2] == "preview" && r.Method == http.MethodGet:
			linked := []appwire.OrgWorkstreamSummary{}
			for _, l := range s.Links {
				linked = append(linked, o.streams[l].OrgWorkstreamSummary)
			}
			orgJSON(w, 200, map[string]any{"linked": linked})
		default:
			http.NotFound(w, r)
		}
	}
}

func attachOrg(t *testing.T) (*orgServer, *factoryWire, string) {
	t.Helper()
	o := startOrgServer(t)
	wire := newFactoryWire(t, testFactoryHub(t))
	var conn appwire.FactoryConnection
	wire.must("factory.attach", map[string]any{"url": o.url, "token": testHostToken}, &conn)
	o.since()
	return o, wire, conn.ID
}

// THE ENGINE SAYS IT SERVES TEAMS AND ORG WORKSTREAMS, by the desk's two words.
func TestOrgMethodsAreAnnouncedInHello(t *testing.T) {
	_, wire, _ := attachOrg(t)
	for _, word := range []string{`"org_teams"`, `"org_workstreams"`} {
		if !strings.Contains(wire.hello, word) {
			t.Fatalf("hello lacks %s: %s", word, wire.hello)
		}
	}
}

// TEAMS: every verb is one request with the person's session, the server's
// answer comes back in its shape, and its refusals keep their status.
func TestOrgTeamsForwardToTheServer(t *testing.T) {
	o, wire, id := attachOrg(t)
	c := func(extra map[string]any) map[string]any {
		out := map[string]any{"connection": id}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	var teams appwire.OrgTeams
	wire.must("factory.teams", c(nil), &teams)
	if teams.Teams == nil || len(teams.Teams) != 0 {
		t.Fatalf("an empty organization answered %+v", teams)
	}
	var platform appwire.OrgTeam
	wire.must("factory.team_create", c(map[string]any{"name": "Platform"}), &platform)
	if platform.ID == "" || platform.Name != "Platform" || platform.Members == nil {
		t.Fatalf("created %+v", platform)
	}
	if msg := wire.mustFail("factory.team_create", c(map[string]any{"name": "Platform"}), appwire.CodeConflict); !strings.Contains(msg, "already exists") {
		t.Fatalf("a duplicate's refusal lost the server's words: %q", msg)
	}
	wire.must("factory.team_rename", c(map[string]any{"team": platform.ID, "name": "Core"}), &platform)
	wire.must("factory.team_member_add", c(map[string]any{"team": platform.ID, "handle": "@santosh"}), &platform)
	wire.must("factory.team_member_add", c(map[string]any{"team": platform.ID, "handle": "abir"}), &platform)
	if platform.Name != "Core" || strings.Join(platform.Members, ",") != "santosh,abir" {
		t.Fatalf("team = %+v", platform)
	}
	wire.must("factory.team_member_remove", c(map[string]any{"team": platform.ID, "handle": "abir"}), &platform)

	var me appwire.FactoryMe
	wire.must("factory.me", c(nil), &me)
	if me.Handle != "santosh" || strings.Join(me.Teams, ",") != platform.ID {
		t.Fatalf("me = %+v", me)
	}

	var ws appwire.OrgWorkstream
	wire.must("factory.org_workstream_create", c(map[string]any{"name": "Webhook reliability", "owner": map[string]any{"kind": "team", "id": platform.ID}}), &ws)
	if msg := wire.mustFail("factory.team_delete", c(map[string]any{"team": platform.ID}), appwire.CodeConflict); !strings.Contains(msg, "owns Webhook reliability") {
		t.Fatalf("delete refusal: %q", msg)
	}

	want := []string{
		"GET /v1/teams", "POST /v1/teams", "POST /v1/teams",
		"PATCH /v1/teams/" + platform.ID, "PUT /v1/teams/" + platform.ID + "/members/santosh",
		"PUT /v1/teams/" + platform.ID + "/members/abir", "DELETE /v1/teams/" + platform.ID + "/members/abir",
		"GET /v1/me", "POST /v1/workstreams", "DELETE /v1/teams/" + platform.ID,
	}
	if got := o.seen(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("sent\n%v\nwant\n%v", got, want)
	}
	for _, b := range o.bearers {
		if b != "Bearer "+testHostToken {
			t.Fatalf("a request went without the session: %q", b)
		}
	}

	o.mu.Lock()
	o.operator = false
	o.mu.Unlock()
	if msg := wire.mustFail("factory.team_create", c(map[string]any{"name": "Growth"}), appwire.CodeForbidden); !strings.Contains(msg, "operator") {
		t.Fatalf("forbidden lost the server's words: %q", msg)
	}
	wire.neverSaid(testHostToken)
}

// WORKSTREAMS: read, write with the rev as If-Match, owner, shares and their
// preview, soft delete and import, each the route the contract names.
func TestOrgWorkstreamsForwardToTheServer(t *testing.T) {
	o, wire, id := attachOrg(t)
	c := func(extra map[string]any) map[string]any {
		out := map[string]any{"connection": id}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	var linked, ws appwire.OrgWorkstream
	wire.must("factory.org_workstream_create", c(map[string]any{"name": "Pricing page"}), &linked)
	wire.must("factory.org_workstream_create", c(map[string]any{"name": "Launch plan", "purpose": "ship it", "links": []string{linked.ID}}), &ws)
	if ws.Owner != (appwire.OrgOwner{Kind: "person", ID: "santosh"}) || ws.Purpose != "ship it" || ws.Rev != 1 {
		t.Fatalf("created %+v", ws)
	}
	// Lists are lists on the wire even when the server leaves them out.
	if ws.Pages == nil || ws.Decisions == nil || ws.Sources == nil || ws.GoAheads == nil || ws.SharedWith == nil {
		t.Fatalf("a list came back null: %+v", ws)
	}

	var list appwire.OrgWorkstreams
	wire.must("factory.org_workstreams", c(nil), &list)
	if len(list.Workstreams) != 2 {
		t.Fatalf("list = %+v", list)
	}
	wire.must("factory.org_workstream", c(map[string]any{"id": ws.ID}), &ws)

	wire.must("factory.org_workstream_update", c(map[string]any{"id": ws.ID, "rev": 1, "name": "Launch plan v2", "links": []string{}}), &ws)
	if ws.Name != "Launch plan v2" || ws.Rev != 2 || len(ws.Links) != 0 {
		t.Fatalf("updated %+v", ws)
	}
	if body := o.lastBody; len(body) != 2 || body["links"] == nil {
		t.Fatalf("the patch sent more or less than it was given: %v", body)
	}
	if got := o.ifMatch[len(o.ifMatch)-1]; got != "1" {
		t.Fatalf("If-Match = %q", got)
	}
	// A rev that moved on is conflict, the server's sentence kept and the
	// reason said, so the desk reloads rather than overwrites.
	msg := wire.mustFail("factory.org_workstream_update", c(map[string]any{"id": ws.ID, "rev": 1, "name": "x"}), appwire.CodeConflict)
	if !strings.Contains(msg, "changed since rev 1") {
		t.Fatalf("412 words: %q", msg)
	}
	err := wire.backend.FactoryOrgCall(context.Background(), id, appwire.OrgRequest{Method: http.MethodPatch, Path: "/v1/workstreams/" + ws.ID, Body: map[string]any{}, IfMatch: "1"}, nil)
	var we *appwire.Error
	if !errors.As(err, &we) || we.Code != appwire.CodeConflict {
		t.Fatalf("412 = %v", err)
	}
	if d, ok := we.Detail.(*appwire.FactoryRefusal); !ok || d.Reason != "stale" {
		t.Fatalf("412 detail = %#v", we.Detail)
	}

	wire.must("factory.org_workstream_update", c(map[string]any{"id": ws.ID, "rev": 2, "links": []string{linked.ID}}), &ws)
	wire.must("factory.org_workstream_owner", c(map[string]any{"id": ws.ID, "kind": "team", "owner": "tm_9"}), &ws)
	if ws.Owner != (appwire.OrgOwner{Kind: "team", ID: "tm_9"}) {
		t.Fatalf("owner = %+v", ws.Owner)
	}
	var preview appwire.OrgSharePreview
	wire.must("factory.org_workstream_share_preview", c(map[string]any{"id": ws.ID, "teams": []string{"tm_1", "tm_2"}}), &preview)
	if len(preview.Linked) != 1 || preview.Linked[0].ID != linked.ID {
		t.Fatalf("preview = %+v", preview)
	}
	var shared appwire.OrgWorkstreamShared
	wire.must("factory.org_workstream_shares", c(map[string]any{"id": ws.ID, "teams": []string{"tm_1", "tm_2"}, "include_linked": true}), &shared)
	if strings.Join(shared.Workstream.SharedWith, ",") != "tm_1,tm_2" || strings.Join(shared.AlsoShared, ",") != linked.ID {
		t.Fatalf("shared = %+v", shared)
	}
	wire.must("factory.org_workstream_delete", c(map[string]any{"id": linked.ID}), nil)

	var moved appwire.OrgWorkstream
	wire.must("factory.org_workstream_import", c(map[string]any{"from_local_id": "local-7", "name": "Brand refresh", "purpose": "from my laptop"}), &moved)
	if moved.Name != "Brand refresh" || o.lastBody["from_local_id"] != "local-7" {
		t.Fatalf("imported %+v from %v", moved, o.lastBody)
	}

	want := []string{
		"POST /v1/workstreams", "POST /v1/workstreams", "GET /v1/workstreams", "GET /v1/workstreams/" + ws.ID,
		"PATCH /v1/workstreams/" + ws.ID, "PATCH /v1/workstreams/" + ws.ID, "PATCH /v1/workstreams/" + ws.ID,
		"PATCH /v1/workstreams/" + ws.ID, "PUT /v1/workstreams/" + ws.ID + "/owner",
		"GET /v1/workstreams/" + ws.ID + "/shares/preview?teams=tm_1%2Ctm_2", "PUT /v1/workstreams/" + ws.ID + "/shares",
		"DELETE /v1/workstreams/" + linked.ID, "POST /v1/workstreams/import",
	}
	if got := o.seen(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("sent\n%v\nwant\n%v", got, want)
	}
	wire.neverSaid(testHostToken)
}

// AN ID NEVER CLIMBS OUT OF ITS SEGMENT: a malformed id, handle, owner or
// missing rev is bad_params before anything is sent.
func TestOrgMethodsRefuseBadIdsBeforeSending(t *testing.T) {
	o, wire, id := attachOrg(t)
	for _, tc := range []struct {
		method string
		params map[string]any
	}{
		{"factory.teams", map[string]any{}},
		{"factory.team_create", map[string]any{"connection": id, "name": " "}},
		{"factory.team_rename", map[string]any{"connection": id, "team": "../me", "name": "x"}},
		{"factory.team_delete", map[string]any{"connection": id, "team": "a?b"}},
		{"factory.team_member_add", map[string]any{"connection": id, "team": "tm_1", "handle": "a/b"}},
		{"factory.org_workstream", map[string]any{"connection": id, "id": "ws_1/owner"}},
		{"factory.org_workstream_update", map[string]any{"connection": id, "id": "ws_1", "name": "x"}},
		{"factory.org_workstream_owner", map[string]any{"connection": id, "id": "ws_1", "kind": "robot", "owner": "x"}},
		{"factory.org_workstream_shares", map[string]any{"connection": id, "id": "ws_1", "teams": []string{"a,b"}}},
		{"factory.org_workstream_import", map[string]any{"connection": id, "name": "x"}},
	} {
		wire.mustFail(tc.method, tc.params, appwire.CodeBadParams)
	}
	if got := o.seen(); len(got) != 0 {
		t.Fatalf("a refused request was sent: %v", got)
	}
	// The older word for the connection is read too.
	var teams appwire.OrgTeams
	wire.must("factory.teams", map[string]any{"factory": id}, &teams)
}

// A SERVER OLDER THAN TEAMS answers 404, said as not_found; factory.me from it
// still answers teams as [].
func TestOrgMethodsOnAnOlderServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/me" || r.URL.Path == "/v1/about" {
			orgJSON(w, 200, api.Me{Contract: api.Contract, Factory: api.Factory{Name: "Old"}, Mode: api.FactoryTeam, Profile: api.Profile{Handle: "santosh"}})
			return
		}
		orgRefuse(w, 404, "factory endpoint not found")
	}))
	t.Cleanup(srv.Close)
	wire := newFactoryWire(t, testFactoryHub(t))
	var conn appwire.FactoryConnection
	wire.must("factory.attach", map[string]any{"url": srv.URL, "token": testHostToken}, &conn)
	wire.mustFail("factory.teams", map[string]any{"connection": conn.ID}, appwire.CodeNotFound)
	var me appwire.FactoryMe
	wire.must("factory.me", map[string]any{"connection": conn.ID}, &me)
	if me.Teams == nil || len(me.Teams) != 0 {
		t.Fatalf("an older server's teams = %#v", me.Teams)
	}
}
