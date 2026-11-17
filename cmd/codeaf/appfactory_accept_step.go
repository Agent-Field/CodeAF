package main

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// FactoryJobAcceptStep is the person's accept of the stuck step a job waits
// on: the factory's own answer route with the one choice, so the factory
// decides whether this job offers it and who may give it.
func (b *appBackend) FactoryJobAcceptStep(ctx context.Context, p appwire.FactoryJobActionParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, "accept_step", p.Job, "answer", struct {
		Choice string      `json:"choice"`
		Expect *api.Expect `json:"expect,omitempty"`
	}{factory.ChoiceAccept, p.Expect})
}
