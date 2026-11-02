package appwire

import (
	"context"
	"encoding/json"
	"strings"
)

// ── factory.read_on_arrival: Read new issues as they arrive ────────────────
//
// The factory's own switch for the AI that reads by itself: the arrival read
// of each new issue (its rubric and judging) and the look closer before an
// automatic start. On until the operator turns it off. A person's Look closer
// works either way. Without `on` the method answers the setting; with it the
// factory changes it (an operator's write) and answers the new one.

// FeatureFactoryReadOnArrival is announced in hello when the engine serves
// factory.read_on_arrival.
const FeatureFactoryReadOnArrival = "factory.read_on_arrival"

// FactoryReadOnArrivalParams reads the setting, or sets it when On is given.
type FactoryReadOnArrivalParams struct {
	Factory string `json:"factory"`
	On      *bool  `json:"on,omitempty"`
}

// FactoryReadOnArrival is the factory's setting.
type FactoryReadOnArrival struct {
	On bool `json:"on"`
}

// FactoryArrivalReader is the engine's half of factory.read_on_arrival.
type FactoryArrivalReader interface {
	FactoryReadOnArrival(ctx context.Context, p FactoryReadOnArrivalParams) (FactoryReadOnArrival, error)
}

func withFactoryReadOnArrivalFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryArrivalReader); ok {
		hello = withFeature(hello, FeatureFactoryReadOnArrival)
	}
	return hello
}

func init() {
	methods["factory.read_on_arrival"] = shaped[FactoryReadOnArrivalParams, FactoryReadOnArrival](readOnArrival)
}

func readOnArrival(e Engine, raw json.RawMessage) (*FactoryReadOnArrival, error) {
	a, ok := e.(FactoryArrivalReader)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve factory.read_on_arrival")
	}
	var p FactoryReadOnArrivalParams
	if err := decodeStrict("factory.read_on_arrival", raw, &p); err != nil {
		return nil, err
	}
	var err error
	if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
		return nil, err
	}
	out, err := a.FactoryReadOnArrival(context.Background(), p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
