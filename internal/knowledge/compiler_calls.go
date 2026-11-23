package knowledge

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/provider"
)

type compilerCallsKey struct{}
type compilerCalls struct {
	before func() error
	record provider.BillingSink
}

// WithCompilerCalls connects background compilation to the application's spend
// gate and ledger without making the vault depend on the session engine.
func WithCompilerCalls(ctx context.Context, before func() error, record provider.BillingSink) context.Context {
	return context.WithValue(ctx, compilerCallsKey{}, compilerCalls{before: before, record: record})
}

func callsFrom(ctx context.Context) compilerCalls {
	calls, _ := ctx.Value(compilerCallsKey{}).(compilerCalls)
	return calls
}
