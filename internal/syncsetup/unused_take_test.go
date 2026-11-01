package syncsetup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
)

func TestAnUnusedTakeReleasesOnlyItsOwnHoldAndKeepsItsFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	clock := directorytest.NewFakeClock()
	dir := directory.NewMemory(clock.Now)
	created, err := dir.For("source").Create(t.Context(), "chat", directory.CellInit{Head: "saved", Class: "chat"})
	mustNil(t, err)
	receiver := dir.For("receiver")
	taken, err := receiver.Acquire(t.Context(), "chat", directory.AcquireOpts{Force: true, RequireConditional: true, Expect: &directory.LeaseExpectation{Device: "source", Fence: created.Cell.Lease.Fence}})
	mustNil(t, err)
	root := t.TempDir()
	kept := filepath.Join(root, "kept-work")
	mustNil(t, os.WriteFile(kept, []byte("all the files stay"), 0600))
	c := &Continuer{sync: &Sync{Dir: receiver}}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	mustNil(t, c.ReleaseUnused(ctx, "chat", taken.Cell.Lease.Fence))
	released, err := receiver.Cell(t.Context(), "chat")
	mustNil(t, err)
	if released.Cell.Lease.Expires != 0 {
		t.Fatal("unused acquisition stayed held")
	}
	later, err := receiver.Acquire(t.Context(), "chat", directory.AcquireOpts{RequireConditional: true, Expect: &directory.LeaseExpectation{Device: "receiver", Fence: taken.Cell.Lease.Fence}})
	mustNil(t, err)
	mustNil(t, c.ReleaseUnused(ctx, "chat", taken.Cell.Lease.Fence))
	current, err := receiver.Cell(t.Context(), "chat")
	mustNil(t, err)
	if current.Cell.Lease != later.Cell.Lease {
		t.Fatal("old cleanup released a later writer")
	}
	raw, err := os.ReadFile(kept)
	mustNil(t, err)
	if string(raw) != "all the files stay" {
		t.Fatal("cleanup changed kept work")
	}
	mustNil(t, c.ReleaseUnused(ctx, "chat", later.Cell.Lease.Fence))
}
