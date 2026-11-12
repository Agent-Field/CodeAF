package main

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// FactoryJobSetRepo gives a job that never ran its repository, through the
// factory's own door (POST /v1/jobs/{id}/repo), which also routes it again.
func (b *appBackend) FactoryJobSetRepo(ctx context.Context, p appwire.FactoryJobSetRepoParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActSetRepo, p.Job, serve.SetRepoVerb, struct {
		Repo   string      `json:"repo"`
		Expect *api.Expect `json:"expect,omitempty"`
	}{p.Repo, p.Expect})
}
