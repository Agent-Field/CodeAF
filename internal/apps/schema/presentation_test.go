package schema

import (
	"reflect"
	"testing"
)

func TestArchetypeInfersPresentationWithoutOverwritingAuthoredFields(t *testing.T) {
	def := load(t, "testdata/gallery-hiring")
	jsonEdit(AppFile, func(v map[string]any) {
		person := at(v, "records", "candidate")
		fields := person["fields"].(map[string]any)
		fields["resume"] = map[string]any{"type": "url"}
		fields["salary"] = map[string]any{"type": "money", "currency": "USD"}
		fields["reviewer"] = map[string]any{"type": "person"}
		fields["private"] = map[string]any{"type": "text", "masked": true}
	})(t, def.Files)
	recipe := Recipe{Kind: "add-screen", ID: "review", Record: "candidate", As: "queue-detail"}
	result, err := CompileRecipe(def, nil, recipe)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseApp(result.App)
	if err != nil {
		t.Fatal(err)
	}
	want := &Presentation{Title: "title", Meta: []string{"interview"}, Trailing: "score", State: "stage", Link: "resume", Money: "salary", Avatar: "reviewer"}
	if p := got.Records["candidate"].Present; !reflect.DeepEqual(p, want) {
		t.Fatalf("presentation = %+v, want %+v", p, want)
	}
	jsonEdit(AppFile, func(v map[string]any) {
		at(v, "records", "candidate")["present"] = map[string]any{"title": "role", "meta": []string{"source"}}
	})(t, def.Files)
	result, err = CompileRecipe(def, nil, recipe)
	if err != nil || len(result.App) != 0 {
		t.Fatalf("authored presentation must keep its settings, got %s (%v)", result.App, err)
	}
	jsonEdit(AppFile, func(v map[string]any) {
		at(v, "records", "candidate", "present")["link"] = "does_not_exist"
	})(t, def.Files)
	if report := Validate(def, nil); !hasCode(report, CodeFieldUnknown) {
		t.Fatalf("invalid presentation link must be rejected: %s", report)
	}
}
