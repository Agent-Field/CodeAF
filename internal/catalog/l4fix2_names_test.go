package catalog

import (
	"path/filepath"
	"testing"
)

// L4-H2, fix round 2: Only the catalog's own legacy and keyed filenames are
// startup cache, and the filename ownership check preserves cache isolation.
func TestL4Fix2CatalogCacheNames(t *testing.T) {
	for _, name := range []string{"model-catalog.json", filepath.Base(cachePath("unused", "custom", DefaultBaseURL))} {
		if !IsCacheName(name) {
			t.Fatalf("catalog does not recognize its own file: %q", name)
		}
	}
	for _, name := range []string{"model-catalog-my-notes.json", "model-catalog-0123456789abcdeF.json", "model-catalog-0123456789abcdef-more.json", "model-catalog-0123.json", "my-work.json"} {
		if IsCacheName(name) {
			t.Fatalf("unowned file accepted as cache: %q", name)
		}
	}
	t.Run("legacy cache", TestALegacyCacheFileIsTheDefaultBasesCache)
	t.Run("base isolation", TestACatalogCachedForOneBaseIsNeverServedToAnother)
}
