package appwire

import (
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// FeatureFactoryJobSpendAnswer answers only a person's spending hold.
const FeatureFactoryJobSpendAnswer = "factory.job_spend_answer"

// JobActSpendAnswer names the spending answer in its receipt.
const JobActSpendAnswer = "spend_answer"

// FactoryJobSpendAnswerParams requires the choice and the job revision read.
type FactoryJobSpendAnswerParams struct {
	Factory string      `json:"factory"`
	Job     int         `json:"job"`
	CarryOn *bool       `json:"carry_on"`
	Expect  *api.Expect `json:"expect"`
}

func init() {
	methods[FeatureFactoryJobSpendAnswer] = shaped[FactoryJobSpendAnswerParams, FactoryJobActed](func(e Engine, raw json.RawMessage) (*FactoryJobActed, error) {
		a, ok := e.(FactoryStepSpending)
		if _, factory := e.(Factory); !ok || !factory {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", FeatureFactoryJobSpendAnswer)
		}
		var p FactoryJobSpendAnswerParams
		if err := decodeStrict(FeatureFactoryJobSpendAnswer, raw, &p); err != nil {
			return nil, err
		}
		if p.CarryOn == nil {
			return nil, Errorf(CodeBadParams, "carry_on must be a boolean")
		}
		if err := checkJob(&p.Factory, p.Job, p.Expect); err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := a.FactoryJobSpendAnswer(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}
