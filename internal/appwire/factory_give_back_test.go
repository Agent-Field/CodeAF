package appwire

import (
	"context"
	"slices"
	"testing"
)

// AN ENGINE WHOSE GIVE BACK CHECKS THE PERSON'S COMMITS SAYS SO, beside job
// control, so the job page offers Take over only where giving the job back
// runs its test and review again; an engine without job control says nothing.
func TestGiveBackChecksAreAnnouncedWithJobControl(t *testing.T) {
	if h := withFactoryActorFeature(Hello{}, &actorEngine{}); slices.Contains(h.Features, FeatureFactoryGiveBackChecks) {
		t.Fatalf("an engine without job control announced %v", h.Features)
	}
	h := withFactoryActorFeature(Hello{}, &controlEngine{})
	if !slices.Contains(h.Features, FeatureFactoryJobControl) || !slices.Contains(h.Features, FeatureFactoryGiveBackChecks) {
		t.Fatalf("a job controller announced %v", h.Features)
	}
}

type controlEngine struct{ actorEngine }

func (*controlEngine) FactoryJobControl(_ context.Context, p FactoryJobControlParams) (FactoryJobActed, error) {
	return FactoryJobActed{Job: p.Job}, nil
}
