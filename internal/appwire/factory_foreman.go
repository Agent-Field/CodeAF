package appwire

import (
	"context"
	"encoding/json"
	"reflect"
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

// FeatureFactoryForemanFresh is announced in hello when the engine serves
// factory.foreman_fresh.
//
// factory.foreman_fresh {factory} STARTS THE MANAGER OVER: a new, empty
// conversation takes its place and its agent id is answered, as an open
// answers it; the one it replaces is stopped and archived, its history kept.
// It is the app's Clear, and the reason a manager has one where every other
// conversation has Archive: the manager is the factory's one conversation
// with one door, so putting it away left that door opening a closed chat,
// while what a person wanted was an empty one.
const FeatureFactoryForemanFresh = "factory.foreman_fresh"

// FactoryForemanFresher is the engine's half of factory.foreman_fresh.
type FactoryForemanFresher interface {
	FactoryForemanFresh(ctx context.Context, p FactoryRef) (FactoryForeman, error)
}

func withFactoryForemanFreshFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryForemanFresher); ok {
		hello = withFeature(hello, FeatureFactoryForemanFresh)
	}
	return hello
}

// EventFactoryForemanChanged says the engine REPLACED a factory's manager: the
// conversation Replaced is the manager no longer, and Agent is. It is sent
// when the manager is started over (factory.foreman_fresh), and when one
// working in a folder the factory refuses is made again, which happens as the
// factory starts with nobody asking. Its data is [FactoryForemanChanged].
//
// IT IS HOW A WINDOW HOLDING THE OLD CONVERSATION LEARNS IT IS HOLDING THE
// WRONG ONE. A tab the app brought back still showed the replaced manager
// under its name, with a box to write in; what was written there reached a
// conversation that could no longer read the factory, while the manager's own
// button opened another. The app swaps the tab for the manager on this event.
const EventFactoryForemanChanged = "factory.foreman_changed"

// FactoryForemanChanged names the manager a factory has now and the
// conversation it replaced. Factory is the id factory.foreman_open takes.
type FactoryForemanChanged struct {
	Factory  string `json:"factory"`
	Agent    string `json:"agent"`
	Replaced string `json:"replaced"`
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
	methods[FeatureFactoryForemanFresh] = shaped[FactoryRef, FactoryForeman](foremanFreshCall)
	eventShapes[EventFactoryForemanChanged] = reflect.TypeFor[FactoryForemanChanged]()
}

func foremanFreshCall(e Engine, raw json.RawMessage) (*FactoryForeman, error) {
	r, ok := e.(FactoryForemanFresher)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", FeatureFactoryForemanFresh)
	}
	var p FactoryRef
	if err := decodeStrict(FeatureFactoryForemanFresh, raw, &p); err != nil {
		return nil, err
	}
	id, err := factoryID(p.Factory)
	if err != nil {
		return nil, err
	}
	p.Factory = id
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := r.FactoryForemanFresh(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
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
