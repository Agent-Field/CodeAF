package appwire

import (
	"encoding/json"
	"testing"
)

// A STEP'S TRIES ARE PART OF THE EDIT LANGUAGE when the engine announces
// factory.job_step_edit_max: `max` maps a conversation step to 1, 2 or 3
// tries. Any other count and a step named twice are refused before the engine
// is asked; tries alone are an edit.
func TestJobStepEditCarriesAStepsTries(t *testing.T) {
	if h := withFactoryJobStepEditMaxFeature(Hello{}, &factoryEngine{}); len(h.Features) != 0 {
		t.Fatalf("a factory that does not edit steps announced %v", h.Features)
	}
	if h := withFactoryJobStepEditMaxFeature(Hello{}, &stepEditEngine{}); len(h.Features) != 1 || h.Features[0] != FeatureFactoryJobStepEditMax {
		t.Fatalf("an editor announced %v", h.Features)
	}
	eng := &stepEditEngine{}
	w := func(edit string) json.RawMessage {
		return json.RawMessage(`{"factory":"f","job":2,` + stepGuard + `,"edit":` + edit + `}`)
	}
	if _, err := methods["factory.job_step_edit"].call(eng, w(`{"max":{" review ":3,"publish":1}}`)); err != nil {
		t.Fatal(err)
	}
	if got := eng.writes[0].Edit.Max; len(got) != 2 || got["review"] != 3 || got["publish"] != 1 {
		t.Fatalf("max: %+v", got)
	}
	for _, bad := range []string{`{"max":{"review":4}}`, `{"max":{"review":0}}`, `{"max":{"review":2," review ":3}}`, `{"max":{"review":"2"}}`} {
		if _, err := methods["factory.job_step_edit"].call(eng, w(bad)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
	if len(eng.writes) != 1 {
		t.Fatalf("a refused edit reached the engine: %d writes", len(eng.writes))
	}
}
