package appwire

import (
	"context"
	"encoding/json"
	"testing"
)

type usageEngine struct {
	factoryEngine
	got []FactoryUsageParams
}

func (u *usageEngine) FactoryUsageByModel(_ context.Context, p FactoryUsageParams) (FactoryUsage, error) {
	u.got = append(u.got, p)
	usd := 1.25
	return FactoryUsage{Factory: p.Factory, From: p.From, To: p.To, Scope: FactoryUsageThisComputer,
		Models: []FactoryModelUse{{Model: "m", Paid: "metered", Calls: 2, TokensIn: 10, TokensOut: 3, CostUSD: &usd}},
		Jobs:   []FactoryJobUse{{Job: 7, Ref: "o/r#7", Models: []FactoryModelUse{{Model: "p", Paid: "plan", Calls: 1, TokensIn: 4, UnpricedCalls: 1}}}},
	}, nil
}

type onlyUsage struct{ fakeEngine }

func (*onlyUsage) FactoryUsageByModel(context.Context, FactoryUsageParams) (FactoryUsage, error) {
	return FactoryUsage{}, nil
}

func TestFactoryUsageByModelIsRegisteredAnnouncedAndChecked(t *testing.T) {
	const name = "factory.usage_by_model"
	if _, ok := methods[name]; !ok {
		t.Fatalf("%s is not registered", name)
	}
	params := `{"factory":"f1","from":"2026-10-26","to":"2026-11-01"}`
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &onlyUsage{}} {
		if _, err := methods[name].call(e, json.RawMessage(params)); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if h := withFactoryUsageFeature(Hello{}, e); len(h.Features) != 0 {
			t.Errorf("%T announced %v", e, h.Features)
		}
	}
	e := &usageEngine{}
	out, err := methods[name].call(e, json.RawMessage(`{"factory":"f1","from":"2026-10-26","to":"2026-11-01","job":7}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(e.got) != 1 || e.got[0].Job != 7 {
		t.Fatalf("handed %+v", e.got)
	}
	want := `{"factory":"f1","from":"2026-10-26","to":"2026-11-01","scope":"this_computer",` +
		`"models":[{"model":"m","paid":"metered","calls":2,"tokens_in":10,"tokens_out":3,"cost_usd":1.25}],` +
		`"jobs":[{"job":7,"ref":"o/r#7","models":[{"model":"p","paid":"plan","calls":1,"tokens_in":4,"tokens_out":0,"unpriced_calls":1}]}]}`
	if got, _ := json.Marshal(out); string(got) != want {
		t.Fatalf("answered %s", got)
	}
	if h := withFactoryUsageFeature(Hello{}, e); len(h.Features) != 1 || h.Features[0] != FeatureFactoryUsageByModel {
		t.Fatalf("announced %v", h.Features)
	}
	for _, bad := range []string{
		`{"factory":"","from":"2026-10-26","to":"2026-11-01"}`,
		`{"factory":"f","from":"2026-11-02","to":"2026-11-01"}`,
		`{"factory":"f","from":"2026-01-01","to":"2026-11-01"}`,
		`{"factory":"f","from":"26-10-26","to":"2026-11-01"}`,
		`{"factory":"f","from":"2026-10-26","to":"2026-11-01","job":-1}`,
	} {
		if _, err := methods[name].call(e, json.RawMessage(bad)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
}
