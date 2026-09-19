package main

import (
	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/embed"
	"github.com/Agent-Field/codeaf/internal/roles"
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
	client, err := settings.MediaClient()
	if err != nil || client == nil {
		return nil
	}
	model := embed.ResolveModel(source, models)
	if model == "" {
		return nil
	}
	return embed.New(client, model, nil).WithSecret(settings.APIKey)
}
