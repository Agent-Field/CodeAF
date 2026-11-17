package provider

import "context"

type servedModelKey struct{}

// WithServedModel reports the model id the provider actually names, before
// forwarding its first content. It carries no credential or catalogue guess.
func WithServedModel(ctx context.Context, report func(string)) context.Context {
	return context.WithValue(ctx, servedModelKey{}, report)
}

func reportServedModel(ctx context.Context, model string) {
	if report, ok := ctx.Value(servedModelKey{}).(func(string)); ok && model != "" {
		report(model)
	}
}
