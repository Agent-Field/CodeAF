package appwire

import (
	"context"
	"encoding/json"
)

const FeatureFactoryJobClose = "factory.job_close"
const JobActClose = "close"
const JobActReopen = "reopen"

type FactoryCloser interface {
	FactoryJobClose(context.Context, FactoryJobActionParams) (FactoryJobActed, error)
	FactoryJobReopen(context.Context, FactoryJobActionParams) (FactoryJobActed, error)
}

func init() {
	for name, call := range map[string]func(FactoryCloser, context.Context, FactoryJobActionParams) (FactoryJobActed, error){"factory.job_close": FactoryCloser.FactoryJobClose, "factory.job_reopen": FactoryCloser.FactoryJobReopen} {
		methods[name] = shaped[FactoryJobActionParams, FactoryJobActed](func(e Engine, raw json.RawMessage) (*FactoryJobActed, error) {
			actor, ok := e.(FactoryCloser)
			if !ok {
				return nil, Errorf(CodeUnknownMethod, "factory.job_close is unavailable")
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
			out, err := call(actor, ctx, p)
			if err != nil {
				return nil, err
			}
			return &out, nil
		})
	}

}
