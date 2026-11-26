package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
