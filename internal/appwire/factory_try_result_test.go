package appwire

import "testing"

// A FACTORY ENGINE SAYS ITS JOB READS CAN TELL HOW EACH TRY ENDED, and a plain
// engine says nothing, so the page shows "Try 1 · not clean" only when the
// engine announced the words.
func TestAFactoryEngineAnnouncesTryResults(t *testing.T) {
	if h := withFactoryTryResultFeature(Hello{}, &fakeEngine{}); len(h.Features) != 0 {
		t.Fatalf("a plain engine announced %v", h.Features)
	}
	if h := withFactoryTryResultFeature(Hello{}, &factoryEngine{}); len(h.Features) != 1 || h.Features[0] != FeatureFactoryTryResult {
		t.Fatalf("a factory engine announced %v", h.Features)
	}
}
