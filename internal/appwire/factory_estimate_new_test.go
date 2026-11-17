package appwire

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

type estimateEngine struct {
	factoryEngine
	got []FactoryEstimateNewParams
}

func (e *estimateEngine) FactoryEstimateNew(_ context.Context, p FactoryEstimateNewParams) (api.NewEstimate, error) {
	e.got = append(e.got, p)
	return api.NewEstimate{Repo: p.Repo, Kind: p.Kind, Estimate: &api.Estimate{USDLo: 0.2, USDHi: 1.1, N: 7, Repo: p.Repo, Basis: "kind"}}, nil
}

// New job's "Start now" asks what a job of a kind in a repository costs
// before it exists: one method, announced only by an engine that serves it.
func TestANewJobsEstimateIsRegisteredAndAnnounced(t *testing.T) {
	const name = "factory.estimate_new"
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}} {
		if _, err := methods[name].call(e, json.RawMessage(`{"factory":"f","repo":"acme/api","kind":"issue"}`)); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if h := withFactoryEstimateNewFeature(Hello{}, e); len(h.Features) != 0 {
			t.Errorf("%T announced %v", e, h.Features)
		}
	}
	e := &estimateEngine{}
	if h := withFactoryEstimateNewFeature(Hello{}, e); len(h.Features) != 1 || h.Features[0] != FeatureFactoryEstimateNew {
		t.Fatalf("announced %v", h.Features)
	}
	out, err := methods[name].call(e, json.RawMessage(`{"factory":"factory_local","repo":" acme/api ","kind":"issue"}`))
	got, _ := json.Marshal(out)
	if err != nil || len(e.got) != 1 || e.got[0].Repo != "acme/api" ||
		string(got) != `{"factory":{"name":"","address":"","version":""},"repo":"acme/api","kind":"issue","estimate":{"usd_lo":0.2,"usd_hi":1.1,"min_lo":0,"min_hi":0,"n":7,"repo":"acme/api","basis":"kind"}}` {
		t.Fatalf("answered %s, %v (%+v)", got, err, e.got)
	}
	for _, bad := range []string{`{"factory":"","repo":"acme/api","kind":"issue"}`, `{"factory":"f","repo":"acme","kind":"issue"}`,
		`{"factory":"f","repo":"acme/api","kind":""}`, `{"factory":"f","repo":"acme/api","kind":"issue","x":1}`} {
		if _, err := methods[name].call(e, json.RawMessage(bad)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
}
