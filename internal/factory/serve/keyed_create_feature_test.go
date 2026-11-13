package serve_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// THE FACTORY SAYS IT RUNS A KEYED CREATE THAT WROTE NOTHING AGAIN, in GET
// /v1/features, because an engine sends a create key only to a Factory that
// says so: an older one keeps every keyed answer, a refusal or a 5xx included,
// and would replay it for a day. The rule lives in the request path, not the
// store, so a host without a store says it too.
func TestTheFactorySaysItRunsAKeyedCreateThatWroteNothingAgain(t *testing.T) {
	stored := newKeyedCreate(t).h
	bare := serve.New(factory.Seam{}, serve.Config{Mode: api.FactorySolo, Address: config.Address, Version: config.Version})
	t.Cleanup(bare.Close)
	for name, h := range map[string]http.Handler{"with a store": stored, "without a store": bare} {
		t.Run(name, func(t *testing.T) {
			w := verbRequest(h, "GET", "/v1/features", "")
			var got api.Features
			if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &got) != nil {
				t.Fatalf("features = %d %s", w.Code, w.Body)
			}
			if !slices.Contains(got.Features, api.FeatureKeyedCreate) {
				t.Fatalf("features = %v, want %q listed", got.Features, api.FeatureKeyedCreate)
			}
		})
	}
}
