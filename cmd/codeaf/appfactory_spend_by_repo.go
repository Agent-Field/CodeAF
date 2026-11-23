package main

import (
	"context"
	"net/url"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactorySpender = (*appBackend)(nil)

// FactorySpendByRepo is one GET of the factory's /v1/spend/repos as the
// connection's caller, so the factory answers only the repositories they can
// read.
func (b *appBackend) FactorySpendByRepo(ctx context.Context, p appwire.FactorySpendByRepoParams) (appwire.FactorySpendByRepo, error) {
	out := appwire.FactorySpendByRepo{Factory: p.Factory}
	query := url.Values{"from": {p.From}, "to": {p.To}}
	if err := b.factoryHub().read(ctx, p.Factory, "/v1/spend/repos?"+query.Encode(), appFactorySmall, &out.SpendByRepo); err != nil {
		return appwire.FactorySpendByRepo{}, err
	}
	if out.Repos == nil {
		out.Repos = []api.RepoSpend{}
	}
	return out, nil
}
