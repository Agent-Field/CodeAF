package appwire

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type workstreamEngine struct {
	fakeEngine
}

func (e *workstreamEngine) SetAgentWorkstream(id, workstream string) (AgentWorkstream, error) {
	if id == "missing" {
		return AgentWorkstream{}, Errorf(CodeNotFound, "there is no conversation with id missing")
	}
	return AgentWorkstream{ID: id, Workstream: workstream}, nil
}

// The mark is announced only by an engine that keeps it, set and cleared by one
// method, and an engine without it refuses by name.
func TestAgentWorkstreamIsAnnouncedAndServedOnlyWithItsDoor(t *testing.T) {
	engine := &workstreamEngine{}
	w := startWireOver(t, engine, &engine.fakeEngine)
	if !slices.Contains(features(w.next()), FeatureAgentWorkstream) {
		t.Fatal("hello lacks agents.workstream")
	}
	for i, word := range []string{"ws-pantry", ""} {
		request, err := json.Marshal(map[string]any{
			"id": i + 1, "method": "agents.set_workstream", "params": map[string]string{"id": "a1", "workstream": word},
		})
		if err != nil {
			t.Fatal(err)
		}
		w.send(string(request))
		got := w.next()
		want := map[string]any{"id": "a1", "workstream": word}
		if got["error"] != nil || !reflect.DeepEqual(got["result"], want) {
			t.Fatalf("workstream %q = %v", word, got)
		}
	}
	plain := startWire(t, &fakeEngine{})
	if slices.Contains(features(plain.next()), FeatureAgentWorkstream) {
		t.Fatal("an engine without the door announced agents.workstream")
	}
	plain.send(`{"id":3,"method":"agents.set_workstream","params":{"id":"a1","workstream":"ws"}}`)
	got := plain.next()
	if failure, ok := got["error"].(map[string]any); !ok || failure["code"] != CodeUnknownMethod || failure["message"] != "this engine does not serve agents.set_workstream" {
		t.Fatalf("an unsupported engine = %v", got)
	}
}

func TestAgentWorkstreamValidatesParametersAndCarriesEngineErrors(t *testing.T) {
	long, _ := json.Marshal(strings.Repeat("w", 129))
	for _, tc := range []struct {
		params, code, message string
	}{
		{`{"workstream":"ws"}`, CodeBadParams, "agents.set_workstream needs an id"},
		{`{"id":"","workstream":"ws"}`, CodeBadParams, "agents.set_workstream needs an id"},
		{`{"id":"a1","workstream":` + string(long) + `}`, CodeBadParams, "workstream is an id of at most 128 characters"},
		{`{"id":"missing","workstream":"ws"}`, CodeNotFound, "there is no conversation with id missing"},
	} {
		_, err := methods["agents.set_workstream"].call(&workstreamEngine{}, json.RawMessage(tc.params))
		failure, ok := err.(*Error)
		if !ok || failure.Code != tc.code || failure.Message != tc.message {
			t.Fatalf("params %s = %v, want %s: %s", tc.params, err, tc.code, tc.message)
		}
	}
}
