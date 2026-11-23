package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
)

// seedOwnPrices writes a connected provider's OWN listing into its catalog
// compartment as a fetched, priced listing would be cached. It is UNIT DATA:
// it stands in for what that provider's /models published, keyed by the ids
// that provider lists, so a direct route has its own price to be weighed at: in, out and cache
// read, dollars per token.
func seedOwnPrices(t *testing.T, dir, provider string, prices map[string][3]float64) {
	t.Helper()
	service, ok := ResolveSources(dir, APIKeyAt(dir), DefaultBaseURL).ByID(provider)
	if !ok {
		t.Fatalf("%s is not connected", provider)
	}
	options := CatalogOptionsFor(service, dir)
	var models []catalog.Model
	for id, p := range prices {
		models = append(models, catalog.Model{ID: id, PromptPrice: p[0], CompletionPrice: p[1], CacheReadPrice: p[2]})
	}
	raw, err := json.Marshal(map[string]any{
		"fetched_at": time.Now().UTC(), "models": models, "source": options.Source, "base": options.BaseURL, "priced": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "model-catalog-"+catalog.CacheKey(options.Source, options.BaseURL)+".json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
