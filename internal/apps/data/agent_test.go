package data

import "testing"

func TestAgentPrincipalRequiresOwnGrantAtTheWriter(t *testing.T) {
	store, err := Open(t.TempDir()+"/data.db", Options{NoRefresh: true, RequireAgentPrincipal: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.RegisterApp(fixture(t, "crm-app.json")); err != nil {
		t.Fatal(err)
	}
	row := mustCreate(t, store, person, "deal", map[string]any{"name": "Northwind"})[0]
	const action = "mark-won"
	agent := AgentPrincipal{ID: "sales-assistant"}
	scope := Scope{App: "crm", Principal: agent, Action: action, Actor: "forged", Cause: "request-1"}
	update := func(sc Scope) error {
		_, err := store.Update(sc, "deal", []string{row.ID()}, map[string]any{"stage": "Won", "name": "Changed"}, UpdateOptions{})
		return err
	}
	wantCode(t, update(Scope{App: "crm", Agent: true}), CodeNotAllowed)
	wantCode(t, update(scope), CodeNotAllowed)
	if err := store.SetAgentGrant("crm", agent, "actions", action, true); err != nil {
		t.Fatal(err)
	}
	other := scope
	other.Principal.ID = "other-assistant"
	wantCode(t, update(other), CodeNotAllowed)
	other = scope
	other.Action = "another-action"
	wantCode(t, update(other), CodeNotAllowed)
	if err := update(scope); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Get(person, "deal", row.ID(), false)
	if err != nil || updated["stage"] != "Won" || updated["name"] != "Northwind" {
		t.Fatalf("record = %v, error = %v", updated, err)
	}
	history, err := store.History(person, "deal", row.ID())
	if err != nil || len(history) != 2 || history[0].Actor != "agent:"+agent.ID || history[0].ByKind != ByAI || history[0].Cause != scope.Cause {
		t.Fatalf("history = %v, error = %v", history, err)
	}
	if err := store.SetAgentGrant("crm", agent, "actions", action, false); err != nil {
		t.Fatal(err)
	}
	wantCode(t, update(scope), CodeNotAllowed)
}
