package main

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactoryRepoFactsReader = (*appBackend)(nil)

// FactoryRepoFacts is one GET of the factory's
// /v1/repos/{owner}/{repo}/facts as the connection's caller, so the factory
// answers only a repository they can read.
func (b *appBackend) FactoryRepoFacts(ctx context.Context, p appwire.FactoryRepoFactsParams) (appwire.FactoryRepoFacts, error) {
	out := appwire.FactoryRepoFacts{Factory: p.Factory}
	owner, name, _ := strings.Cut(p.Repo, "/")
	query := url.Values{"days": {strconv.Itoa(p.Days)}, "months": {strconv.Itoa(p.Months)}}
	path := appFactoryRepoRoute + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/facts?" + query.Encode()
	if err := b.factoryHub().read(ctx, p.Factory, path, appFactorySmall, &out.RepoFacts); err != nil {
		return appwire.FactoryRepoFacts{}, err
	}
	if out.Facts == nil {
		out.Facts = []api.Fact{}
	}
	return out, nil
}
