package syncsetup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

// A turn ends and is sealed. Then the title is written, an aux call is journaled
// and the model keeps a memory: all after the turn-end seal. The chat B takes
// holds all of it, because the watch seals the tail once the writes stop and
// sends it through the same upload as every other seal.
func TestTakeCarriesWhatWasWrittenAfterTheTurnEndSeal(t *testing.T) {
	ctx := context.Background()
	h := newTwoHomes(t)
	seedTree(t, h.work)
	a := h.openA()
	watch := &cellstore.SealWatch{Quiet: 20 * time.Millisecond, OnTail: func() {
		executor.Settle(ctx, a.seat)
		a.drive.Idle()
	}}
	session.CellMemories.Bind(h.cell.ID, h.cell.Root, watch.Wrote)

	a.mustSay("the answer")
	executor.Settle(ctx, a.seat) // the turn-end seal
	watch.TurnEnded()

	sealed, _ := cellstore.Turns(h.cell)
	appendTo(t, transcriptOf(h.cell), `{"type":"message","role":"assistant","content":"title: tax rates"}`+"\n")
	watch.Wrote()
	err := session.CellMemories.Record(store.LedgerMemoryEvent{Session: h.cell.ID, Time: time.Now(), ID: "mem_late",
		Kind: store.EventMemoryAdd, Payload: []byte(`{"text":"kept after the turn"}`)})
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, "the tail seal to reach the directory", func() bool {
		turns, _ := cellstore.Turns(h.cell)
		head := h.directoryHead(h.cell.ID)
		return len(turns) > len(sealed) && head == headOf(t, h.cell)
	})
	got := h.takeOnB(a)
	log, err := os.ReadFile(transcriptOf(got.Taken.Cell))
	if err != nil || !strings.Contains(string(log), "title: tax rates") {
		t.Fatalf("B lacks the line written after the turn-end seal (%v):\n%s", err, log)
	}
	rows, err := os.ReadFile(filepath.Join(got.Taken.Cell.Root, ".cell", "memories.jsonl"))
	if err != nil || !strings.Contains(string(rows), "mem_late") {
		t.Fatalf("B lacks the memory written after the turn-end seal (%v): %s", err, rows)
	}
}
