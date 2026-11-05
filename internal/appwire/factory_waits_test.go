package appwire

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"
)

type waitsEngine struct {
	factoryEngine
	sent int
	got  FactoryWaitsSetParams
}

func (e *waitsEngine) FactoryWaitsSet(_ context.Context, p FactoryWaitsSetParams) (FactoryWaitsSet, error) {
	e.sent++
	e.got = p
	return FactoryWaitsSet{Factory: p.Factory, Job: p.Job, WaitsOn: p.WaitsOn}, nil
}

func (e *waitsEngine) FactoryFeatures(context.Context, FactoryFeaturesParams) (FactoryFeatures, error) {
	return FactoryFeatures{Features: []string{"waits_on"}}, nil
}

// W9: both methods register independently of job actions and advertise only when served.
func TestWaitsMethodsAndFeatures(t *testing.T) {
	e := &waitsEngine{}
	if _, ok := factoryActionMethods[FeatureFactoryWaitsSet]; ok {
		t.Fatal("waits_set was registered as a job action")
	}
	for _, name := range []string{FeatureFactoryWaitsSet, FeatureFactoryFeatures} {
		if _, ok := methods[name]; !ok {
			t.Fatalf("missing %s", name)
		}
	}
	h := withFactoryFeaturesFeature(withFactoryWaitsSetFeature(Hello{}, e), e)
	if !slices.Contains(h.Features, FeatureFactoryWaitsSet) || !slices.Contains(h.Features, FeatureFactoryFeatures) {
		t.Fatalf("hello = %+v", h)
	}
	for _, old := range []Engine{&fakeEngine{}, &factoryEngine{}} {
		h := withFactoryFeaturesFeature(withFactoryWaitsSetFeature(Hello{}, old), old)
		if len(h.Features) != 0 {
			t.Fatalf("old engine hello = %+v", h)
		}
		for _, name := range []string{FeatureFactoryWaitsSet, FeatureFactoryFeatures} {
			if _, err := methods[name].call(old, json.RawMessage(`{}`)); wireCode(t, err) != CodeUnknownMethod {
				t.Fatalf("%s = %v", name, err)
			}
		}
	}
	got, err := methods[FeatureFactoryFeatures].call(e, json.RawMessage(`{"factory":"f"}`))
	if err != nil || !reflect.DeepEqual(got, &FactoryFeatures{Features: []string{"waits_on"}}) {
		t.Fatalf("features = %+v, %v", got, err)
	}
}

// W10: invalid input never reaches the engine.
func TestWaitsWireBounds(t *testing.T) {
	ids := make([]int, 21)
	for i := range ids {
		ids[i] = i + 1
	}
	tooMany, _ := json.Marshal(ids)
	for _, raw := range []string{
		`{"factory":"f","job":1,"waits_on":` + string(tooMany) + `}`,
		`{"factory":"f","job":1,"waits_on":[0]}`,
		`{"factory":"f","job":1,"waits_on":[-1]}`,
		`{"factory":"f","job":1,"waits_on":[2,2]}`,
		`{"factory":"f","job":0,"waits_on":[]}`,
		`{"factory":"f","job":1}`, `{"factory":"f","job":1,"waits_on":null}`,
		`{"factory":"f","job":1,"waits_on":[],"extra":true}`,
	} {
		t.Run(raw, func(t *testing.T) {
			e := &waitsEngine{}
			_, err := methods[FeatureFactoryWaitsSet].call(e, json.RawMessage(raw))
			if wireCode(t, err) != CodeBadParams || e.sent != 0 {
				t.Fatalf("invalid waits = %v; sends = %d", err, e.sent)
			}
		})
	}
	for _, ids := range []string{`[]`, `[2,3]`} {
		e := &waitsEngine{}
		_, err := methods[FeatureFactoryWaitsSet].call(e, json.RawMessage(fmt.Sprintf(`{"factory":" f ","job":1,"waits_on":%s}`, ids)))
		if err != nil || e.sent != 1 || e.got.Factory != "f" {
			t.Fatalf("valid waits = %v; sends = %d; params = %+v", err, e.sent, e.got)
		}
	}
}
