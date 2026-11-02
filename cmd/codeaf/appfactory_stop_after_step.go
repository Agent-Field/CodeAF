package main

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func (b *appBackend) FactoryJobStopAfterStep(ctx context.Context, p appwire.FactoryJobStopAfterStepParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActStopAfterStep, p.Job, "stop-after-step", struct {
		On     bool        `json:"on"`
		Expect *api.Expect `json:"expect,omitempty"`
	}{*p.On, p.Expect})
}
