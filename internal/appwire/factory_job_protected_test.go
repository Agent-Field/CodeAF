package appwire

import "testing"

// A factory engine announces that its queue's Backlog jobs say which
// protected paths they name; an engine with no factory does not.
func TestFactoryJobProtectedIsAnnouncedByAFactoryEngine(t *testing.T) {
	if h := withFactoryJobProtectedFeature(Hello{}, &fakeEngine{}); len(h.Features) != 0 {
		t.Fatalf("announced %v", h.Features)
	}
	if h := withFactoryJobProtectedFeature(Hello{}, &factoryEngine{}); len(h.Features) != 1 || h.Features[0] != FeatureFactoryJobProtected || FeatureFactoryJobProtected != "factory.job_protected" {
		t.Fatalf("announced %v", h.Features)
	}
}
