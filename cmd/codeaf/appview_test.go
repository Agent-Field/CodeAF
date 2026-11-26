package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

func TestAppDeclaredViewUsesPolicy(t *testing.T) {
	b, _ := dataBackend(t)
	d, err := b.AppDefinitions().NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"app.json":  `{"id":"labels","name":"Label proof","version":1,"kit":2,"records":{"label":{"fields":{"name":{"type":"text"},"notes":{"type":"text","masked":true}}}},"capabilities":{"records":{"label":"rw"}}}`,
		"plan.json": `{"ask":"Label proof","data":[{"id":"label","line":"Labels"}],"screens":[{"id":"proof","line":"Proof"}]}`,
		"screens/proof.json": `{"screen":"proof","title":"Label proof","views":{"labels":{"type":"label","select":["name","notes"]}},
			"body":{"id":"proof","label":"Label proof","part":"custom/proof","bind":{"records":"label","view":"labels"}}}`,
		"custom/proof.json": `{"name":"custom/proof","source":"custom/proof.tsx","version":1,"summary":"Label proof","reason":"Proof layout.",
			"example":{},"props":{"type":"object"},"children":"none","interactive":false,"events":[],"bindings":["records"],"binding_required":true,
			"capabilities":"r","inputs":["name","notes"],"sizes":[800,1120],"themes":["dark","black","light"]}`,
		"custom/proof.tsx": `<part><title field="name"/><text field="notes"/></part>`,
	}
	for name, contents := range files {
		target := filepath.Join(d.Dir(), name)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	meta, err := b.AppDefinitions().Promote(d, "Keep label proof")
	if err != nil {
		t.Fatal(err)
	}
	scope := appwire.DataScope{App: meta.ID}
	if _, err := b.DataCreate(appwire.DataCreateParams{DataScope: scope, Type: "label",
		Set: map[string]any{"name": "Exit", "notes": "Unreleased location"},
	}); err != nil {
		t.Fatal(err)
	}
	w := newActionWire(t, b)
	p := appwire.DataViewParams{DataScope: scope, Screen: "proof", View: "labels", Limit: 24}
	raw, wireErr := w.call("data.view.query", p)
	if wireErr != nil {
		t.Fatal(wireErr)
	}
	var page appwire.DataQueryResult
	if err := json.Unmarshal(raw, &page); err != nil || len(page.Rows) != 1 || page.Rows[0]["name"] != "Exit" || page.Rows[0]["notes"] != "••••" {
		t.Fatalf("view = %s, %v", raw, err)
	}
	p.View = "undeclared"
	if _, err := w.call("data.view.query", p); err == nil {
		t.Fatal("the view door accepted an undeclared query")
	}
}

func TestAppCheckRunsTheCustomStepAgainstExampleRows(t *testing.T) {
	root := resolvedTempDir(t)
	t.Setenv("CODEAF_HOME", root)
	b := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { b.Close() })
	draft, err := b.AppDefinitions().NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	def := schema.Definition{Files: map[string][]byte{
		schema.AppFile: []byte(`{"id":"office","name":"Office","version":1,"kit":2,"records":{"booking":{"fields":{"name":{"type":"text"},"days":{"type":"number"}}}},
			"capabilities":{"records":{"booking":"rw"},"tools":["custom.count"]}}`),
		"custom/count.step": []byte(`{"version":1,"summary":"Count days","reason":"No date-span step in the Kit.","inputs":[],"output":"integer","effects":[],
			"timeout_ms":100,"memory_pages":1,"source":"(module (func (export \"run\") (result i64) i64.const 7))"}`),
		"runs/book.yaml": []byte(`run: book
steps:
  - id: count
    tool: custom.count
  - id: write
    records.create: {type: booking, set: {name: Example, days: "$steps.count.output"}}
`),
	}}
	if err := b.seedBuildSample(draft.ID(), def.Files[schema.AppFile], nil); err != nil {
		t.Fatal(err)
	}
	if err := b.CheckDraftRun(t.Context(), draft.ID(), "book", def); err != nil {
		t.Fatal(err)
	}
	rows, err := b.DataQuery(appwire.DataQueryParams{DataScope: appwire.DataScope{App: "office", Draft: draft.ID()}, Type: "booking"})
	if err != nil || len(rows.Rows) != 1 || rows.Rows[0]["days"] != float64(7) {
		t.Fatalf("sample booking = %+v, %v", rows, err)
	}
}
