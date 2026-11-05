package session

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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

// A journal is opened under the ledger lock each time because compaction renames it.
type usagePending struct {
	mu       sync.Mutex
	ready    bool
	retrying bool
	logged   map[string]bool
	// appendFile is a filesystem seam for persistent disk failures.
	appendFile func(*os.File, []byte) error
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

func usageRetryDelay(attempt int) time.Duration {
	return min(10*time.Millisecond<<min(attempt, 7), usageRetryCap)
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
	for attempt := 0; ; attempt++ {
		time.Sleep(usageRetryDelay(attempt))
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

// withUsageLedger retries replacement before touching the sidecar of that generation.
func withUsageLedger(path string, fn func(*os.File) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0600)
		if err != nil {
			return err
		}
		err = filelock.Lock(f, true, false)
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

// Ordinary appends inspect only the final byte; startup reads the full backlog.
func repairUsagePendingTail(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err == nil && info.Size() > 0 {
		var last [1]byte
		_, err = f.ReadAt(last[:], info.Size()-1)
		if err == nil && last[0] != '\n' {
			f.Close()
			_, err = repairUsagePending(path)
			return err
		}
	}
	f.Close()
	return err
}

func (p *usagePending) append(path string, row usageWrite) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return withUsageLedger(path, func(_ *os.File) error {
		journal := path + usagePendingSuffix
		if err := repairUsagePendingTail(journal); err != nil {
			return err
		}
		f, err := os.OpenFile(journal, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		if p.appendFile != nil {
			err = p.appendFile(f, row.line)
		} else {
			_, err = f.Write(row.line)
			if err == nil {
				err = f.Sync()
			}
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		// Syncing every time also covers a prior failed creation-directory sync.
		return syncUsageDir(journal)
	})
}

func (p *usagePending) recover(path string) error {
	journal := path + usagePendingSuffix
	if _, err := os.Stat(journal); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return withUsageLedger(path, func(ledger *os.File) error {
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
		if err = syncUsageDir(path); err != nil {
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
