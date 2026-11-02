package session

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
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
	if owner, ok := a.jobBudgetOf().(interface{ MemoryRefusal() string }); ok {
		if why := owner.MemoryRefusal(); why != "" {
			return nil, errors.New(why)
		}
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
	for {
		j.mu.Lock()
		before := j.offset
		j.mu.Unlock()
		if err := a.scanKnowledge(ctx, j, a.file.journalName(), a.id); err != nil {
			return
		}
		j.mu.Lock()
		after := j.offset
		j.mu.Unlock()
		if after == before {
			break
		}
	}
	if err := a.captureJobKnowledgeTasks(ctx, j); err != nil {
		return
	}
	key := a.config.Sources.OrDefault(a.config.APIKey, a.config.BaseURL).Default().Key
	if err := j.vault.Flush(provider.WithSession(ctx, a.id), key, true); err != nil && ctx.Err() == nil {
		j.mu.Lock()
		j.lastError = err.Error()
		j.mu.Unlock()
	}
}

// Task source identities remain intact so saved citations still name the report.
// The owning turn admits them; idle compilation must never buy work for a job.
func (a *Agent) captureJobKnowledgeTasks(ctx context.Context, j *knowledgeJournal) error {
	if a.config.Place.Dir == "" {
		return nil
	}
	history := KnowledgeHistory(filepath.Dir(filepath.Dir(a.config.Place.Dir)), a.config.ProfileDir)
	journals, err := history.Inventory(ctx)
	if err != nil {
		return err
	}
	_, since := j.vault.ConfirmConsent()
	for _, journal := range journals {
		if !strings.HasPrefix(journal.Session, "task:"+a.id+":") {
			continue
		}
		cursor := knowledge.JournalCursor{Scope: journal.Project}
		for cursor.Offset < journal.Size {
			batch, err := history.Read(ctx, journal, cursor)
			if err != nil {
				return err
			}
			for _, source := range batch.Sources {
				stamp, err := time.Parse(time.RFC3339Nano, source.TS)
				if err != nil {
					return err
				}
				if stamp.Before(since) {
					continue
				}
				source.Job = true
				if err := j.vault.Capture(source); err != nil {
					return err
				}
			}
			if batch.Cursor.Offset <= cursor.Offset {
				break
			}
			cursor = batch.Cursor
		}
	}
	return nil
}
