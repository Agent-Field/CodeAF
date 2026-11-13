package cellsync

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
)

// TestAFreshLedgerDoesNotResendWhatTheStoreHolds is the rotation case in its
// general form: a device that never published to a store, and so has an empty
// ledger for it, publishes a small turn on top of a chat the store already
// holds. Only the turn may cross the wire, and the held objects must be
// recorded so the turn after it does not ask again.
func TestAFreshLedgerDoesNotResendWhatTheStoreHolds(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	chat := map[string]string{"chat": strings.Repeat("a long conversation ", 1<<16)} // about 1.25 MiB
	r.publishFirst(chat)

	counters := &blobstore.Counters{}
	c := cell.Cell{ID: cellID, Root: t.TempDir()}
	eng := NewFakeEngine(t.TempDir())
	pub := &Publisher{Engine: eng, Store: blobstore.Counting{Inner: r.mem, C: counters}}
	eng.Seal(c, chat)
	turn := map[string]string{"chat": chat["chat"], "turn": "one small turn"}
	head := eng.Seal(c, turn)

	whole, err := eng.Ledger(t.TempDir()).Export(ctx, c, head)
	if err != nil {
		t.Fatal(err)
	}
	ex, err := pub.Upload(ctx, c, head)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("bytes sent: %d before, %d after; has requests %d", whole.Bytes, counters.BytesUp.Load(), counters.Has.Load())
	if got := counters.BytesUp.Load(); got > askFloor || got != ex.Bytes {
		t.Fatalf("a fresh ledger sent %d bytes for a small turn (reported %d)", got, ex.Bytes)
	}
	again, err := eng.Export(ctx, c, head)
	if err != nil || again.Objects != 0 {
		t.Fatalf("after the publish the ledger still lacks %d objects (%v)", again.Objects, err)
	}
}

// A new relay holds no objects. Recording a held subset would add a native
// bookkeeping round trip and an avoidable error before the ordinary upload.
func TestEmptyRelaySkipsHeldObjectBookkeeping(t *testing.T) {
	ctx := context.Background()
	c := cell.Cell{ID: cellID, Root: t.TempDir()}
	engine := NewFakeEngine(t.TempDir())
	checked := &nonemptyPublishedEngine{Engine: engine}
	store := blobstore.NewMemory()
	publisher := Publisher{Engine: checked, Store: store}
	head := engine.Seal(c, map[string]string{"mock.txt": strings.Repeat("mock fresh relay content\n", 8192)})
	exported, err := publisher.Upload(ctx, c, head)
	if err != nil {
		t.Fatal(err)
	}
	if exported.Bytes < askFloor {
		t.Fatal("fixture does not exercise the holdings question")
	}
	if checked.emptyRecords != 0 {
		t.Fatalf("new relay caused %d empty native bookkeeping calls", checked.emptyRecords)
	}
	again, err := engine.Export(ctx, c, head)
	if err != nil || again.Objects != 0 {
		t.Fatalf("uploaded objects not recorded: %d, %v", again.Objects, err)
	}
}

type nonemptyPublishedEngine struct {
	Engine
	emptyRecords int
}

func (e *nonemptyPublishedEngine) Published(ctx context.Context, c cell.Cell, paths []string) error {
	if len(paths) == 0 {
		e.emptyRecords++
		return errors.New("native engine requires at least one published frame")
	}
	return e.Engine.Published(ctx, c, paths)
}
