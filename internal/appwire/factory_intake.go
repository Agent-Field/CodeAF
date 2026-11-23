package appwire

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory/inbox"
)

// FeatureFactoryIntake says the engine serves factory.intake_add.
const FeatureFactoryIntake = "factory.intake"

// FactoryIntakeParams is factory.intake_add: one [inbox.Entry], flat, with
// the factory it is filed on.
type FactoryIntakeParams struct {
	Factory string `json:"factory"`
	inbox.Entry
}

// FactoryIntaker is the engine's half of factory.intake_add. It files the
// entry on the factory this engine serves, through [inbox.Add] in its own
// process (there is no HTTP intake), and answers what was done: the job's id, and whether it is new.
type FactoryIntaker interface {
	FactoryIntakeAdd(ctx context.Context, p FactoryIntakeParams) (inbox.Result, error)
}

func withFactoryIntakeFeature(hello Hello, e Engine) Hello {
	if _, ok := factoryIntakerOf(e); ok {
		hello = withFeature(hello, FeatureFactoryIntake)
	}
	return hello
}

// factoryIntakerOf is e's intake, when e serves the factory at all.
func factoryIntakerOf(e Engine) (FactoryIntaker, bool) {
	in, ok := e.(FactoryIntaker)
	_, isFactory := e.(Factory)
	return in, ok && isFactory
}

// prepIntake checks the params with the inbox's own check, the one every
// door shares, so the wire refuses in the same sentence the factory would.
func prepIntake(p *FactoryIntakeParams) error {
	id, err := factoryID(strings.TrimSpace(p.Factory))
	if err != nil {
		return err
	}
	p.Factory = id
	if p.Entry, err = inbox.Validate(p.Entry); err != nil {
		return Errorf(CodeBadParams, "%s", err.Error())
	}
	return nil
}

func init() {
	methods["factory.intake_add"] = shaped[FactoryIntakeParams, inbox.Result](func(e Engine, raw json.RawMessage) (*inbox.Result, error) {
		in, ok := factoryIntakerOf(e)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve factory.intake_add")
		}
		var p FactoryIntakeParams
		if err := decodeStrict("factory.intake_add", raw, &p); err != nil {
			return nil, err
		}
		if err := prepIntake(&p); err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := in.FactoryIntakeAdd(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}
