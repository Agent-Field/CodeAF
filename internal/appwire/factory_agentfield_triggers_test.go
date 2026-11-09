package appwire

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/route"
)

type routesEngine struct{ factoryEngine }

func (routesEngine) FactoryRoutes(context.Context, string) (FactoryRoutes, error) {
	return FactoryRoutes{}, nil
}
func (routesEngine) FactoryRoutesSet(context.Context, FactoryRoutesSetParams) (FactoryRoutes, error) {
	return FactoryRoutes{}, nil
}
func (routesEngine) FactoryRoutesTest(context.Context, FactoryRoutesTestParams) (FactoryRoutesTried, error) {
	return FactoryRoutesTried{}, nil
}

type agentFieldTriggersEngine struct {
	agentFieldWireEngine
	added []FactoryAgentFieldAddTriggerParams
}

func (a *agentFieldTriggersEngine) FactoryAgentFieldAddTrigger(_ context.Context, p FactoryAgentFieldAddTriggerParams) (FactoryAgentFieldAddedTrigger, error) {
	a.added = append(a.added, p)
	return FactoryAgentFieldAddedTrigger{Factory: p.Factory, Trigger: FactoryAgentFieldTrigger{ID: "t1", Source: p.Source, EntrySource: "agentfield:" + p.Source},
		Setup: &FactoryAgentFieldTriggerSetup{WebhookURL: "http://cp/sources/t1"}}, nil
}

// factory.agentfield_add_trigger is served and its feature announced only by
// a factory engine that has it, its params are bounded before the engine is
// asked, and a refusal never quotes the config it was sent.
func TestFactoryAgentFieldAddTriggerIsServedAnnouncedAndNeverQuotesItsConfig(t *testing.T) {
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &agentFieldWireEngine{}} {
		if _, err := methods["factory.agentfield_add_trigger"].call(e, json.RawMessage(`{"factory":"f","source":"linear"}`)); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if h := withFactoryAgentFieldTriggersFeature(Hello{}, e); len(h.Features) != 0 {
			t.Errorf("%T announced %v", e, h.Features)
		}
	}
	e := &agentFieldTriggersEngine{}
	if h := withFactoryAgentFieldTriggersFeature(Hello{}, e); !slices.Equal(h.Features, []string{FeatureFactoryAgentFieldTriggers}) {
		t.Fatalf("announced %v", h.Features)
	}
	out, err := methods["factory.agentfield_add_trigger"].call(e, json.RawMessage(`{"factory":" factory_local ","source":" linear ","event_types":[" Issue.create ",""],"config":{"tolerance_seconds":60}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := out.(*FactoryAgentFieldAddedTrigger); got.Trigger.EntrySource != "agentfield:linear" || got.Setup == nil || got.Setup.WebhookURL == "" {
		t.Fatalf("answered %+v", got)
	}
	if len(e.added) != 1 || e.added[0].Factory != "factory_local" || e.added[0].Source != "linear" || !slices.Equal(e.added[0].EventTypes, []string{"Issue.create"}) {
		t.Fatalf("handed %+v", e.added)
	}
	for _, bad := range []string{
		`{"factory":"f"}`,
		`{"factory":"f","source":"linear","config":[1]}`,
		`{"factory":"f","source":"linear","event_types":["two words"]}`,
	} {
		if _, err := methods["factory.agentfield_add_trigger"].call(e, json.RawMessage(bad)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
	for _, name := range []string{"factory.agentfield_add_trigger", "factory.agentfield_trigger_add"} {
		_, err := methods[name].call(e, json.RawMessage(`{"factory":"f","source":"generic_bearer","config":"leak-me","extra":"leak-me-too"}`))
		if wireCode(t, err) != CodeBadParams || strings.Contains(err.Error(), "leak-me") {
			t.Errorf("%s, a malformed add: %v", name, err)
		}
	}
}

// A ROUTE ENGINE ANNOUNCES run_on beside its routing rules: a rule's then
// carries it on the wire both ways.
func TestRoutesAnnounceRunOnAndCarryIt(t *testing.T) {
	e := &routesEngine{}
	h := withFactoryRoutesFeature(Hello{}, e)
	if !slices.Contains(h.Features, FeatureFactoryRoutes) || !slices.Contains(h.Features, FeatureFactoryRouteRunOn) {
		t.Fatalf("announced %v", h.Features)
	}
	if h := withFactoryRoutesFeature(Hello{}, &factoryEngine{}); slices.Contains(h.Features, FeatureFactoryRouteRunOn) {
		t.Fatalf("an engine without routes announced %v", h.Features)
	}
	p, err := CheckRoutesSet(FactoryRoutesSetParams{Factory: "f", Rules: &[]route.Rule{{Name: "support", Then: route.Action{RunOn: " bot.reply ", Start: "auto"}}}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal((*p.Rules)[0])
	if !strings.Contains(string(raw), `"run_on":"bot.reply"`) {
		t.Fatalf("a rule on the wire: %s", raw)
	}
	if _, err := CheckRoutesSet(FactoryRoutesSetParams{Factory: "f", Rules: &[]route.Rule{{Name: "support", Then: route.Action{RunOn: "bot", Start: "auto"}}}}); wireCode(t, err) != CodeBadParams || !strings.Contains(err.Error(), "node.reasoner") {
		t.Fatalf("a bad run_on: %v", err)
	}
}
