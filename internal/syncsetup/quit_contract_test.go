package syncsetup

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/executor"
)

// R29/C28/C31 drive the real engine and recorded call path. Failing only the
// upload leaves the directory reachable, so an incorrect clean-quit marker
// cannot hide behind a network failure affecting both wires.
func TestR29C28C31FailedQuitKeepsFinalRecordsAndDoesNotRelease(t *testing.T) {
	h := newTwoHomes(t)
	a := h.openA()
	a.mustSay("first published answer")
	h.durable(h.cell.ID, h.cell)
	before, e := h.b.Dir.Cell(context.Background(), h.cell.ID)
	if e != nil {
		t.Fatal(e)
	}
	h.block.down.Store(true)
	a.mustSay("final locally saved answer")
	executor.Settle(context.Background(), a.seat)
	if e = a.drive.Close(context.Background()); e == nil {
		t.Fatal("failed final upload reported a successful quit")
	}
	v, e := h.b.Dir.Cell(context.Background(), h.cell.ID)
	if e != nil {
		t.Fatal(e)
	}
	if v.Cell.Head != before.Cell.Head || v.Cell.Lease.Expires == 0 || v.Cell.Lease.ReleasedAt != 0 {
		t.Fatalf("failed quit advertised a release: %+v", v)
	}
	if _, e = h.b.Dir.Acquire(context.Background(), h.cell.ID, directory.AcquireOpts{}); !errors.Is(e, directory.ErrLeaseHeld) {
		t.Fatalf("failed quit made old head automatically takeable: %v", e)
	}
	raw, e := os.ReadFile(transcriptOf(h.cell))
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(raw), "final locally saved answer") {
		t.Fatal("final call/conversation record was lost")
	}
}
