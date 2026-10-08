package factory

import "context"

// dryRunKey marks a context whose door should say what it would do and do
// nothing.
type dryRunKey struct{}

// DryRun is ctx marked so a door that spends ([Seam.RefreshAll]) answers its
// count and its cost without spending: the surface asks first and acts on the
// person's yes.
func DryRun(ctx context.Context) context.Context { return context.WithValue(ctx, dryRunKey{}, true) }

// IsDryRun says whether ctx was marked by [DryRun].
func IsDryRun(ctx context.Context) bool {
	v, _ := ctx.Value(dryRunKey{}).(bool)
	return v
}
