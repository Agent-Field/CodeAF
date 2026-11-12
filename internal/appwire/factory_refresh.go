package appwire

import (
	"context"
	"encoding/json"
)

// ── factory.refresh: Check GitHub now ─────────────────────────────────────
//
// A person asks a factory to read GitHub now rather than on its next tick.
// The factory wakes its poll and answers at once; it does not wait for the
// read. What the read finds arrives the way every read does, on the next
// factory.queue: the github row of `desk.sources` says `polling` while it
// reads, then `polled` (when GitHub was last read well) or `trouble`.

// FeatureFactoryRefresh is announced in hello when the engine serves
// factory.refresh.
const FeatureFactoryRefresh = "factory.refresh"

// FactoryRefreshParams names one connected factory.
type FactoryRefreshParams struct {
	Factory string `json:"factory"`
}

// FactoryRefresher is the engine's half of factory.refresh. It makes one
// request of the factory and never waits for the read it starts.
type FactoryRefresher interface {
	FactoryRefresh(ctx context.Context, id string) error
}

func withFactoryRefreshFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryRefresher); ok {
		hello = withFeature(hello, FeatureFactoryRefresh)
	}
	return hello
}

func init() {
	methods["factory.refresh"] = shaped[FactoryRefreshParams, Empty](factoryRefresh)
}

func factoryRefresh(e Engine, raw json.RawMessage) (*Empty, error) {
	const name = "factory.refresh"
	r, ok := e.(FactoryRefresher)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	var p FactoryRefreshParams
	if err := decodeStrict(name, raw, &p); err != nil {
		return nil, err
	}
	id, err := factoryID(p.Factory)
	if err != nil {
		return nil, err
	}
	ctx, cancel := factoryCall()
	defer cancel()
	return done(r.FactoryRefresh(ctx, id))
}
