package appwire

import (
	"context"
	"encoding/json"
)

// FeatureFactoryAcceptStep advertises the person's accept of a stuck step.
const FeatureFactoryAcceptStep = "factory.accept_step"

// FactoryStepAccepter is separate so older engines do not advertise the door.
type FactoryStepAccepter interface {
	FactoryJobAcceptStep(context.Context, FactoryJobActionParams) (FactoryJobActed, error)
}

func withFactoryAcceptStepFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); ok {
		if _, ok := e.(FactoryStepAccepter); ok {
			return withFeature(hello, FeatureFactoryAcceptStep)
		}
	}
	return hello
}

func init() {
	methods["factory.job_accept_step"] = shaped[FactoryJobActionParams, FactoryJobActed](func(e Engine, raw json.RawMessage) (*FactoryJobActed, error) {
		a, ok := e.(FactoryStepAccepter)
		if _, factoryOK := e.(Factory); !ok || !factoryOK {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve factory.job_accept_step")
		}
		var p FactoryJobActionParams
		if err := decodeStrict("factory.job_accept_step", raw, &p); err != nil {
			return nil, err
		}
		if err := prepAction(&p); err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := a.FactoryJobAcceptStep(ctx, p)
		return &out, err
	})
}
