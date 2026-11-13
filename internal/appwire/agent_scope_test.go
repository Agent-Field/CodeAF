package appwire

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
)

type scopeEngine struct {
	fakeEngine
}

func (e *scopeEngine) SetAgentScope(id, scope string) (AgentScope, error) {
	if id == "missing" {
		return AgentScope{}, Errorf(CodeNotFound, "there is no conversation with id missing")
	}
	return AgentScope{ID: id, Scope: scope}, nil
}

// L-FIX9-cross A7
func TestAgentScopeIsAnnouncedAndServedOnlyWithItsDoor(t *testing.T) {
	engine := &scopeEngine{}
	w := startWireOver(t, engine, &engine.fakeEngine)
	if !slices.Contains(features(w.next()), FeatureAgentScope) {
		t.Fatal("hello lacks agents.scope")
	}
	for i, word := range []string{"conversation", "project"} {
		request, err := json.Marshal(map[string]any{
			"id": i + 1, "method": "agents.set_scope", "params": map[string]string{"id": "a1", "scope": word},
		})
		if err != nil {
			t.Fatal(err)
		}
		w.send(string(request))
		got := w.next()
		want := map[string]any{"id": "a1", "scope": word}
		if got["error"] != nil || !reflect.DeepEqual(got["result"], want) {
			t.Fatalf("scope %q = %v", word, got)
		}
	}
	plain := startWire(t, &fakeEngine{})
	if slices.Contains(features(plain.next()), FeatureAgentScope) {
		t.Fatal("an engine without the door announced agents.scope")
	}
	plain.send(`{"id":3,"method":"agents.set_scope","params":{"id":"a1","scope":"conversation"}}`)
	got := plain.next()
	if failure, ok := got["error"].(map[string]any); !ok || failure["code"] != CodeUnknownMethod || failure["message"] != "this engine does not serve agents.set_scope" {
		t.Fatalf("an unsupported engine = %v", got)
	}
}

// L-FIX9-cross A7
func TestAgentScopeValidatesParametersAndCarriesEngineErrors(t *testing.T) {
	for _, tc := range []struct {
		params, code, message string
	}{
		{`{"scope":"conversation"}`, CodeBadParams, "agents.set_scope needs an id"},
		{`{"id":"","scope":"conversation"}`, CodeBadParams, "agents.set_scope needs an id"},
		{`{"id":"a1"}`, CodeBadParams, "scope is project or conversation"},
		{`{"id":"a1","scope":"everywhere"}`, CodeBadParams, "scope is project or conversation"},
		{`{"id":"a1","scope":"Conversation"}`, CodeBadParams, "scope is project or conversation"},
		{`{"id":"missing","scope":"conversation"}`, CodeNotFound, "there is no conversation with id missing"},
	} {
		_, err := methods["agents.set_scope"].call(&scopeEngine{}, json.RawMessage(tc.params))
		failure, ok := err.(*Error)
		if !ok || failure.Code != tc.code || failure.Message != tc.message {
			t.Fatalf("params %s = %v, want %s: %s", tc.params, err, tc.code, tc.message)
		}
	}
}

// L-FIX9-cross A7
func TestAgentScopeSchemaNamesBothWordsAndTheResult(t *testing.T) {
	doc := Schema()
	method := doc["methods"].(map[string]any)["agents.set_scope"].(map[string]any)
	params := method["params"].(map[string]any)["properties"].(map[string]any)
	result := doc["$defs"].(map[string]any)["AgentScope"].(map[string]any)
	for name, properties := range map[string]map[string]any{
		"params": params, "result": result["properties"].(map[string]any),
	} {
		if properties["id"].(map[string]any)["type"] != "string" {
			t.Fatalf("%s lacks a string id: %v", name, properties)
		}
		if got := properties["scope"].(map[string]any)["enum"]; !reflect.DeepEqual(got, []string{"project", "conversation"}) {
			t.Fatalf("%s scope enum = %v", name, got)
		}
	}
	if got := method["result"].(map[string]any)["$ref"]; got != "#/$defs/AgentScope" {
		t.Fatalf("result schema = %v", method["result"])
	}
}
