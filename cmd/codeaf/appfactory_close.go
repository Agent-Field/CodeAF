package main

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

func (b *appBackend) FactoryJobClose(ctx context.Context, p appwire.FactoryJobActionParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActClose, p.Job, "close", guardBody{p.Expect})
}

func (b *appBackend) FactoryJobReopen(ctx context.Context, p appwire.FactoryJobActionParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActReopen, p.Job, "reopen", guardBody{p.Expect})
}
