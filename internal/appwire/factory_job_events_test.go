package appwire

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

type jobEventsEngine struct {
	factoryEngine
	got []FactoryEventsSinceParams
}

func (e *jobEventsEngine) FactoryEventsSince(_ context.Context, p FactoryEventsSinceParams) (FactoryEvents, error) {
	e.got = append(e.got, p)
	return FactoryEvents{Factory: p.Factory, Supported: true, Reset: p.After == nil}, nil
}

type onlyJobEvents struct{ fakeEngine }

func (*onlyJobEvents) FactoryEventsSince(context.Context, FactoryEventsSinceParams) (FactoryEvents, error) {
	return FactoryEvents{}, nil
}

// E10: only a Factory implementing the reader announces and serves the feature.
func TestFactoryJobEventsCapability(t *testing.T) {
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &onlyJobEvents{}} {
		if _, err := methods["factory.events_since"].call(e, json.RawMessage(`{"factory":"f"}`)); wireCode(t, err) != CodeUnknownMethod {
			t.Fatalf("%T: %v", e, err)
		}
		if h := withFactoryJobEventsFeature(Hello{}, e); len(h.Features) != 0 {
			t.Fatalf("%T announced %v", e, h.Features)
		}
	}
	if h := withFactoryJobEventsFeature(Hello{}, &jobEventsEngine{}); len(h.Features) != 1 || h.Features[0] != FeatureFactoryJobEvents {
		t.Fatalf("hello: %+v", h)
	}
}

// E14, E5: validation keeps absent and zero cursors distinct and defaults the limit.
func TestFactoryJobEventsParamsAndShape(t *testing.T) {
	e := &jobEventsEngine{}
	for _, raw := range []string{`{}`, `{"factory":" "}`, `{"factory":"f","after":-1}`, `{"factory":"f","limit":501}`, `{"factory":"f","limit":-1}`, `{"factory":"f","after":"0"}`} {
		if _, err := methods["factory.events_since"].call(e, json.RawMessage(raw)); wireCode(t, err) != CodeBadParams {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	for _, raw := range []string{`{"factory":"f"}`, `{"factory":"f","after":0,"follow":true}`} {
		out, err := methods["factory.events_since"].call(e, json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		got := out.(*FactoryEvents)
		if got.Events == nil || !got.Supported {
			t.Fatalf("answer: %+v", got)
		}
	}
	if len(e.got) != 2 || e.got[0].After != nil || e.got[1].After == nil || *e.got[1].After != 0 || e.got[0].Limit != DefaultFactoryEvents || !e.got[1].Follow {
		t.Fatalf("params: %+v", e.got)
	}
	if EventShape(EventFactoryEvent) != reflect.TypeFor[FactoryEvent]() {
		t.Fatal("event shape was not registered")
	}
	schema := Schema()
	if schema["events"].(map[string]any)[EventFactoryEvent] == nil || schema["methods"].(map[string]any)["factory.events_since"] == nil {
		t.Fatal("schema omits the method or event")
	}
	defs := schema["$defs"].(map[string]any)
	props := defs["FactoryEvent"].(map[string]any)["properties"].(map[string]any)
	if !reflect.DeepEqual(props["kind"].(map[string]any)["enum"], []string{"finished", "merged", "failed", "needs_you"}) {
		t.Fatalf("kind schema: %+v", props["kind"])
	}
}
