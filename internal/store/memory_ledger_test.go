package store

import (
	"testing"
)

type capturedMemoryLedger struct{ events []LedgerMemoryEvent }

func (l *capturedMemoryLedger) Record(event LedgerMemoryEvent) error {
	l.events = append(l.events, event)
	return nil
}

// Current writes keep their transaction's duplicate and room rules while the
// admitted event travels with the conversation that supplied it.
func TestCellMemoryLedgerKeepsTheCurrentWriteRules(t *testing.T) {
	s := memoryStore(t)
	ledger := &capturedMemoryLedger{}
	s.SetMemoryLedger(ledger)
	req := WriteRequest{Owner: OwnerUser, Text: "use tabs", SourceSession: "chat"}
	kept := mustWrite(t, s, req)
	if _, err := s.Write(req); err != nil {
		t.Fatal(err)
	}
	if len(ledger.events) != 1 || ledger.events[0].ID != kept.ID || ledger.events[0].Session != "chat" {
		t.Fatalf("admitted and duplicate writes sealed %+v, want one attributed add", ledger.events)
	}
	other := memoryStore(t)
	if err := other.ImportMemoryEvents(ledger.events); err != nil {
		t.Fatal(err)
	}
	if got, found, err := other.MemoryRecord(kept.ID); err != nil || !found || got.Text != req.Text || got.Owner != req.Owner {
		t.Fatalf("transferred memory = %+v, found %v, err %v", got, found, err)
	}
	// Storage is unbounded and the prompt budget is applied at assembly, so a
	// write past the block's size is admitted and travels like any other add.
	fillTo(t, s, OwnerUser, "", "full", MemoryBlockRunes)
	before := len(ledger.events)
	added, err := s.Write(WriteRequest{Owner: OwnerUser, Text: "another line", SourceSession: "chat"})
	if err != nil {
		t.Fatalf("write past the block's size = %v, want it admitted", err)
	}
	if len(ledger.events) != before+1 || ledger.events[before].ID != added.Memory.ID {
		t.Fatalf("an admitted write past the block's size sealed %+v, want its one add", ledger.events[before:])
	}
}

// Replacing a line with words already kept forgets the old line on dev. Its
// forget event must travel too, or the other computer keeps both lines.
func TestCellMemoryLedgerCarriesADuplicateReplacement(t *testing.T) {
	s := memoryStore(t)
	ledger := &capturedMemoryLedger{}
	s.SetMemoryLedger(ledger)
	old := mustWrite(t, s, WriteRequest{Owner: OwnerUser, Text: "old wording", SourceSession: "chat"})
	kept := mustWrite(t, s, WriteRequest{Owner: OwnerUser, Text: "current wording", SourceSession: "chat"})
	got, added, err := s.SupersedeMemoryForOwners([]string{OwnerUser}, old.ID, Memory{Owner: OwnerUser, Text: kept.Text, SourceSession: "chat"})
	if err != nil || added || got.ID != kept.ID {
		t.Fatalf("replacement = %+v, added %v, err %v", got, added, err)
	}
	if len(ledger.events) != 3 || ledger.events[2].Kind != EventMemoryForget || ledger.events[2].ID != old.ID {
		t.Fatalf("replacement sealed %+v, want the old line's forget", ledger.events)
	}
	other := memoryStore(t)
	if err := other.ImportMemoryEvents(ledger.events); err != nil {
		t.Fatal(err)
	}
	rows, err := other.MemoriesFor([]string{OwnerUser})
	if err != nil || len(rows) != 1 || rows[0].ID != kept.ID {
		t.Fatalf("transferred active lines = %+v, err %v", rows, err)
	}
}
