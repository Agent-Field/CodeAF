package session

import (
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestOrchestrateCostKeepsABareNameOutOfAVendorsRow(t *testing.T) {
	withoutReportedCost := &ai.Response{Usage: &ai.Usage{PromptTokens: 1_000_000}}
	for _, test := range []struct {
		model string
		want  float64
	}{
		{model: "deepseek-v4-flash", want: 1.25},
		{model: "deepseek/deepseek-v4-flash", want: 0.14},
	} {
		if got := orchestrateCost(withoutReportedCost, test.model); got != test.want {
			t.Errorf("orchestrateCost without a reported cost for %q = %v, want %v", test.model, got, test.want)
		}
	}

	reportedCost := 0.73
	withReportedCost := &ai.Response{Usage: &ai.Usage{
		PromptTokens: 1_000_000,
		Cost:         &reportedCost,
	}}
	for _, model := range []string{"deepseek-v4-flash", "deepseek/deepseek-v4-flash"} {
		if got := orchestrateCost(withReportedCost, model); got != reportedCost {
			t.Errorf("orchestrateCost with a reported cost for %q = %v, want %v", model, got, reportedCost)
		}
	}
}
