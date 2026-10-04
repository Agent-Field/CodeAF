package cellstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// TestExportPacksAFlushIntoFewFrames pins that one export carries every unsent
// object of a burst of tool calls in one frame, over every way the engine is
// reached. A request that says "no frame size" must mean the engine's default,
// because a size of zero would give each object a frame of its own and turn
// every object into a paid put at the relay.
func TestExportPacksAFlushIntoFewFrames(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		e := realEngine(t)
		c := newCell(t)
		var head string
		for i := range 12 {
			name := filepath.Join(c.Root, fmt.Sprintf("f%02d.txt", i))
			if err := os.WriteFile(name, []byte(fmt.Sprintf("body %d", i)), 0o600); err != nil {
				t.Fatal(err)
			}
			sealed, err := e.Seal(context.Background(), c, TurnInfo{})
			if err != nil {
				t.Fatal(err)
			}
			head = sealed.Turn.ID
		}
		sync := e.Sync(SyncKeys{CellKey: make([]byte, 32), Dedup: make([]byte, 32)}, LedgerName("http://relay", "id_test"))
		sync.Outbox = func(_ cell.Cell) string { return t.TempDir() }
		out, err := sync.Export(context.Background(), c, head)
		if err != nil {
			t.Fatal(err)
		}
		if out.Objects < 12 || len(out.Frames) != 1 {
			t.Fatalf("export of %d objects made %d frames, want 1", out.Objects, len(out.Frames))
		}
	})
}
