package tui3

import (
	modelcatalog "github.com/Agent-Field/codeaf/internal/catalog"
	"testing"
)

func TestCatalogListingTimeSurvivesPickerCacheWithoutGuessing(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	rows := surfaceModels([]modelcatalog.Model{{ID: "dated", Created: 1760000000}, {ID: "unknown-2099-12-31"}, {ID: "negative", Created: -1}}, true)
	if len(rows) != 3 || rows[0].ListedAt != 1760000000 || rows[1].ListedAt != 0 || rows[2].ListedAt != 0 {
		t.Fatalf("catalog listing times: %+v", rows)
	}
	if err := WriteModelCacheFor("test", "https://example.test", rows); err != nil {
		t.Fatal(err)
	}
	back := CachedModelsFor("test", "https://example.test")
	if len(back) != 3 || back[0].ListedAt != 1760000000 || back[1].ListedAt != 0 || back[2].ListedAt != 0 {
		t.Fatalf("cache lost listing facts: %+v", back)
	}
	negative := cleanModels([]Model{{ID: "cached", ListedAt: -1}})
	if negative[0].ListedAt != 0 {
		t.Fatal("negative cache date retained")
	}
}
