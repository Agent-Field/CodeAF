package serve

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

const testOrgKey = "sk-or-v1-0123456789abcdefWXYZ"

// THE ORGANIZATION'S MODEL KEY GOES IN AND NEVER COMES BACK. Only the
// operator reads the list or changes it; the list says the last four
// characters; the file holding it is the server user's alone.
func TestOrgKeyIsWrittenAndNeverAnswered(t *testing.T) {
	o := newWSOrg(t)
	o.do(t, "abir", "GET", "/v1/org/keys", -1, nil, 403, nil)
	o.do(t, "abir", "PUT", "/v1/org/keys/openrouter", -1, api.OrgKeyPut{Key: testOrgKey}, 403, nil)
	o.do(t, "owner", "PUT", "/v1/org/keys/openrouter", -1, api.OrgKeyPut{Key: "short"}, 422, nil)
	o.do(t, "owner", "PUT", "/v1/org/keys/anthropic", -1, api.OrgKeyPut{Key: testOrgKey}, 404, nil)
	var set api.OrgKey
	o.do(t, "owner", "PUT", "/v1/org/keys/openrouter", -1, api.OrgKeyPut{Key: testOrgKey}, 200, &set)
	if set.Provider != "openrouter" || set.Last4 != "WXYZ" || set.SetBy != "owner" {
		t.Fatalf("set %+v", set)
	}
	r := httptest.NewRequest("GET", "/v1/org/keys", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Authorization", "Bearer "+o.tokens["owner"])
	w := httptest.NewRecorder()
	o.h.ServeHTTP(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), "0123456789") || !strings.Contains(w.Body.String(), `"last4":"WXYZ"`) {
		t.Fatalf("the list answered %d %s", w.Code, w.Body)
	}
	info, err := os.Stat(filepath.Join(o.st.Root(), "org-keys.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key file %v, %v", info, err)
	}
	if key, ok := o.st.OrgKey("openrouter"); !ok || key != testOrgKey {
		t.Fatalf("the server's own runner reads %q", key)
	}
	o.do(t, "owner", "DELETE", "/v1/org/keys/openrouter", -1, nil, 204, nil)
	var list api.OrgKeyList
	o.do(t, "owner", "GET", "/v1/org/keys", -1, nil, 200, &list)
	if len(list.Keys) != 0 || !slices.Equal(list.Providers, []string{"openrouter"}) {
		t.Fatalf("after delete %+v", list)
	}
}

// "SERVER" IS OFFERED ONLY BY A SERVER THAT CAN RUN AGENTS: one with a runner
// AND a key. Choosing it lets go of every driver's hand.
func TestServerRunnerIsOfferedWithARunnerAndAKey(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	server := "server"
	runners := func() []string {
		var got api.FactorySettingsView
		o.do(t, "abir", "GET", "/v1/settings", -1, nil, 200, &got)
		return got.AgentRunners
	}
	o.h.config.WorkstreamAgent = func(context.Context, AgentTurn) (AgentTurnEnd, error) { return AgentTurnEnd{}, nil }
	if got := runners(); !slices.Equal(got, []string{"driver"}) {
		t.Fatalf("a runner with no key offers %v", got)
	}
	o.do(t, "owner", "PATCH", "/v1/settings", -1, api.PatchFactorySettings{AgentRunner: &server}, 422, nil)
	o.do(t, "owner", "PUT", "/v1/org/keys/openrouter", -1, api.OrgKeyPut{Key: testOrgKey}, 200, nil)
	if got := runners(); !slices.Equal(got, []string{"driver", "server"}) {
		t.Fatalf("a runner with a key offers %v", got)
	}
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+webhook.ID+"/driver", -1, nil, 200, nil)
	o.do(t, "owner", "PATCH", "/v1/settings", -1, api.PatchFactorySettings{AgentRunner: &server}, 200, nil)
	var ws api.Workstream
	o.do(t, "abir", "GET", "/v1/workstreams/"+webhook.ID, -1, nil, 200, &ws)
	if ws.Driver != nil {
		t.Fatalf("still driven by %+v in server mode", ws.Driver)
	}
	o.do(t, "abir", "PUT", "/v1/workstreams/"+webhook.ID+"/driver", -1, nil, 409, nil)
	o.h.config.WorkstreamAgent = nil
	if got := runners(); !slices.Equal(got, []string{"driver"}) {
		t.Fatalf("no runner offers %v", got)
	}
}

// serverMode turns the organization to running agents on its server, with
// runner as the turn.
func (o *wsOrg) serverMode(t *testing.T, runner AgentRunner) {
	t.Helper()
	o.h.config.WorkstreamAgent = runner
	o.do(t, "owner", "PUT", "/v1/org/keys/openrouter", -1, api.OrgKeyPut{Key: testOrgKey}, 200, nil)
	server := "server"
	o.do(t, "owner", "PATCH", "/v1/settings", -1, api.PatchFactorySettings{AgentRunner: &server}, 200, nil)
}

// settle waits for every answer in flight to end.
func (o *wsOrg) settle() { o.h.agentDesk.wg.Wait() }

// ONE CONVERSATION, READ BY EVERYONE WHO CAN SEE THE WORKSTREAM, AND SPOKEN
// TO BY THOSE WHO CAN EDIT IT. The agent works with the organization's key,
// writes as the person who asked, proposes decisions a person keeps, and what
// it spent is posted under that person.
func TestServerAgentAnswersEveryoneWhoCanEdit(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, brand := o.orgScene(t)
	var page api.Workstream
	o.do(t, "abir", "POST", "/v1/workstreams/"+webhook.ID+"/pages", -1, api.PageNew{Title: "Retries"}, 201, &page)
	retries := page.Pages[len(page.Pages)-1].ID

	var seen AgentTurn
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		seen = turn
		ws, err := turn.Door.Workstream(ctx)
		if err != nil || ws.Name != "Webhook reliability" {
			t.Errorf("the door read %q, %v", ws.Name, err)
		}
		turn.Step(api.AgentEntryStep, "read the workstream", "workstream", false)
		line, err := turn.Door.WritePage(ctx, retries, "", "Retry three times with backoff.")
		if err != nil || !strings.Contains(line, "wrote the page") {
			t.Errorf("writing an unopened page: %q, %v", line, err)
		}
		if _, err := turn.Door.WritePage(ctx, "", "Runbook", "# Runbook\n\nPage someone."); err != nil {
			t.Errorf("a new page: %v", err)
		}
		if _, err := turn.Door.Decide(ctx, "Retries stop after three tries."); err != nil {
			t.Errorf("deciding: %v", err)
		}
		turn.Step(api.AgentEntryAgent, "Done: retries are written down.", "", false)
		return AgentTurnEnd{USD: 0.25}, nil
	})

	// A reader (the operator only views) reads it and has no composer, and
	// someone who cannot see the workstream learns nothing.
	var got api.WorkstreamAgent
	o.do(t, "owner", "GET", "/v1/workstreams/"+webhook.ID+"/agent", -1, nil, 200, &got)
	if got.CanTalk || got.Cannot != "" || got.State != api.AgentIdle {
		t.Fatalf("the operator reads %+v", got)
	}
	o.do(t, "owner", "POST", "/v1/workstreams/"+webhook.ID+"/agent/messages", -1, api.AgentMessageNew{Text: "hi"}, 403, nil)
	o.do(t, "abir", "GET", "/v1/workstreams/"+brand.ID+"/agent", -1, nil, 404, nil)

	o.do(t, "mia", "GET", "/v1/workstreams/"+webhook.ID+"/agent", -1, nil, 200, &got)
	if !got.CanTalk || got.Cannot != "" {
		t.Fatalf("mia (edit through Growth) reads %+v", got)
	}
	o.do(t, "mia", "POST", "/v1/workstreams/"+webhook.ID+"/agent/messages", -1, api.AgentMessageNew{Text: "Write down how retries work."}, 202, &got)
	o.settle()
	if seen.Asker != "mia" || seen.Key != testOrgKey || seen.Name != "Webhook reliability" || !strings.HasPrefix(seen.Dir, o.st.Root()) {
		t.Fatalf("the runner was handed %+v", seen)
	}

	o.do(t, "abir", "GET", "/v1/workstreams/"+webhook.ID+"/agent", -1, nil, 200, &got)
	kinds := []string{}
	for _, e := range got.Entries {
		kinds = append(kinds, e.Kind)
	}
	if !slices.Equal(kinds, []string{"person", "step", "agent"}) || got.Entries[0].By != "mia" || got.State != api.AgentIdle || got.Seq != 3 {
		t.Fatalf("abir reads %v %+v", kinds, got)
	}
	var since api.WorkstreamAgent
	o.do(t, "abir", "GET", "/v1/workstreams/"+webhook.ID+"/agent?since=2", -1, nil, 200, &since)
	if len(since.Entries) != 1 || since.Entries[0].Kind != "agent" {
		t.Fatalf("since=2 reads %+v", since.Entries)
	}

	var ws api.Workstream
	o.do(t, "abir", "GET", "/v1/workstreams/"+webhook.ID, -1, nil, 200, &ws)
	if ws.SpendUSD != 0.25 {
		t.Fatalf("spend %v", ws.SpendUSD)
	}
	at := slices.IndexFunc(ws.Pages, func(p api.WorkstreamPage) bool { return p.ID == retries })
	if ws.Pages[at].BodyMD != "Retry three times with backoff." || ws.Pages[at].UpdatedBy != "mia" {
		t.Fatalf("retries page %+v", ws.Pages[at])
	}
	if !slices.ContainsFunc(ws.Pages, func(p api.WorkstreamPage) bool {
		return p.Title == "Runbook" && strings.Contains(p.BodyMD, "Page someone")
	}) {
		t.Fatalf("no runbook in %+v", ws.Pages)
	}
	if n := len(ws.Decisions); n != 1 || ws.Decisions[0].State != "proposed" || ws.Decisions[0].By != "mia" {
		t.Fatalf("decisions %+v", ws.Decisions)
	}
	spends, err := o.st.WorkstreamSpend()
	if err != nil || spends[webhook.ID] != 0.25 {
		t.Fatalf("spend kept %v, %v", spends, err)
	}

	// The conversation survives a restart of the server.
	o.h.agentDesk.mu.Lock()
	o.h.agentDesk.agents = nil
	o.h.agentDesk.mu.Unlock()
	o.do(t, "mia", "GET", "/v1/workstreams/"+webhook.ID+"/agent", -1, nil, 200, &got)
	if len(got.Entries) != 3 || got.Seq != 3 {
		t.Fatalf("after a restart %+v", got)
	}
}

// ONE MESSAGE AT A TIME, AND ANYONE WHO CAN EDIT MAY STOP IT. A second
// message while one is answered is refused naming whose it is answering.
func TestServerAgentIsBusyThenStopped(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	started := make(chan struct{})
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		close(started)
		<-ctx.Done()
		return AgentTurnEnd{USD: 0.01}, ctx.Err()
	})
	path := "/v1/workstreams/" + webhook.ID + "/agent"
	o.do(t, "santosh", "POST", path+"/messages", -1, api.AgentMessageNew{Text: "think hard"}, 202, nil)
	<-started
	var busy map[string]string
	o.do(t, "abir", "POST", path+"/messages", -1, api.AgentMessageNew{Text: "me too"}, 409, &busy)
	if busy["reason"] != "busy" || busy["by"] != "santosh" || !strings.Contains(busy["error"], "still answering santosh") {
		t.Fatalf("abir was told %v", busy)
	}
	o.do(t, "owner", "POST", path+"/stop", -1, nil, 403, nil)
	var got api.WorkstreamAgent
	o.do(t, "abir", "POST", path+"/stop", -1, nil, 200, &got)
	if got.State != api.AgentStopping || got.For != "abir" {
		t.Fatalf("stopping %+v", got.AgentState)
	}
	o.settle()
	o.do(t, "abir", "GET", path, -1, nil, 200, &got)
	last := got.Entries[len(got.Entries)-1]
	if got.State != api.AgentIdle || last.Kind != api.AgentEntryNote || last.Text != "Stopped by abir." {
		t.Fatalf("after the stop %+v, last %+v", got.AgentState, last)
	}
}

// A PAGE BEING CO-EDITED IS NOT WRITTEN OVER: the agent's text becomes a
// version in its history, and the live words stay as they are.
func TestServerAgentKeepsALivePagesTextAsAVersion(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	var page api.Workstream
	o.do(t, "abir", "POST", "/v1/workstreams/"+webhook.ID+"/pages", -1, api.PageNew{Title: "Plan"}, 201, &page)
	pid := page.Pages[len(page.Pages)-1].ID
	ds, err := o.h.docStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ds.Append("ws/"+webhook.ID+"/page/"+pid, "abir", o.now, []byte{1, 2, 3}, 0, true, nil); err != nil {
		t.Fatal(err)
	}
	var line string
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		line, err = turn.Door.WritePage(ctx, pid, "", "The agent's plan.")
		return AgentTurnEnd{}, err
	})
	o.do(t, "santosh", "POST", "/v1/workstreams/"+webhook.ID+"/agent/messages", -1, api.AgentMessageNew{Text: "plan it"}, 202, nil)
	o.settle()
	if err != nil || !strings.Contains(line, "kept as version 1") {
		t.Fatalf("the agent was told %q, %v", line, err)
	}
	var versions api.PageVersions
	o.do(t, "abir", "GET", "/v1/workstreams/"+webhook.ID+"/pages/"+pid+"/versions", -1, nil, 200, &versions)
	if len(versions.Versions) != 1 || versions.Versions[0].Reason != "agent" || versions.Versions[0].By != "santosh" {
		t.Fatalf("versions %+v", versions.Versions)
	}
	var ws api.Workstream
	o.do(t, "abir", "GET", "/v1/workstreams/"+webhook.ID, -1, nil, 200, &ws)
	at := slices.IndexFunc(ws.Pages, func(p api.WorkstreamPage) bool { return p.ID == pid })
	if ws.Pages[at].BodyMD != "" {
		t.Fatalf("the live page's mirror was written: %q", ws.Pages[at].BodyMD)
	}
}

// A WORKSTREAM THAT HAS SPENT ITS BUDGET TAKES NO MORE MESSAGES, and a person
// who lost edit cannot write through the agent.
func TestServerAgentStopsAtTheBudgetAndChecksRightsOnEveryWrite(t *testing.T) {
	o := newWSOrg(t)
	_, launch, _, _ := o.orgScene(t)
	var writeErr error
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		// santosh hands the workstream to mia mid-answer.
		if _, err := o.st.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
			w := all[launch.ID]
			w.Owner = factory.WorkstreamOwner{Kind: "person", ID: "mia"}
			w.Shares = []factory.Share{{Kind: "person", ID: "santosh", Role: "view"}}
			all[launch.ID] = w
			return []string{launch.ID}, nil
		}); err != nil {
			t.Error(err)
		}
		_, writeErr = turn.Door.Decide(ctx, "Ship Friday.")
		return AgentTurnEnd{USD: 2}, nil
	})
	budget := 1.0
	var ws api.Workstream
	o.do(t, "santosh", "GET", "/v1/workstreams/"+launch.ID, -1, nil, 200, &ws)
	o.do(t, "santosh", "PATCH", "/v1/workstreams/"+launch.ID, ws.Rev, api.WorkstreamPatch{BudgetUSD: &budget}, 200, nil)
	o.do(t, "santosh", "POST", "/v1/workstreams/"+launch.ID+"/agent/messages", -1, api.AgentMessageNew{Text: "decide"}, 202, nil)
	o.settle()
	if writeErr == nil || writeErr.Error() != errAgentNoWrite.Error() {
		t.Fatalf("a write after santosh lost edit: %v", writeErr)
	}
	var got api.WorkstreamAgent
	o.do(t, "mia", "GET", "/v1/workstreams/"+launch.ID+"/agent", -1, nil, 200, &got)
	if got.CanTalk || got.Cannot != "This workstream has spent its budget." {
		t.Fatalf("mia reads %+v", got)
	}
	o.do(t, "mia", "POST", "/v1/workstreams/"+launch.ID+"/agent/messages", -1, api.AgentMessageNew{Text: "more"}, 409, nil)
}

// THE AGENT'S EVENTS REACH EXACTLY THOSE WHO CAN SEE THE WORKSTREAM.
func TestServerAgentEventsFollowView(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	data, _ := json.Marshal(api.AgentEvent{ID: webhook.ID, Entry: &api.AgentEntry{Seq: 1, Kind: "person", Text: "hi"}})
	e := live.Event{Kind: api.EventAgentEntry, Data: data}
	for who, want := range map[string]bool{"abir": true, "mia": true, "owner": true} {
		r := httptest.NewRequest("GET", "/v1/events", nil)
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("Authorization", "Bearer "+o.tokens[who])
		if _, ok := o.h.liveForViewer(r, e); ok != want {
			t.Fatalf("%s hears it: %v", who, ok)
		}
	}
	private := o.create(t, "abir", api.WorkstreamNew{Name: "Mine", Private: true})
	data, _ = json.Marshal(api.AgentEvent{ID: private.ID, AgentState: &api.AgentState{State: api.AgentWorking}})
	for who, want := range map[string]bool{"abir": true, "santosh": false, "owner": false} {
		r := httptest.NewRequest("GET", "/v1/events", nil)
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("Authorization", "Bearer "+o.tokens[who])
		if _, ok := o.h.liveForViewer(r, live.Event{Kind: api.EventAgentState, Data: data}); ok != want {
			t.Fatalf("%s hears the private one: %v", who, ok)
		}
	}
}
