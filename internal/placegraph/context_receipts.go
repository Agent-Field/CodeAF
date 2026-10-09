package placegraph

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ContextUndoReceipt names one exact committed mutation, never the latest one.
// This provenance file crosses the desktop/hosted-session process boundary. It
// does not persist undo snapshots: the running store still validates authority.
func ContextUndoReceipt(path string, before, after uint64) []string {
	var receipts []Receipt
	data, err := os.ReadFile(path + ".context-receipts.json")
	if err != nil || json.Unmarshal(data, &receipts) != nil {
		return nil
	}
	var matched []string
	for _, rc := range receipts {
		if rc.ID != "" && rc.BeforeRevision == before && rc.AfterRevision == after {
			matched = append(matched, rc.ID)
		}
	}
	if len(matched) == 1 {
		return matched
	}
	return nil
}

// Called under the graph file lock. A missing/corrupt provenance document is
// safe: notes omit Undo, and writes never become failed graph commits.
func (s *Store) recordContextReceipt(rc Receipt) {
	path := s.opts.Path + ".context-receipts.json"
	var receipts []Receipt
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &receipts)
	}
	receipts = append(receipts, rc)
	if len(receipts) > MaxUndo {
		receipts = receipts[len(receipts)-MaxUndo:]
	}
	data, err := json.Marshal(receipts)
	if err != nil {
		return
	}
	f, err := os.CreateTemp(filepath.Dir(s.opts.Path), ".context-receipts-*")
	if err != nil {
		return
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return
	}
	if err = f.Close(); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}
