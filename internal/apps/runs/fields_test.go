package runs_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
)

func TestAIAutoFieldFillsOnChangeAndPreservesPersonValue(t *testing.T) {
	root := t.TempDir()
	db, err := data.Open(filepath.Join(root, "records.db"), data.Options{NoRefresh: true, RequireAgentPrincipal: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const manifest = `{
		"id":"marketing","name":"Marketing","version":1,"kit":2,
		"records":{"mention":{"fields":{
			"text":{"type":"text"},"private":{"type":"text","masked":true},
			"topic":{"type":"text","ai":"Classify the mention.","ai_on_change":true}
		}}},
		"capabilities":{"records":{"mention":"rw"},"ai":["sort"]},
		"budget":{"per_run":0.02}
	}`
	if err := db.RegisterApp([]byte(manifest)); err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(root, "apps", "marketing")
	if err := os.MkdirAll(filepath.Join(app, "current"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"meta.json": `{"version":1}`, "current/app.json": manifest} {
		if err := os.WriteFile(filepath.Join(app, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files := runs.Files(filepath.Join(root, "apps"))
	records := runs.Data{Store: func() (*data.Store, error) { return db, nil }}
	calls := 0
	engine, err := runs.Open(runs.Config{
		Root: filepath.Join(root, "runs"), Load: files.Load, Triggers: files.Triggers, Records: records.Write, RecordInput: records.RecordInput,
		Agent: func(_ context.Context, call runs.AgentCall) (runs.Reply, error) {
			calls++
			if !strings.Contains(call.Prompt, "Classify the mention.") || strings.Contains(call.Prompt, "secret-canary") {
				t.Fatalf("model prompt = %q", call.Prompt)
			}
			return runs.Reply{Output: `{"topic":"Docs"}`, Cost: 0.001}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	consume := func() {
		t.Helper()
		if err := records.ConsumeOutbox(t.Context(), engine); err != nil {
			t.Fatal(err)
		}
		tick(t, engine)
	}
	consume()
	scope := data.Scope{}
	created, err := db.Create(scope, "mention", []map[string]any{{"text": "Read the guide?", "private": "secret-canary"}})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Rows[0].ID()
	consume()
	consume()
	row, err := db.Get(scope, "mention", id, false)
	if err != nil || row["topic"] != "Docs" || row["_meta"].(data.RowMeta).By["topic"] != data.ByAI || calls != 1 {
		t.Fatalf("after create = %v, calls=%d, %v", row, calls, err)
	}
	if _, err := db.Update(scope, "mention", []string{id}, map[string]any{"text": "Does this have a guide?", "topic": "Customer questions"}, data.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	consume()
	consume()
	row, err = db.Get(scope, "mention", id, false)
	if err != nil || row["topic"] != "Customer questions" || row["_meta"].(data.RowMeta).By["topic"] != data.ByPerson || calls != 2 {
		t.Fatalf("after a person edit = %v, calls=%d, %v", row, calls, err)
	}
}
