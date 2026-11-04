package provider

import (
	"context"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type usageProgressKey struct{}

// WithUsageProgress observes cumulative measured stream usage per HTTP attempt.
// A nil usage retires the live reading before ordinary billing settles it.
// Progress is a lower bound, not a second charge or a final provider receipt.
func WithUsageProgress(ctx context.Context, observe func(string, *ai.Usage)) context.Context {
	return context.WithValue(ctx, usageProgressKey{}, observe)
}

func noteUsageProgress(ctx context.Context, usage *ai.Usage, traces ...*callTrace) {
	if observe, _ := ctx.Value(usageProgressKey{}).(func(string, *ai.Usage)); observe != nil {
		id := ""
		if len(traces) > 0 && traces[0] != nil {
			id = traces[0].attemptID
		}
		observe(id, usage)
	}
}
