package appwire

import (
	"context"
	"encoding/json"
)

// FeatureFactoryJobPRReady announces the person's ready-for-review verb.
const FeatureFactoryJobPRReady = "factory.job_pr_ready"

// JobActPRReady names a successful ready-for-review action.
const JobActPRReady = "pr_ready"

// FactoryPRReadier marks a job's draft pull request ready once, as the caller.
type FactoryPRReadier interface {
	FactoryJobPRReady(context.Context, FactoryJobActionParams) (FactoryJobActed, error)
}

func withFactoryJobPRReadyFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryPRReadier); ok {
		hello = withFeature(hello, FeatureFactoryJobPRReady)
	}
	return hello
}

func init() {
	methods[FeatureFactoryJobPRReady] = shaped[FactoryJobActionParams, FactoryJobActed](prReady)
}

func prReady(e Engine, raw json.RawMessage) (*FactoryJobActed, error) {
	r, ok := e.(FactoryPRReadier)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", FeatureFactoryJobPRReady)
	}
	var p FactoryJobActionParams
	if err := decodeStrict(FeatureFactoryJobPRReady, raw, &p); err != nil {
		return nil, err
	}
	if err := checkJob(&p.Factory, p.Job, p.Expect); err != nil {
		return nil, err
	}
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := r.FactoryJobPRReady(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
