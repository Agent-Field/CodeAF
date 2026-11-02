package session

import (
	"context"
	"time"

	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
)

func init() {
	knowledge.SetUsageRecorder(func(ctx context.Context, billed provider.Billed) {
		RecordUsage(UsageLedgerPath(), withUsageBilling(UsageLine{
			At: time.Now(), Model: billed.Model, Role: string(lane.RoleMemory), Session: provider.SessionFrom(ctx),
			Calls: 1, Input: billed.PromptTokens, Output: billed.CompletionTokens, USD: billed.Cost,
			Price: usagePriceProvider,
		}, billed.Billing))
	})
}

func (a *Agent) knowledgeCallContext(ctx context.Context) (context.Context, error) {
	a.mu.Lock()
	closed, stopped := a.closed, a.workStopped
	a.mu.Unlock()
	if closed || stopped {
		return nil, context.Canceled
	}
	if err := a.admitJobCall(ctx); err != nil {
		return nil, err
	}
	ctx = provider.WithSession(ctx, a.id)
	bill := func(b provider.Billed) {
		used := Usage{Input: b.PromptTokens, Output: b.CompletionTokens, CacheRead: b.CachedTokens, CostUSD: b.Cost, Calls: 1}
		a.bank(bankedCall{used: used, model: b.Model, role: string(lane.RoleMemory), ledger: true, late: true, billing: b.Billing, price: usagePriceProvider})
		a.file.appendUsage(used, b.Model, true, string(lane.RoleMemory))
		a.stampSpend()
	}
	ctx = provider.WithBilling(ctx, bill)
	ctx = provider.WithReconcile(ctx, func(r provider.Reconciled) {
		if r.Found {
			bill(r.Billed)
		} else {
			a.recordUnbilledReceipt(r.Model)
		}
	})
	return provider.WithUnmeteredReceipts(ctx), nil
}

// Job compilation finishes before the stage stream closes. Pause cancels this
// same turn, so the process worker cannot continue spending on its behalf.
func (a *Agent) finishJobKnowledge(ctx context.Context) {
	if a.jobBudgetOf() == nil || ctx.Err() != nil {
		return
	}
	a.knowledgeMu.RLock()
	j := a.knowledgeJournal
	a.knowledgeMu.RUnlock()
	if j == nil || !j.vault.Enabled() {
		return
	}
	if err := a.scanKnowledge(ctx, j, a.file.journalName(), a.id); err != nil {
		return
	}
	key := a.config.Sources.OrDefault(a.config.APIKey, a.config.BaseURL).Default().Key
	if err := j.vault.Flush(provider.WithSession(ctx, a.id), key, true); err != nil && ctx.Err() == nil {
		j.mu.Lock()
		j.lastError = err.Error()
		j.mu.Unlock()
	}
}
