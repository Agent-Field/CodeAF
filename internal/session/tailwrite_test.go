package session

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/store"
)

// Every line the journal takes is announced, so the seal watch can seal what is
// written after a turn's own seal. The title and the summaries the model writes
// after its answer are lines like any other.
func TestEveryJournalLineIsAnnouncedToTheSealWatch(t *testing.T) {
	journal, _, err := openSessionFile(filepath.Join(t.TempDir(), "s.jsonl"), "/w", "m", "")
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	var heard int
	journal.onWrite = func() { heard++ }

	journal.appendTook("c1", time.Second)
	journal.appendCaption("c1", "ran the tests", "")
	if heard != 2 {
		t.Fatalf("heard %d writes of 2 lines", heard)
	}
}

// A memory the model keeps after the turn is a row of the chat's own ledger,
// and the chat that owns it is told at once.
func TestAMemoryRowIsAnnouncedToTheChatThatOwnsIt(t *testing.T) {
	var ledger MemoryLedger
	var heard int
	ledger.Bind("chat", t.TempDir(), func() { heard++ })

	err := ledger.Record(store.LedgerMemoryEvent{Session: "chat", Time: time.Now(), ID: "mem_1", Kind: store.EventMemoryAdd, Payload: []byte(`{}`)})
	if err != nil || heard != 1 {
		t.Fatalf("recorded with err %v and told %d times", err, heard)
	}
	if err := ledger.Record(store.LedgerMemoryEvent{Session: "another", Kind: store.EventMemoryAdd, Payload: []byte(`{}`)}); err != nil || heard != 1 {
		t.Fatalf("a row of another chat told this one: err %v, told %d", err, heard)
	}
}
