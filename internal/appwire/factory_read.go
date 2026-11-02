package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── factory.read: a person's Read of chosen jobs ───────────────────────────
//
// The arrival read (complexity rubric and judging) of exactly the jobs the
// person chose, now, whether or not the factory reads new issues as they
// arrive: Backlog's "Read N new issues" sends those N, a row's Read sends
// one. At most 20 are read per call; the rest come back skipped.

// FeatureFactoryRead is announced in hello when the engine serves
// factory.read.
const FeatureFactoryRead = "factory.read"

// readCallBudget is twenty reads one after another.
const readCallBudget = 3 * time.Minute

// FactoryReadParams is one Read.
type FactoryReadParams struct {
	Factory string `json:"factory"`
	Jobs    []int  `json:"jobs"`
}

// FactoryReader is the engine's half of factory.read.
type FactoryReader interface {
	FactoryRead(ctx context.Context, p FactoryReadParams) (api.ReadResult, error)
}

func withFactoryReadFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryReader); ok {
		hello = withFeature(hello, FeatureFactoryRead)
	}
	return hello
}

func init() {
	methods["factory.read"] = shaped[FactoryReadParams, api.ReadResult](readJobs)
}

func readJobs(e Engine, raw json.RawMessage) (*api.ReadResult, error) {
	rd, ok := e.(FactoryReader)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve factory.read")
	}
	var p FactoryReadParams
	if err := decodeStrict("factory.read", raw, &p); err != nil {
		return nil, err
	}
	var err error
	if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
		return nil, err
	}
	if len(p.Jobs) == 0 {
		return nil, Errorf(CodeBadParams, "choose at least one job to read")
	}
	for _, id := range p.Jobs {
		if id < 1 {
			return nil, Errorf(CodeBadParams, "jobs are whole numbers from 1")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), readCallBudget)
	defer cancel()
	out, err := rd.FactoryRead(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
