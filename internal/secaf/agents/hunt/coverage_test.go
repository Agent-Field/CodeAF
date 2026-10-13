package hunt

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/agentsession/appx"
	"github.com/Agent-Field/codeaf/internal/secaf/config"
	"github.com/Agent-Field/codeaf/internal/secaf/schemas"
)

func TestResumedHuntsKeepPartialFindingsAndFailures(t *testing.T) {
	withStrategyTable(t,
		strategyEntry{schemas.HuntStrategyInjection, func(context.Context, appx.Harnesser, string, schemas.ReconResult, config.DepthProfile) (schemas.HuntResult, error) {
			return schemas.HuntResult{}, errors.New("cannot search")
		}},
		strategyEntry{schemas.HuntStrategyAuth, func(context.Context, appx.Harnesser, string, schemas.ReconResult, config.DepthProfile) (schemas.HuntResult, error) {
			result := schemas.NewHuntResult()
			result.Findings = []schemas.RawFinding{testFinding("app/a.py", "one")}
			return result, nil
		}},
	)
	withDedup(t, passthroughDedup)
	result, err := RunHunt(t.Context(), &appx.Fake{}, ".", emptyRecon(), "standard", 2, 1, nil)
	if err != nil || len(result.Findings) != 1 || result.Coverage == nil || result.Coverage.Attempted != 2 || len(result.Coverage.Failures) != 1 || result.Coverage.Failures[0].Strategy != "injection" {
		t.Fatalf("partial hunt=%+v err=%v", result, err)
	}
}
