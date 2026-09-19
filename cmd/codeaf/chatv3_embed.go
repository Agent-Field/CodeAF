package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/embed"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/standing"
	"github.com/Agent-Field/codeaf/internal/wsdiscover"
)

// v3Embedder is the shipped RoleEmbed adapter, or nil when this install
// cannot embed. NIL IS DELAYED, NEVER A STUB: a capability that cannot work
// is absent, so discovery reports delayed/degraded instead of succeeding
// with empty vectors.
//
// The model id is the pin, then the catalog, then the slug Spark's provider
// actually served (internal/embed.ResolveModel). Spend is tagged
// roles.RoleEmbed on the request. RoleAuditor is not on this path.
func v3Embedder(settings config.Config, source roles.Source, models *catalog.Catalog) embed.Embedder {
	return v3EmbedderAccount(settings, source, models, nil)
}

func v3EmbedderAccount(settings config.Config, source roles.Source, models *catalog.Catalog, account embed.Account) embed.Embedder {
	client, err := settings.MediaClient()
	if err != nil || client == nil {
		return nil
	}
	model := embed.ResolveModel(source, models)
	if model == "" {
		return nil
	}
	return embed.New(client, model, account).WithSecret(settings.APIKey)
}

// deferredEmbedder resolves the RoleEmbed pin the way generate_image does:
// at the call that needs a vector, not while assembling the first frame.
// Constructing v3Embedder at launch asked the catalog two blocking questions
// (ResolveModel → CandidateMediaModel / Supports).
type deferredEmbedder struct {
	settings config.Config
	source   roles.Source
	models   *catalog.Catalog
	mu       sync.Mutex
	inner    embed.Embedder
	ready    bool
}

var (
	_ embed.Embedder      = (*deferredEmbedder)(nil)
	_ wsdiscover.Embedder = (*deferredEmbedder)(nil)
)

func v3DeferredEmbedder(settings config.Config, source roles.Source, models *catalog.Catalog) embed.Embedder {
	return &deferredEmbedder{settings: settings, source: source, models: models}
}

// v3StandingEmbedAccount reserves RoleEmbed on the standing DailyRail. There is
// no session Agent here to fold usage through addAuxiliaryUsageAs; a nil Cost
// still embeds and does not claim the call was free.
func v3StandingEmbedAccount() embed.Account {
	return func(_ string, usage *ai.Usage) {
		if usage == nil || usage.Cost == nil || *usage.Cost <= 0 {
			return
		}
		store, err := standing.Open(v3StandingRoot())
		if err != nil {
			return
		}
		_ = store.Append(standing.EmbedSpend(*usage.Cost))
	}
}

func (d *deferredEmbedder) bound() embed.Embedder {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.ready {
		d.inner = v3EmbedderAccount(d.settings, d.source, d.models, v3StandingEmbedAccount())
		d.ready = true
	}
	return d.inner
}

func (d *deferredEmbedder) peek() embed.Embedder {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.inner
}

func (d *deferredEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, string, string, int, error) {
	inner := d.bound()
	if inner == nil {
		return nil, "", "", 0, fmt.Errorf("embeddings: unavailable")
	}
	return inner.Embed(ctx, texts)
}

func (d *deferredEmbedder) Available(ctx context.Context) (string, bool, error) {
	inner := d.bound()
	if inner == nil {
		return "", false, nil
	}
	return inner.Available(ctx)
}
