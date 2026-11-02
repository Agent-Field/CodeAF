package main

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// factoryVerbBriefSeen is the factory's route for a person's seen Brief
// revision (POST /v1/jobs/{id}/brief_seen).
const factoryVerbBriefSeen = "brief_seen"

// FactoryJobBriefSeen carries the caller's seen Brief revision to the
// factory, which keeps the higher one; the answer's Current is the job as the
// caller now reads it, with their brief_seen.
func (b *appBackend) FactoryJobBriefSeen(ctx context.Context, p appwire.FactoryJobBriefSeenParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActBriefSeen, p.Job, factoryVerbBriefSeen, api.BriefSeenRequest{Rev: p.Rev})
}
