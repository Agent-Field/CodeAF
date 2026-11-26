package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
)

func TestAppAssistantGrantsBuilderRunThroughCLIAndGrantPanel(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	definitions := store.New()
	draft, err := definitions.NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(draft.Dir(), os.DirFS("../../internal/apps/store/testdata/crm")); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(draft.Dir(), "app.json")
	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var app map[string]json.RawMessage
	if err := json.Unmarshal(raw, &app); err != nil {
		t.Fatal(err)
	}
	// The Builder can produce the Mark Won button and run without a separate command.
	delete(app, "commands")
	raw, err = json.Marshal(app)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(draft.Dir(), "runs", "mark-won.yaml"), []byte(`run: mark-won
title: Mark Won
params:
  deal: {type: ref, to: deal}
steps:
  - id: won
    records.update: {type: deal, id: $params.deal.id, set: {stage: Won}}
`), 0600); err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func([]byte) []byte{
		"plan.json": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"automation": [`), []byte(`"automation":[{"id":"mark-won","line":"Mark won"},`), 1)
		},
		"screens/deals.json": func(raw []byte) []byte {
			raw = bytes.Replace(raw, []byte(`"actions": ["follow-up", "mark-won"]`), []byte(`"actions":[]`), 1)
			return bytes.Replace(raw, []byte(`"children": [`), []byte(`"children":[
				{"part":"Button","label":"Mark Won","on":{"click":[{"runs.start":"mark-won","params":{"deal":"$selection.deals.id"}}]}},`), 1)
		},
	} {
		file := filepath.Join(draft.Dir(), name)
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if changed := edit(raw); bytes.Equal(changed, raw) {
			t.Fatalf("fixture %s did not match", name)
		} else if err := os.WriteFile(file, changed, 0600); err != nil {
			t.Fatal(err)
		}
	}
	meta, err := definitions.Promote(draft, "Keep built CRM")
	if err != nil {
		t.Fatal(err)
	}

	b := newAppBackend(newAppRecorder(), refuseAppOpen)
	t.Cleanup(func() { _ = b.Close() })
	db, err := b.dataStore("")
	if err != nil {
		t.Fatal(err)
	}
	person := data.Scope{}
	companies, err := db.Create(person, "company", []map[string]any{{"name": "Acme"}})
	if err != nil {
		t.Fatal(err)
	}
	deals, err := db.Create(person, "deal", []map[string]any{
		{"name": "Acme", "company": companies.Rows[0].ID()},
		{"name": "Canary", "company": companies.Rows[0].ID()},
	})
	if err != nil {
		t.Fatal(err)
	}
	files := runs.Files(home.Join("apps"))
	dataAdapter := runs.Data{Store: func() (*data.Store, error) { return db, nil }}
	engine, err := runs.Open(runs.Config{Root: filepath.Join(root, "test-runs"), Load: files.Load,
		CheckAgentAction: b.checkAppAgentAction, AppVersion: files.Version,
		ResolveParams: dataAdapter.ResolveParams, Records: dataAdapter.Write})
	if err != nil {
		t.Fatal(err)
	}
	b.appRuns.Store(engine)
	t.Cleanup(func() { _ = engine.Close() })

	call := func(verb string, params any) (json.RawMessage, error) {
		t.Helper()
		input, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		result, err := b.runAppCLICommand(t.Context(), verb, input)
		if err != nil {
			return nil, err
		}
		return json.Marshal(result)
	}
	var catalog appContentCatalog
	got, err := call("show", map[string]string{"app": meta.ID})
	if err != nil || json.Unmarshal(got, &catalog) != nil {
		t.Fatalf("show = %s, %v", got, err)
	}
	var found bool
	for _, action := range catalog.Actions {
		if action.ID == "mark-won" && action.Label == "Mark Won" && action.Args["deal"].To == "deal" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Mark Won is missing from show: %s", got)
	}
	intent := map[string]any{"app": meta.ID, "params": map[string]any{"action": "mark-won", "args": map[string]string{"deal": deals.Rows[0].ID()}}}
	if _, err := call("invoke", intent); err == nil {
		t.Fatal("Mark Won ran without a grant")
	}
	if _, err := call("agent_grant", appwire.AppAgentGrant{App: meta.ID, Agent: meta.ID, Group: "actions", ID: "mark-won", Allowed: true}); err != nil {
		t.Fatal(err)
	}
	var grants []appAgentGrantRow
	wireOver(t, b).must("agents.query", appwire.AppAgentQuery{App: meta.ID, Kind: "grants"}, &grants)
	found = false
	for _, row := range grants {
		for _, action := range row.Actions {
			found = found || (row.Agent.ID == meta.ID && action.ID == "mark-won" && action.Allowed)
		}
	}
	if !found {
		t.Fatalf("Mark Won grant is missing from the grant panel: %+v", grants)
	}
	if _, err := call("invoke", intent); err != nil {
		t.Fatal(err)
	}
	if err := engine.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"Won", "Lead"} {
		row, err := db.Get(person, "deal", deals.Rows[i].ID(), false)
		if err != nil || row["stage"] != want {
			t.Fatalf("deal %d = %+v, %v, want stage %s", i, row, err, want)
		}
	}
}
