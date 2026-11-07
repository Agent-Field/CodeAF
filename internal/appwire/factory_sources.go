package appwire

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/trigger"
)

// FeatureFactorySources says the engine serves factory.source_add,
// factory.source_list and factory.source_revoke: the Trigger API's registered
// sources, the same doors as `codeaf factory source add|list|revoke`.
const FeatureFactorySources = "factory.sources"

// FactorySourceAddParams is factory.source_add: register name on the factory,
// with the address its webhooks go to and the most its work is trusted.
type FactorySourceAddParams struct {
	Factory  string       `json:"factory"`
	Name     string       `json:"name"`
	Callback string       `json:"callback,omitempty"`
	Trust    factory.Tier `json:"trust,omitempty"`
}

// FactorySourceParams names one source on one factory (factory.source_revoke).
type FactorySourceParams struct {
	Factory string `json:"factory"`
	Name    string `json:"name"`
}

// FactorySources is factory.source_list: every source, never a secret.
type FactorySources struct {
	Sources []trigger.Public `json:"sources"`
}

// FactorySourceRevoked is factory.source_revoke's reply.
type FactorySourceRevoked struct {
	Revoked string `json:"revoked"`
}

// FactorySourcer is the engine's half of the three methods. Each goes to the
// named factory's /v1/sources, which only its operator may call.
type FactorySourcer interface {
	FactorySourceAdd(ctx context.Context, p FactorySourceAddParams) (trigger.Minted, error)
	FactorySourceList(ctx context.Context, p FactoryRef) (FactorySources, error)
	FactorySourceRevoke(ctx context.Context, p FactorySourceParams) (FactorySourceRevoked, error)
}

func withFactorySourcesFeature(hello Hello, e Engine) Hello {
	if _, ok := factorySourcerOf(e); ok {
		hello = withFeature(hello, FeatureFactorySources)
	}
	return hello
}

func factorySourcerOf(e Engine) (FactorySourcer, bool) {
	s, ok := e.(FactorySourcer)
	_, isFactory := e.(Factory)
	return s, ok && isFactory
}

// prepSource checks the factory and the name with the registry's own checks,
// so the wire refuses in the sentence the factory would.
func prepSource(id *string, name *string) error {
	clean, err := factoryID(strings.TrimSpace(*id))
	if err != nil {
		return err
	}
	*id = clean
	*name = strings.TrimSpace(*name)
	if err := trigger.CheckName(*name); err != nil {
		return Errorf(CodeBadParams, "%s", err.Error())
	}
	return nil
}

func init() {
	methods["factory.source_add"] = shaped[FactorySourceAddParams, trigger.Minted](func(e Engine, raw json.RawMessage) (*trigger.Minted, error) {
		s, ok := factorySourcerOf(e)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve factory.source_add")
		}
		var p FactorySourceAddParams
		if err := decodeStrict("factory.source_add", raw, &p); err != nil {
			return nil, err
		}
		if err := prepSource(&p.Factory, &p.Name); err != nil {
			return nil, err
		}
		var err error
		if p.Trust, err = trigger.CheckTrust(p.Trust); err != nil {
			return nil, Errorf(CodeBadParams, "%s", err.Error())
		}
		if p.Callback, err = trigger.CheckCallback(p.Callback); err != nil {
			return nil, Errorf(CodeBadParams, "%s", err.Error())
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := s.FactorySourceAdd(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
	methods["factory.source_list"] = shaped[FactoryRef, FactorySources](func(e Engine, raw json.RawMessage) (*FactorySources, error) {
		s, ok := factorySourcerOf(e)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve factory.source_list")
		}
		var p FactoryRef
		if err := decodeStrict("factory.source_list", raw, &p); err != nil {
			return nil, err
		}
		id, err := factoryID(strings.TrimSpace(p.Factory))
		if err != nil {
			return nil, err
		}
		p.Factory = id
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := s.FactorySourceList(ctx, p)
		if err != nil {
			return nil, err
		}
		if out.Sources == nil {
			out.Sources = []trigger.Public{}
		}
		return &out, nil
	})
	methods["factory.source_revoke"] = shaped[FactorySourceParams, FactorySourceRevoked](func(e Engine, raw json.RawMessage) (*FactorySourceRevoked, error) {
		s, ok := factorySourcerOf(e)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve factory.source_revoke")
		}
		var p FactorySourceParams
		if err := decodeStrict("factory.source_revoke", raw, &p); err != nil {
			return nil, err
		}
		if err := prepSource(&p.Factory, &p.Name); err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := s.FactorySourceRevoke(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}
