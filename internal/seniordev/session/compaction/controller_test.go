//go:build !windows

package compaction

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/calc"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/overflow"
)

// The controller hands the assistant's token count and the session's config
// to the overflow arithmetic; the trigger is the high watermark of the
// capacity, and nothing else fires it.
func TestControllerIsOverflowTriggersAtTheHighWatermark(t *testing.T) {
	capacity := 100_000.0
	service := NewService(Dependencies{
		Config: ConfigProviderFunc(func(context.Context) (overflow.Config, error) {
			return overflow.Config{Compaction: &overflow.CompactionConfig{
				CapacityTokens: &capacity,
			}}, nil
		}),
	})
	controller := Controller{Compaction: service}
	model := steploop.Model{Calc: calc.Model{
		Limit: calc.ModelLimit{Context: 200_000, Output: 32_768},
	}}

	tests := []struct {
		name   string
		tokens uint64
		want   bool
	}{
		{name: "below the high watermark", tokens: 59_999},
		{name: "at the high watermark", tokens: 60_000, want: true},
		{name: "far above", tokens: 150_000, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := controller.IsOverflow(context.Background(), msgmodel.Assistant{
				Tokens: msgmodel.Tokens{Input: test.tokens, Cache: msgmodel.TokenCache{}},
			}, model)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("IsOverflow() = %v, want %v", got, test.want)
			}
		})
	}
}
