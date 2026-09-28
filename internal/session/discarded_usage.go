package session

import "context"

type detachedUsageKey struct{}

// withDetachedUsage identifies a request whose caller banks outside the active
// turn. Its internally discarded attempts must keep that same ownership.
func withDetachedUsage(ctx context.Context) context.Context {
	return context.WithValue(ctx, detachedUsageKey{}, true)
}

func detachedUsageFrom(ctx context.Context) bool {
	detached, _ := ctx.Value(detachedUsageKey{}).(bool)
	return detached
}
