package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── factory.spend_by_model: what each model spent, by the factory ───────────
//
// factory.spend_by_model {factory, from, to} reads the factory's
// GET /v1/spend/models. The days are the factory's own calendar days, from
// and to inclusive (YYYY-MM-DD, at most [factory.SpendDaysMax] days), the
// same days factory.spend_by_repo takes.
//
// IT IS THE SAME DOLLARS AS factory.spend_by_repo. Each job's whole spend is
// placed on one day, the day its run started (a job that never ran: the day
// it was created), and only the jobs the caller can read are counted. The
// model lines and unknown_usd add up to that read's total. A call's model is
// whatever was recorded when the call was priced: a stage, the job's own
// chat, or the brief writer. A triage read, a scout, and a page-AI call that
// only reached this engine's spend ledger are not on the job, so they are in
// neither figure.
//
// SPEND WITH NO RECORDED MODEL IS unknown_usd. It is not dropped and it is
// not guessed from the job's model or a step's model. A job recorded before
// model lines existed contributes its whole spend there.

// FeatureFactorySpendByModel is announced in hello when the engine serves
// factory.spend_by_model.
const FeatureFactorySpendByModel = "factory.spend_by_model"

// FactorySpendByModelParams names one factory and a range of its days.
type FactorySpendByModelParams struct {
	Factory string `json:"factory"`
	From    string `json:"from"`
	To      string `json:"to"`
}

// FactorySpendByModel is the factory's answer with the connection it came from.
type FactorySpendByModel struct {
	Factory string `json:"factory"`
	api.SpendByModel
}

// FactoryModelSpender is the engine's half of factory.spend_by_model.
type FactoryModelSpender interface {
	FactorySpendByModel(ctx context.Context, p FactorySpendByModelParams) (FactorySpendByModel, error)
}

func withFactorySpendByModelFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryModelSpender); ok {
		hello = withFeature(hello, FeatureFactorySpendByModel)
	}
	return hello
}

func init() {
	const name = "factory.spend_by_model"
	methods[name] = shaped[FactorySpendByModelParams, FactorySpendByModel](func(e Engine, raw json.RawMessage) (*FactorySpendByModel, error) {
		s, ok := e.(FactoryModelSpender)
		if _, isFactory := e.(Factory); !ok || !isFactory {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
		}
		var p FactorySpendByModelParams
		if err := decodeStrict(name, raw, &p); err != nil {
			return nil, err
		}
		id, err := factoryID(p.Factory)
		if err != nil {
			return nil, err
		}
		p.Factory, p.From, p.To = id, strings.TrimSpace(p.From), strings.TrimSpace(p.To)
		if _, _, err := factory.SpendRange(p.From, p.To, time.UTC); err != nil {
			return nil, Errorf(CodeBadParams, "%s", err.Error())
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := s.FactorySpendByModel(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}
