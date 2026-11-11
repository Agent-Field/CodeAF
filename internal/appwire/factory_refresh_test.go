package appwire

import (
	"context"
	"encoding/json"
	"testing"
)

type refreshEngine struct {
	factoryEngine
	got []string
}

func (r *refreshEngine) FactoryRefresh(_ context.Context, id string) error {
	r.got = append(r.got, id)
	return nil
}

type onlyRefresher struct{ fakeEngine }

func (*onlyRefresher) FactoryRefresh(context.Context, string) error { return nil }

func TestFactoryRefreshIsRegisteredAnnouncedAndNamesOneFactory(t *testing.T) {
	const name = "factory.refresh"
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &onlyRefresher{}} {
		if _, err := methods[name].call(e, json.RawMessage(`{"factory":"f"}`)); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if h := withFactoryRefreshFeature(Hello{}, e); len(h.Features) != 0 {
			t.Errorf("%T announced %v", e, h.Features)
		}
	}
	e := &refreshEngine{}
	if _, err := methods[name].call(e, json.RawMessage(`{"factory":"factory_local"}`)); err != nil {
		t.Fatal(err)
	}
	if len(e.got) != 1 || e.got[0] != "factory_local" {
		t.Fatalf("handed %v", e.got)
	}
	if h := withFactoryRefreshFeature(Hello{}, e); len(h.Features) != 1 || h.Features[0] != FeatureFactoryRefresh {
		t.Fatalf("announced %v", h.Features)
	}
	for _, bad := range []string{`{"factory":""}`, `{}`, `{"factory":"f","repo":"o/r"}`} {
		if _, err := methods[name].call(e, json.RawMessage(bad)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
}
