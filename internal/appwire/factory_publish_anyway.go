package appwire

import (
	"context"
	"encoding/json"
)

// FeatureFactoryPublishAnyway advertises the person-only job action.
const FeatureFactoryPublishAnyway = "factory.publish_anyway"

// FactoryPublishAnywayer is separate so older engines do not advertise the door.
type FactoryPublishAnywayer interface {
	FactoryJobPublishAnyway(context.Context, FactoryJobActionParams) (FactoryJobActed, error)
}

func withFactoryPublishAnywayFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); ok {
		if _, ok := e.(FactoryPublishAnywayer); ok {
			return withFeature(hello, FeatureFactoryPublishAnyway)
		}
	}
	return hello
}

func init() {
	methods["factory.job_publish_anyway"] = shaped[FactoryJobActionParams, FactoryJobActed](func(e Engine, raw json.RawMessage) (*FactoryJobActed, error) {
		a, ok := e.(FactoryPublishAnywayer)
		if _, factoryOK := e.(Factory); !ok || !factoryOK {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve factory.job_publish_anyway")
		}
		var p FactoryJobActionParams
		if err := decodeStrict("factory.job_publish_anyway", raw, &p); err != nil {
			return nil, err
		}
		if err := prepAction(&p); err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := a.FactoryJobPublishAnyway(ctx, p)
		return &out, err
	})
}
