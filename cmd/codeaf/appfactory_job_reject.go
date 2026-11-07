package main

// ── codeaf app: REJECTING AN APPROVAL ───────────────────────────────────────
//
// The engine's half of appwire.FactoryRejecter: ONE POST to the factory's own
// typed route, /v1/jobs/{id}/reject, with {"words":…,"expect":…}, then the
// receipt read (jobWrite). The factory decides under its own lock whether the
// job still waits on an approval the caller may answer; nothing is read here
// first, and nothing is retried on another route.

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/agentfield"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactoryRejecter = (*appBackend)(nil)

// factoryVerbReject is the factory's typed route for a rejection.
const factoryVerbReject = "reject"

func (b *appBackend) FactoryJobReject(ctx context.Context, p appwire.FactoryJobRejectParams) (appwire.FactoryJobActed, error) {
	// A run's pause on this machine's factory is told to the control plane
	// first, then ends here through the same door ([appBackend.answerAgentField]).
	if out, handled, err := b.answerAgentField(ctx, p.Factory, p.Job, appwire.JobActReject, agentfield.DecisionRejected, p.Words, p.Expect); handled {
		return out, err
	}
	return b.jobWrite(ctx, p.Factory, appwire.JobActReject, p.Job, factoryVerbReject, api.Reject{Words: p.Words, Expect: p.Expect})
}
