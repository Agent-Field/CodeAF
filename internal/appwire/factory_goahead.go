package appwire

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── GO-AHEADS THE FACTORY FOLLOWS ──────────────────────────────────────────
//
// A group policy is a person's standing yes for one group of jobs: a list of
// allowances, each naming the jobs it covers, a dollar cap and an end
// ([factory.GroupPolicy]). factory.policy_get reads one group's policy and
// factory.policy_set replaces it whole; factory.waits_set sets the jobs one job
// waits on; factory.features says which of these one attached factory keeps.
// THE FACTORY FOLLOWS A POLICY BY ITSELF: a covered job that never ran starts
// when what it waits on has merged, inside its allowance's cap and before its
// end, with no app attached. Stopping the policy, dropping an allowance or
// passing its end starts nothing further.
//
// An engine that serves them announces factory.group_policy and
// factory.waits_set in hello and says group_policy and waits_on in
// factory.capabilities. Whether ONE factory keeps them is its own
// factory.features answer: an older factory answers none, and then these
// methods are refused by it rather than half-done.

// The hello words.
const (
	FeatureFactoryGroupPolicy = "factory.group_policy"
	FeatureFactoryWaitsSet    = "factory.waits_set"
)

// FactoryFeatures is factory.features's answer: what one attached factory does
// beyond its contract, as words ("group_policy", "waits_on"). An older factory
// answers an empty list.
type FactoryFeatures struct {
	Features []string `json:"features"`
}

// FactoryPolicyGetParams names one group's policy on one factory.
type FactoryPolicyGetParams struct {
	Factory string `json:"factory"`
	Group   string `json:"group"`
}

// FactoryPolicySetParams is one group's whole policy as it is sent: what
// replaces it, never a change to merge in.
type FactoryPolicySetParams struct {
	Factory    string              `json:"factory"`
	Group      string              `json:"group"`
	Stopped    bool                `json:"stopped"`
	Allowances []factory.Allowance `json:"allowances"`
}

// FactoryWaitsSetParams sets the jobs one job waits on: the whole list, [] for
// none. expect is optional and, sent, refuses a job that moved since it was read.
type FactoryWaitsSetParams struct {
	Factory string      `json:"factory"`
	Job     int         `json:"job"`
	WaitsOn []int       `json:"waits_on"`
	Expect  *api.Expect `json:"expect,omitempty"`
}

// FactoryWaitsSetResult is factory.waits_set's answer: the list kept and the
// job as it is now.
type FactoryWaitsSetResult struct {
	Factory string   `json:"factory"`
	Job     int      `json:"job"`
	WaitsOn []int    `json:"waits_on"`
	Current *api.Job `json:"current,omitempty"`
}

// FactoryGoAheads is the engine's half of the four methods. Each write is sent
// once.
type FactoryGoAheads interface {
	FactoryFeatures(ctx context.Context, id string) (FactoryFeatures, error)
	FactoryPolicyGet(ctx context.Context, p FactoryPolicyGetParams) (api.Policy, error)
	FactoryPolicySet(ctx context.Context, p FactoryPolicySetParams) (api.Policy, error)
	FactoryWaitsSet(ctx context.Context, p FactoryWaitsSetParams) (FactoryWaitsSetResult, error)
}

func withFactoryGoAheadFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryGoAheads); ok {
		hello = withFeature(hello, FeatureFactoryGroupPolicy)
		hello = withFeature(hello, FeatureFactoryWaitsSet)
	}
	return hello
}

func goAheadsOf(e Engine, name string) (FactoryGoAheads, error) {
	g, ok := e.(FactoryGoAheads)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return g, nil
}

// policyTarget trims and checks a policy's factory and group.
func policyTarget(id, group *string) error {
	f, err := factoryID(strings.TrimSpace(*id))
	if err != nil {
		return err
	}
	g, err := factory.CleanPolicyGroup(*group)
	if err != nil {
		return Errorf(CodeBadParams, "%s", err.Error())
	}
	*id, *group = f, g
	return nil
}

func init() {
	methods["factory.features"] = typed(func(e Engine, p FactoryRef) (*FactoryFeatures, error) {
		g, err := goAheadsOf(e, "factory.features")
		if err != nil {
			return nil, err
		}
		id, err := factoryID(strings.TrimSpace(p.Factory))
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := g.FactoryFeatures(ctx, id)
		if err != nil {
			return nil, err
		}
		if out.Features == nil {
			out.Features = []string{}
		}
		return &out, nil
	})
	methods["factory.policy_get"] = shaped[FactoryPolicyGetParams, api.Policy](func(e Engine, raw json.RawMessage) (*api.Policy, error) {
		g, err := goAheadsOf(e, "factory.policy_get")
		if err != nil {
			return nil, err
		}
		var p FactoryPolicyGetParams
		if err := decodeStrict("factory.policy_get", raw, &p); err != nil {
			return nil, err
		}
		if err := policyTarget(&p.Factory, &p.Group); err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := g.FactoryPolicyGet(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
	methods["factory.policy_set"] = shaped[FactoryPolicySetParams, api.Policy](func(e Engine, raw json.RawMessage) (*api.Policy, error) {
		g, err := goAheadsOf(e, "factory.policy_set")
		if err != nil {
			return nil, err
		}
		var p FactoryPolicySetParams
		if err := decodeStrict("factory.policy_set", raw, &p); err != nil {
			return nil, err
		}
		if err := policyTarget(&p.Factory, &p.Group); err != nil {
			return nil, err
		}
		if p.Allowances == nil {
			p.Allowances = []factory.Allowance{}
		}
		if err := factory.CheckAllowances(p.Allowances); err != nil {
			return nil, Errorf(CodeBadParams, "%s", err.Error())
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := g.FactoryPolicySet(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
	methods["factory.waits_set"] = shaped[FactoryWaitsSetParams, FactoryWaitsSetResult](func(e Engine, raw json.RawMessage) (*FactoryWaitsSetResult, error) {
		g, err := goAheadsOf(e, "factory.waits_set")
		if err != nil {
			return nil, err
		}
		var p FactoryWaitsSetParams
		if err := decodeStrict("factory.waits_set", raw, &p); err != nil {
			return nil, err
		}
		if err := checkJobTarget(&p.Factory, p.Job); err != nil {
			return nil, err
		}
		if p.WaitsOn == nil {
			p.WaitsOn = []int{}
		}
		if err := factory.CheckWaitsOn(p.Job, p.WaitsOn); err != nil {
			return nil, Errorf(CodeBadParams, "%s", err.Error())
		}
		if x := p.Expect; x != nil && (len(x.State) > 64 || len(x.Step) > MaxFactoryStepName) {
			return nil, Errorf(CodeBadParams, "expect.state and expect.step are too long")
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := g.FactoryWaitsSet(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}
