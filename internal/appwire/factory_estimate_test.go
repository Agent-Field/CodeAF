package appwire

import "testing"

func TestAFactoryEngineAnnouncesMeasuredEstimates(t *testing.T) {
	if h := withFactoryEstimateFeature(Hello{}, &fakeEngine{}); len(h.Features) != 0 {
		t.Fatalf("an engine with no factory announced %v", h.Features)
	}
	if h := withFactoryEstimateFeature(Hello{}, &factoryEngine{}); len(h.Features) != 1 || h.Features[0] != FeatureFactoryEstimate {
		t.Fatalf("a factory engine announced %v", h.Features)
	}
}
