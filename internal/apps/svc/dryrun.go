package svc

import "context"

type dryRunKey struct{}

// WithDryRun marks a context whose runs are a check's rehearsal on sample data.
// A rehearsal never sleeps out a retry backoff: the wait is real time the
// builder's worker sits through, and the answer is the same without it.
func WithDryRun(ctx context.Context) context.Context {
	return context.WithValue(ctx, dryRunKey{}, true)
}

// DryRun says the context belongs to a check's rehearsal.
func DryRun(ctx context.Context) bool { ok, _ := ctx.Value(dryRunKey{}).(bool); return ok }
