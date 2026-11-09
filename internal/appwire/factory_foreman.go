package appwire

import (
	"context"
	"encoding/json"
)

// ── factory.foreman_open: the factory's own manager, as a conversation ────
//
// A job has a manager (factory.job_chats names it); the factory as a whole has
// one too, the foreman: ONE conversation per factory home, which reads every
// job (what is queued, running, waiting on the person, shipped today), starts
// or prioritises a job when the person asks, and sums up shipped work. Until
// this method nothing could open it (the 2026-11-06 audit found its door hung
// and never called).
//
// The read answers the foreman's agent id, making the conversation the first
// time and the same one every time after; the app opens it as it opens any
// conversation. IT IS THIS MACHINE'S FACTORY ONLY, for the reason
// factory.job_chats is: the foreman reads the floor of the factory this engine
// serves, its own home and no other, and any other factory is refused.

// FeatureFactoryForemanOpen is announced in hello when the engine serves
// factory.foreman_open.
const FeatureFactoryForemanOpen = "factory.foreman_open"

// FactoryForeman is the factory's foreman conversation: Agent is an agent id
// the app opens like any conversation.
type FactoryForeman struct {
	Factory string `json:"factory"`
	Agent   string `json:"agent"`
}

// FactoryForemanOpener is the engine's half of factory.foreman_open.
type FactoryForemanOpener interface {
	FactoryForemanOpen(ctx context.Context, p FactoryRef) (FactoryForeman, error)
}

func withFactoryForemanOpenFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryForemanOpener); ok {
		hello = withFeature(hello, FeatureFactoryForemanOpen)
	}
	return hello
}

func init() {
	methods[FeatureFactoryForemanOpen] = shaped[FactoryRef, FactoryForeman](foremanOpen)
}

func foremanOpen(e Engine, raw json.RawMessage) (*FactoryForeman, error) {
	r, ok := e.(FactoryForemanOpener)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", FeatureFactoryForemanOpen)
	}
	var p FactoryRef
	if err := decodeStrict(FeatureFactoryForemanOpen, raw, &p); err != nil {
		return nil, err
	}
	id, err := factoryID(p.Factory)
	if err != nil {
		return nil, err
	}
	p.Factory = id
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := r.FactoryForemanOpen(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
