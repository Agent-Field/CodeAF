package appwire

import (
	"context"
	"encoding/json"
	"testing"
)

type foremanEngine struct {
	factoryEngine
	got []FactoryRef
}

func (f *foremanEngine) FactoryForemanOpen(_ context.Context, p FactoryRef) (FactoryForeman, error) {
	f.got = append(f.got, p)
	return FactoryForeman{Factory: p.Factory, Agent: "foreman1"}, nil
}

type onlyForeman struct{ fakeEngine }

func (*onlyForeman) FactoryForemanOpen(context.Context, FactoryRef) (FactoryForeman, error) {
	return FactoryForeman{}, nil
}

// THE FACTORY'S OWN MANAGER HAS A DOOR. The 2026-11-06 audit found the foreman
// made by code nothing called; factory.foreman_open answers its agent id, is
// announced only by a factory engine that serves it, and refuses a bad ref.
func TestForemanOpenIsRegisteredAnnouncedAndAbsentWithoutItsEngine(t *testing.T) {
	const name = "factory.foreman_open"
	if _, ok := methods[name]; !ok {
		t.Fatalf("%s is not registered", name)
	}
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &onlyForeman{}} {
		if _, err := methods[name].call(e, json.RawMessage(`{"factory":"factory_local"}`)); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if h := withFactoryForemanOpenFeature(Hello{}, e); len(h.Features) != 0 {
			t.Errorf("%T announced %v", e, h.Features)
		}
	}
	e := &foremanEngine{}
	out, err := methods[name].call(e, json.RawMessage(`{"factory":"factory_local"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := json.Marshal(out); string(got) != `{"factory":"factory_local","agent":"foreman1"}` || len(e.got) != 1 {
		t.Fatalf("answered %s after %+v", got, e.got)
	}
	if h := withFactoryForemanOpenFeature(Hello{}, e); len(h.Features) != 1 || h.Features[0] != FeatureFactoryForemanOpen {
		t.Fatalf("announced %v", h.Features)
	}
	for _, bad := range []string{`{"factory":""}`, `{"factory":"f","job":1}`} {
		if _, err := methods[name].call(e, json.RawMessage(bad)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

type freshForemanEngine struct {
	foremanEngine
	fresh []FactoryRef
}

func (f *freshForemanEngine) FactoryForemanFresh(_ context.Context, p FactoryRef) (FactoryForeman, error) {
	f.fresh = append(f.fresh, p)
	return FactoryForeman{Factory: p.Factory, Agent: "foreman2"}, nil
}

// THE MANAGER CAN BE STARTED OVER. factory.foreman_fresh answers the new
// conversation's agent id, is announced only by a factory engine that serves
// it, beside factory.foreman_open and never instead of it, and refuses a bad
// ref as the open does.
func TestForemanFreshIsRegisteredAnnouncedAndAbsentWithoutItsEngine(t *testing.T) {
	const name = "factory.foreman_fresh"
	if _, ok := methods[name]; !ok {
		t.Fatalf("%s is not registered", name)
	}
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &foremanEngine{}} {
		if _, err := methods[name].call(e, json.RawMessage(`{"factory":"factory_local"}`)); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if h := withFactoryForemanFreshFeature(Hello{}, e); len(h.Features) != 0 {
			t.Errorf("%T announced %v", e, h.Features)
		}
	}
	e := &freshForemanEngine{}
	out, err := methods[name].call(e, json.RawMessage(`{"factory":"factory_local"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := json.Marshal(out); string(got) != `{"factory":"factory_local","agent":"foreman2"}` || len(e.fresh) != 1 || len(e.got) != 0 {
		t.Fatalf("answered %s after %+v (opens %+v)", got, e.fresh, e.got)
	}
	h := withFactoryForemanFreshFeature(withFactoryForemanOpenFeature(Hello{}, e), e)
	if len(h.Features) != 2 || h.Features[0] != FeatureFactoryForemanOpen || h.Features[1] != FeatureFactoryForemanFresh {
		t.Fatalf("announced %v", h.Features)
	}
	for _, bad := range []string{`{"factory":""}`, `{"factory":"f","job":1}`} {
		if _, err := methods[name].call(e, json.RawMessage(bad)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
}
