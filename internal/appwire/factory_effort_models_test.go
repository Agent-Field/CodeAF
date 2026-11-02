package appwire

import "testing"

// factoryTaskModelsEngine is a factory engine whose settings serve the task
// models.
type factoryTaskModelsEngine struct {
	factoryEngine
	taskModelsEngine
}

// Only an engine that serves both a factory and the task models announces
// that a factory step's effort runs on the small-work and checks models.
func TestFactoryEffortModelsIsAnnouncedByAFactoryWithTaskModels(t *testing.T) {
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &taskModelsEngine{}} {
		if h := withFactoryEffortModelsFeature(Hello{}, e); len(h.Features) != 0 {
			t.Errorf("%T announced %v", e, h.Features)
		}
	}
	h := withFactoryEffortModelsFeature(Hello{}, &factoryTaskModelsEngine{})
	if len(h.Features) != 1 || h.Features[0] != FeatureFactoryEffortModels || FeatureFactoryEffortModels != "factory.effort_models" {
		t.Fatalf("announced %v", h.Features)
	}
}
