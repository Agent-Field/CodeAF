package appwire

import (
	"context"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// FeatureFactoryJobStopAfterStep lets a person finish this step before deciding
// whether to spend on the next one, including the final landing.
const FeatureFactoryJobStopAfterStep = "factory.job_stop_after_step"

// JobActStopAfterStep names the spending toggle in its action receipt.
const JobActStopAfterStep = "stop_after_step"

// FactoryJobStopAfterStepParams sets or withdraws the next step boundary hold.
// On is required; Expect optionally guards the job the person read.
type FactoryJobStopAfterStepParams struct {
	Factory string      `json:"factory"`
	Job     int         `json:"job"`
	On      *bool       `json:"on"`
	Expect  *api.Expect `json:"expect,omitempty"`
}

// FactoryStepSpending is the engine's single-write spending hold door.
type FactoryStepSpending interface {
	FactoryJobSpendAnswer(context.Context, FactoryJobSpendAnswerParams) (FactoryJobActed, error)
	FactoryJobStopAfterStep(context.Context, FactoryJobStopAfterStepParams) (FactoryJobActed, error)
}

func init() {
	methods[FeatureFactoryJobStopAfterStep] = shaped[FactoryJobStopAfterStepParams, FactoryJobActed](func(e Engine, raw json.RawMessage) (*FactoryJobActed, error) {
		a, ok := e.(FactoryStepSpending)
		if _, factory := e.(Factory); !ok || !factory {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", FeatureFactoryJobStopAfterStep)
		}
		var p FactoryJobStopAfterStepParams
		if err := decodeStrict(FeatureFactoryJobStopAfterStep, raw, &p); err != nil {
			return nil, err
		}
		id, err := factoryID(p.Factory)
		if err != nil {
			return nil, err
		}
		p.Factory = id
		if p.Job < 1 || p.On == nil {
			return nil, Errorf(CodeBadParams, "job must be positive and on must be a boolean")
		}
		if p.Expect != nil {
			if err := checkJob(&p.Factory, p.Job, p.Expect); err != nil {
				return nil, err
			}
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := a.FactoryJobStopAfterStep(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}
