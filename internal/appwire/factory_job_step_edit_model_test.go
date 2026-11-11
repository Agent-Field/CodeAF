package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

// A STEP'S MODEL IS PART OF THE EDIT LANGUAGE when the engine announces
// factory.job_step_edit_model: `model` maps a step to a model id, trimmed, ""
// for the job's model. A model with a space or longer than a model id, and a
// step named twice, are refused before the engine is asked; a model edit alone
// is an edit.
func TestJobStepEditCarriesAStepsModel(t *testing.T) {
	if h := withFactoryJobStepEditModelFeature(Hello{}, &factoryEngine{}); len(h.Features) != 0 {
		t.Fatalf("a factory that does not edit steps announced %v", h.Features)
	}
	if h := withFactoryJobStepEditModelFeature(Hello{}, &stepEditEngine{}); len(h.Features) != 1 || h.Features[0] != FeatureFactoryJobStepEditModel {
		t.Fatalf("an editor announced %v", h.Features)
	}
	eng := &stepEditEngine{}
	w := func(edit string) json.RawMessage {
		return json.RawMessage(`{"factory":"f","job":2,` + stepGuard + `,"edit":` + edit + `}`)
	}
	if _, err := methods["factory.job_step_edit"].call(eng, w(`{"model":{" review ":" anthropic/claude-opus-5.5 ","publish":""}}`)); err != nil {
		t.Fatal(err)
	}
	if got := eng.writes[0].Edit.Model; len(got) != 2 || got["review"] != "anthropic/claude-opus-5.5" || got["publish"] != "" {
		t.Fatalf("model: %+v", got)
	}
	for _, bad := range []string{
		`{"model":{"review":"two words"}}`,
		`{"model":{"review":"` + strings.Repeat("m", 201) + `"}}`,
		`{"model":{"review":"a"," review ":"b"}}`,
		`{"model":{"":"a"}}`,
	} {
		if _, err := methods["factory.job_step_edit"].call(eng, w(bad)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
	if len(eng.writes) != 1 {
		t.Fatalf("a refused edit reached the engine: %d writes", len(eng.writes))
	}
}
