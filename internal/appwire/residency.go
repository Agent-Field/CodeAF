package appwire

import (
	"github.com/Agent-Field/codeaf/internal/apps/connections"
	"github.com/Agent-Field/codeaf/internal/apps/data"
)

type AppResidencyParams struct {
	App  string `json:"app"`
	Type string `json:"type"`
}

type ApplyCutoverParams struct {
	connections.CutoverRequest
	Confirmation string `json:"confirmation"`
}

type AppResidency interface {
	AppResidency(AppResidencyParams) (connections.ResidencyStatus, error)
	PreviewAppCutover(connections.CutoverRequest) (data.ResidencyPlan, error)
	ApplyAppCutover(ApplyCutoverParams) (data.ResidencyPlan, error)
}

func residencyMethod[P, R any](fn func(AppResidency, P) (R, error)) method {
	return typed(func(e Engine, p P) (*R, error) {
		operator, ok := e.(AppResidency)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not manage app residency")
		}
		result, err := fn(operator, p)
		return &result, err
	})
}

func init() {
	methods["residency.show"] = residencyMethod(AppResidency.AppResidency)
	methods["residency.preview"] = residencyMethod(AppResidency.PreviewAppCutover)
	methods["residency.cutover"] = residencyMethod(AppResidency.ApplyAppCutover)
}
