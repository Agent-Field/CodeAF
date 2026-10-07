package praf

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/praf/afx"
	"github.com/Agent-Field/codeaf/internal/praf/reasoners"
)

// handler is one router reasoner bound to its deps: a request map in, its
// result map out, as the orchestrator calls it by name (orch.LocalCaller).
type handler func(ctx context.Context, input map[string]any) (any, error)

// routerReasoners is the 16 router reasoners in pr-af's §B.1 order. pr-af's
// served node registered each on its control plane and the orchestrator
// called them through it; here they are called in process by name, through
// the phase tracker that turns each call into the run's stage and step
// records (phases.go).
var routerReasoners = []struct {
	name string
	bind func(reasoners.Deps) handler
}{
	{reasoners.NameIntakePhase, bindReasoner(reasoners.IntakePhase)},
	{reasoners.NameAnatomyPhase, bindReasoner(reasoners.AnatomyPhase)},
	{reasoners.NamePlanningPhase, bindReasoner(reasoners.PlanningPhase)},
	{reasoners.NameMetaSemantic, bindReasoner(reasoners.MetaSemantic)},
	{reasoners.NameMetaMechanical, bindReasoner(reasoners.MetaMechanical)},
	{reasoners.NameMetaSystemic, bindReasoner(reasoners.MetaSystemic)},
	{reasoners.NameReviewDimension, bindReasoner(reasoners.ReviewDimension)},
	{reasoners.NameCompoundFinderPhase, bindReasoner(reasoners.CompoundFinderPhase)},
	{reasoners.NamePostWorthinessGate, bindReasoner(reasoners.PostWorthinessGate)},
	{reasoners.NameCompoundDedupPhase, bindReasoner(reasoners.CompoundDedupPhase)},
	{reasoners.NameEvidenceVerifier, bindReasoner(reasoners.EvidenceVerifier)},
	{reasoners.NameAdversaryPhase, bindReasoner(reasoners.AdversaryPhase)},
	{reasoners.NameDeepenFindings, bindReasoner(reasoners.DeepenFindings)},
	{reasoners.NameExtractObligations, bindReasoner(reasoners.ExtractObligations)},
	{reasoners.NameVerifyObligation, bindReasoner(reasoners.VerifyObligation)},
	{reasoners.NameCoverageGate, bindReasoner(reasoners.CoverageGate)},
}

// routerHandlers is every router reasoner keyed by name, each backed by deps.
func routerHandlers(deps reasoners.Deps) map[string]handler {
	handlers := make(map[string]handler, len(routerReasoners))
	for _, r := range routerReasoners {
		handlers[r.name] = r.bind(deps)
	}
	return handlers
}

// bindReasoner adapts a typed reasoner by binding the request map into its
// input (afx.Bind, which seeds the input's defaults).
func bindReasoner[T any](fn func(context.Context, reasoners.Deps, T) (map[string]any, error)) func(reasoners.Deps) handler {
	return func(deps reasoners.Deps) handler {
		return func(ctx context.Context, input map[string]any) (any, error) {
			in, err := afx.Bind[T](input)
			if err != nil {
				return nil, err
			}
			return fn(ctx, deps, in)
		}
	}
}
