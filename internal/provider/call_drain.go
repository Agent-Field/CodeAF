package provider

import "context"

type callDrainKey struct{}

// WithCallDrain makes a completion join its canceled stream arms before returning.
// Owners whose books close can then wait for receipts knowing every arm has
// already billed or registered its pending receipt. Ordinary interactive calls
// retain their immediate cancellation and background accounting.
func WithCallDrain(ctx context.Context) context.Context {
	return context.WithValue(ctx, callDrainKey{}, true)
}

func callDrainFrom(ctx context.Context) bool {
	joined, _ := ctx.Value(callDrainKey{}).(bool)
	return joined
}
