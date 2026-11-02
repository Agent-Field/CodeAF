package appwire

import (
	"context"
	"encoding/json"
	"testing"
)

func (f *factoryEngine) FactorySessionsTake(context.Context) (FactorySessionsTaken, error) {
	f.note("take")
	return FactorySessionsTaken{}, nil
}

func (f *factoryEngine) FactorySessionRestore(_ context.Context, p FactorySessionRestoreParams) (FactorySessionRestored, error) {
	f.note("restore " + p.Factory + " " + p.URL)
	return FactorySessionRestored{}, nil
}

// A HAND-BACK NAMES A CONNECTION AND A TOKEN, AND NOTHING ELSE: a missing token
// or an unknown field is refused before the engine sees it, and a take with
// params is refused too.
func TestFactorySessionMethodsReadTheirParamsStrictly(t *testing.T) {
	eng := &factoryEngine{}
	for _, c := range []struct{ name, params string }{
		{"factory.session_restore", `{"factory":"f","url":"https://f.example"}`},
		{"factory.session_restore", `{"factory":"f","url":"https://f.example","token":"  "}`},
		{"factory.session_restore", `{"factory":"","url":"https://f.example","token":"t"}`},
		{"factory.session_restore", `{"factory":"f","url":"https://f.example","token":"t","path":"/x"}`},
		{"factory.sessions_take", `{"factory":"f"}`},
	} {
		_, err := methods[c.name].call(eng, json.RawMessage(c.params))
		if wireCode(t, err) != CodeBadParams {
			t.Errorf("%s %s: %v", c.name, c.params, err)
		}
	}
	if len(eng.calls) != 0 {
		t.Fatalf("a refused request reached the engine: %v", eng.calls)
	}
	if got := withFactorySessionsFeature(Hello{}, eng); len(got.Features) != 1 || got.Features[0] != FeatureFactorySessions {
		t.Fatalf("features = %v", got.Features)
	}
	if got := withFactorySessionsFeature(Hello{}, &fakeEngine{}); len(got.Features) != 0 {
		t.Fatalf("a plain engine announced %v", got.Features)
	}
}
