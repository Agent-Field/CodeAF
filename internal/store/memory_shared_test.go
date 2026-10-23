package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestC30LaterMemoryEditWinsEvenWhenOlderArrivesLast proves C30.2: ordering is
// per line and by edit time; a delayed edit/forget cannot undo newer words.
func TestC30LaterMemoryEditWinsEvenWhenOlderArrivesLast(t *testing.T) {
	brain := memoryStore(t)
	m, err := brain.AddMemory(Memory{Owner: OwnerUser, Text: "The initial wording."})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(time.Hour)
	event := func(at time.Time, text string) LedgerMemoryEvent {
		raw, _ := json.Marshal(memoryUpdatePayload{ID: m.ID, Text: text, Title: MemoryTitleOf(text)})
		return LedgerMemoryEvent{ID: m.ID, Kind: EventMemoryUpdate, Time: at, Payload: raw}
	}
	later := event(base.Add(time.Second), "The later wording.")
	earlier := event(base, "The earlier wording.")
	if err := brain.ImportSharedMemoryEvents([]LedgerMemoryEvent{later}); err != nil {
		t.Fatal(err)
	}
	if err := brain.ImportSharedMemoryEvents([]LedgerMemoryEvent{earlier}); err != nil {
		t.Fatal(err)
	}
	check := func() {
		got, found, err := brain.MemoryRecord(m.ID)
		if err != nil || !found || got.Text != "The later wording." {
			t.Fatalf("older arrival won: %+v %v", got, err)
		}
	}
	check()
	if err := brain.Rebuild(); err != nil {
		t.Fatal(err)
	}
	check()
}

// TestC30LaterEditOfAReplacedLineWins proves C30.2 for two offline computers:
// replacing a line must not defeat a later edit merely by changing its id.
func TestC30LaterEditOfAReplacedLineWins(t *testing.T) {
	a, b := memoryStore(t), memoryStore(t)
	m, err := a.AddMemory(Memory{Owner: OwnerUser, Text: "Initial shared line."})
	if err != nil {
		t.Fatal(err)
	}
	events, err := a.SharedMemoryEvents()
	if err != nil {
		t.Fatal(err)
	}
	if err := b.ImportSharedMemoryEvents(events); err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(time.Hour)
	update, _ := json.Marshal(memoryUpdatePayload{ID: m.ID, Text: "Later offline edit.", Title: "Later offline edit"})
	later := LedgerMemoryEvent{ID: m.ID, Kind: EventMemoryUpdate, Time: base.Add(time.Second), Payload: update}
	replacement := Memory{ID: NewMemoryID(), Owner: OwnerUser, Text: "Earlier offline replacement."}
	fresh, err := memoryPayloadFrom(replacement)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(memorySupersedePayload{OldID: m.ID, New: fresh})
	earlier := LedgerMemoryEvent{ID: fresh.ID, Kind: EventMemorySupersede, Time: base, Payload: raw}
	if err := a.ImportSharedMemoryEvents([]LedgerMemoryEvent{later}); err != nil {
		t.Fatal(err)
	}
	if err := b.ImportSharedMemoryEvents([]LedgerMemoryEvent{earlier}); err != nil {
		t.Fatal(err)
	}
	if err := a.ImportSharedMemoryEvents([]LedgerMemoryEvent{earlier}); err != nil {
		t.Fatal(err)
	}
	if err := b.ImportSharedMemoryEvents([]LedgerMemoryEvent{later}); err != nil {
		t.Fatal(err)
	}
	for _, brain := range []*Store{a, b} {
		for i := 0; i < 2; i++ {
			lines, err := brain.MemoriesFor([]string{OwnerUser})
			if err != nil || len(lines) != 1 || lines[0].Text != "Later offline edit." {
				t.Fatalf("same-line conflict lost later words: %+v %v", lines, err)
			}
			if err := brain.Rebuild(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestC30IdleMemoryCaptureOnlyReads proves C30.2/C30.4: an idle sharing round
// reads committed memory without creating an empty brain or migrating the
// existing database. No sleep is needed to distinguish a changed file clock.
func TestC30IdleMemoryCaptureOnlyReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory #1.db")
	if events, err := SharedMemoryEventsAt(path); err != nil || len(events) != 0 {
		t.Fatalf("missing brain: %v %v", events, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("idle capture created a brain")
	}
	brain, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := brain.AddMemory(Memory{Owner: OwnerUser, Text: "Read this shared line."}); err != nil {
		t.Fatal(err)
	}
	if err := brain.Close(); err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1700000000, 0)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
	events, err := SharedMemoryEventsAt(path)
	if err != nil || len(events) != 1 {
		t.Fatalf("committed memory missing: %v %v", events, err)
	}
	info, err := os.Stat(path)
	if err != nil || !info.ModTime().Equal(at) {
		t.Fatalf("capture rewrote brain: %v %v", info, err)
	}
}
