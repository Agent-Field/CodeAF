package main

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

// FactoryJobPublishAnyway forwards a guarded person action to the factory.
func (b *appBackend) FactoryJobPublishAnyway(ctx context.Context, p appwire.FactoryJobActionParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, "publish_anyway", p.Job, "publish_anyway", guardBody{p.Expect})
}
