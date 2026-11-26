package store_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
)

func TestGalleryAppsPublishAndLoadSyntheticRecords(t *testing.T) {
	root := t.TempDir()
	place := data.NewPlace(root, data.Options{})
	t.Cleanup(func() { _ = place.Close() })
	definitions := store.NewAt(root, nil).WithData(place.Main)
	for _, fixture := range []struct{ app, screen string }{
		{"gallery-kyc", "queue"},
		{"gallery-hiring", "candidates"},
		{"gallery-line", "lines"},
		{"gallery-support", "inbox"},
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
			if _, err := definitions.Screen(meta.ID, fixture.screen); err != nil {
				t.Fatal(err)
			}
		})
	}
}
