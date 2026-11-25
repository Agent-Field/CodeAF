package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/coder/websocket"
)

const hostedSessionsPath = "/v1/hosted/sessions"

func hostedGrant(t *testing.T, rg *hostedRig, owner, chat, guest, role string) acl.Grant {
	t.Helper()
	var grant acl.Grant
	rg.do(t, owner, http.MethodPost, "/v1/resources/session/"+chat+"/grants", -1, personGrant(guest, role), http.StatusCreated, &grant)
	return grant
}

// verbsJSON is a list of verbs as a JSON decoder reads it back into an untyped row.
func verbsJSON(verbs ...string) []any {
	out := make([]any, 0, len(verbs))
	for _, verb := range verbs {
		out = append(out, verb)
	}
	return out
}

// P2-1
// A private chat appears only to its owner and its current grantees.
func TestP2_1_PrivateHostedChatsAndEvents(t *testing.T) {
	rg := newHostedRig(t)
	assertEvents := func(chat string, start int, want map[string]int) int {
		t.Helper()
		events, _, err := live.Read(rg.h.eventRoot())
		if err != nil {
			t.Fatal(err)
		}
		for _, who := range []string{"santosh", "abir", "mia", "owner"} {
			body := liveBytes(t, rg.h, rg.tokens[who], events[start:])
			change := "event: resource.changed\n" + `data: {"kind":"session","id":"` + chat + `"}`
			if got := strings.Count(body, change); got != want[who] || want[who] == 0 && strings.Contains(body, chat) {
				t.Errorf("events for %s contain %d resource changes for %s, want %d: %s", who, got, chat, want[who], body)
			}
		}
		return len(events)
	}
	chat := rg.create(t, "santosh")
	row, found, err := rg.st.Resource(store.SessionKind, chat)
	if err != nil || !found || row.Owner != (acl.Subject{Kind: "person", ID: "santosh"}) ||
		row.CreatedBy != (acl.Principal{Kind: "person", ID: "santosh"}) || row.Parent != (acl.Resource{}) || row.Visibility != acl.Private {
		t.Fatalf("private registration = %+v, found %t, error %v", row, found, err)
	}
	for _, who := range []string{"santosh", "abir", "mia", "owner"} {
		list := rg.send(who, http.MethodGet, hostedSessionsPath, nil, nil)
		if list.Code != http.StatusOK || strings.Contains(list.Body.String(), chat) != (who == "santosh") {
			t.Fatalf("list for %s = %d %s", who, list.Code, list.Body)
		}
	}
	unknown := "hs_" + strings.Repeat("0", 24)
	for _, who := range []string{"mia", "owner"} {
		for _, route := range []struct{ method, suffix string }{
			{http.MethodGet, ""}, {http.MethodPost, "/stop"}, {http.MethodDelete, ""}, {http.MethodGet, "/wire"},
		} {
			absent := rg.send(who, route.method, hostedSessionsPath+"/"+unknown+route.suffix, nil, nil)
			private := rg.send(who, route.method, hostedSessionsPath+"/"+chat+route.suffix, nil, nil)
			if absent.Code != http.StatusNotFound || private.Code != absent.Code || absent.Body.String() != hostNoSuchChat || private.Body.String() != absent.Body.String() {
				t.Fatalf("%s %s as %s: absent = %d %q, private = %d %q", route.method, route.suffix, who, absent.Code, absent.Body, private.Code, private.Body)
			}
		}
	}
	ownerOnly := map[string]int{"santosh": 1}
	shared := map[string]int{"santosh": 1, "abir": 1}
	cursor := assertEvents(chat, 0, ownerOnly)
	grant := hostedGrant(t, rg, "santosh", chat, "abir", "view")
	cursor = assertEvents(chat, cursor, shared)
	policy := acl.DefaultPolicy()
	policy.Defaults[store.SessionKind] = acl.KindDefault{Visibility: acl.Org}
	rg.do(t, "owner", http.MethodPut, "/v1/org/policy", 0, policy, http.StatusOK, nil)
	cursor = assertEvents(chat, cursor, nil)
	rg.do(t, "santosh", http.MethodPost, hostedSessionsPath+"/"+chat+"/stop", -1, nil, http.StatusNoContent, nil)
	cursor = assertEvents(chat, cursor, shared)
	rg.do(t, "santosh", http.MethodDelete, "/v1/resources/session/"+chat+"/grants/"+grant.ID, -1, nil, http.StatusNoContent, nil)
	cursor = assertEvents(chat, cursor, shared)
	hostedGrant(t, rg, "santosh", chat, "abir", "view")
	cursor = assertEvents(chat, cursor, shared)
	rg.do(t, "santosh", http.MethodDelete, hostedSessionsPath+"/"+chat, -1, nil, http.StatusNoContent, nil)
	assertEvents(chat, cursor, shared)
}

// P2-2
// The list keeps the caller's host rows and adds filtered shared and operator rows.
func TestP2_2_HostedListKeepsOwnRowsAndLimitsSharedMetadata(t *testing.T) {
	rg := newHostedRig(t)
	mine := rg.create(t, "abir")
	first := rg.create(t, "santosh")
	second := rg.create(t, "santosh")
	hidden := rg.create(t, "santosh")
	third := rg.create(t, "mia")
	hostedGrant(t, rg, "santosh", first, "abir", "edit")
	hostedGrant(t, rg, "santosh", second, "abir", "view")
	hostedGrant(t, rg, "mia", third, "abir", "view")
	rg.setAccessPolicy(t, func(p *acl.Policy) {
		p.Defaults[store.SessionKind] = acl.KindDefault{Visibility: acl.Owner}
		p.Hosting.LocalSessions = "warn"
	})
	operatorRow := rg.create(t, "santosh")

	type listing struct {
		Sessions []map[string]any  `json:"sessions"`
		Shared   []map[string]any  `json:"shared"`
		Hosting  map[string]string `json:"hosting"`
	}
	read := func(who string) listing {
		t.Helper()
		var out listing
		rg.do(t, who, http.MethodGet, hostedSessionsPath, -1, nil, http.StatusOK, &out)
		if !reflect.DeepEqual(out.Hosting, map[string]string{"local_sessions": "warn", "words": acl.HostingWarnWords}) || out.Shared == nil {
			t.Fatalf("list for %s = %+v", who, out)
		}
		return out
	}
	before := rg.host.count()
	got := read("abir")
	called := map[string]int{}
	for _, req := range rg.host.requests()[before:] {
		if req.method != http.MethodGet || req.path != "/v1/sessions" {
			t.Fatalf("unexpected host request: %s %s", req.method, req.path)
		}
		called[req.header.Get("X-Codeaf-Person")]++
	}
	if !reflect.DeepEqual(called, map[string]int{"abir": 1, "santosh": 1, "mia": 1}) {
		t.Fatalf("host list calls = %+v", called)
	}
	var original listing
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+rigSecret)
	req.Header.Set("X-Codeaf-Person", "abir")
	rg.host.ServeHTTP(resp, req)
	if err := json.Unmarshal(resp.Body.Bytes(), &original); err != nil {
		t.Fatal(err)
	}
	// The caller's own rows are the host's rows with the caller's verbs added, and nothing else changed.
	if len(got.Sessions) != 1 || got.Sessions[0]["id"] != mine || !reflect.DeepEqual(got.Sessions[0]["verbs"], verbsJSON(acl.Verbs...)) {
		t.Fatalf("own list = %+v, host list = %+v", got.Sessions, original.Sessions)
	}
	delete(got.Sessions[0], "verbs")
	if !reflect.DeepEqual(got.Sessions, original.Sessions) {
		t.Fatalf("own list = %+v, host list = %+v", got.Sessions, original.Sessions)
	}
	editVerbs := []string{"view", "comment", "edit", "run", "steer", "answer"}
	metadata := func(id, owner, access, role string, verbs ...string) map[string]any {
		row := map[string]any{"id": id, "owner": owner, "access": access, "state": "suspended",
			"created": "2026-11-24T00:00:00Z", "last_active": "2026-11-24T00:01:00Z", "verbs": verbsJSON(verbs...)}
		if role != "" {
			row["role"] = role
		}
		return row
	}
	want := map[string]map[string]any{
		first: metadata(first, "santosh", "shared", "edit", editVerbs...), second: metadata(second, "santosh", "shared", "view", "view"),
		third: metadata(third, "mia", "shared", "view", "view"),
	}
	for _, row := range got.Shared {
		id, _ := row["id"].(string)
		if !reflect.DeepEqual(row, want[id]) {
			t.Errorf("shared row = %+v, want %+v", row, want[id])
		}
		delete(want, id)
	}
	if len(want) != 0 {
		t.Errorf("shared rows missing: %+v", want)
	}
	got = read("owner")
	if len(got.Sessions) != 0 || !reflect.DeepEqual(got.Shared, []map[string]any{metadata(operatorRow, "santosh", "operator", "")}) {
		t.Fatalf("operator list = %+v", got)
	}
	for _, tc := range []struct {
		who, id, access, role string
		verbs                 []string
	}{{"abir", first, "shared", "edit", editVerbs}, {"owner", operatorRow, "operator", "", nil}} {
		var meta map[string]any
		before := rg.host.count()
		rg.do(t, tc.who, http.MethodGet, hostedSessionsPath+"/"+tc.id, -1, nil, http.StatusOK, &meta)
		want := metadata(tc.id, "santosh", tc.access, tc.role, tc.verbs...)
		want["created"], want["last_active"] = "", ""
		if !reflect.DeepEqual(meta, want) {
			t.Errorf("get as %s = %+v, want %+v", tc.who, meta, want)
		}
		reqs := rg.host.requests()[before:]
		if len(reqs) != 1 || reqs[0].header.Get("X-Codeaf-Person") != "santosh" {
			t.Errorf("get as %s generated %d requests instead of one as the owner", tc.who, len(reqs))
		}
	}
	if w := rg.send("abir", http.MethodGet, hostedSessionsPath+"/"+hidden, nil, nil); w.Code != http.StatusNotFound {
		t.Fatalf("unshared chat read = %d %s", w.Code, w.Body)
	}

	rg.setAccessPolicy(t, func(p *acl.Policy) {
		p.Defaults[store.SessionKind] = acl.KindDefault{Visibility: acl.Org}
	})
	orgChat := rg.create(t, "mia")
	grants, err := rg.st.GrantsOn(store.SessionKind, orgChat)
	if err != nil || len(grants) != 1 || grants[0].Subject != (acl.Subject{Kind: "org", ID: "*"}) || grants[0].Role != "view" {
		t.Fatalf("organization default grants = %+v, %v", grants, err)
	}
	got = read("owner")
	for _, row := range got.Shared {
		if row["id"] == orgChat && reflect.DeepEqual(row, metadata(orgChat, "mia", "shared", "view", "view")) {
			return
		}
	}
	t.Fatalf("organization chat missing from shared entries: %+v", got.Shared)
}

// P2-2
// An unavailable shared owner listing fails the complete request.
func TestP2_2_HostedListFailsWhenSharedOwnerCannotBeListed(t *testing.T) {
	rg := newHostedRig(t)
	chat := rg.create(t, "santosh")
	hostedGrant(t, rg, "santosh", chat, "abir", "view")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Codeaf-Person") == "santosh" && r.URL.Path == "/v1/sessions" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		rg.host.ServeHTTP(w, r)
	}))
	defer srv.Close()
	rg.configure(t, func(c *HostedConfig) { c.URL = srv.URL })
	w := rg.send("abir", http.MethodGet, hostedSessionsPath, nil, nil)
	if w.Code != http.StatusBadGateway || errorOf(t, w.Body.Bytes()) != "The place chats run on is not answering." {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}
}

// P2-3
// Each first touch backfills a legacy chat as private, and an unknown chat creates no row.
func TestP2_3_LegacyChatsBackfillOnFirstTouch(t *testing.T) {
	rg := newHostedRig(t)
	rg.setAccessPolicy(t, func(p *acl.Policy) {
		p.Defaults[store.SessionKind] = acl.KindDefault{Visibility: acl.Org}
	})
	srv := httptest.NewServer(rg.h)
	defer srv.Close()
	for _, step := range []struct {
		name, digit, method, suffix string
		status                      int
	}{
		{"list", "1", http.MethodGet, "", http.StatusOK},
		{"get", "2", http.MethodGet, "", http.StatusOK},
		{"wire", "3", http.MethodGet, "/wire", http.StatusSwitchingProtocols},
		{"stop", "4", http.MethodPost, "/stop", http.StatusNoContent},
		{"delete", "5", http.MethodDelete, "", http.StatusNoContent},
	} {
		t.Run(step.name, func(t *testing.T) {
			id := "hs_" + strings.Repeat(step.digit, 24)
			rg.host.adopt(id, "santosh")
			if _, found, err := rg.st.Resource(store.SessionKind, id); found || err != nil {
				t.Fatalf("legacy registration already exists: %v, %v", found, err)
			}
			path := hostedSessionsPath + "/" + id + step.suffix
			switch step.name {
			case "wire":
				dial(t, srv, rg.tokens["santosh"], id).Close(websocket.StatusNormalClosure, "")
			case "list":
				w := rg.send("santosh", http.MethodGet, hostedSessionsPath, nil, nil)
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), id) {
					t.Fatalf("legacy list = %d %s", w.Code, w.Body)
				}
			default:
				rg.do(t, "santosh", step.method, path, -1, nil, step.status, nil)
			}
			row, found, err := rg.st.Resource(store.SessionKind, id)
			if step.name == "delete" {
				if found || err != nil || rg.host.ownerOf(id) != "" {
					t.Fatalf("legacy delete kept %+v, found %v, error %v", row, found, err)
				}
				events, _, err := live.Read(rg.h.eventRoot())
				if err != nil || !strings.Contains(liveBytes(t, rg.h, rg.tokens["santosh"], events), id) {
					t.Fatalf("legacy delete did not publish for its owner: %v", err)
				}
				return
			}
			if err != nil || !found || row.Owner != (acl.Subject{Kind: "person", ID: "santosh"}) ||
				row.CreatedBy != (acl.Principal{Kind: "person", ID: "santosh"}) || row.Visibility != acl.Private {
				t.Fatalf("backfilled row = %+v, found %v, error %v", row, found, err)
			}
			w := rg.send("abir", http.MethodGet, hostedSessionsPath+"/"+id, nil, nil)
			if w.Code != http.StatusNotFound || w.Body.String() != hostNoSuchChat {
				t.Fatalf("another person read a legacy chat: %d %s", w.Code, w.Body)
			}
		})
	}
	unknown := "hs_" + strings.Repeat("f", 24)
	for _, route := range []struct{ method, suffix string }{
		{http.MethodGet, ""}, {http.MethodGet, "/wire"}, {http.MethodPost, "/stop"}, {http.MethodDelete, ""},
	} {
		w := rg.send("santosh", route.method, hostedSessionsPath+"/"+unknown+route.suffix, nil, nil)
		if w.Code != http.StatusNotFound || w.Body.String() != hostNoSuchChat {
			t.Errorf("unknown %s %s = %d %s", route.method, route.suffix, w.Code, w.Body)
		}
	}
	if row, found, err := rg.st.Resource(store.SessionKind, unknown); found || err != nil {
		t.Fatalf("unknown chat registered: %+v, found %v, error %v", row, found, err)
	}
}

// P2-3
// With no store, the host alone checks ownership and the list has the P1 shape.
func TestP2_3_HostedRoutesWithoutStoreKeepP1Behavior(t *testing.T) {
	rg := newHostedRig(t)
	rg.cfg.Store = nil
	rg.configure(t, func(*HostedConfig) {})
	chat := rg.create(t, "santosh")
	var list map[string]json.RawMessage
	rg.do(t, "santosh", http.MethodGet, hostedSessionsPath, -1, nil, http.StatusOK, &list)
	if len(list) != 1 || !bytes.Contains(list["sessions"], []byte(chat)) {
		t.Fatalf("P1 list = %s", list)
	}
	path := hostedSessionsPath + "/" + chat
	for _, route := range []struct{ method, suffix string }{
		{http.MethodGet, ""}, {http.MethodGet, "/wire"}, {http.MethodPost, "/stop"}, {http.MethodDelete, ""},
	} {
		w := rg.send("abir", route.method, path+route.suffix, nil, nil)
		if w.Code != http.StatusNotFound || w.Body.String() != hostNoSuchChat {
			t.Fatalf("another person's %s %s = %d %s", route.method, route.suffix, w.Code, w.Body)
		}
	}
	rg.do(t, "santosh", http.MethodGet, path, -1, nil, http.StatusOK, nil)
	srv := httptest.NewServer(rg.h)
	defer srv.Close()
	dial(t, srv, rg.tokens["santosh"], chat).Close(websocket.StatusNormalClosure, "")
	rg.do(t, "santosh", http.MethodPost, path+"/stop", -1, nil, http.StatusNoContent, nil)
	rg.do(t, "santosh", http.MethodDelete, path, -1, nil, http.StatusNoContent, nil)
	if row, found, err := rg.st.Resource(store.SessionKind, chat); found || err != nil {
		t.Fatalf("store-less chat registered: %+v, found %v, error %v", row, found, err)
	}
}

// P2-4
// Only an owner with run gets the owner wire, and operator access adds no wire rights.
func TestP2_4_HostedWireClassUsesCurrentVerbsWithoutOperator(t *testing.T) {
	rg := newHostedRig(t)
	rg.setAccessPolicy(t, func(p *acl.Policy) {
		p.Defaults[store.SessionKind] = acl.KindDefault{Visibility: acl.Owner}
		p.Operator.Role = "full"
	})
	chat := rg.create(t, "santosh")
	hostedGrant(t, rg, "santosh", chat, "abir", "full")
	res, found, err := rg.h.lookupAccessResource(store.SessionKind, chat)
	if err != nil || !found {
		t.Fatalf("registered chat missing: %v", err)
	}
	for _, tc := range []struct {
		name, person string
		operator     bool
		agent        *acl.Agent
		class        hostedWireClass
	}{
		{"owner", "santosh", false, nil, hostedWireOwner},
		{"owner bot with run", "santosh", false, &acl.Agent{ID: "bot:drive", Sponsor: "santosh", Only: []string{"view", "run"}}, hostedWireOwner},
		{"owner bot with view", "santosh", false, &acl.Agent{ID: "bot:watch", Sponsor: "santosh", Only: []string{"view"}}, hostedWireGuest},
		{"owner bot without view", "santosh", false, &acl.Agent{ID: "bot:noview", Sponsor: "santosh", Only: []string{"run"}}, hostedWireNone},
		{"full guest", "abir", false, nil, hostedWireGuest},
		{"guest who is also operator", "abir", true, nil, hostedWireGuest},
		{"operator only", "owner", true, nil, hostedWireNone},
		{"unshared member", "mia", false, nil, hostedWireNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := rg.h.viewerOf(tc.person)
			v.Operator, v.Agent = tc.operator, tc.agent
			if got := hostedWireClassFor(v, res.item, "santosh"); got != tc.class {
				t.Fatalf("class = %v, want %v", got, tc.class)
			}
		})
	}
	before := rg.host.count()
	w := rg.send("owner", http.MethodGet, hostedSessionsPath+"/"+chat+"/wire", nil, nil)
	if w.Code != http.StatusNotFound || w.Body.String() != hostNoSuchChat {
		t.Fatalf("operator wire = %d %s", w.Code, w.Body)
	}
	if got := rg.host.count(); got != before {
		t.Fatalf("operator called the owner wire: %d requests", got-before)
	}
	opened := recordGuestHostWires(rg)
	srv := httptest.NewServer(rg.h)
	defer srv.Close()
	client, _ := guestConnect(t, srv, opened, rg.tokens["abir"], chat)
	guestWrite(t, client, guestCall(1, "SetModel"))
	guestRefusal(t, client, 1, guestOwnerWords)
}

// P2-7
// A creation rejected by the host rolls back the prior registration and model access.
func TestP2_7_HostedCreateRegistersBeforeHostAndRollsBackFailure(t *testing.T) {
	rg := newHostedRig(t)
	rg.setAccessPolicy(t, func(p *acl.Policy) {
		p.Defaults[store.SessionKind] = acl.KindDefault{Visibility: acl.Org}
	})
	type observation struct {
		id, token string
		found     bool
		grants    int
		err       error
	}
	seen := make(chan observation, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		id := jsonField(data, "id")
		_, found, lookupErr := rg.st.Resource(store.SessionKind, id)
		grants, err := rg.st.GrantsOn(store.SessionKind, id)
		if lookupErr != nil {
			err = lookupErr
		}
		seen <- observation{id, jsonField(data, "model_token"), found, len(grants), err}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, "{\"error\":\"This chat already exists.\"}\n")
	}))
	defer srv.Close()
	rg.configure(t, func(c *HostedConfig) { c.URL = srv.URL })
	w := rg.send("santosh", http.MethodPost, hostedSessionsPath, nil, nil)
	if w.Code != http.StatusConflict || w.Body.String() != "{\"error\":\"This chat already exists.\"}\n" {
		t.Fatalf("create refusal = %d %s", w.Code, w.Body)
	}
	got := <-seen
	if !got.found || got.grants != 1 || got.token == "" || got.err != nil {
		t.Fatalf("host did not see complete registration: %+v", got)
	}
	if row, found, err := rg.st.Resource(store.SessionKind, got.id); found || err != nil {
		t.Fatalf("failed creation kept a row: %+v, found %v, error %v", row, found, err)
	}
	if grants, err := rg.st.GrantsOn(store.SessionKind, got.id); len(grants) != 0 || err != nil {
		t.Fatalf("failed creation kept grants: %+v, %v", grants, err)
	}
	if w := rg.send("", http.MethodGet, "/v1/model/models", nil, map[string]string{"Authorization": "Bearer " + got.token}); w.Code != http.StatusUnauthorized {
		t.Fatalf("failed creation retained model access: %d %s", w.Code, w.Body)
	}
}

// P2-7
// Lifecycle operations require manage, act as the owner, and remove grants on deletion.
func TestP2_7_HostedStopAndDeleteRequireManage(t *testing.T) {
	rg := newHostedRig(t)
	chat := rg.create(t, "santosh")
	token := rg.host.tokenOf(chat)
	hostedGrant(t, rg, "santosh", chat, "abir", "edit")
	hostedGrant(t, rg, "santosh", chat, "mia", "full")
	path := hostedSessionsPath + "/" + chat
	before := rg.host.count()
	for _, step := range []struct{ method, suffix string }{{http.MethodPost, "/stop"}, {http.MethodDelete, ""}} {
		w := rg.send("abir", step.method, path+step.suffix, nil, nil)
		if w.Code != http.StatusForbidden || errorOf(t, w.Body.Bytes()) != "Only this chat's owner, or someone it is shared with in full, can stop or delete it." {
			t.Fatalf("%s as a grantee without manage = %d %s", step.method, w.Code, w.Body)
		}
	}
	if got := rg.host.count(); got != before {
		t.Fatalf("forbidden stop or delete reached the host: %d calls", got-before)
	}
	rg.do(t, "mia", http.MethodPost, path+"/stop", -1, nil, http.StatusNoContent, nil)
	if w := rg.send("", http.MethodGet, "/v1/model/models", nil, map[string]string{"Authorization": "Bearer " + token}); w.Code != http.StatusUnauthorized {
		t.Fatalf("stopped chat kept model access: %d %s", w.Code, w.Body)
	}
	rg.do(t, "mia", http.MethodDelete, path, -1, nil, http.StatusNoContent, nil)
	for _, req := range rg.host.requests()[before:] {
		if req.header.Get("X-Codeaf-Person") != "santosh" {
			t.Fatalf("%s %s ran as %q", req.method, req.path, req.header.Get("X-Codeaf-Person"))
		}
	}
	if row, found, err := rg.st.Resource(store.SessionKind, chat); found || err != nil {
		t.Fatalf("deleted chat kept a row: %+v, found %v, error %v", row, found, err)
	}
	if grants, err := rg.st.GrantsOn(store.SessionKind, chat); len(grants) != 0 || err != nil {
		t.Fatalf("deleted chat kept grants: %+v, %v", grants, err)
	}
}

// P2-10
// Sharing a hosted chat leaves its owner's wire byte-for-byte intact.
func TestP2_10_OwnerWireStillCopiesOriginalBytes(t *testing.T) {
	rg := newHostedRig(t)
	chat := rg.create(t, "santosh")
	hostedGrant(t, rg, "santosh", chat, "abir", "full")
	srv := httptest.NewServer(rg.h)
	defer srv.Close()
	conn := dial(t, srv, rg.tokens["santosh"], chat)
	defer conn.CloseNow()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	frame := []byte("{ \"kind\": \"call\", \"id\": 17, \"method\": \"Submit\", \"payload\": {\"text\":\"hello\"} }\n")
	if err := conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
		t.Fatal(err)
	}
	kind, got, err := conn.Read(ctx)
	if err != nil || kind != websocket.MessageBinary || !bytes.Equal(got, frame) {
		t.Fatalf("host echoed %v %q, error %v, want %q", kind, got, err, frame)
	}
}
