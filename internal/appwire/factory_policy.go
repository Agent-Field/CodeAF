package appwire

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

const FeatureFactoryGroupPolicy = "factory.group_policy"

type FactoryPolicy api.Policy

type FactoryPolicyGetParams struct {
	Factory string `json:"factory"`
	Group   string `json:"group"`
}

type FactoryPolicySetParams struct {
	Factory    string              `json:"factory"`
	Group      string              `json:"group"`
	Stopped    bool                `json:"stopped"`
	Allowances []factory.Allowance `json:"allowances"`
	Version    *int                `json:"version,omitempty"`
}

type FactoryPolicyKeeper interface {
	FactoryPolicyGet(context.Context, FactoryPolicyGetParams) (FactoryPolicy, error)
	FactoryPolicySet(context.Context, FactoryPolicySetParams) (FactoryPolicy, error)
}

func withFactoryGroupPolicyFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryPolicyKeeper); ok {
		return withFeature(hello, FeatureFactoryGroupPolicy)
	}
	return hello
}

// CheckPolicyGet is shared with direct backend callers.
func CheckPolicyGet(p FactoryPolicyGetParams) (FactoryPolicyGetParams, error) {
	var err error
	p.Factory, err = factoryID(strings.TrimSpace(p.Factory))
	if err != nil {
		return p, err
	}
	p.Group, err = factory.CheckGroup(p.Group)
	if err != nil {
		return p, Errorf(CodeBadParams, "%s", err)
	}
	if p.Group == "" {
		return p, Errorf(CodeBadParams, "a policy requires a non-empty group")
	}
	return p, nil
}

// CheckPolicySet refuses invalid replacements before any request is sent.
func CheckPolicySet(p FactoryPolicySetParams) (FactoryPolicySetParams, error) {
	target, err := CheckPolicyGet(FactoryPolicyGetParams{Factory: p.Factory, Group: p.Group})
	if err != nil {
		return p, err
	}
	p.Factory, p.Group = target.Factory, target.Group
	if p.Version != nil && *p.Version < 0 {
		return p, Errorf(CodeBadParams, "policy version must not be negative")
	}
	if err := (factory.GroupPolicy{Group: p.Group, Allowances: p.Allowances}).Check(); err != nil {
		return p, Errorf(CodeBadParams, "%s", err)
	}
	if p.Allowances == nil {
		p.Allowances = []factory.Allowance{}
	}
	return p, nil
}

func init() {
	methods["factory.policy_get"] = shaped[FactoryPolicyGetParams, FactoryPolicy](func(e Engine, raw json.RawMessage) (*FactoryPolicy, error) {
		keeper, ok := e.(FactoryPolicyKeeper)
		if _, serves := e.(Factory); !ok || !serves {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve factory.policy_get")
		}
		var p FactoryPolicyGetParams
		if err := decodeStrict("factory.policy_get", raw, &p); err != nil {
			return nil, err
		}
		p, err := CheckPolicyGet(p)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := keeper.FactoryPolicyGet(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
	methods["factory.policy_set"] = shaped[FactoryPolicySetParams, FactoryPolicy](func(e Engine, raw json.RawMessage) (*FactoryPolicy, error) {
		keeper, ok := e.(FactoryPolicyKeeper)
		if _, serves := e.(Factory); !ok || !serves {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve factory.policy_set")
		}
		var p FactoryPolicySetParams
		if err := decodeStrict("factory.policy_set", raw, &p); err != nil {
			return nil, err
		}
		p, err := CheckPolicySet(p)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := keeper.FactoryPolicySet(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}
