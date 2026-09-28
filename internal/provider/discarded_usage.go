package provider

import (
	"context"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type discardedUsageKey struct{}

// WithDiscardedUsage observes a paid response superseded by an internal retry.
// The final response keeps its own usage: summing attempts into it would make
// its context size and cache counts describe a different request. Observers
// compose so the ledger and each budget owner can retain their own accounting.
// A full WithBilling owner already receives every attempt; ledger observers
// must defer to that owner, while budget observers still need this notice.
func WithDiscardedUsage(ctx context.Context, observe func(model, tag string, response *ai.Response)) context.Context {
	if observe == nil {
		return ctx
	}
	previous, _ := ctx.Value(discardedUsageKey{}).(func(string, string, *ai.Response))
	return context.WithValue(ctx, discardedUsageKey{}, func(model, tag string, response *ai.Response) {
		if previous != nil {
			previous(model, tag, response)
		}
		observe(model, tag, response)
	})
}

func noteDiscardedUsage(ctx context.Context, response *ai.Response) {
	observe, _ := ctx.Value(discardedUsageKey{}).(func(string, string, *ai.Response))
	if observe == nil || response == nil || response.Usage == nil {
		return
	}
	model := response.Model
	if served := CallFrom(ctx).Model(); served != "" {
		model = served
	}
	observe(model, callTag(ctx), response)
}
