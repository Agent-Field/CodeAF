package session

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/knowledge"
)

// Knowledge is the profile-wide vault the conversation's tools read.
// Capture is opt-in and only persisted, human-facing conversations write it.
func (a *Agent) Knowledge() *knowledge.Vault {
	a.knowledgeMu.RLock()
	defer a.knowledgeMu.RUnlock()
	return a.knowledgeVault
}

// KnowledgeCaptureError reports a failed journal-to-vault copy. The durable
// journal remains the recovery source; a failed copy never advances its cursor.
func (a *Agent) KnowledgeCaptureError() string {
	a.knowledgeMu.RLock()
	j := a.knowledgeJournal
	a.knowledgeMu.RUnlock()
	if j == nil {
		return ""
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.lastError
}

type knowledgeJournal struct {
	vault     *knowledge.Vault
	project   string
	scope     string
	mu        sync.Mutex
	offset    int64
	skip      int
	lastError string
	cancel    context.CancelFunc
	done      chan struct{}
}

func (a *Agent) initKnowledge() {
	if a.config.knowledgeReadVault != nil {
		a.knowledgeVault = a.config.knowledgeReadVault
		return
	}
	if strings.TrimSpace(a.config.transcriptPath()) == "" || a.config.InTask || a.config.taskID != 0 {
		return
	}
	a.knowledgeVault = knowledge.Open(config.ProfilePath(a.config.ProfileDir, ""))
}
func (a *Agent) startKnowledge() {
	vault := a.Knowledge()
	if vault == nil || a.config.InTask || a.config.taskID != 0 || a.config.knowledgeReadVault != nil || a.file == nil {
		return
	}
	path := a.file.journalName()
	if path == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &knowledgeJournal{cancel: cancel, done: make(chan struct{}), vault: vault, project: a.config.Workspace}
	a.knowledgeMu.Lock()
	a.knowledgeJournal = j
	a.knowledgeMu.Unlock()
	sessionID := a.id
	go func() {
		defer close(j.done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				// A bounded final copy helps small chats close immediately. The
				// process worker recovers the remaining durable journal later.
				finalCtx, finish := context.WithTimeout(context.Background(), time.Second)
				a.scanKnowledge(finalCtx, j, path, sessionID)
				finish()
				return
			case <-ticker.C:
				a.scanKnowledge(ctx, j, path, sessionID)
			}
		}
	}()
}

// rebindKnowledgeLocked restarts capture after a persisted project scope change.
// Caller holds a.mu. The retired scanner can finish without holding that lock;
// stable source IDs make overlapping scans safe.
func (a *Agent) rebindKnowledgeLocked(project string) {
	a.knowledgeMu.Lock()
	old := a.knowledgeJournal
	if old != nil {
		old.cancel()
		a.knowledgeRetired = append(a.knowledgeRetired, old)
	}
	a.knowledgeVault = knowledge.Open(config.ProfilePath(a.config.ProfileDir, ""))
	a.knowledgeJournal = nil
	a.knowledgeMu.Unlock()
	a.startKnowledge()
}
func (a *Agent) stopKnowledge() {
	a.knowledgeMu.RLock()
	workers := append([]*knowledgeJournal(nil), a.knowledgeRetired...)
	if a.knowledgeJournal != nil {
		workers = append(workers, a.knowledgeJournal)
	}
	a.knowledgeMu.RUnlock()
	for _, j := range workers {
		j.cancel()
	}
	timer := time.NewTimer(closeGrace)
	defer timer.Stop()
	for _, j := range workers {
		select {
		case <-j.done:
		case <-timer.C:
			log.Printf("knowledge: capture shutdown deadline; journal recovery will retry")
			return
		}
	}
}

// scanKnowledge reads complete journal lines by byte offset. Re-journaled
// compaction windows are skipped, so stubs and summaries never become new
// claims. IDs survive reopen/retry and do not conflate equal human messages.
func (a *Agent) scanKnowledge(ctx context.Context, j *knowledgeJournal, path, sessionID string) (err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	defer func() {
		if err != nil {
			j.lastError = err.Error()
			log.Printf("knowledge: capture pending: %v", err)
		} else {
			j.lastError = ""
		}
	}()
	// Idle chat tickers share consent reads instead of reopening SQLite each second.
	enabled, since := j.vault.CaptureConsent()
	if !enabled {
		return nil
	}
	if since.IsZero() {
		return fmt.Errorf("knowledge: missing opt-in timestamp")
	}
	cursor := knowledge.JournalCursor{Offset: j.offset, Skip: j.skip, Scope: j.scope}
	if cursor.Scope == "" {
		cursor.Scope = j.project
	}
	history := knowledgeHistory{}
	journal := knowledge.Journal{ID: path, Session: sessionID, Project: j.project}
	{
		batch, e := history.Read(ctx, journal, cursor)
		if e != nil {
			return e
		}
		for _, source := range batch.Sources {
			if e = ctx.Err(); e != nil {
				return e
			}
			stamp, e := time.Parse(time.RFC3339Nano, source.TS)
			if e != nil {
				return e
			}
			if stamp.Before(since) {
				continue
			}
			if e = j.vault.Capture(source); e != nil {
				return e
			}
		}
		j.offset = batch.Cursor.Offset
		j.skip = batch.Cursor.Skip
		j.scope = batch.Cursor.Scope
	}
	return nil
}
