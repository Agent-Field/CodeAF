package session

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
)

const usagePendingSuffix = ".pending-v1.jsonl"
const usageRetryCap = time.Second

// Open the journal only after locking the current ledger generation.
// No append descriptor survives a compaction rename.
type usagePending struct {
	mu       sync.Mutex
	rows     uint64
	syncing  bool
	wake     chan struct{}
	synced   chan struct{}
	ready    bool
	retrying bool
	logged   map[string]bool
	// appendFile is a filesystem seam for persistent disk failures.
	appendFile func(*os.File, []byte) error
	syncFile   func(*os.File) error
}

var usagePendingPaths sync.Map

func pendingUsage(path string) *usagePending {
	value, _ := usagePendingPaths.LoadOrStore(path, &usagePending{})
	return value.(*usagePending)
}

func (p *usagePending) failed(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.logged == nil {
		p.logged = map[string]bool{}
	}
	if !p.logged[id] {
		p.logged[id] = true
		// Neither row contents nor filesystem errors belong in accounting logs.
		digest := sha256.Sum256([]byte(id))
		log.Printf("usage: accounting pending identity %x; retaining for retry", digest[:8])
	}
}

func nextUsageRetryDelay(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 10 * time.Millisecond
	}
	return min(2*delay, usageRetryCap)
}

// RecoverUsage replays exact rows before chat reconciliation reads booked money.
// Failures leave the source intact and never become chat failures.
func RecoverUsage(path string) { recoverUsage(path, true) }

func prepareUsage(path string) { recoverUsage(path, false) }

func recoverUsage(path string, force bool) {
	if strings.TrimSpace(path) == "" {
		return
	}
	p := pendingUsage(path)
	p.mu.Lock()
	if !force && (p.ready || p.retrying) {
		p.mu.Unlock()
		return
	}
	err := p.recover(path)
	p.ready = err == nil
	if err != nil && !p.retrying {
		p.retrying = true
		go p.retryRecovery(path)
	}
	p.mu.Unlock()
	if err != nil {
		p.failed("recovery")
	}
}

func (p *usagePending) retryRecovery(path string) {
	delay := time.Duration(0)
	for {
		delay = nextUsageRetryDelay(delay)
		time.Sleep(delay)
		p.mu.Lock()
		if !p.retrying || p.ready {
			p.retrying = false
			p.mu.Unlock()
			return
		}
		err := p.recover(path)
		if err == nil {
			p.ready = true
			p.retrying = false
		}
		p.mu.Unlock()
		if err == nil {
			return
		}
	}
}

func syncUsageDir(path string) error {
	f, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// withUsageLedgerLock validates the ledger generation before touching its sidecar.
func withUsageLedgerLock(path string, exclusive bool, fn func(*os.File) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	for {
		f, err := openUsageCreated(path, os.O_RDWR)
		if err != nil {
			return err
		}
		err = filelock.Lock(f, exclusive, false)
		if err != nil {
			f.Close()
			return err
		}
		info, e := f.Stat()
		current, e2 := os.Stat(path)
		if e == nil && e2 == nil && !os.SameFile(info, current) {
			filelock.Unlock(f)
			f.Close()
			continue
		}
		if e != nil {
			err = e
		} else if e2 != nil {
			err = e2
		} else {
			err = fn(f)
		}
		filelock.Unlock(f)
		f.Close()
		return err
	}
}

func repairUsagePending(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	end := bytes.LastIndexByte(raw, '\n') + 1
	if end == len(raw) {
		return raw, nil
	}
	// A torn tail was never acknowledged. Preserve it before repairing append position.
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".torn-*")
	if err != nil {
		return nil, err
	}
	_, err = f.Write(raw[end:])
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	if err = syncUsageDir(path); err != nil {
		return nil, err
	}
	f, err = os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	err = f.Truncate(int64(end))
	if err == nil {
		err = f.Sync()
	}
	closeErr = f.Close()
	if err == nil {
		err = closeErr
	}
	return raw[:end], err
}

// Creation and rename need directory durability; ordinary appends do not.
func openUsageCreated(path string, flags int) (*os.File, error) {
	f := openUsageLedger(path, os.O_CREATE|os.O_EXCL|flags)
	if f == nil {
		f = openUsageLedger(path, flags)
		if f == nil {
			return nil, errors.New("open usage accounting file")
		}
		return f, nil
	}
	if err := syncUsageDir(path); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

const usageSyncInterval = 100 * time.Millisecond
const usageSyncRows = 64

// append reports whether the owner needs to start a new sync burst for the row.
func (p *usagePending) append(path string, row usageWrite) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	err := withUsageLedgerLock(path, false, func(_ *os.File) error {
		f, err := openUsageCreated(path+usagePendingSuffix, os.O_WRONLY)
		if err != nil {
			return err
		}
		if p.appendFile != nil {
			err = p.appendFile(f, row.line)
		} else {
			var n int
			n, err = f.Write(row.line)
			if err == nil && n != len(row.line) {
				err = io.ErrShortWrite
			}
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		return err
	})
	if err != nil {
		return false, err
	}
	p.rows++
	if p.syncing && p.rows >= usageSyncRows {
		select {
		case p.wake <- struct{}{}:
		default:
		}
	}
	return !p.syncing, nil
}

// Only the process-owned writer starts a synchronizer. A caller can record
// inside a shorter-lived fake-clock bubble without lending it channels to close.
func (p *usagePending) startSync(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.syncing || p.rows == 0 {
		return
	}
	p.syncing = true
	p.wake = make(chan struct{}, 1)
	p.synced = make(chan struct{})
	go p.syncBatches(path, p.wake, p.synced)
	if p.rows >= usageSyncRows {
		p.wake <- struct{}{}
	}
}

// A clean close joins the final sync before replay can compact the journal.
func (p *usagePending) finishSync() {
	p.mu.Lock()
	if !p.syncing {
		p.mu.Unlock()
		return
	}
	done := p.synced
	select {
	case p.wake <- struct{}{}:
	default:
	}
	p.mu.Unlock()
	<-done
}

func (p *usagePending) syncJournal(f *os.File) error {
	if p.syncFile != nil {
		return p.syncFile(f)
	}
	return f.Sync()
}

// Sync outside the admission mutex, so a slow disk never holds up an appender.
// The shared ledger lock keeps the journal generation alive during the sync.
func (p *usagePending) syncBatches(path string, wake <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	timer := time.NewTimer(usageSyncInterval)
	defer timer.Stop()
	backoff := time.Duration(0)
	for {
		if backoff > 0 {
			<-timer.C
		} else {
			select {
			case <-timer.C:
			case <-wake:
			}
		}
		p.mu.Lock()
		rows := p.rows
		select {
		case <-wake:
		default:
		}
		p.mu.Unlock()
		started := time.Now()
		err := withUsageLedgerLock(path, false, func(_ *os.File) error {
			f, err := os.OpenFile(path+usagePendingSuffix, os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			defer f.Close()
			return p.syncJournal(f)
		})
		p.mu.Lock()
		if err == nil {
			p.rows -= rows
		}
		if p.rows == 0 {
			p.syncing = false
			p.mu.Unlock()
			return
		}
		p.mu.Unlock()
		delay := max(time.Duration(0), time.Until(started.Add(usageSyncInterval)))
		if err != nil {
			p.failed("journal-sync")
			backoff = nextUsageRetryDelay(backoff)
			delay = backoff
		} else {
			backoff = 0
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(delay)
	}
}

func (p *usagePending) recover(path string) error {
	journal := path + usagePendingSuffix
	if _, err := os.Stat(journal); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return withUsageLedgerLock(path, true, func(ledger *os.File) error {
		f, err := os.OpenFile(journal, os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		err = p.syncJournal(f)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		raw, err := repairUsagePending(journal)
		if err != nil {
			return err
		}
		var rows []usageWrite
		var keep bytes.Buffer
		for _, line := range bytes.SplitAfter(raw, []byte{'\n'}) {
			if len(line) == 0 {
				continue
			}
			var row UsageLine
			decoder := json.NewDecoder(bytes.NewReader(line))
			decoder.DisallowUnknownFields()
			if len(line) > usageLineBytes || decoder.Decode(&row) != nil || row.At.IsZero() || row.ChargeID() == "" {
				keep.Write(line)
				// Preserve unknown records without exposing their payload in logs.
				if p.logged == nil {
					p.logged = map[string]bool{}
				}
				if !p.logged["malformed"] {
					log.Print("usage: unknown pending record retained")
					p.logged["malformed"] = true
				}
				continue
			}
			rows = append(rows, usageWrite{id: row.ChargeID(), line: line})
		}
		var cursor usageIdentityCursor
		if _, err = cursor.appendLocked(ledger, rows); err != nil {
			return err
		}
		// Even duplicate rows must be synced before their journal copies can be removed.
		if err = ledger.Sync(); err != nil {
			return err
		}
		return rewriteUsagePending(journal, keep.Bytes())
	})
}

func rewriteUsagePending(path string, raw []byte) error {
	stale, err := filepath.Glob(path + ".tmp-*")
	if err != nil {
		return err
	}
	for _, name := range stale {
		if err = os.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = fsreplace.Rename(name, path); err != nil {
		return fmt.Errorf("compact usage journal: %w", err)
	}
	return syncUsageDir(path)
}
