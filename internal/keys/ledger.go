package keys

import (
	"io/fs"
	"os"
	"sync"
)

// stamp is what tells a file is the one already judged.
type stamp struct {
	size, mtime int64
	info        fs.FileInfo
}

// Ledger remembers the files a scan found clean. A file is looked at again when
// its identity, permissions, size or modification time is not the one noted.
// The zero value and a nil Ledger remember
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
	// Editors often replace a file atomically while preserving its timestamp.
	// Size and time alone must not clear the replacement without reading it.
	// Loading identity now also fixes the identity before a later Windows lookup.
	if !os.SameFile(info, info) {
		return stamp{}, false
	}
	st := stamp{size: info.Size(), mtime: info.ModTime().UnixNano(), info: info}
	l.mu.Lock()
	defer l.mu.Unlock()
	before, ok := l.clean[abs]
	return st, ok && before.size == st.size && before.mtime == st.mtime && before.info.Mode() == st.info.Mode() && os.SameFile(before.info, st.info)
}

func (l *Ledger) note(abs string, st stamp) {
	if l == nil || st == (stamp{}) {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.clean[abs] = st
}
