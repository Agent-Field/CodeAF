package main

import (
	"context"
	"net/url"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactoryModelSpender = (*appBackend)(nil)

// FactorySpendByModel is one GET of the factory's /v1/spend/models as the
// connection's caller, so the factory answers only the jobs they can read.
func (b *appBackend) FactorySpendByModel(ctx context.Context, p appwire.FactorySpendByModelParams) (appwire.FactorySpendByModel, error) {
	out := appwire.FactorySpendByModel{Factory: p.Factory}
	query := url.Values{"from": {p.From}, "to": {p.To}}
	if err := b.factoryHub().read(ctx, p.Factory, "/v1/spend/models?"+query.Encode(), appFactorySmall, &out.SpendByModel); err != nil {
		return appwire.FactorySpendByModel{}, err
	}
	if out.Models == nil {
		out.Models = []api.ModelSpend{}
	}
	return out, nil
}
