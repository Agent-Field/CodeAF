package main

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
)

func TestAppMCPOnlyShowsGrantedQueriesAndRechecksRevocation(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	meta := installAppAssistantFixture(t, "../../internal/apps/schema/testdata/support-inbox")
	b := newAppBackend(newAppRecorder(), refuseAppOpen)
	defer b.Close()
	db, err := b.dataStore("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Create(data.Scope{}, "ticket", []map[string]any{{"subject": "Northwind support"}}); err != nil {
		t.Fatal(err)
	}
	call := func(kind, id string, args json.RawMessage) (json.RawMessage, error) {
		t.Helper()
		p := appMCPRequest{App: meta.ID, Agent: "outside", Harness: "codex", Kind: kind, Target: id, Args: args}
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		return b.CallAppMCP(t.Context(), raw)
	}
	grant := appwire.AppAgentGrant{App: meta.ID, Agent: "outside", Group: "data", ID: "inbox/ticket-list", Allowed: true}
	if raw, err := call("register", "", nil); err != nil || string(raw) != "[]" {
		t.Fatalf("new principal = %s, %v", raw, err)
	}
	var refusal *appwire.Error
	if _, err := call("query", grant.ID, nil); !errors.As(err, &refusal) || refusal.Code != appwire.CodeForbidden {
		t.Fatalf("query without grant = %v", err)
	}
	typo := grant
	typo.ID = "inbox/missing-view"
	if err := b.GrantAppAgent(typo); !errors.As(err, &refusal) || refusal.Code != appwire.CodeNotFound {
		t.Fatalf("grant for an undeclared view = %v", err)
	}
	if err := b.GrantAppAgent(grant); err != nil {
		t.Fatal(err)
	}
	raw, err := call("list", "", nil)
	var catalog []appMCPTool
	if err != nil || json.Unmarshal(raw, &catalog) != nil || len(catalog) != 1 || catalog[0].Kind != "query" || catalog[0].ID != grant.ID {
		t.Fatalf("granted catalog = %s, %v", raw, err)
	}
	raw, err = call("query", grant.ID, nil)
	var reply struct{ Rows []map[string]any }
	if err != nil || json.Unmarshal(raw, &reply) != nil || len(reply.Rows) != 1 || reply.Rows[0]["subject"] != "Northwind support" {
		t.Fatalf("granted query = %s, %v", raw, err)
	}
	grant.Allowed = false
	if err := b.GrantAppAgent(grant); err != nil {
		t.Fatal(err)
	}
	if _, err := call("query", grant.ID, nil); !errors.As(err, &refusal) || refusal.Code != appwire.CodeForbidden {
		t.Fatalf("query with revoked grant = %v", err)
	}
}

func TestAppMCPActionWriterKeepsTheExternalPrincipal(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	meta := installAppAssistantCRM(t)
	b := newAppBackend(newAppRecorder(), refuseAppOpen)
	defer b.Close()
	db, err := b.dataStore("")
	if err != nil {
		t.Fatal(err)
	}
	person := data.Scope{}
	company, err := db.Create(person, "company", []map[string]any{{"name": "Northwind"}})
	if err != nil {
		t.Fatal(err)
	}
	record, err := db.Create(person, "deal", []map[string]any{{"name": "Example", "company": company.Rows[0].ID()}})
	if err != nil {
		t.Fatal(err)
	}
	id := record.Rows[0].ID()

	call := func(kind, target string, params any) error {
		t.Helper()
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		request, err := json.Marshal(appMCPRequest{App: meta.ID, Agent: "outside", Harness: "claude_code", Kind: kind, Target: target, Args: raw})
		if err != nil {
			t.Fatal(err)
		}
		_, err = b.CallAppMCP(t.Context(), request)
		return err
	}
	if err := call("register", "", nil); err != nil {
		t.Fatal(err)
	}
	action := "mark-won"
	args := map[string]any{"args": map[string]string{"record": id}, "expected_rev": 1}
	var refusal *appwire.Error
	if err := call("action", action, args); !errors.As(err, &refusal) || refusal.Code != appwire.CodeForbidden {
		t.Fatalf("ungranted action = %v", err)
	}
	if err := b.GrantAppAgent(appwire.AppAgentGrant{App: meta.ID, Agent: "outside", Group: "actions", ID: action, Allowed: true}); err != nil {
		t.Fatal(err)
	}
	if err := call("action", action, args); err != nil {
		t.Fatal(err)
	}
	history, err := db.History(person, "deal", id)
	if err != nil || len(history) != 2 || history[0].Actor != "agent:outside" || history[0].ByKind != data.ByAI {
		t.Fatalf("action history = %+v, %v", history, err)
	}
}
