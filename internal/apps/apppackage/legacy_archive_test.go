package apppackage

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func TestFormatOneManifestSuppliesSharedFields(t *testing.T) {
	files := map[string][]byte{schema.AppFile: []byte(`{"id":"crm","name":"CRM","version":1,"kit":2,
		"records":{"contact":{"common":"person","uses":true,"fields":{}}},
		"capabilities":{"records":{"contact":"rw"}}}`)}
	manifest, err := describe(files, "Example", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	// Old exporters recorded the shared field only in the manifest.
	manifest.Entities["contact"].Fields["name"] = Field{Type: "text", Required: true}
	old := Archive{Format: 1, Manifest: manifest, Files: files}
	old.Manifest.Hash = old.digest()
	raw, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	for _, catalog := range []schema.TypeSet{nil, {"people": {ID: "1", Common: "person", Owner: "directory", Fields: map[string]schema.Field{}}}} {
		files, bindings, err := archive.Bind(catalog, "crm", false)
		if err != nil {
			t.Fatal(err)
		}
		app, err := schema.ParseApp([]byte(files[schema.AppFile]))
		contact := app.Records["contact"]
		if err != nil || !contact.Fields["name"].Required || contact.Fields["name"].Type != "text" {
			t.Fatalf("legacy archive lost the shared field: %+v (%v)", contact, err)
		}
		if catalog != nil && (!contact.Uses || bindings["contact"].Type != "people") {
			t.Fatalf("missing field must extend existing People: %+v, %+v", contact, bindings)
		}
	}
}
