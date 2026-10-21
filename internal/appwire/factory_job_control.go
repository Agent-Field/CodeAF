package appwire

import (
	"context"
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

const FeatureFactoryJobControl = "factory.job_control"

type FactoryJobControlParams struct {
	Factory string      `json:"factory"`
	Job     int         `json:"job"`
	Mode    string      `json:"mode"`
	Words   string      `json:"words,omitempty"`
	Expect  *api.Expect `json:"expect"`
}
type FactoryJobController interface {
	FactoryJobControl(context.Context, FactoryJobControlParams) (FactoryJobActed, error)
}

func init() {
	methods["factory.job_control"] = shaped[FactoryJobControlParams, FactoryJobActed](func(e Engine, raw json.RawMessage) (*FactoryJobActed, error) {
		c, ok := e.(FactoryJobController)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "factory.job_control is unavailable")
		}
		var p FactoryJobControlParams
		if err := decodeStrict("factory.job_control", raw, &p); err != nil {
			return nil, err
		}
		if err := checkJob(&p.Factory, p.Job, p.Expect); err != nil {
			return nil, err
		}
		if p.Mode != "take_over" && p.Mode != "resume_pipeline" {
			return nil, Errorf(CodeBadParams, "unknown control mode")
		}
		if len([]rune(p.Words)) > MaxFactoryJobWords {
			return nil, Errorf(CodeBadParams, "words exceed limit")
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := c.FactoryJobControl(ctx, p)
		return &out, err
	})
}
