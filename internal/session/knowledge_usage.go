package session

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
)

func init() {
	knowledge.SetUsageRecorder(func(ctx context.Context, billed provider.Billed) {
		RecordUsage(UsageLedgerPath(), withUsageBilling(UsageLine{
			GenerationID: billed.GenerationID, At: time.Now(), Model: billed.Model, Role: string(lane.RoleMemory), Session: provider.SessionFrom(ctx),
			Calls: 1, Input: billed.PromptTokens, Output: billed.CompletionTokens, USD: billed.Cost,
			Price: usagePriceProvider,
		}, billed.Billing))
	})
}

func (a *Agent) knowledgeCallContext(ctx context.Context) (context.Context, error) {
	a.mu.Lock()
	closed, stopped := a.closed, a.workStopped
	a.mu.Unlock()
	continuing, _ := ctx.Value(jobKnowledgeContextKey{}).(bool)
	if stopped || (closed && (!continuing || a.config.JobContext == nil)) {
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
	if a.config.JobContext != nil && a.config.JobContext.Err() != nil {
		return nil, context.Canceled
	}
	ctx = provider.WithSession(ctx, a.id)
	ctx = provider.WithCallNode(ctx, a.id)
	bill := func(b provider.Billed) {
		used := Usage{Input: b.PromptTokens, Output: b.CompletionTokens, CacheRead: b.CachedTokens, CostUSD: b.Cost, Calls: 1}
		a.bank(bankedCall{generationID: b.GenerationID, used: used, model: b.Model, role: string(lane.RoleMemory), ledger: true, late: true, billing: b.Billing, price: usagePriceProvider})
		a.file.appendUsage(used, b.Model, true, string(lane.RoleMemory))
		a.stampSpend()
	}
	ctx = provider.WithBilling(ctx, bill)
	if a.config.JobPendingSpend != nil {
		ctx = provider.WithReceiptPending(ctx, a.config.JobPendingSpend)
	}
	ctx = provider.WithReconcile(ctx, func(r provider.Reconciled) {
		if r.Found {
			bill(r.Billed)
		} else {
			a.recordUnbilledReceipt(r.Model)
		}
	})
	return a.withReceiptTracking(provider.WithUnmeteredReceipts(ctx)), nil
}

// jobKnowledgeWait limits the part of optional learning a stage waits for.
const jobKnowledgeWait = 2 * time.Second

type jobKnowledgeContextKey struct{}
type jobKnowledgeCompile struct {
	cancel context.CancelFunc
	done   chan struct{}
}

var jobKnowledgeCompilers = struct {
	sync.Mutex
	active map[string]bool
}{active: make(map[string]bool)}

// A later finished turn leaves its durable journal for the next pass instead
// of stacking another compiler behind the current one.
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
	owner, _, err := knowledgeJobOwner(a.file.journalName())
	if err != nil {
		return
	}
	if owner == "" {
		owner = a.id
	}
	key := config.ProfilePath(a.config.ProfileDir, "") + ":" + owner
	jobKnowledgeCompilers.Lock()
	if jobKnowledgeCompilers.active[key] {
		jobKnowledgeCompilers.Unlock()
		return
	}
	jobKnowledgeCompilers.active[key] = true
	jobKnowledgeCompilers.Unlock()
	parent := j.ctx
	if a.config.JobContext != nil {
		parent = a.config.JobContext
	}
	compileCtx, cancel := context.WithCancel(context.WithValue(context.WithoutCancel(ctx), jobKnowledgeContextKey{}, true))
	detach := context.AfterFunc(parent, cancel)
	work := &jobKnowledgeCompile{cancel: cancel, done: make(chan struct{})}
	j.compile.Store(work)
	settled := func() {}
	if a.config.JobPendingSpend != nil {
		settled = a.config.JobPendingSpend()
	}
	go func() {
		defer settled()
		defer close(work.done)
		defer cancel()
		defer detach()
		defer func() {
			jobKnowledgeCompilers.Lock()
			delete(jobKnowledgeCompilers.active, key)
			jobKnowledgeCompilers.Unlock()
		}()
		a.compileJobKnowledge(compileCtx, j)
	}()
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-work.done:
				return
			case <-compileCtx.Done():
				return
			case <-ticker.C:
				if owner, ok := a.jobBudgetOf().(interface{ MemoryRefusal() string }); ok && owner.MemoryRefusal() != "" {
					cancel()
					return
				}
			}
		}
	}()
	timer := time.NewTimer(jobKnowledgeWait)
	defer timer.Stop()
	select {
	case <-work.done:
	case <-timer.C:
	case <-ctx.Done():
		cancel()
	}
}

func (a *Agent) cancelJobKnowledge() {
	a.knowledgeMu.RLock()
	j := a.knowledgeJournal
	a.knowledgeMu.RUnlock()
	if j == nil {
		return
	}
	work := j.compile.Load()
	if work != nil {
		work.cancel()
	}
}

func (a *Agent) compileJobKnowledge(ctx context.Context, j *knowledgeJournal) {
	sessions, err := a.captureJobKnowledge(ctx, j)
	if err != nil {
		return
	}
	key := a.config.Sources.OrDefault(a.config.APIKey, a.config.BaseURL).Default().Key
	ctx = knowledge.WithOwnerSessions(provider.WithSession(ctx, a.id), sessions)
	if err := j.vault.Flush(ctx, key, true); err != nil && ctx.Err() == nil {
		j.mu.Lock()
		j.lastError = err.Error()
		j.mu.Unlock()
	}
}

// The next finished turn includes sibling stages skipped during an earlier
// pass. Sources retain their original session IDs and searchable citations.
func (a *Agent) captureJobKnowledge(ctx context.Context, j *knowledgeJournal) ([]string, error) {
	if a.config.Place.Dir == "" {
		for {
			j.mu.Lock()
			before := j.offset
			j.mu.Unlock()
			if err := a.scanKnowledge(ctx, j, a.file.journalName(), a.id, true); err != nil {
				return nil, err
			}
			j.mu.Lock()
			after := j.offset
			j.mu.Unlock()
			if before == after {
				break
			}
		}
		return []string{a.id}, nil
	}
	owner, _, err := knowledgeJobOwner(a.file.journalName())
	if err != nil {
		return nil, err
	}
	history := KnowledgeHistory(filepath.Dir(filepath.Dir(a.config.Place.Dir)), a.config.ProfileDir)
	journals, err := history.Inventory(ctx)
	if err != nil {
		return nil, err
	}
	sessions := []string{a.id}
	_, since := j.vault.ConfirmConsent()
	for _, journal := range journals {
		if journal.JobOwner != owner || owner == "" {
			continue
		}
		if !strings.HasPrefix(journal.Session, "task:") && journal.Session != a.id {
			sessions = append(sessions, journal.Session)
		}
		cursor := knowledge.JournalCursor{Scope: journal.Project}
		for cursor.Offset < journal.Size {
			batch, err := history.Read(ctx, journal, cursor)
			if err != nil {
				return nil, err
			}
			for _, source := range batch.Sources {
				stamp, err := time.Parse(time.RFC3339Nano, source.TS)
				if err != nil {
					return nil, err
				}
				if stamp.Before(since) {
					continue
				}
				source.Job = true
				if err := j.vault.Capture(source); err != nil {
					return nil, err
				}
			}
			if batch.Cursor.Offset <= cursor.Offset {
				break
			}
			cursor = batch.Cursor
		}
	}
	return sessions, nil
}
