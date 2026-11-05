package appwire

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

type policyEngine struct {
	factoryEngine
	sent int
}

func (e *policyEngine) FactoryPolicyGet(_ context.Context, p FactoryPolicyGetParams) (FactoryPolicy, error) {
	e.sent++
	return FactoryPolicy{Group: p.Group}, nil
}
func (e *policyEngine) FactoryPolicySet(_ context.Context, p FactoryPolicySetParams) (FactoryPolicy, error) {
	e.sent++
	return FactoryPolicy{Group: p.Group, Stopped: p.Stopped}, nil
}

// P11, P12: policy methods register outside job actions and announce their feature.
func TestPolicyWire(t *testing.T) {
	e := &policyEngine{}
	for _, name := range []string{"factory.policy_get", "factory.policy_set"} {
		if _, ok := factoryActionMethods[name]; ok {
			t.Fatalf("job action %s", name)
		}
		if _, ok := methods[name]; !ok {
			t.Fatalf("missing %s", name)
		}
		if _, err := methods[name].call(&factoryEngine{}, json.RawMessage(`{}`)); wireCode(t, err) != CodeUnknownMethod {
			t.Fatal(err)
		}
	}
	if h := withFactoryGroupPolicyFeature(Hello{}, e); !slices.Contains(h.Features, "factory.group_policy") {
		t.Fatalf("hello = %+v", h)
	}
	if h := withFactoryGroupPolicyFeature(Hello{}, &factoryEngine{}); len(h.Features) != 0 {
		t.Fatal(h)
	}
	if !slices.Contains(helloFeatures(t, e), FeatureFactoryGroupPolicy) {
		t.Fatal("the server omitted group policy from hello")
	}
	defs := Schema()["$defs"].(map[string]any)
	props := defs["FactoryCapabilities"].(map[string]any)["properties"].(map[string]any)
	for _, flag := range []string{"group_policy", "create_group"} {
		if props[flag] == nil {
			t.Fatalf("schema omitted %s", flag)
		}
	}
	if _, err := methods["factory.policy_get"].call(e, json.RawMessage(`{"factory":" f ","group":" g "}`)); err != nil || e.sent != 1 {
		t.Fatalf("get = %v, %d", err, e.sent)
	}
	if _, err := methods["factory.policy_set"].call(e, json.RawMessage(`{"factory":"f","group":"g","stopped":false,"allowances":[],"version":0}`)); err != nil || e.sent != 2 {
		t.Fatalf("set = %v, %d", err, e.sent)
	}
}

// P11: every bound is checked before reaching the backend.
func TestPolicyWireBounds(t *testing.T) {
	valid := func() FactoryPolicySetParams {
		return FactoryPolicySetParams{Factory: "f", Group: "g", Allowances: []factory.Allowance{{ID: "m2", Jobs: []int{1}, CapUSD: 15, EndsAt: "2026-11-12T12:00:00Z"}}}
	}
	for _, change := range []func(*FactoryPolicySetParams){
		func(p *FactoryPolicySetParams) { p.Group = "" },
		func(p *FactoryPolicySetParams) { p.Group = strings.Repeat("x", 201) },
		func(p *FactoryPolicySetParams) { p.Group = "g\x00" },
		func(p *FactoryPolicySetParams) { p.Allowances = make([]factory.Allowance, 51) },
		func(p *FactoryPolicySetParams) { p.Allowances[0].ID = "" },
		func(p *FactoryPolicySetParams) { p.Allowances[0].ID = strings.Repeat("x", 65) },
		func(p *FactoryPolicySetParams) { p.Allowances[0].ID = "bad id" },
		func(p *FactoryPolicySetParams) { p.Allowances = append(p.Allowances, p.Allowances[0]) },
		func(p *FactoryPolicySetParams) { p.Allowances[0].Jobs = nil },
		func(p *FactoryPolicySetParams) { p.Allowances[0].Jobs = make([]int, 201) },
		func(p *FactoryPolicySetParams) { p.Allowances[0].Jobs = []int{0} },
		func(p *FactoryPolicySetParams) { p.Allowances[0].Jobs = []int{-1} },
		func(p *FactoryPolicySetParams) { p.Allowances[0].Jobs = []int{1, 1} },
		func(p *FactoryPolicySetParams) { p.Allowances[0].CapUSD = 0 },
		func(p *FactoryPolicySetParams) { p.Allowances[0].CapUSD = 10001 },
		func(p *FactoryPolicySetParams) { p.Allowances[0].EndsAt = "tomorrow" },
		func(p *FactoryPolicySetParams) { n := -1; p.Version = &n },
	} {
		p := valid()
		change(&p)
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		e := &policyEngine{}
		if _, err := methods["factory.policy_set"].call(e, raw); wireCode(t, err) != CodeBadParams || e.sent != 0 {
			t.Fatalf("accepted %s: %v, sends %d", raw, err, e.sent)
		}
	}
}

// P11, P12: create_group is a typed scalar and shares the service's bounds.
func TestPolicyCreateGroupWire(t *testing.T) {
	e := &actorEngine{}
	if _, err := methods["factory.job_create"].call(e, json.RawMessage(`{"factory":"f","repo":"owner/api","title":"job","group":" g "}`)); err != nil {
		t.Fatal(err)
	}
	if p, ok := e.got[0].(FactoryJobCreateParams); !ok || p.Group != "g" {
		t.Fatalf("create = %+v", e.got)
	}
	for _, group := range []string{strings.Repeat("x", 201), "g\x00"} {
		raw, _ := json.Marshal(map[string]any{"factory": "f", "repo": "owner/api", "title": "job", "group": group})
		if _, err := methods["factory.job_create"].call(e, raw); wireCode(t, err) != CodeBadParams || len(e.got) != 1 {
			t.Fatalf("invalid group = %v", err)
		}
	}
}
