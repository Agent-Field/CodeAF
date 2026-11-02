package appwire

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── factory.estimate_new: what a job will cost before it exists ───────────
//
// New job's "Start now" shows what a job of this kind in this repository
// is likely to cost, measured from the latest finished jobs of the kind
// there (then in every repository), with the same basis words and the same
// five-job floor as a job's own figure. No model is asked.

// FeatureFactoryEstimateNew is announced in hello when the engine serves
// factory.estimate_new.
const FeatureFactoryEstimateNew = "factory.estimate_new"

// FactoryEstimateNewParams is one estimate before a job exists.
type FactoryEstimateNewParams struct {
	Factory string `json:"factory"`
	Repo    string `json:"repo"`
	Kind    string `json:"kind"`
}

// FactoryEstimatorNew is the engine's half of factory.estimate_new.
type FactoryEstimatorNew interface {
	FactoryEstimateNew(ctx context.Context, p FactoryEstimateNewParams) (api.NewEstimate, error)
}

func withFactoryEstimateNewFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryEstimatorNew); ok {
		hello = withFeature(hello, FeatureFactoryEstimateNew)
	}
	return hello
}

func init() {
	methods["factory.estimate_new"] = shaped[FactoryEstimateNewParams, api.NewEstimate](estimateNew)
}

func estimateNew(e Engine, raw json.RawMessage) (*api.NewEstimate, error) {
	est, ok := e.(FactoryEstimatorNew)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve factory.estimate_new")
	}
	var p FactoryEstimateNewParams
	if err := decodeStrict("factory.estimate_new", raw, &p); err != nil {
		return nil, err
	}
	var err error
	if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
		return nil, err
	}
	if p.Repo, err = FactoryRepoName(p.Repo); err != nil {
		return nil, err
	}
	if p.Kind = strings.TrimSpace(p.Kind); p.Kind == "" {
		return nil, Errorf(CodeBadParams, "kind is required")
	}
	out, err := est.FactoryEstimateNew(context.Background(), p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
