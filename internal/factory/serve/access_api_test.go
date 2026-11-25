package serve

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/canvasrpc"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

const policyPath = "/v1/org/policy"

func accessWorkstreamPath(id string) string { return "/v1/resources/workstream/" + id }

func (o *wsOrg) setAccessPolicy(t *testing.T, edit func(*acl.Policy)) acl.Policy {
	t.Helper()
	var p acl.Policy
	o.do(t, "owner", "GET", policyPath, -1, nil, 200, &p)
	edit(&p)
	var saved acl.Policy
	o.do(t, "owner", "PUT", policyPath, p.Rev, p, 200, &saved)
	return saved
}

func accessAgentRequest(t *testing.T, o *wsOrg, method, path, sponsor, agent string, body any, status int, out any) string {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Authorization", "Bearer "+o.tokens[sponsor])
	r = r.WithContext(context.WithValue(r.Context(), agentCallKey{}, agentCall{handle: sponsor, agent: agent}))
	w := httptest.NewRecorder()
	o.h.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s as %s for %s: %d %s, want %d", method, path, agent, sponsor, w.Code, w.Body.String(), status)
	}
	if out != nil {
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatal(err)
		}
	}
	return w.Body.String()
}

func personGrant(handle, role string) api.GrantNew {
	return api.GrantNew{Subject: acl.Subject{Kind: "person", ID: handle}, Role: role}
}

// Contract A1, C1, C2. The policy is readable by members and writes need the current revision.
func TestAccessPolicyAPI(t *testing.T) {
	o := newWSOrg(t)
	var p acl.Policy
	o.do(t, "mia", "GET", policyPath, -1, nil, 200, &p)
	if !reflect.DeepEqual(p, acl.DefaultPolicy()) {
		t.Fatalf("policy = %+v", p)
	}
	w := o.raw(t, "mia", "GET", policyPath, nil)
	if w.Header().Get("ETag") != `"0"` {
		t.Fatalf("ETag = %q", w.Header().Get("ETag"))
	}
	o.do(t, "", "PUT", policyPath, 0, p, 401, nil)
	o.do(t, "mia", "PUT", policyPath, 0, p, 403, nil)
	o.do(t, "owner", "PUT", policyPath, 0, map[string]any{"unknown": true}, 422, nil)
	p.Roles[0].Verbs = []string{acl.VerbView, "admin"}
	o.do(t, "owner", "PUT", policyPath, 0, p, 422, nil)
	p = acl.DefaultPolicy()
	p.Operator.SeesPrivate = true
	var saved acl.Policy
	o.do(t, "owner", "PUT", policyPath, -1, p, 200, &saved)
	if saved.Rev != 1 || saved.UpdatedBy != "owner" || !saved.Operator.SeesPrivate || saved.UpdatedAt == "" {
		t.Fatalf("saved policy = %+v", saved)
	}
	o.do(t, "owner", "PUT", policyPath, -1, p, 412, nil)
	o.do(t, "owner", "PUT", policyPath, 0, saved, 412, nil)
	o.do(t, "abir", "GET", policyPath, -1, nil, 200, &p)
	if !reflect.DeepEqual(p, saved) {
		t.Fatalf("reader saw %+v, want %+v", p, saved)
	}
	var audit struct {
		Events []record.Event `json:"events"`
	}
	o.do(t, "mia", "GET", "/v1/audit", -1, nil, 200, &audit)
	if !slices.ContainsFunc(audit.Events, func(e record.Event) bool {
		return e.Kind == record.OrgPolicyChanged && e.Verb == "org policy changed" && e.Handle == "owner"
	}) {
		t.Fatalf("policy change missing from the record: %+v", audit.Events)
	}
}

// Contract B3, B4, B5, E4, G1. Shares can be previewed and stored through the same access rules.
func TestAccessGrantAPI(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Release notes"})
	path := accessWorkstreamPath(ws.ID)
	grants := path + "/grants"
	o.do(t, "mia", "GET", grants, -1, nil, 404, nil)
	o.do(t, "mia", "POST", grants, -1, personGrant("mia", "view"), 404, nil)
	var listed api.ResourceGrants
	o.do(t, "owner", "GET", grants, -1, nil, 200, &listed)
	if len(listed.Grants) != 1 || listed.Grants[0].Source != "owner" || listed.Grants[0].Role != "full" {
		t.Fatalf("owner tuple = %+v", listed)
	}

	legacy := []factory.Share{{Kind: "person", ID: "abir", Role: "edit"}}
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+ws.ID+"/shares", -1, api.WorkstreamSharesPut{Shares: &legacy}, 200, nil)
	o.do(t, "abir", "GET", grants, -1, nil, 200, &listed)
	if len(listed.Grants) != 2 || listed.Grants[1].Source != "share" || listed.Grants[1].ID != "share:person:abir" {
		t.Fatalf("legacy tuple = %+v", listed.Grants)
	}
	w := o.raw(t, "santosh", "DELETE", grants+"/share:person:abir", nil)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "/v1/workstreams/{id}/shares") {
		t.Fatalf("legacy deletion = %d %s", w.Code, w.Body)
	}
	o.do(t, "abir", "POST", grants, -1, personGrant("mia", "view"), 403, nil)
	o.do(t, "owner", "POST", grants, -1, personGrant("mia", "edit"), 403, nil)
	o.do(t, "santosh", "POST", grants, -1, personGrant("nonmember", "view"), 422, nil)
	o.do(t, "santosh", "POST", grants, -1, personGrant("mia", "unknown"), 422, nil)
	past := o.now.Add(-time.Minute)
	expired := personGrant("mia", "view")
	expired.ExpiresAt = &past
	o.do(t, "santosh", "POST", grants, -1, expired, 422, nil)
	agent := api.GrantNew{Subject: acl.Subject{Kind: "agent", ID: "afi"}, Role: "view"}
	o.do(t, "santosh", "POST", grants, -1, agent, 422, nil)

	var preview api.GrantPreview
	o.do(t, "santosh", "POST", grants+"/preview", -1, personGrant("mia", "view"), 200, &preview)
	if !slices.Equal(preview.Gains, []string{o.h.personNames()("mia")}) || len(preview.Loses) != 0 ||
		slices.Contains(preview.AudienceBefore.Persons, "mia") || !slices.Contains(preview.AudienceAfter.Persons, "mia") {
		t.Fatalf("preview = %+v", preview)
	}
	o.do(t, "mia", "GET", path, -1, nil, 404, nil)
	var added acl.Grant
	o.do(t, "santosh", "POST", grants, -1, personGrant("mia", "view"), 201, &added)
	if added.GrantedBy != (acl.Principal{Kind: "person", ID: "santosh"}) || !strings.HasPrefix(added.ID, "g_") {
		t.Fatalf("added = %+v", added)
	}
	o.do(t, "mia", "GET", grants, -1, nil, 200, &listed)
	if len(listed.Grants) != 3 || listed.Grants[2].ID != added.ID || !listed.Grants[2].Live {
		t.Fatalf("live grants = %+v", listed.Grants)
	}
	var audit struct {
		Events []record.Event `json:"events"`
	}
	o.do(t, "mia", "GET", "/v1/audit", -1, nil, 200, &audit)
	if !slices.ContainsFunc(audit.Events, func(e record.Event) bool {
		return e.Kind == record.ResourceAccessChanged && e.Verb == "shared" && e.Handle == "santosh"
	}) {
		t.Fatalf("grant missing from the record: %+v", audit.Events)
	}
	o.do(t, "mia", "DELETE", grants+"/"+added.ID, -1, nil, 403, nil)
	o.do(t, "santosh", "DELETE", grants+"/"+added.ID, -1, nil, 204, nil)
	o.do(t, "mia", "GET", grants, -1, nil, 404, nil)
}

// Contract B2, C3, C5, D2. A run grant changes real job doors without giving edit.
func TestAccessCustomRoleAndExpiry(t *testing.T) {
	o := jobOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Queue handling"})
	p := o.setAccessPolicy(t, func(p *acl.Policy) {
		p.Roles = append(p.Roles, acl.Role{Name: "runner", Verbs: []string{acl.VerbView, acl.VerbRun}})
		p.Sharing.Who = acl.OwnerOnly
	})
	grants := accessWorkstreamPath(ws.ID) + "/grants"
	o.do(t, "santosh", "POST", grants, -1, personGrant("abir", "full"), 201, nil)
	o.do(t, "abir", "POST", grants, -1, personGrant("mia", "view"), 403, nil)
	o.do(t, "santosh", "POST", grants, -1, personGrant("mia", "view"), 201, nil)
	job := o.santoshJob(t, ws.ID)
	o.do(t, "mia", "POST", jobPath(job, "stop"), -1, map[string]any{}, 403, nil)

	var added acl.Grant
	expiry := o.now.Add(time.Hour)
	req := personGrant("mia", "runner")
	req.ExpiresAt = &expiry
	o.do(t, "santosh", "POST", grants, -1, req, 201, &added)
	var reader api.OrgResource
	o.do(t, "mia", "GET", accessWorkstreamPath(ws.ID), -1, nil, 200, &reader)
	if reader.MyRole != "runner" || !slices.Equal(reader.MyVerbs, []string{acl.VerbView, acl.VerbRun}) {
		t.Fatalf("runner resource = %+v", reader)
	}
	o.do(t, "mia", "PATCH", "/v1/workstreams/"+ws.ID, ws.Rev, map[string]any{"purpose": "change"}, 403, nil)
	o.do(t, "mia", "POST", jobPath(job, "stop"), -1, map[string]any{}, 200, nil)
	o.do(t, "owner", "PUT", policyPath, p.Rev, acl.DefaultPolicy(), 422, nil)

	o.now = expiry
	o.do(t, "mia", "GET", accessWorkstreamPath(ws.ID), -1, nil, 200, &reader)
	if reader.MyRole != "view" {
		t.Fatalf("expired grant still active: %+v", reader)
	}
	var list api.ResourceGrants
	o.do(t, "mia", "GET", grants, -1, nil, 200, &list)
	if slices.ContainsFunc(list.Grants, func(g api.GrantTuple) bool { return g.ID == added.ID }) {
		t.Fatalf("expired grant was listed: %+v", list)
	}
	rows, err := o.st.GrantsOn("workstream", ws.ID)
	if err != nil || !slices.ContainsFunc(rows, func(g acl.Grant) bool { return g.ID == added.ID }) {
		t.Fatalf("expiry removed the audit row: %+v, %v", rows, err)
	}
}

// Contract B5, C6, E1, E3 and E5. Grant attribution names the current agent and its sponsor.
func TestAccessAgentGrantAndScope(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Status reports"})
	path := accessWorkstreamPath(ws.ID)
	grants := path + "/grants"
	refused := accessAgentRequest(t, o, "POST", grants, "santosh", "afi", personGrant("mia", "view"), 403, nil)
	if !strings.Contains(refused, "agents cannot grant access") {
		t.Fatalf("agent refusal = %s", refused)
	}
	o.setAccessPolicy(t, func(p *acl.Policy) {
		p.Agents.MayGrant = true
		p.Agents.Scopes = map[string]string{"reporter": "edit"}
	})
	accessAgentRequest(t, o, "POST", grants, "santosh", "reporter", personGrant("mia", "view"), 403, nil)
	accessAgentRequest(t, o, "POST", grants, "santosh", "afi", personGrant("mia", "full"), 403, nil)
	var g acl.Grant
	accessAgentRequest(t, o, "POST", grants, "santosh", "afi", personGrant("abir", "view"), 201, &g)
	if g.GrantedBy != (acl.Principal{Kind: "agent", ID: "afi", Sponsor: "santosh"}) {
		t.Fatalf("agent attribution = %+v", g)
	}
	var got api.OrgResource
	accessAgentRequest(t, o, "GET", path, "abir", "afi", nil, 200, &got)
	if got.MyRole != "view" || !slices.Equal(got.MyVerbs, []string{acl.VerbView}) {
		t.Fatalf("agent reached more than its sponsor: %+v", got)
	}
	o.do(t, "santosh", "DELETE", grants+"/"+g.ID, -1, nil, 204, nil)
	accessAgentRequest(t, o, "GET", path, "abir", "afi", nil, 404, nil)
}

// Contract C4, E6, F1, G1. New chats keep their creation audience and agent-created chats name their sponsor.
func TestAccessChatRegistry(t *testing.T) {
	o := newWSOrg(t)
	o.h.config.WorkstreamAgent = func(context.Context, AgentTurn) (AgentTurnEnd, error) { return AgentTurnEnd{}, nil }
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Research"})
	chats := "/v1/workstreams/" + ws.ID + "/chats"
	var oldChat, orgChat, newer api.AgentChat
	o.do(t, "santosh", "POST", chats, -1, api.AgentChatNew{Title: "Private idea"}, 201, &oldChat)
	oldPath := "/v1/resources/chat/" + oldChat.ID
	o.do(t, "owner", "GET", oldPath, -1, nil, 404, nil)
	o.do(t, "owner", "GET", oldPath+"/grants", -1, nil, 404, nil)
	o.setAccessPolicy(t, func(p *acl.Policy) {
		p.Defaults["chat"] = acl.KindDefault{Visibility: acl.Org}
	})
	o.do(t, "santosh", "POST", chats, -1, api.AgentChatNew{Title: "Shared idea"}, 201, &orgChat)
	orgPath := "/v1/resources/chat/" + orgChat.ID
	o.do(t, "abir", "GET", oldPath, -1, nil, 404, nil)
	var org api.OrgResource
	o.do(t, "abir", "GET", orgPath, -1, nil, 200, &org)
	if org.Visibility != acl.Org || org.Parent == nil || org.Parent.ID != ws.ID || org.Owner.ID != "santosh" || org.MyRole != "view" {
		t.Fatalf("org chat = %+v", org)
	}
	var grantList api.ResourceGrants
	o.do(t, "mia", "GET", orgPath+"/grants", -1, nil, 200, &grantList)
	if !slices.ContainsFunc(grantList.Grants, func(g api.GrantTuple) bool {
		return g.Subject == (acl.Subject{Kind: "org", ID: "*"}) && g.Role == "view" && g.Source == "policy"
	}) {
		t.Fatalf("org chat grant = %+v", grantList)
	}

	o.setAccessPolicy(t, func(p *acl.Policy) {
		p.Agents.MayCreate = false
		p.Defaults["chat"] = acl.KindDefault{Visibility: acl.Private}
	})
	o.do(t, "santosh", "POST", chats, -1, api.AgentChatNew{Title: "Private again"}, 201, &newer)
	o.do(t, "mia", "GET", orgPath, -1, nil, 200, nil)
	o.do(t, "mia", "GET", "/v1/resources/chat/"+newer.ID, -1, nil, 404, nil)
	create := api.ResourceNew{Kind: "chat", Parent: ws.ID, Label: "Assistant notes"}
	if words := accessAgentRequest(t, o, "POST", "/v1/resources", "santosh", "afi", create, 403, nil); !strings.Contains(words, "agents cannot create") {
		t.Fatalf("creation refusal = %s", words)
	}
	accessAgentRequest(t, o, "POST", chats, "santosh", "afi", api.AgentChatNew{}, 403, nil)
	o.setAccessPolicy(t, func(p *acl.Policy) { p.Agents.MayCreate = true })
	var agentChat api.OrgResource
	accessAgentRequest(t, o, "POST", "/v1/resources", "santosh", "afi", create, 201, &agentChat)
	if agentChat.Owner.ID != "santosh" || agentChat.CreatedBy == nil ||
		*agentChat.CreatedBy != (acl.Principal{Kind: "agent", ID: "afi", Sponsor: "santosh"}) {
		t.Fatalf("agent chat = %+v", agentChat)
	}
	o.do(t, "santosh", "POST", oldPath+"/grants", -1, personGrant("mia", "view"), 201, nil)
	o.do(t, "mia", "GET", oldPath, -1, nil, 200, nil)
	var list api.ResourceList
	o.do(t, "owner", "GET", "/v1/resources/chat", -1, nil, 200, &list)
	// CONTRACT CHANGE (org-follow A4): oldChat is private and shared with mia alone, so
	// it stays private and the operator does not list it; before, its first grant
	// un-privated it and the operator saw it. Only the org-visible chat remains.
	if len(list.Resources) != 1 || list.Resources[0].ID != orgChat.ID || slices.ContainsFunc(list.Resources, func(res api.OrgResource) bool {
		return res.ID == newer.ID || res.ID == agentChat.ID || res.ID == oldChat.ID
	}) {
		t.Fatalf("operator list includes private chats: %+v", list)
	}
}

// Contract A3, B4, C7, D2, G1. Privacy changes revoke stored grants and explicit values take precedence.
func TestAccessWorkstreamDefaultsAndPrivacy(t *testing.T) {
	o := newWSOrg(t)
	o.setAccessPolicy(t, func(p *acl.Policy) { p.Defaults["workstream"] = acl.KindDefault{Visibility: acl.Org} })
	shared := o.create(t, "santosh", api.WorkstreamNew{Name: "Bulletin"})
	var explicit api.Workstream
	o.do(t, "santosh", "POST", "/v1/workstreams", -1, map[string]any{"name": "Planning", "private": false}, 201, &explicit)
	o.do(t, "mia", "GET", accessWorkstreamPath(shared.ID), -1, nil, 200, nil)
	o.do(t, "mia", "GET", accessWorkstreamPath(explicit.ID), -1, nil, 404, nil)
	var g acl.Grant
	o.do(t, "santosh", "POST", accessWorkstreamPath(shared.ID)+"/grants", -1, personGrant("mia", "edit"), 201, &g)
	private := true
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+shared.ID+"/shares", -1, api.WorkstreamSharesPut{Private: &private}, 200, nil)
	o.do(t, "owner", "GET", accessWorkstreamPath(shared.ID), -1, nil, 404, nil)
	rows, err := o.st.GrantsOn("workstream", shared.ID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("private workstream retained grants: %+v, %v", rows, err)
	}
	private = false
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+shared.ID+"/shares", -1, api.WorkstreamSharesPut{Private: &private}, 200, nil)
	o.do(t, "mia", "GET", accessWorkstreamPath(shared.ID), -1, nil, 404, nil)
}

type accessSSEEvent struct {
	id, kind string
	data     []byte
}

func readPolicySSE(t *testing.T, o *wsOrg, who, cursor string) []accessSSEEvent {
	t.Helper()
	server := httptest.NewServer(o.h)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+o.tokens[who])
	req.Header.Set("Last-Event-ID", cursor)
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("events status = %d", resp.StatusCode)
	}
	var out []accessSSEEvent
	var event accessSSEEvent
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "id: "):
			event.id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			event.kind = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			event.data = bytes.Clone(scanner.Bytes()[6:])
		case line == "" && event.kind != "":
			out = append(out, event)
			if event.kind == eventOrgPolicyChanged {
				return out
			}
			event = accessSSEEvent{}
		}
	}
	t.Fatalf("stream ended before the policy event: %v, events %+v", scanner.Err(), out)
	return nil
}

// Contract A4, C7, D1, D2. SSE sends one event per changed audience, including to someone who lost access.
func TestAccessPolicyAudienceEvents(t *testing.T) {
	o := newWSOrg(t)
	private := o.create(t, "santosh", api.WorkstreamNew{Name: "Private draft", Private: true})
	open := o.create(t, "santosh", api.WorkstreamNew{Name: "Ordinary work"})
	var chat api.OrgResource
	o.do(t, "santosh", "POST", "/v1/resources", -1, api.ResourceNew{Kind: "chat", Parent: open.ID, Label: "Private chat"}, 201, &chat)
	const canvas = "cv_demo01234567"
	if _, err := o.st.EditCanvases(func(all factory.OrgCanvases) (bool, error) {
		all[canvas] = factory.OrgCanvas{ID: canvas, Owner: factory.WorkstreamOwner{Kind: "person", ID: "santosh"}, Private: true}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	_, cursor, err := live.Read(o.st.Root())
	if err != nil || cursor == "" {
		t.Fatalf("initial event cursor = %q, %v", cursor, err)
	}
	for _, see := range []bool{true, false} {
		o.setAccessPolicy(t, func(p *acl.Policy) { p.Operator.SeesPrivate = see })
		status := 404
		if see {
			status = 200
		}
		o.do(t, "owner", "GET", accessWorkstreamPath(private.ID), -1, nil, status, nil)
		o.do(t, "owner", "GET", "/v1/resources/chat/"+chat.ID, -1, nil, status, nil)
		o.do(t, "owner", "GET", "/v1/resources/canvas/"+canvas, -1, nil, status, nil)
		for _, who := range []string{"owner", "santosh", "abir", "mia"} {
			events := readPolicySSE(t, o, who, cursor)
			seen := map[string]int{}
			for _, e := range events {
				if !slices.Contains([]string{api.EventWorkstreamChanged, api.EventCanvasChanged, eventResourceChanged}, e.kind) {
					continue
				}
				var payload struct {
					ID      string   `json:"id"`
					Persons []string `json:"persons"`
				}
				if err := json.Unmarshal(e.data, &payload); err != nil || len(payload.Persons) != 0 {
					t.Fatalf("event payload = %s, %v", e.data, err)
				}
				seen[payload.ID]++
			}
			want := map[string]int{}
			if who == "owner" || who == "santosh" {
				want = map[string]int{private.ID: 1, chat.ID: 1, canvas: 1}
			}
			if !reflect.DeepEqual(seen, want) {
				t.Fatalf("%s saw %v, wanted %v (ordinary %s)", who, seen, want, open.ID)
			}
		}
		_, cursor, err = live.Read(o.st.Root())
		if err != nil {
			t.Fatal(err)
		}
	}
}

// Contract A1, A2, A3, A4. Explicitly saving the default preserves the absent-row behavior.
func TestDefaultPolicyReproducesToday(t *testing.T) {
	for _, persisted := range []bool{false, true} {
		name := "absent"
		if persisted {
			name = "stored"
		}
		t.Run(name, func(t *testing.T) {
			o := newWSOrg(t)
			if persisted {
				o.do(t, "owner", "PUT", policyPath, 0, acl.DefaultPolicy(), 200, nil)
			}
			ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Shared release"})
			private := o.create(t, "santosh", api.WorkstreamNew{Name: "Private release", Private: true})
			o.do(t, "mia", "GET", "/v1/workstreams/"+ws.ID, -1, nil, 404, nil)
			o.do(t, "owner", "GET", "/v1/workstreams/"+private.ID, -1, nil, 404, nil)
			o.do(t, "owner", "GET", "/v1/workstreams/"+ws.ID, -1, nil, 200, nil)
			o.do(t, "owner", "PATCH", "/v1/workstreams/"+ws.ID, ws.Rev, map[string]string{"purpose": "change"}, 403, nil)
			var shared api.WorkstreamShared
			o.do(t, "owner", "PUT", "/v1/workstreams/"+ws.ID+"/shares", -1, api.WorkstreamSharesPut{Teams: []string{"tm_growth"}}, 200, &shared)
			var edited api.Workstream
			o.do(t, "mia", "PATCH", "/v1/workstreams/"+ws.ID, shared.Workstream.Rev, map[string]string{"purpose": "change"}, 200, &edited)
			if edited.MyRole != "edit" || edited.Purpose != "change" {
				t.Fatalf("legacy edit = %+v", edited)
			}
		})
	}
}

// Contract D1. A large update remains visible as a batch until the resync threshold.
func TestAccessPolicyAudienceEventLimit(t *testing.T) {
	o := newWSOrg(t)
	o.create(t, "santosh", api.WorkstreamNew{Name: "Cursor"})
	addPrivate := func(first, last int) {
		t.Helper()
		_, err := o.st.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
			changed := make([]string, 0, last-first)
			for i := first; i < last; i++ {
				id := "ws_private_" + itoa(i)
				all[id] = factory.OrgWorkstream{ID: id, Name: "Private work",
					Owner: factory.WorkstreamOwner{Kind: "person", ID: "santosh"}, Private: true}
				changed = append(changed, id)
			}
			return changed, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	cursor := func() string {
		t.Helper()
		_, id, err := live.Read(o.st.Root())
		if err != nil || id == "" {
			t.Fatalf("event cursor = %q, %v", id, err)
		}
		return id
	}
	addPrivate(0, live.PolicyChangeLimit)
	before := cursor()
	o.setAccessPolicy(t, func(p *acl.Policy) { p.Operator.SeesPrivate = true })
	events := readPolicySSE(t, o, "owner", before)
	count := 0
	for _, e := range events {
		if e.kind == api.EventWorkstreamChanged {
			count++
		}
	}
	if count != live.PolicyChangeLimit {
		t.Fatalf("operator received %d item events, want %d", count, live.PolicyChangeLimit)
	}
	addPrivate(live.PolicyChangeLimit, live.PolicyChangeLimit+1)
	before = cursor()
	o.setAccessPolicy(t, func(p *acl.Policy) { p.Operator.SeesPrivate = false })
	events = readPolicySSE(t, o, "owner", before)
	for _, e := range events {
		if e.kind == api.EventWorkstreamChanged {
			t.Fatalf("sent an item event above the resync threshold: %s", e.data)
		}
	}
}

// Contract C4, F1 and G1. A restart registers old chats privately under the current policy.
func TestAccessBackfillsOlderChats(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Historical work"})
	o.setAccessPolicy(t, func(p *acl.Policy) { p.Defaults["chat"] = acl.KindDefault{Visibility: acl.Org} })
	oldID := "ch_" + strings.Repeat("1", 26)
	at := o.now.UTC().Format(time.RFC3339Nano)
	if err := writeChatMeta(o.h.chatDir(ws.ID, "santosh", oldID), chatMeta{Title: "Older chat", CreatedAt: at, UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	o.h.Close()
	o.h = New(o.seam, o.cfg)
	t.Cleanup(o.h.Close)

	path := "/v1/resources/chat/" + oldID
	var got api.OrgResource
	o.do(t, "santosh", "GET", path, -1, nil, 200, &got)
	if got.Visibility != acl.Private || got.Label != "Older chat" || got.CreatedAt == nil ||
		got.CreatedAt.Format(time.RFC3339Nano) != at || got.Parent == nil || got.Parent.ID != ws.ID {
		t.Fatalf("backfilled chat = %+v", got)
	}
	o.do(t, "owner", "GET", path, -1, nil, 404, nil)
	o.do(t, "mia", "GET", path, -1, nil, 404, nil)
}

// Contract B1, B3 and F1. A canvas uses grants on its own item and its one parent.
func TestAccessCanvasInheritsWorkstreamGrants(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Project"})
	const canvas = "cv_demo01234567"
	if _, err := o.st.EditCanvases(func(all factory.OrgCanvases) (bool, error) {
		all[canvas] = factory.OrgCanvas{ID: canvas, Workstream: ws.ID, Owner: factory.WorkstreamOwner{Kind: "person", ID: "santosh"},
			Shares: []factory.Share{{Kind: "person", ID: "mia", Role: "comment"}}}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	path := "/v1/resources/canvas/" + canvas
	o.do(t, "abir", "GET", path, -1, nil, 404, nil)
	var shared acl.Grant
	parentGrants := accessWorkstreamPath(ws.ID) + "/grants"
	o.do(t, "santosh", "POST", parentGrants, -1, personGrant("abir", "edit"), 201, &shared)
	var got api.OrgResource
	o.do(t, "abir", "GET", path, -1, nil, 200, &got)
	if got.Parent == nil || got.Parent.ID != ws.ID || got.Owner.ID != "santosh" || got.MyRole != "edit" {
		t.Fatalf("inherited canvas = %+v", got)
	}
	o.do(t, "mia", "GET", path, -1, nil, 200, &got)
	if got.MyRole != "comment" {
		t.Fatalf("canvas share did not add comment: %+v", got)
	}
	o.do(t, "santosh", "DELETE", parentGrants+"/"+shared.ID, -1, nil, 204, nil)
	o.do(t, "abir", "GET", path, -1, nil, 404, nil)
	o.do(t, "mia", "GET", path, -1, nil, 200, nil)
}

// Contract C7. The operator's view bundle and governance flag give separate permissions.
func TestAccessOperatorPolicyControlsGovernance(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Operator choices"})
	path := "/v1/workstreams/" + ws.ID
	o.setAccessPolicy(t, func(p *acl.Policy) {
		p.Operator.MayGovern = false
		p.Operator.Role = "edit"
	})
	var got api.Workstream
	o.do(t, "owner", "GET", path, -1, nil, 200, &got)
	if got.MyRole != "edit" {
		t.Fatalf("operator role = %q", got.MyRole)
	}
	o.do(t, "owner", "PATCH", path, got.Rev, map[string]any{"purpose": "Keep decisions visible"}, 200, nil)
	o.do(t, "owner", "PUT", path+"/owner", -1, api.WorkstreamOwner{Kind: "person", ID: "mia"}, 403, nil)
	o.do(t, "owner", "POST", accessWorkstreamPath(ws.ID)+"/grants", -1, personGrant("mia", "view"), 403, nil)
}

// Contract E6. A named agent's canvas belongs to the sponsor, even in a team workstream.
func TestAccessAgentCanvasCreation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the canvas response fixture uses a Unix shell")
	}
	o := newWSOrg(t)
	sidecar := filepath.Join(t.TempDir(), "canvas")
	// The sidecar supplies the two canvas responses needed to exercise server ownership.
	script := `#!/bin/sh
IFS= read -r request || :
case "$request" in
  *'"cmd":"create"'*) printf '%s\n' '{"id":"cv_demo01234567"}' ;;
  *'"cmd":"summary"'*) printf '%s\n' '{"title":"Agent canvas","seq":1}' ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(sidecar, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	o.h.config.Canvas = canvasrpc.New(sidecar, o.st.CanvasRoot())
	o.h.canvasRoutes()
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Team work",
		Owner: &api.WorkstreamOwner{Kind: "team", ID: "tm_platform"}})
	body := api.CanvasNew{Title: "Agent canvas", Workstream: ws.ID}
	o.setAccessPolicy(t, func(p *acl.Policy) { p.Agents.MayCreate = false })
	if words := accessAgentRequest(t, o, "POST", "/v1/canvases", "santosh", "designer", body, 403, nil); !strings.Contains(words, "agents cannot create") {
		t.Fatalf("canvas refusal = %s", words)
	}
	o.setAccessPolicy(t, func(p *acl.Policy) { p.Agents.MayCreate = true })
	var canvas api.CanvasRow
	accessAgentRequest(t, o, "POST", "/v1/canvases", "santosh", "designer", body, 201, &canvas)
	var got api.OrgResource
	o.do(t, "abir", "GET", "/v1/resources/canvas/"+canvas.ID, -1, nil, 200, &got)
	if got.Owner != (acl.Subject{Kind: "person", ID: "santosh"}) || got.CreatedBy == nil ||
		*got.CreatedBy != (acl.Principal{Kind: "agent", ID: "designer", Sponsor: "santosh"}) || got.MyRole != "full" {
		t.Fatalf("agent canvas = %+v", got)
	}
}

// Contract F1, G1. A grant on a private chat does not disclose its private parent.
func TestAccessSharedChatHidesPrivateParent(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Private work", Private: true})
	var chat api.OrgResource
	o.do(t, "santosh", "POST", "/v1/resources", -1, api.ResourceNew{Kind: "chat", Parent: ws.ID, Label: "Shared notes"}, 201, &chat)
	path := "/v1/resources/chat/" + chat.ID
	o.do(t, "santosh", "POST", path+"/grants", -1, personGrant("mia", "view"), 201, nil)
	var shared api.OrgResource
	o.do(t, "mia", "GET", path, -1, nil, 200, &shared)
	if shared.Parent != nil {
		t.Fatalf("private parent disclosed in %+v", shared)
	}
	o.do(t, "owner", "GET", accessWorkstreamPath(ws.ID), -1, nil, 404, nil)
}
