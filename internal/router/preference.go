package router

import (
	"context"
	"strings"
)

type avoidedModelContextKey struct{}

// WithAvoidModel asks a cascade to prefer a different opener when the panel
// permits it. The named model stays available as a fallback, so a preference
// never turns a healthy single-model configuration into a failure.
func WithAvoidModel(ctx context.Context, model string) context.Context {
	model = strings.TrimSpace(model)
	if model == "" {
		return ctx
	}
	return context.WithValue(ctx, avoidedModelContextKey{}, model)
}

func avoidedModelFrom(ctx context.Context) string {
	model, _ := ctx.Value(avoidedModelContextKey{}).(string)
	return model
}

func preferDifferent(order []*rung, avoided string) []*rung {
	if avoided == "" || len(order) < 2 || order[0].spec.Slug != avoided {
		return order
	}
	preferred := append([]*rung(nil), order[1:]...)
	return append(preferred, order[0])
}
