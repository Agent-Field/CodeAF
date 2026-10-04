package cellindex

import (
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

// memoryIndex is this session's share of the machine's graph.db: the memories
// its ledger (.cell/memories.jsonl) says it wrote. The ledger is the truth and
// the graph is the index of it, so a graph that lost them is read back from it.
type memoryIndex struct{}

func (memoryIndex) Name() string { return "graph.db memories" }

// Present reports whether the graph holds every memory the ledger brought into
// being. A session with no ledger, or nothing in it, owes the graph nothing.
func (memoryIndex) Present(c cell.Cell) bool {
	events, err := session.ReadMemoryEvents(c.Root)
	if err != nil || len(events) == 0 {
		return true
	}
	brain, err := store.Open(home.Join("graph.db"))
	if err != nil {
		return true // an unreadable graph is not ours to write into
	}
	defer brain.Close()
	return holdsEvery(brain, events)
}

func (memoryIndex) Build(c cell.Cell, _ session.Digest) error {
	events, err := session.ReadMemoryEvents(c.Root)
	if err != nil {
		return err
	}
	brain, err := store.Open(home.Join("graph.db"))
	if err != nil {
		return err
	}
	defer brain.Close()
	return brain.ImportMemoryEvents(events)
}

// Behind reports whether the ledger holds a memory event the graph never saw: a
// write of a newer copy, such as an edit of a memory both copies had.
func (memoryIndex) Behind(c cell.Cell, _ session.Digest) bool {
	events, err := session.ReadMemoryEvents(c.Root)
	if err != nil || len(events) == 0 {
		return false
	}
	brain, err := store.Open(home.Join("graph.db"))
	if err != nil {
		return false
	}
	defer brain.Close()
	for _, e := range events {
		if held, err := brain.HoldsMemoryEvent(e); err == nil && !held {
			return true
		}
	}
	return false
}

func holdsEvery(brain *store.Store, events []store.LedgerMemoryEvent) bool {
	for _, e := range events {
		if !e.Creates() {
			continue
		}
		if _, found, err := brain.MemoryRecord(e.ID); err != nil || !found {
			return false
		}
	}
	return true
}
