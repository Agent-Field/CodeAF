package appwire

import (
	"encoding/json"
	"testing"
)

// RUN AGAIN FROM HERE WITH ANOTHER MODEL is factory.job_step_rerun with
// `model` where the engine announces factory.job_step_rerun_model: the model
// is trimmed and carried, one shaped unlike a model id is refused before the
// engine is asked, and a step stop takes no model.
func TestAStepRerunCanNameAModel(t *testing.T) {
	if h := withFactoryJobStepRerunModelFeature(Hello{}, &factoryEngine{}); len(h.Features) != 0 {
		t.Fatalf("a factory without actions announced %v", h.Features)
	}
	if h := withFactoryJobStepRerunModelFeature(Hello{}, &actorEngine{}); len(h.Features) != 1 || h.Features[0] != FeatureFactoryJobStepRerunModel {
		t.Fatalf("an actor announced %v", h.Features)
	}
	eng := &actorEngine{}
	call := func(name, params string) error {
		_, err := methods[name].call(eng, json.RawMessage(params))
		return err
	}
	if err := call("factory.job_step_rerun", `{"factory":"f","job":1,"step":"review",`+guard+`,"model":" vendor/new "}`); err != nil {
		t.Fatal(err)
	}
	if p := eng.got[0].(FactoryJobStepParams); p.Model != "vendor/new" || p.Step != "review" {
		t.Fatalf("rerun: %+v", p)
	}
	if err := call("factory.job_step_rerun", `{"factory":"f","job":1,"step":"review",`+guard+`,"model":"two words"}`); wireCode(t, err) != CodeBadParams {
		t.Fatalf("a model with a space: %v", err)
	}
	if err := call("factory.job_step_stop", `{"factory":"f","job":1,"step":"review",`+guard+`,"model":"vendor/new"}`); wireCode(t, err) != CodeBadParams {
		t.Fatalf("a stop with a model: %v", err)
	}
	if len(eng.got) != 1 {
		t.Fatalf("a refused call reached the engine: %d", len(eng.got))
	}
}
