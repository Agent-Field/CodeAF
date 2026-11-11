package main

import (
	"context"
	"net/url"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactoryEstimatorNew = (*appBackend)(nil)

// FactoryEstimateNew is what a job of a kind in a repository will likely
// cost before it exists: GET /v1/repos/{owner}/{repo}/estimate on the factory.
func (b *appBackend) FactoryEstimateNew(ctx context.Context, p appwire.FactoryEstimateNewParams) (api.NewEstimate, error) {
	owner, name, _ := strings.Cut(p.Repo, "/")
	path := "/v1/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/estimate?kind=" + url.QueryEscape(p.Kind)
	var out api.NewEstimate
	if err := b.factoryHub().read(ctx, p.Factory, path, appFactoryBody, &out); err != nil {
		if routeMissing(err) {
			return api.NewEstimate{}, factoryTooOld()
		}
		return api.NewEstimate{}, err
	}
	return out, nil
}
