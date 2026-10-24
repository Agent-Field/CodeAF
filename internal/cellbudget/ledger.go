package cellbudget

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

const (
	ledgerFile = "budget.json"
	lockFile   = "budget.lock"
	schemaV    = 1
)

// Pointer says where an evicted cell's working files went.
type Pointer struct {
	Snapshot string `json:"snapshot"`
	AtMs     int64  `json:"at_ms"`
	// Paths are the top-level names eviction removed, and so all a restore writes.
	Paths []string `json:"paths"`
}

// Entry is one cell's device-local ledger. V is first (L11).
type Entry struct {
	V          uint16   `json:"V"`
	Root       string   `json:"root"`
	OpenedMs   int64    `json:"opened_ms"`
	Bytes      int64    `json:"bytes"`
	MeasuredMs int64    `json:"measured_ms"`
	Evicted    *Pointer `json:"evicted,omitempty"`
}

func readEntry(dir string) (Entry, error) {
	raw, err := os.ReadFile(filepath.Join(dir, ledgerFile))
	if errors.Is(err, fs.ErrNotExist) {
		return Entry{V: schemaV}, nil
	}
	if err != nil {
		return Entry{}, err
	}
	var e Entry
	return e, json.Unmarshal(raw, &e)
}

func writeEntry(dir string, e Entry) error {
	e.V = schemaV
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, ledgerFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// errLocked means another process holds the cell's budget lock.
var errLocked = errors.New("locked by another process")

// lock takes the cell's budget lock, the one writer of its ledger and of its
// working files while they are removed or restored. Waiting is for the open
// path, which must not proceed on a half-evicted tree; the sweep never waits.
func lock(dir string, wait bool) (release func(), err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, lockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := filelock.Lock(f, true, !wait); err != nil {
		_ = f.Close()
		if filelock.IsBusy(err) {
			return nil, errLocked
		}
		return nil, err
	}
	return func() { _ = filelock.Unlock(f); _ = f.Close() }, nil
}
