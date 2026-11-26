package builder

import (
	"maps"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func TestRestoreReusePreservesOtherRecordsAndCapabilities(t *testing.T) {
	pins := map[string]reuseBinding{"customer": {Type: "contact", Owner: "crm", Common: "person", Access: "rw", Line: schema.PlanLine{ID: "customer", Line: "Customers", Uses: true, Create: true}}}
	types := schema.TypeSet{"contact": {Owner: "crm", Common: "person", Fields: map[string]schema.Field{"name": {Type: "text"}}}}
	for _, manifest := range []string{
		`{"records":[],"capabilities":{"tools":["mail"]}}`,
		`{"records":{"customer":{},"deal":{}},"capabilities":[]}`,
		`{"records":{"customer":{},"deal":{}},"capabilities":{"records":[]}}`,
		`{"records":{"customer":[],"deal":{}}}`,
	} {
		files := map[string]string{schema.AppFile: manifest}
		before := maps.Clone(files)
		if err := restoreReuse(files, pins, types); err == nil || !maps.Equal(files, before) {
			t.Errorf("malformed edit must leave its input intact: %s, %v", files[schema.AppFile], err)
		}
	}
	files := map[string]string{
		schema.AppFile:  `{"records":{"deal":{"fields":{"name":{"type":"text"}}}},"capabilities":{"records":{"deal":"rw"},"tools":["mail"]}}`,
		schema.PlanFile: `{"ask":"Track deals","data":[{"id":"deal","line":"Deals"}],"screens":[]}`,
	}
	if err := restoreReuse(files, pins, types); err != nil {
		t.Fatal(err)
	}
	app, err := schema.ParseApp([]byte(files[schema.AppFile]))
	if err != nil || !app.Records["customer"].Uses || app.Records["customer"].Common != "person" ||
		app.Records["deal"].Fields["name"].Type != "text" || app.Capabilities.Records["deal"] != "rw" ||
		app.Capabilities.Records["customer"] != "rw" || len(app.Capabilities.Tools) != 1 {
		t.Fatalf("restored app = %+v, %v", app, err)
	}
	second := maps.Clone(files)
	if err := restoreReuse(files, pins, types); err != nil || !maps.Equal(files, second) {
		t.Fatalf("restoring a second time changed the app: %v", err)
	}
}
