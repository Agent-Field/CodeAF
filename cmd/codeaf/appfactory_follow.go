package main

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// factoryVerbFollow is the factory's route for a person following a job
// (POST /v1/jobs/{id}/follow).
const factoryVerbFollow = "follow"

// FactoryJobFollow carries the caller's follow switch to the factory; the
// answer's Current is the job as the caller now reads it, with their
// following.
func (b *appBackend) FactoryJobFollow(ctx context.Context, p appwire.FactoryJobFollowParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActFollow, p.Job, factoryVerbFollow, api.FollowRequest{On: p.On})
}
