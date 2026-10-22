package appwire

import (
	"encoding/json"
	"testing"
)

type modelPolicyEngine struct {
	fakeEngine
	calls     []string
	automatic bool
}

func (e *modelPolicyEngine) SetAutomaticModels(id string, on bool) error {
	e.calls = append(e.calls, "policy")
	e.automatic = on
	return nil
}
func (e *modelPolicyEngine) SetModel(id, model string) (Agent, error) {
	e.calls = append(e.calls, "model")
	return Agent{ID: id, Model: model}, nil
}
func TestModelIdentitySelectionPolicyPrecedesModelChange(t *testing.T) {
	e := &modelPolicyEngine{automatic: true}
	raw := json.RawMessage(`{"id":"a1","model":"vendor/model","automatic":false}`)
	_, err := methods["agents.set_model"].call(e, raw)
	if err != nil {
		t.Fatal(err)
	}
	if e.automatic || len(e.calls) != 2 || e.calls[0] != "policy" || e.calls[1] != "model" {
		t.Fatalf("selection: %v %v", e.calls, e.automatic)
	}
	if _, err := methods["agents.set_model"].call(&fakeEngine{}, raw); err == nil {
		t.Fatal("unsupported policy was silently ignored")
	}
}
