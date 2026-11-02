package appwire

import (
	"context"
	"encoding/json"
	"testing"
)

type arrivalEngine struct {
	factoryEngine
	on  bool
	set []bool
}

func (a *arrivalEngine) FactoryReadOnArrival(_ context.Context, p FactoryReadOnArrivalParams) (FactoryReadOnArrival, error) {
	if p.On != nil {
		a.set = append(a.set, *p.On)
		a.on = *p.On
	}
	return FactoryReadOnArrival{On: a.on}, nil
}

// "Read new issues as they arrive" is one method: without `on` it answers the
// factory's setting, with it the factory changes it and answers the new one.
func TestReadOnArrivalIsReadAndSetThroughOneMethod(t *testing.T) {
	const name = "factory.read_on_arrival"
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}} {
		if _, err := methods[name].call(e, json.RawMessage(`{"factory":"f"}`)); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if h := withFactoryReadOnArrivalFeature(Hello{}, e); len(h.Features) != 0 {
			t.Errorf("%T announced %v", e, h.Features)
		}
	}
	e := &arrivalEngine{on: true}
	if h := withFactoryReadOnArrivalFeature(Hello{}, e); len(h.Features) != 1 || h.Features[0] != FeatureFactoryReadOnArrival {
		t.Fatalf("announced %v", h.Features)
	}
	out, err := methods[name].call(e, json.RawMessage(`{"factory":"factory_local"}`))
	if got, _ := json.Marshal(out); err != nil || string(got) != `{"on":true}` || len(e.set) != 0 {
		t.Fatalf("read answered %s, %v, set %v", got, err, e.set)
	}
	out, err = methods[name].call(e, json.RawMessage(`{"factory":"factory_local","on":false}`))
	if got, _ := json.Marshal(out); err != nil || string(got) != `{"on":false}` || len(e.set) != 1 {
		t.Fatalf("set answered %s, %v, set %v", got, err, e.set)
	}
	if _, err := methods[name].call(e, json.RawMessage(`{"factory":""}`)); wireCode(t, err) != CodeBadParams {
		t.Errorf("no factory: %v", err)
	}
}
