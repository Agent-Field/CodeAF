package appwire

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// FeatureFactoryWaitsSet announces the engine's typed waits door.
const FeatureFactoryWaitsSet = "factory.waits_set"

// FeatureFactoryFeatures announces discovery of a connected factory's features.
const FeatureFactoryFeatures = "factory.features"

// FactoryWaitsSetParams replaces one unstarted job's parents; an empty list clears them.
type FactoryWaitsSetParams struct {
	Factory string      `json:"factory"`
	Job     int         `json:"job"`
	WaitsOn []int       `json:"waits_on"`
	Expect  *api.Expect `json:"expect,omitempty"`
}

// FactoryWaitsSet includes one read after the accepted change, when available.
type FactoryWaitsSet struct {
	Factory string   `json:"factory"`
	Job     int      `json:"job"`
	WaitsOn []int    `json:"waits_on"`
	Current *api.Job `json:"current,omitempty"`
}

// FactoryWaitsSetter is optional on top of Factory and sends each change once.
type FactoryWaitsSetter interface {
	FactoryWaitsSet(context.Context, FactoryWaitsSetParams) (FactoryWaitsSet, error)
}

// FactoryFeaturesParams names the factory whose own features are read.
type FactoryFeaturesParams struct {
	Factory string `json:"factory"`
}

// FactoryFeatures is what the connected factory serves, independent of this engine.
type FactoryFeatures struct {
	Features []string `json:"features"`
}

// FactoryFeaturesReader is the engine's half of per-factory discovery.
type FactoryFeaturesReader interface {
	FactoryFeatures(context.Context, FactoryFeaturesParams) (FactoryFeatures, error)
}

func withFactoryWaitsSetFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryWaitsSetter); ok {
		return withFeature(hello, FeatureFactoryWaitsSet)
	}
	return hello
}

func withFactoryFeaturesFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryFeaturesReader); ok {
		return withFeature(hello, FeatureFactoryFeatures)
	}
	return hello
}

// CheckWaitsSet is shared with the backend so invalid requests cannot be sent.
func CheckWaitsSet(p FactoryWaitsSetParams) (FactoryWaitsSetParams, error) {
	if err := checkJobTarget(&p.Factory, p.Job); err != nil {
		return p, err
	}
	if p.WaitsOn == nil {
		return p, Errorf(CodeBadParams, "waits_on must be a list of job ids; use an empty list to clear it")
	}
	if err := factory.CheckWaitsIDs(p.WaitsOn); err != nil {
		return p, Errorf(CodeBadParams, "%s", err)
	}
	if exp := p.Expect; exp != nil {
		if exp.Version != nil && *exp.Version < 1 {
			return p, Errorf(CodeBadParams, "expect.version must be a positive whole number")
		}
		if len(exp.State) > 64 || len(exp.Step) > MaxFactoryStepName {
			return p, Errorf(CodeBadParams, "expect.state and expect.step are too long")
		}
	}
	return p, nil
}

func init() {
	methods[FeatureFactoryWaitsSet] = shaped[FactoryWaitsSetParams, FactoryWaitsSet](func(e Engine, raw json.RawMessage) (*FactoryWaitsSet, error) {
		r, ok := e.(FactoryWaitsSetter)
		if _, isFactory := e.(Factory); !ok || !isFactory {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", FeatureFactoryWaitsSet)
		}
		var p FactoryWaitsSetParams
		if err := decodeStrict(FeatureFactoryWaitsSet, raw, &p); err != nil {
			return nil, err
		}
		p, err := CheckWaitsSet(p)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := r.FactoryWaitsSet(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
	methods[FeatureFactoryFeatures] = shaped[FactoryFeaturesParams, FactoryFeatures](func(e Engine, raw json.RawMessage) (*FactoryFeatures, error) {
		r, ok := e.(FactoryFeaturesReader)
		if _, isFactory := e.(Factory); !ok || !isFactory {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", FeatureFactoryFeatures)
		}
		var p FactoryFeaturesParams
		if err := decodeStrict(FeatureFactoryFeatures, raw, &p); err != nil {
			return nil, err
		}
		var err error
		p.Factory, err = factoryID(strings.TrimSpace(p.Factory))
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := r.FactoryFeatures(ctx, p)
		if err != nil {
			return nil, err
		}
		if out.Features == nil {
			out.Features = []string{}
		}
		return &out, nil
	})
}
