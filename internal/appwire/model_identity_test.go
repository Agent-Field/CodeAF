package appwire

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/session"
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

type modelPairEngine struct {
	fakeEngine
	pair session.ConversationModels
}

func (e *modelPairEngine) SetConversationModels(id string, pair session.ConversationModels) (Agent, error) {
	e.pair = pair
	return Agent{ID: id, Model: pair.Main}, nil
}
func TestConversationModelsPairIsAtomicAtAppWireDoor(t *testing.T) {
	e := &modelPairEngine{}
	_, err := methods["agents.set_models"].call(e, json.RawMessage(`{"id":"chat","main":"vendor/main","fallback":"vendor/backup"}`))
	if err != nil {
		t.Fatal(err)
	}
	if e.pair.Main != "vendor/main" || e.pair.Fallback != "vendor/backup" {
		t.Fatalf("pair: %+v", e.pair)
	}
	if _, err := methods["agents.set_models"].call(&fakeEngine{}, json.RawMessage(`{"id":"chat","main":"vendor/main","fallback":"vendor/backup"}`)); err == nil {
		t.Fatal("old engine silently accepted pair")
	}
}

func (e *modelPairEngine) SetConversationMain(id, main string) (Agent, error) {
	e.pair = session.ConversationModels{Main: main, Fallback: "kept/" + id}
	return Agent{ID: id, Model: main}, nil
}

// A FALLBACK LEFT OUT IS KEPT; AN EXPLICIT "" IS NONE. A client that moves only
// the main model cannot clear the fallback by not naming it.
func TestConversationModelsMissingFallbackKeepsTheCurrentOne(t *testing.T) {
	e := &modelPairEngine{}
	if _, err := methods["agents.set_models"].call(e, json.RawMessage(`{"id":"chat","main":"vendor/main"}`)); err != nil {
		t.Fatal(err)
	}
	if e.pair.Main != "vendor/main" || e.pair.Fallback != "kept/chat" {
		t.Fatalf("a missing fallback reached the engine as %+v", e.pair)
	}
	if _, err := methods["agents.set_models"].call(e, json.RawMessage(`{"id":"chat","main":"vendor/main","fallback":""}`)); err != nil {
		t.Fatal(err)
	}
	if e.pair.Fallback != "" {
		t.Fatalf("an explicit empty fallback reached the engine as %+v", e.pair)
	}
}
