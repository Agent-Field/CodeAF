package knowledge

import (
	"context"
	"sync"

	"github.com/Agent-Field/codeaf/internal/provider"
)

var usageRecorder struct {
	sync.RWMutex
	record func(context.Context, provider.Billed)
}

// SetUsageRecorder connects the application's ledger without making the vault
// depend on the session engine. An owned call's billing sink takes precedence.
func SetUsageRecorder(record func(context.Context, provider.Billed)) {
	usageRecorder.Lock()
	defer usageRecorder.Unlock()
	usageRecorder.record = record
}

func memoryBilling(ctx context.Context) context.Context {
	if provider.BillingSinkFrom(ctx) != nil {
		return ctx
	}
	usageRecorder.RLock()
	record := usageRecorder.record
	usageRecorder.RUnlock()
	if record == nil {
		return ctx
	}
	base := ctx
	ctx = provider.WithBilling(ctx, func(b provider.Billed) { record(base, b) })
	ctx = provider.WithReconcile(ctx, func(r provider.Reconciled) {
		billed := r.Billed
		if billed.GenerationID == "" {
			billed.GenerationID = r.Ref
		}
		record(base, billed)
	})
	return provider.WithUnmeteredReceipts(ctx)
}

// WithCalls binds compilation to its owner's admission and billing context.
// The returned handle shares durable state with other handles of this vault.
func (v *Vault) WithCalls(calls func(context.Context) (context.Context, error)) *Vault {
	copy := *v
	copy.calls = calls
	return &copy
}

type ownerSessionsKey struct{}

// WithOwnerSessions admits the saved conversations of one job to a compilation.
// Citation identities and the initiating call's billing session remain unchanged.
func WithOwnerSessions(ctx context.Context, sessions []string) context.Context {
	return context.WithValue(ctx, ownerSessionsKey{}, append([]string(nil), sessions...))
}

// LibraryActivity reports the recorded purpose of a knowledge request for spending.
// It changes attribution only; compiler roles and quality checks stay unchanged.
func LibraryActivity(ctx context.Context) string {
	activity, _ := ctx.Value(libraryActivityKey{}).(string)
	return activity
}

type libraryActivityKey struct{}

func withLibraryActivity(ctx context.Context, role string) context.Context {
	activity := "learning"
	if role == "query" || role == "recall" {
		activity = "answers"
	}
	return context.WithValue(ctx, libraryActivityKey{}, activity)
}
