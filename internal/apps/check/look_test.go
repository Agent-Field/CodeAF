package check

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func TestLookFindsWeakCompositionAndKeepsPersonalWarningsNonBlocking(t *testing.T) {
	def, err := schema.Load("../schema/testdata/gallery-support")
	if err != nil {
		t.Fatal(err)
	}
	s, err := schema.ParseScreen(def.Files["screens/inbox.json"])
	if err != nil {
		t.Fatal(err)
	}
	s.Body = &schema.Node{ID: "table", Part: "RecordList", Label: "Tickets",
		Props: map[string]any{"views": []string{"table"}, "columns": []string{"subject", "body", "status"}},
		Bind:  &schema.Bind{Records: "ticket", View: "work"}}
	def.Files["screens/inbox.json"], _ = json.Marshal(s)
	found := map[string]bool{}
	for _, warning := range LookFindings(def, nil) {
		found[warning.Code] = true
	}
	for _, code := range []string{"look_table_without_detail", "look_signature_missing"} {
		if !found[code] {
			t.Errorf("missing %s: %v", code, found)
		}
	}
	if !(schema.Report{Warnings: LookFindings(def, nil)}).OK() {
		t.Fatal("personal layout warnings must allow the app to be kept")
	}
}

func TestGalleryExamplesPassLook(t *testing.T) {
	for _, name := range []string{"gallery-kyc", "gallery-hiring", "gallery-line", "gallery-support"} {
		t.Run(name, func(t *testing.T) {
			def, err := schema.Load("../schema/testdata/" + name)
			if err != nil {
				t.Fatal(err)
			}
			if findings := LookFindings(def, nil); len(findings) != 0 {
				t.Fatalf("gallery look: %+v", findings)
			}
		})
	}
}
