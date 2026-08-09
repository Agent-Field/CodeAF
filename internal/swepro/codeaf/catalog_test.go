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
		// aforge-embed: one directory shallower. Upstream this package was
		// cmd/codeaf, two levels under the repo root; here it is
		// internal/swepro/codeaf, one level under the vendored root, and the
		// fixture it reads moved with it.
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
	if _, err := models.GetModel(
		context.Background(), "openrouter", "fixture/unknown",
	); err == nil {
		t.Fatal("unknown catalog model unexpectedly resolved")
	}
}
