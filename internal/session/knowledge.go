package session

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/knowledge"
)

// The test hook counts scans without changing the journal or the capture path.
var knowledgeScanHook atomic.Pointer[func(string)]

// knowledgeReadHook counts the scans that read the journal, for tests.
var knowledgeReadHook atomic.Pointer[func(string)]

// knowledgeCloseWait caps all scanner and cancelled compiler joins together.
const knowledgeCloseWait = 10 * time.Second

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
	ctx context.Context
	// Compilation is published separately because scanning holds mu during SQLite I/O.
	compile   atomic.Pointer[jobKnowledgeCompile]
	path      string
	vault     *knowledge.Vault
	project   string
	scope     string
	mu        sync.Mutex
	offset    int64
	skip      int
	lastError string
	// read is the journal as a scan that reached its end found it: a look
	// that finds the file the same has nothing to read ([Agent.scanKnowledge]).
	read   knowledgeJournalStamp
	cancel context.CancelFunc
	done   chan struct{}
}

// knowledgeJournalStamp is what a stat says of a journal: its size and when it
// last changed.
type knowledgeJournalStamp struct {
	size int64
	mod  time.Time
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
	a.knowledgeVault = knowledge.Open(config.ProfilePath(a.config.ProfileDir, "")).WithCalls(a.knowledgeCallContext)
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
	j := &knowledgeJournal{path: path, ctx: ctx, cancel: cancel, done: make(chan struct{}), vault: vault, project: a.config.Workspace}
	a.knowledgeMu.Lock()
	a.knowledgeJournal = j
	a.knowledgeMu.Unlock()
	sessionID := a.id
	if a.jobBudgetOf() != nil {
		// The marker survives a restart: recovery must not turn a stage into an
		// independent source of spending after its job has stopped.
		marker := "turn-owned\n"
		if named, ok := a.jobBudgetOf().(JobLedgerName); ok {
			marker = "job:" + named.FactoryJob() + "\n"
		}
		if err := os.WriteFile(path+".knowledge-job", []byte(marker), 0600); err != nil {
			j.lastError = err.Error()
		}
		close(j.done)
		return
	}
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
		// A resumed chat may already have durable lines waiting for capture,
		// and a rebind may have taken over a write whose wake the retired
		// scanner consumed: look now, and once more with fresh consent.
		look(false)
		trail()
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
	a.knowledgeVault = knowledge.Open(config.ProfilePath(a.config.ProfileDir, "")).WithCalls(a.knowledgeCallContext)
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
	started := time.Now()
	limit := a.knowledgeCloseWait
	if limit == 0 {
		limit = knowledgeCloseWait
	}
	ctx, cancel := context.WithDeadline(context.Background(), started.Add(limit))
	defer cancel()
	joinCompiler := a.config.JobContext == nil || a.config.JobContext.Err() != nil
	// Cancel every writer before waiting for any one of them.
	for _, j := range workers {
		j.cancel()
		if work := j.compile.Load(); work != nil && joinCompiler {
			work.cancel()
		}
	}
	wait := func(j *knowledgeJournal, kind string, done <-chan struct{}) {
		// Prefer an already finished writer even after the shared deadline expired.
		select {
		case <-done:
			return
		default:
		}
		select {
		case <-done:
		case <-ctx.Done():
			// Hash identities so a miss is traceable without exposing private paths.
			journal := sha256.Sum256([]byte(j.path))
			profile := sha256.Sum256([]byte(a.config.ProfileDir))
			log.Printf("knowledge: close timeout: writer=%s journal=%x profile=%x waited=%s", kind, journal[:8], profile[:8], time.Since(started))
		}
	}
	for _, j := range workers {
		wait(j, "scanner", j.done)
		// Active jobs retain ownership across stage closes.
		if work := j.compile.Load(); work != nil && joinCompiler {
			wait(j, "compiler", work.done)
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
	// A JOURNAL THAT HAS NOT MOVED SINCE A READ REACHED ITS END HAS NOTHING
	// TO CAPTURE, whatever consent says now: an idle chat's safety tick costs
	// one stat, not an open, a read and a consent answer. The ninth live
	// round's idle desk had dozens of chats doing that every five seconds.
	var stamp knowledgeJournalStamp
	if info, statErr := os.Stat(path); statErr == nil {
		stamp = knowledgeJournalStamp{size: info.Size(), mod: info.ModTime()}
		if j.offset == stamp.size && j.read == stamp {
			return nil
		}
	}
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
		if hook := knowledgeReadHook.Load(); hook != nil && *hook != nil {
			(*hook)(path)
		}
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
			source.Job = a.jobBudgetOf() != nil
			if e = j.vault.Capture(source); e != nil {
				return e
			}
		}
		j.offset = batch.Cursor.Offset
		j.skip = batch.Cursor.Skip
		j.scope = batch.Cursor.Scope
		// The stamp was taken before the read, so a write that landed during
		// it changes the file from the stamp and the next look reads again.
		if stamp.size != 0 && j.offset == stamp.size {
			j.read = stamp
		}
	}
	return nil
}
