package schema

import (
	"encoding/json"
	"testing"
)

func TestOutputCheck(t *testing.T) {
	fixture := func(t *testing.T, raw string) Definition {
		t.Helper()
		def := load(t, "testdata/investor-update")
		app := object(def.Files[AppFile])
		app["outputs"] = json.RawMessage(raw)
		var err error
		def.Files[AppFile], err = json.Marshal(app)
		if err != nil {
			t.Fatal(err)
		}
		return def
	}
	cases := []struct {
		name, declaration, code string
	}{
		{"full projection", `{"report":{"label":"Report","format":"pdf","query":{"type":"metric"},"body":"{{title}} {{id}}"}}`, ""},
		{"projected system field", `{"report":{"label":"Report","format":"pdf","query":{"type":"metric","select":["title"]},"body":"{{created_at}}"}}`, ""},
		{"missing projection", `{"report":{"label":"Report","format":"pdf","query":{"type":"metric","select":["title"]},"body":"{{arr}}"}}`, CodeFieldUnknown},
		{"unsupported format", `{"report":{"label":"Report","format":"html","record":"metric","body":"{{title}}"}}`, CodeSchema},
		{"null comparison", `{"report":{"label":"Report","format":"pdf","query":{"type":"metric","filter":[{"field":"title","op":"eq","value":null}]},"body":"{{title}}"}}`, CodeRegistryEntry},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Validate(fixture(t, c.declaration), nil)
			if c.code == "" && !r.OK() || c.code != "" && !hasCode(r, c.code) {
				t.Fatalf("want %s, got %s", c.code, r.String())
			}
		})
	}
	t.Run("masked field", func(t *testing.T) {
		def := fixture(t, `{"report":{"label":"Report","format":"pdf","record":"metric","body":"{{arr}}"}}`)
		textEdit(AppFile, `"currency":"USD"`, `"currency":"USD","masked":true`)(t, def.Files)
		if r := Validate(def, nil); !hasCode(r, CodeFieldUnknown) {
			t.Fatalf("expected masked merge field to fail: %s", r.String())
		}
	})
	t.Run("mismatched run parameter", func(t *testing.T) {
		def := fixture(t, `{"report":{"label":"Report","format":"pdf","record":"metric","body":"{{title}}"}}`)
		def.Files["runs/render.yaml"] = []byte("run: render\nparams:\n  item: {type: ref, to: contact}\nsteps:\n  - id: make\n    render: {output: report, record: $params.item.id}\n")
		if r := Validate(def, nil); !hasCode(r, CodeActionVerb) {
			t.Fatalf("expected wrong record binding to fail: %s", r.String())
		}
	})
}
