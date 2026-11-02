package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── factory.scout: Look closer on one job ──────────────────────────────────
//
// A model with read-only tools on the factory's own checkout reads one job's
// issue again and answers the files it expects to change, a three-step plan,
// what the issue does not say, the complexity rubric with file:line evidence,
// and the tests that would prove it (internal/factory/scout). Every path in
// the answer exists at the head it read. The same issue text at the same head
// answers the kept look, free (cached); refresh asks for a new one. It is
// refused at the month's AI limit or the repository's daily scout limit, in
// those words (CodeLimit is not a code here: the factory's refusal is carried).

// FeatureFactoryScout is announced in hello when the engine serves
// factory.scout.
const FeatureFactoryScout = "factory.scout"

// scoutCallBudget is a look's own minute and the answer's margin.
const scoutCallBudget = 90 * time.Second

// FactoryScoutParams is one Look closer.
type FactoryScoutParams struct {
	Factory string `json:"factory"`
	Job     int    `json:"job"`
	Refresh bool   `json:"refresh,omitempty"`
}

// FactoryScouter is the engine's half of factory.scout.
type FactoryScouter interface {
	FactoryScout(ctx context.Context, p FactoryScoutParams) (api.ScoutLook, error)
}

func withFactoryScoutFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryScouter); ok {
		hello = withFeature(hello, FeatureFactoryScout)
	}
	return hello
}

func init() {
	methods["factory.scout"] = shaped[FactoryScoutParams, api.ScoutLook](postScout)
}

func postScout(e Engine, raw json.RawMessage) (*api.ScoutLook, error) {
	s, ok := e.(FactoryScouter)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve factory.scout")
	}
	var p FactoryScoutParams
	if err := decodeStrict("factory.scout", raw, &p); err != nil {
		return nil, err
	}
	var err error
	if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
		return nil, err
	}
	if p.Job < 1 {
		return nil, Errorf(CodeBadParams, "job must be a whole number from 1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), scoutCallBudget)
	defer cancel()
	out, err := s.FactoryScout(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
