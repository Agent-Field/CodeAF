package store_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
)

func TestGalleryAppsPublishAndLoadSyntheticRecords(t *testing.T) {
	root := t.TempDir()
	place := data.NewPlace(root, data.Options{})
	t.Cleanup(func() { _ = place.Close() })
	definitions := store.NewAt(root, nil).WithData(place.Main)
	for _, fixture := range []struct {
		app, screen, body string
		wants             []string
	}{
		{"gallery-kyc", "queue", "Row", []string{"RecordList", "Panel", "FileViewer", "Checklist"}},
		{"gallery-hiring", "candidates", "Row", []string{"RecordList", "Panel", "RecordPage"}},
		{"gallery-line", "lines", "Stack", []string{"Chart", "Timeline", "Text"}},
		{"gallery-support", "inbox", "Row", []string{"RecordList", "Panel", "RecordPage"}},
	} {
		t.Run(fixture.app, func(t *testing.T) {
			dir := filepath.Join("..", "schema", "testdata", fixture.app)
			draft, err := definitions.NewDraft("")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.CopyFS(draft.Dir(), os.DirFS(dir)); err != nil {
				t.Fatal(err)
			}
			definition, err := schema.Load(draft.Dir())
			if err != nil {
				t.Fatal(err)
			}
			if report := check.Definition(definition, nil); !report.OK() {
				t.Fatalf("gallery check: %+v", report.Problems)
			}
			var sample schema.Sample
			if err := json.Unmarshal(definition.Files[schema.SampleFile], &sample); err != nil {
				t.Fatal(err)
			}
			if err := place.SeedSample(draft.ID(), definition.Files[schema.AppFile], sample.Records); err != nil {
				t.Fatal(err)
			}
			if err := place.CloseDraft(draft.ID()); err != nil {
				t.Fatal(err)
			}
			meta, err := definitions.Promote(draft, "Keep the gallery fixture")
			if err != nil {
				t.Fatal(err)
			}
			raw, err := definitions.Screen(meta.ID, fixture.screen)
			if err != nil {
				t.Fatal(err)
			}
			screen, err := schema.ParseScreen(raw)
			if err != nil || screen.Body == nil || screen.Body.Part != fixture.body {
				t.Fatalf("gallery %s needs a %s body: %v", fixture.screen, fixture.body, err)
			}
			seen := map[string]bool{}
			var visit func(*schema.Node)
			visit = func(node *schema.Node) {
				seen[node.Part] = true
				for _, child := range node.Children {
					visit(child)
				}
			}
			visit(screen.Body)
			for _, part := range fixture.wants {
				if !seen[part] {
					t.Errorf("gallery %s is missing %s", fixture.screen, part)
				}
			}
		})
	}
}
