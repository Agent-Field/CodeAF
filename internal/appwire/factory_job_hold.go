package appwire

import (
	"context"
	"encoding/json"
)

// FeatureFactoryJobHold is factory.job_hold and factory.job_resume: a person
// keeps a queued job from starting, then lets it go on. The job's own
// can.hold and can.resume say when each applies.
const FeatureFactoryJobHold = "factory.job_hold"

// The actions a [FactoryJobActed] from a hold names.
const (
	JobActHold   = "hold"
	JobActResume = "resume"
)

// FactoryJobHolder is the engine's half of factory.job_hold and
// factory.job_resume. Like [FactoryActor], each method makes at most one
// write to the factory and never sends it twice.
type FactoryJobHolder interface {
	FactoryJobHold(ctx context.Context, p FactoryJobActionParams) (FactoryJobActed, error)
	FactoryJobResume(ctx context.Context, p FactoryJobActionParams) (FactoryJobActed, error)
}

func init() {
	methods["factory.job_hold"] = holdMethod("factory.job_hold", FactoryJobHolder.FactoryJobHold)
	methods["factory.job_resume"] = holdMethod("factory.job_resume", FactoryJobHolder.FactoryJobResume)
}

// holdMethod reads params STRICTLY, checks them, then asks the engine once.
func holdMethod(name string, call func(FactoryJobHolder, context.Context, FactoryJobActionParams) (FactoryJobActed, error)) method {
	return shaped[FactoryJobActionParams, FactoryJobActed](func(e Engine, raw json.RawMessage) (*FactoryJobActed, error) {
		h, ok := e.(FactoryJobHolder)
		if _, isFactory := e.(Factory); !ok || !isFactory {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
		}
		var p FactoryJobActionParams
		if err := decodeStrict(name, raw, &p); err != nil {
			return nil, err
		}
		if err := prepAction(&p); err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := call(h, ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}
