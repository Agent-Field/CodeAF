package session

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/knowledge"
)

// The test hook counts scans without changing the journal or the capture path.
var knowledgeScanHook atomic.Pointer[func(string)]

// knowledgeSafetyTick bounds discovery of journal lines this process did not
// write, such as a session host's or terminal's writes. With consentRecheck,
// lines covered by consent changed elsewhere wait at most
// consentRecheck + knowledgeSafetyTick.
const knowledgeSafetyTick = 5 * time.Second

// knowledgeCatchUp is the former capture cadence. A look follows a write burst
// by this long, and follows any look that moved the cursor, so a backlog keeps
// moving one batch a second and the timer stops once a look finds nothing new.
const knowledgeCatchUp = time.Second

// knowledgeWroteConsentAge is how old the shared consent answer may be for a
// look a write woke: a turn written just after consent changed elsewhere is
// captured on the trailing look, a second later, not at the next safety tick.
// The answer is shared, so writing chats open the database at most once a
// second between them, and idle chats never.
const knowledgeWroteConsentAge = time.Second

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
	a.knowledgeWake = make(chan struct{}, 1)
	if a.config.knowledgeReadVault != nil {
		a.knowledgeVault = a.config.knowledgeReadVault
		return
	}
	if strings.TrimSpace(a.config.transcriptPath()) == "" || a.config.InTask || a.config.taskID != 0 {
		return
	}
	a.knowledgeVault = knowledge.Open(config.ProfilePath(a.config.ProfileDir, ""))
}

// wakeKnowledge is called by the journal writer's hook. It never blocks or
// takes a lock, because writers call it with locks held.
func (a *Agent) wakeKnowledge() {
	select {
	case a.knowledgeWake <- struct{}{}:
	default:
	}
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
		safety := time.NewTicker(knowledgeSafetyTick)
		defer safety.Stop()
		catchUpTimer := time.NewTimer(knowledgeCatchUp)
		catchUpTimer.Stop()
		defer catchUpTimer.Stop()
		var catchUp <-chan time.Time
		// look scans once and says whether it moved the cursor; wrote says a
		// write woke it, or it trails one, so its consent answer is fresh.
		look := func(wrote bool) bool {
			j.mu.Lock()
			before := j.offset
			j.mu.Unlock()
			a.scanKnowledge(ctx, j, path, sessionID, wrote)
			j.mu.Lock()
			defer j.mu.Unlock()
			return j.offset != before
		}
		// trail asks for one more look knowledgeCatchUp from now.
		trail := func() {
			catchUpTimer.Reset(knowledgeCatchUp)
			catchUp = catchUpTimer.C
		}
		// A resumed chat may already have durable lines waiting for capture.
		if look(false) {
			trail()
		}
		for {
			select {
			case <-ctx.Done():
				// A bounded final copy helps small chats close immediately. The
				// process worker recovers the remaining durable journal later.
				finalCtx, finish := context.WithTimeout(context.Background(), time.Second)
				a.scanKnowledge(finalCtx, j, path, sessionID, true)
				finish()
				return
			case <-a.knowledgeWake:
				// Every write is followed by one trailing look, which catches
				// lines still being written and consent that changed elsewhere.
				look(true)
				trail()
			case <-safety.C:
				if look(false) {
					trail()
				}
			case <-catchUp:
				catchUp = nil
				if look(true) {
					trail()
				}
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
func (a *Agent) scanKnowledge(ctx context.Context, j *knowledgeJournal, path, sessionID string, wrote bool) (err error) {
	if hook := knowledgeScanHook.Load(); hook != nil && *hook != nil {
		(*hook)(path)
	}
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
	// Idle chats share consent reads instead of each reopening SQLite. The
	// shared answer only decides whether to look; sources found
	// below are admitted, or passed over, on consent read afresh.
	enabled, since := false, time.Time{}
	if wrote {
		enabled, since = j.vault.CaptureConsentWithin(knowledgeWroteConsentAge)
	} else {
		enabled, since = j.vault.CaptureConsent()
	}
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
		if len(batch.Sources) > 0 {
			// The cursor moves past these sources only on the database's own
			// opt-in time: a replaced or reset vault is judged as it is now.
			if enabled, since = j.vault.ConfirmConsent(); !enabled {
				return nil
			}
			if since.IsZero() {
				return fmt.Errorf("knowledge: missing opt-in timestamp")
			}
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
