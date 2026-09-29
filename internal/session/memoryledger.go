package session

// memoryledger.go keeps the memories a conversation wrote as truth in its own
// folder. A memory is said by a person or the agent and by nothing else, so no
// transcript can restate it; graph.db holds it as an event and would lose it
// with the file. The ledger is .cell/memories.jsonl: one line per committed
// memory event, appended by one writer, sealed with the workspace. graph.db is
// the index of it, and internal/cellindex reads it back when the index is gone.
//
// A memory whose event names no conversation (a person's edit in the memory
// place, a ranking snapshot) has no folder to belong to and stays in graph.db
// alone: memories are shared by the whole machine, and only what a session
// supplied is sealed with that session.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/Agent-Field/codeaf/internal/store"
)

const (
	placeMemories       = "memories.jsonl"
	memoryLedgerVersion = 1
)

// memoryRow is one line of the ledger.
type memoryRow struct {
	V int `json:"v"`
	store.MemoryEvent
}

// CellMemories is the process's ledger: the store is given it once, and each
// conversation binds itself to it when it opens.
var CellMemories = &MemoryLedger{}

// MemoryLedger implements [store.MemoryLedger] over session folders.
type MemoryLedger struct {
	mu   sync.Mutex
	dirs map[string]string
}

// Bind says where the conversation named id keeps its folder.
func (l *MemoryLedger) Bind(id, dir string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.dirs == nil {
		l.dirs = map[string]string{}
	}
	l.dirs[id] = dir
}

// Record appends the event to its conversation's ledger. An event of no bound
// conversation is not this ledger's to keep.
func (l *MemoryLedger) Record(e store.MemoryEvent) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	dir, ok := l.dirs[e.Session]
	if !ok {
		return nil
	}
	return appendMemoryRow(memoryLedgerPath(dir), memoryRow{memoryLedgerVersion, e})
}

func memoryLedgerPath(dir string) string { return truthPath(dir, placeMemories) }

func appendMemoryRow(path string, row memoryRow) error {
	line, err := json.Marshal(row)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("seal memory: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("seal memory: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("seal memory: %w", err)
	}
	return f.Close()
}

// ReadMemoryEvents is what the folder's ledger holds, oldest first. A folder
// with no ledger holds none.
func ReadMemoryEvents(dir string) ([]store.MemoryEvent, error) {
	f, err := os.Open(memoryLedgerPath(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []store.MemoryEvent
	scan := bufio.NewScanner(f)
	scan.Buffer(nil, 1<<20)
	for scan.Scan() {
		var row memoryRow
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil || row.V != memoryLedgerVersion {
			continue // a torn or foreign line is not a memory
		}
		out = append(out, row.MemoryEvent)
	}
	return out, scan.Err()
}

// SealMemories gives a folder with no ledger the one its graph owes it: every
// memory event the journal attributes to the conversation, so a chat that
// remembered before ledgers existed keeps what it said. A folder that has a
// ledger is left as it is.
func SealMemories(dir string, brain *store.Store) error {
	path := memoryLedgerPath(dir)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	events, err := brain.MemoryEventsOf(filepath.Base(dir))
	if err != nil {
		return err
	}
	return writeMemoryLedger(path, events)
}

// writeMemoryLedger writes the whole ledger at once and moves it into place, so
// a crash leaves either no ledger (the next open seals again) or all of it.
func writeMemoryLedger(path string, events []store.MemoryEvent) error {
	tmp := path + ".tmp"
	_ = os.Remove(tmp)
	for _, e := range events {
		if err := appendMemoryRow(tmp, memoryRow{memoryLedgerVersion, e}); err != nil {
			return err
		}
	}
	if len(events) == 0 {
		if err := os.WriteFile(tmp, nil, 0o600); err != nil {
			return err
		}
	}
	return os.Rename(tmp, path)
}

// bindMemoryLedger points the process ledger at this conversation's folder. A
// session with no folder (the zero Place) has none to seal into.
func bindMemoryLedger(p Place) {
	if p.Dir != "" {
		CellMemories.Bind(p.ID(), p.Dir)
	}
}
