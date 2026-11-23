package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type agentFieldWireEngine struct {
	factoryEngine
	connects []FactoryAgentFieldConnectParams
	adds     []FactoryAgentFieldTriggerAddParams
}

func (a *agentFieldWireEngine) FactoryAgentFieldConnect(_ context.Context, p FactoryAgentFieldConnectParams) (FactoryAgentFieldStatus, error) {
	a.connects = append(a.connects, p)
	return FactoryAgentFieldStatus{Factory: p.Factory, Connected: true, URL: p.URL, KeyKept: p.APIKey != ""}, nil
}
func (a *agentFieldWireEngine) FactoryAgentFieldStatus(_ context.Context, id string) (FactoryAgentFieldStatus, error) {
	return FactoryAgentFieldStatus{Factory: id}, nil
}
func (a *agentFieldWireEngine) FactoryAgentFieldDisconnect(_ context.Context, id string) (FactoryAgentFieldStatus, error) {
	return FactoryAgentFieldStatus{Factory: id}, nil
}
func (a *agentFieldWireEngine) FactoryAgentFieldTriggers(_ context.Context, id string) (FactoryAgentFieldTriggers, error) {
	return FactoryAgentFieldTriggers{Factory: id, NodeID: "codeaf-factory"}, nil
}
func (a *agentFieldWireEngine) FactoryAgentFieldSources(_ context.Context, id string) (FactoryAgentFieldSources, error) {
	return FactoryAgentFieldSources{Factory: id}, nil
}
func (a *agentFieldWireEngine) FactoryAgentFieldTriggerAdd(_ context.Context, p FactoryAgentFieldTriggerAddParams) (FactoryAgentFieldTrigger, error) {
	a.adds = append(a.adds, p)
	return FactoryAgentFieldTrigger{ID: "t1", Source: p.Source, Reasoner: "intake"}, nil
}

var agentFieldMethods = []string{"factory.agentfield_connect", "factory.agentfield_status", "factory.agentfield_disconnect", "factory.agentfield_triggers", "factory.agentfield_sources", "factory.agentfield_trigger_add"}

// The six methods are served and announced only by a factory engine that has
// them; a connect is bounded before the engine is asked, and no answer or
// refusal ever quotes the key.
func TestFactoryAgentFieldMethodsAreRegisteredAnnouncedAndBounded(t *testing.T) {
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}} {
		for _, name := range agentFieldMethods {
			if _, err := methods[name].call(e, json.RawMessage(`{"factory":"f","source":"linear"}`)); wireCode(t, err) != CodeUnknownMethod {
				t.Errorf("%T %s: %v", e, name, err)
			}
		}
		if h := withFactoryAgentFieldFeature(Hello{}, e); len(h.Features) != 0 {
			t.Errorf("%T announced %v", e, h.Features)
		}
	}
	e := &agentFieldWireEngine{}
	if h := withFactoryAgentFieldFeature(Hello{}, e); len(h.Features) != 1 || h.Features[0] != FeatureFactoryAgentField {
		t.Fatalf("announced %v", h.Features)
	}
	out, err := methods["factory.agentfield_connect"].call(e, json.RawMessage(`{"factory":" factory_local ","url":" http://cp:8080 ","api_key":" k3y-s3cret ","base_url":" http://10.0.0.5:8765 "}`))
	if err != nil {
		t.Fatal(err)
	}
	if st := out.(*FactoryAgentFieldStatus); !st.Connected || st.URL != "http://cp:8080" || !st.KeyKept {
		t.Fatalf("answered %+v", st)
	}
	// NO base_url IS A LINK-ONLY CONNECT, and is taken.
	if _, err := methods["factory.agentfield_connect"].call(e, json.RawMessage(`{"factory":"factory_local","url":"http://cp:8080"}`)); err != nil {
		t.Fatalf("a link-only connect: %v", err)
	}
	e.connects = e.connects[:1]
	if len(e.connects) != 1 || e.connects[0].Factory != "factory_local" || e.connects[0].APIKey != "k3y-s3cret" || e.connects[0].BaseURL != "http://10.0.0.5:8765" {
		t.Fatalf("handed %+v", e.connects)
	}
	answer, _ := json.Marshal(out)
	if strings.Contains(string(answer), "k3y-s3cret") || strings.Contains(string(answer), "api_key") {
		t.Fatalf("the answer carries the key: %s", answer)
	}
	for _, bad := range []string{
		`{"factory":"f","base_url":"http://x:1"}`,
		`{"factory":"f","url":"http://cp","internal_token":"two words"}`,
		`{"factory":"f","url":"http://cp","base_url":"http://x:1","api_key":"two words"}`,
	} {
		if _, err := methods["factory.agentfield_connect"].call(e, json.RawMessage(bad)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
	_, err = methods["factory.agentfield_connect"].call(e, json.RawMessage(`{"factory":"f","url":"u","base_url":"b","api_key":"leak-me","extra":1}`))
	if wireCode(t, err) != CodeBadParams || strings.Contains(err.Error(), "leak-me") {
		t.Fatalf("a malformed connect: %v", err)
	}
	if _, err := methods["factory.agentfield_trigger_add"].call(e, json.RawMessage(`{"factory":"f","source":"linear","config":[1]}`)); wireCode(t, err) != CodeBadParams {
		t.Fatalf("a config that is not an object: %v", err)
	}
	if _, err := methods["factory.agentfield_trigger_add"].call(e, json.RawMessage(`{"factory":"f","source":"generic_bearer","config":{"header":"Authorization"},"secret_env_var":"AF_SECRET"}`)); err != nil || len(e.adds) != 1 || e.adds[0].SecretEnvVar != "AF_SECRET" {
		t.Fatalf("a trigger add: %v %+v", err, e.adds)
	}
	for _, name := range agentFieldMethods[1:5] {
		if _, err := methods[name].call(e, json.RawMessage(`{"factory":"factory_local"}`)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
