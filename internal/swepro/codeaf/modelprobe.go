package codeaf

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/modelsdev"
)

// This file is the one question aforge is allowed to ask the engine without
// starting it: "would you find this model?"
//
// models.dev is the table the engine prices every call from, and it lives at
// internal/swepro/internal/modelsdev — behind Go's internal wall, reachable
// from inside internal/swepro and nowhere else. Rather than move the catalog
// out of the engine, the engine answers on its behalf: the parent hands over a
// model id and gets a yes or a no, which is the whole of what a translation on
// the parent's side needs to know before it substitutes a spelling.
//
// Nothing here can start a run, spend anything, or reach the network on a
// machine that already has the cache — Client.Get reads the cache file before
// it considers fetching, and the engine's own refresh keeps that file fresh.

var (
	engineCatalogMu     sync.Mutex
	engineCatalogLoaded modelsdev.Catalog
)

// ErrNoModelCatalog is what a caller gets when the answer is "nobody can say":
// the catalog would not load, or loaded empty because fetching is disabled. It
// is deliberately distinct from a model that is genuinely absent — a caller
// that cannot verify must forward what it was given rather than substitute.
var ErrNoModelCatalog = errors.New("codeaf: models.dev catalog is unavailable")

// ModelResolves reports whether the engine's catalog carries modelID, in either
// of the two spellings the parent might hold it in: aforge's `<vendor>/<model>`
// or the engine's `openrouter/<vendor>/<model>`. The error is only ever about
// the catalog itself, never about the model.
func ModelResolves(ctx context.Context, modelID string) (bool, error) {
	catalog, err := engineCatalog(ctx)
	if err != nil {
		return false, err
	}
	return catalogHasModel(catalog, modelID), nil
}

// ModelResolver is ModelResolves bound to a catalog loaded once, for a caller
// with a list to check or a translation to run per leaf. It fails at the point
// the catalog fails rather than answering "no" a thousand times.
func ModelResolver(ctx context.Context) (func(string) bool, error) {
	catalog, err := engineCatalog(ctx)
	if err != nil {
		return nil, err
	}
	return func(modelID string) bool { return catalogHasModel(catalog, modelID) }, nil
}

func catalogHasModel(catalog modelsdev.Catalog, modelID string) bool {
	provider, model := engineModelRef(modelID)
	if model == "" {
		return false
	}
	_, err := catalog.Resolve(provider, model)
	return err == nil
}

// engineModelRef splits a model id the way the engine's own model resolution
// does (normalizeModelRef in engine_client.go): everything aforge names is an
// OpenRouter model, and the `openrouter/` prefix is the engine's spelling of
// that provider rather than part of the model's name. The leading "~" is
// aforge's floating-alias marker and belongs to neither.
func engineModelRef(modelID string) (string, string) {
	model := strings.TrimPrefix(strings.TrimSpace(modelID), "~")
	return normalizeModelRef("", "openrouter/"+strings.TrimPrefix(model, "openrouter/"))
}

// engineCatalog loads models.dev once per process and keeps only successes.
// A failure is not memoized: a probe that ran while the machine was offline
// must not tell every later caller, for the life of a resident that stays up
// for days, that the catalog is gone.
func engineCatalog(ctx context.Context) (modelsdev.Catalog, error) {
	engineCatalogMu.Lock()
	defer engineCatalogMu.Unlock()
	if len(engineCatalogLoaded) > 0 {
		return engineCatalogLoaded, nil
	}
	client, err := modelsdev.NewFromEnv(version)
	if err != nil {
		return nil, errors.Join(ErrNoModelCatalog, err)
	}
	catalog, err := client.Get(ctx)
	if err != nil {
		return nil, errors.Join(ErrNoModelCatalog, err)
	}
	if len(catalog) == 0 {
		// CODEAF_DISABLE_MODELS_FETCH with no cache yields an empty catalog
		// rather than an error. An empty table knows nothing, and reporting
		// that as "the model is missing" would be this probe inventing a
		// verdict out of its own blindness.
		return nil, ErrNoModelCatalog
	}
	engineCatalogLoaded = catalog
	return catalog, nil
}
