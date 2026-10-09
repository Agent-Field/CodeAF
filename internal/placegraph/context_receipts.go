package placegraph

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const contextReceiptByteLimit = 16 * 1024

type contextReceipt struct {
	ID     string `json:"id"`
	Before uint64 `json:"beforeRevision"`
	After  uint64 `json:"afterRevision"`
}

func validContextReceipt(rc contextReceipt) bool {
	if !strings.HasPrefix(rc.ID, "rc_") || len(rc.ID) > 128 || len(rc.ID) <= 3 || rc.After <= rc.Before || rc.After-rc.Before != 1 {
		return false
	}
	for _, c := range rc.ID {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func readContextReceipts(path string) []contextReceipt {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, contextReceiptByteLimit+1))
	if err != nil || len(data) > contextReceiptByteLimit {
		return nil
	}
	var receipts []contextReceipt
	if json.Unmarshal(data, &receipts) != nil || len(receipts) > MaxUndo {
		return nil
	}
	ids := map[string]bool{}
	for _, rc := range receipts {
		if !validContextReceipt(rc) || ids[rc.ID] {
			return nil
		}
		ids[rc.ID] = true
	}
	return receipts
}

// ContextUndoReceipt names one exact committed mutation, never the latest one.
// Bounded minimal provenance crosses the hosted-session process boundary; the
// running store still validates execution, with no persisted undo snapshots.
func ContextUndoReceipt(path string, before, after uint64) []string {
	var matched []string
	for _, rc := range readContextReceipts(path + ".context-receipts.json") {
		if rc.Before == before && rc.After == after {
			matched = append(matched, rc.ID)
		}
	}
	if len(matched) == 1 {
		return matched
	}
	return nil
}

// Called under the graph file lock. Missing/corrupt/oversize provenance is safe:
// notes omit Undo and a saved graph never becomes a failed graph commit.
func (s *Store) recordContextReceipt(rc Receipt) {
	record := contextReceipt{rc.ID, rc.BeforeRevision, rc.AfterRevision}
	if !validContextReceipt(record) {
		return
	}
	path := s.opts.Path + ".context-receipts.json"
	receipts := append(readContextReceipts(path), record)
	if len(receipts) > MaxUndo {
		receipts = receipts[len(receipts)-MaxUndo:]
	}
	data, err := json.Marshal(receipts)
	if err != nil || len(data) > contextReceiptByteLimit {
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
