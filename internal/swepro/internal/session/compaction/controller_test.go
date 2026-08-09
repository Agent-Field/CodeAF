package compaction

import (
	"context"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/engine/calc"
	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
	"github.com/Agent-Field/swe-pro-go/internal/engine/steploop"
	"github.com/Agent-Field/swe-pro-go/internal/session/overflow"
)

func TestControllerIsOverflowThreadsAgentAndDrift(t *testing.T) {
	// Validation contract 4: caller-provided agent and drift reach overflow.go.
	service := NewService(Dependencies{
		Config: ConfigProviderFunc(func(context.Context) (overflow.Config, error) {
			return overflow.Config{}, nil
		}),
	})
	controller := Controller{Compaction: service}
	model := steploop.Model{Calc: calc.Model{
		Limit: calc.ModelLimit{Context: 200_000, Output: 32_768},
	}}
	coder := "coder"
	build := "build"
	highDrift := 0.8

	tests := []struct {
		name    string
		tokens  uint64
		options steploop.OverflowOptions
		want    bool
	}{
		{name: "coder leaf trigger", tokens: 60_001, options: steploop.OverflowOptions{Agent: &coder}, want: true},
		{name: "non-coder below global trigger", tokens: 60_001, options: steploop.OverflowOptions{Agent: &build}},
		{name: "provided drift", tokens: 50_000, options: steploop.OverflowOptions{Agent: &build, Drift: &highDrift}, want: true},
		{name: "zero-value compatibility", tokens: 50_000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := controller.IsOverflow(context.Background(), msgmodel.Assistant{
				Tokens: msgmodel.Tokens{Input: test.tokens, Cache: msgmodel.TokenCache{}},
			}, model, test.options)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("IsOverflow() = %v, want %v", got, test.want)
			}
		})
	}
}
