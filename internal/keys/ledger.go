package keys

import (
	"io/fs"
	"sync"
)

// stamp is what tells a file is the one already judged.
type stamp struct{ size, mtime int64 }

// Ledger remembers the files a scan found clean. A file is looked at again when
// its size or modification time is not the one noted, which is the test the
// engine's own stat cache uses. The zero value and a nil Ledger remember
// nothing; a Ledger is safe for concurrent use.
type Ledger struct {
	mu    sync.Mutex
	clean map[string]stamp
}

// NewLedger returns an empty ledger.
func NewLedger() *Ledger { return &Ledger{clean: map[string]stamp{}} }

// known returns the file's stamp and whether the ledger holds it clean.
func (l *Ledger) known(abs string, d fs.DirEntry) (stamp, bool) {
	if l == nil {
		return stamp{}, false
	}
	info, err := d.Info()
	if err != nil {
		return stamp{}, false
	}
	st := stamp{info.Size(), info.ModTime().UnixNano()}
	l.mu.Lock()
	defer l.mu.Unlock()
	return st, l.clean[abs] == st
}

func (l *Ledger) note(abs string, st stamp) {
	if l == nil || st == (stamp{}) {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.clean[abs] = st
}
