package codeaf

import (
	"context"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/modelsdev"
)

func codeafCatalogFixture(t *testing.T) modelsdev.Catalog {
	t.Helper()
	client, err := modelsdev.New(modelsdev.Options{
		CatalogPath:  "../internal/modelsdev/testdata/catalog.json",
		CacheDir:     t.TempDir(),
		DisableFetch: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := client.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestCodeafCatalogMetadataReachesSessionModel(t *testing.T) {
	models := codeafModels{
		backend:   &openRouterBackend{catalog: codeafCatalogFixture(t)},
		sessionID: "ses_catalog",
		agent:     "coder",
	}
	resolved, err := models.Resolve(context.Background(), msgmodel.User{
		Model: msgmodel.UserModel{
			ProviderID: "openrouter",
			ModelID:    "fixture/vendor-model",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Calc.Cost == nil || resolved.Calc.Cost.Input != 1.25 ||
		resolved.Calc.Cost.Output != 4.5 || resolved.Calc.Limit.Context != 240_000 ||
		resolved.Calc.Limit.Input == nil || *resolved.Calc.Limit.Input != 220_000 ||
		resolved.Calc.Limit.Output != 12_000 || resolved.Request.MaxOutputTokens == nil ||
		*resolved.Request.MaxOutputTokens != 12_000 {
		t.Fatalf("resolved catalog model = %#v", resolved)
	}
	projection, _, err := models.projection("openrouter", "fixture/vendor-model")
	if err != nil {
		t.Fatal(err)
	}
	if !projection.Capabilities.Temperature || !projection.Capabilities.Reasoning ||
		!projection.Capabilities.Attachment || !projection.Capabilities.ToolCall ||
		!projection.Capabilities.Input["text"] || !projection.Capabilities.Input["image"] ||
		projection.Capabilities.Input["audio"] || !projection.Capabilities.Output["text"] {
		t.Fatalf("engine capability projection = %#v", projection.Capabilities)
	}

	// OpenRouter ids are split at the provider prefix before the exact catalog
	// key lookup, matching Provider.parseModel/splitModel.
	if _, err := models.GetModel(
		context.Background(), "", "openrouter/fixture/vendor-model",
	); err != nil {
		t.Fatalf("normalized OpenRouter id: %v", err)
	}
	// A model the cost table has no row for is served unpriced, not refused.
	// The table is a price list; OpenRouter is the authority on what exists,
	// and a missing price must never be the reason a coding leaf never starts.
	if _, err := models.GetModel(context.Background(), "openrouter", "fixture/unknown"); err != nil {
		t.Fatalf("unpriced-but-served model was refused: %v", err)
	}
	unpriced, err := models.catalogModel("openrouter", "fixture/unknown")
	if err != nil {
		t.Fatalf("unpriced-but-served model was refused: %v", err)
	}
	if unpriced.Cost == nil || unpriced.Cost.Input != 0 || unpriced.Cost.Output != 0 {
		t.Fatalf("unpriced model carried a price: %#v", unpriced.Cost)
	}
	if !unpriced.Capabilities.ToolCall {
		t.Fatalf("unpriced model lost tool calling: %#v", unpriced.Capabilities)
	}
}

// TestUnpricedModelDoesNotKillTheLeaf is the validation battery's T2a in one
// call: `deepseek/deepseek-v4-flash-latest` is an id OpenRouter serves and
// models.dev has never carried, and asking the engine to price it killed the
// whole coding pipeline one second in, for $0.
func TestUnpricedModelDoesNotKillTheLeaf(t *testing.T) {
	models := codeafModels{
		backend:   &openRouterBackend{catalog: codeafCatalogFixture(t)},
		sessionID: "ses_unpriced",
		agent:     "coder",
	}
	for _, modelID := range []string{
		"deepseek/deepseek-v4-flash-latest",
		"openrouter/deepseek/deepseek-v4-flash-latest",
	} {
		if _, err := models.GetModel(context.Background(), "", modelID); err != nil {
			t.Fatalf("GetModel(%q) = %v, want a graceful unpriced model", modelID, err)
		}
	}
	// A provider the catalog has never heard of is the same claim about the
	// same table, and is degraded the same way.
	if _, err := models.catalogModel("nobody", "fixture/vendor-model"); err != nil {
		t.Fatalf("unknown provider refused instead of degrading: %v", err)
	}
}
