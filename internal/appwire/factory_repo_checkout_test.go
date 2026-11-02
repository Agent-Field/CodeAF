package appwire

import "testing"

// An engine that serves factory.queue announces that the queue's
// repositories carry checkout_state; one that serves no factory does not.
func TestFactoryRepoCheckoutFeatureFollowsTheFactory(t *testing.T) {
	if h := withFactoryRepoCheckoutFeature(Hello{}, &fakeEngine{}); len(h.Features) != 0 {
		t.Fatalf("an engine with no factory announced %v", h.Features)
	}
	if h := withFactoryRepoCheckoutFeature(Hello{}, &factoryEngine{}); len(h.Features) != 1 || h.Features[0] != FeatureFactoryRepoCheckout {
		t.Fatalf("a factory engine announced %v", h.Features)
	}
}
