package appwire

import (
	"slices"
	"testing"
)

// An engine announces collections.factory_job only when it serves
// collections, so a client can tell before it adds a job.
func TestCollectionsFactoryJobFeatureFollowsCollections(t *testing.T) {
	if h := withCollectionsFactoryJobFeature(Hello{}, &fakeEngine{}); len(h.Features) != 0 {
		t.Fatalf("an engine without collections announces %v", h.Features)
	}
	h := withCollectionsFactoryJobFeature(Hello{}, &fakeCollector{fakeEngine: &fakeEngine{}})
	if len(h.Features) != 1 || h.Features[0] != FeatureCollectionsFactoryJob {
		t.Fatalf("a collector announces %v", h.Features)
	}
}

// The announced feature is served: a factory_job ref with its factory reaches
// the collector, and one without a factory is refused before it.
func TestCollectionsFactoryJobFeatureIsServed(t *testing.T) {
	w, engine := startCollectorWire(t)
	if !slices.Contains(features(w.next()), FeatureCollectionsFactoryJob) {
		t.Fatal("hello lacks collections.factory_job")
	}
	w.send(`{"id":1,"method":"collections.add","params":{"id":"c1","refs":[{"kind":"factory_job","id":"7","factory":"f"}]}}`)
	if got := w.next(); got["error"] != nil {
		t.Fatalf("factory_job add = %v", got)
	}
	if len(engine.added) != 1 || engine.added[0].Kind != RefFactoryJob || engine.added[0].ID != "7" || engine.added[0].Factory != "f" {
		t.Fatalf("collector got %+v", engine.added)
	}
	w.send(`{"id":2,"method":"collections.add","params":{"id":"c1","refs":[{"kind":"factory_job","id":"7"}]}}`)
	if got := w.next(); got["error"] == nil || got["error"].(map[string]any)["code"] != CodeBadParams {
		t.Fatalf("factory_job without a factory = %v", got)
	}
}
