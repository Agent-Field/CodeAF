package syncsetup

import (
	"context"
	"testing"
)

// TestTwoHomesThirdTakeAfterTakeBack is the move harness's failing leg as one
// test. The harness, after the cold take, runs: A seals a change, B takes it
// back warm (in place), B seals a change, A takes it back, A seals a long run
// of turns through the batcher (its latency probes: many turns, few flushes),
// and then B takes again — and that take's in-place materialize failed in the
// harness with the engine exiting on a missing file.
func TestTwoHomesThirdTakeAfterTakeBack(t *testing.T) {
	h := bContinued(t)
	ctx := context.Background()
	takeB := func() Continued {
		cb := h.continuerB()
		h.backToA() // the project folder exists while B takes: the restore is in place
		took, err := cb.Take(ctx, h.cell.ID)
		if err != nil {
			t.Fatalf("B's take failed: %v", err)
		}
		return took
	}

	// A seals a change so B's next take is warm, and B takes it in place.
	a := h.openA()
	a.mustSay("a three")
	h.durable(h.cell.ID, h.cell)
	took := takeB()

	// B answers and A takes the chat back, the harness's takeback.
	b := h.openOn(h.b, h.engB, took.Taken.Cell, workspaceOf(took.Taken.Cell.Root), nameB)
	b.mustSay("b two")
	h.durable(h.cell.ID, took.Taken.Cell)
	if err := b.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := h.continuerA().Take(ctx, h.cell.ID); err != nil {
		t.Fatal(err)
	}

	// A's latency probes: many turns through the batcher, few flushes.
	a2 := h.openA()
	for i := 0; i < 20; i++ {
		a2.mustSay("a batch")
	}
	h.durable(h.cell.ID, h.cell)
	if err := a2.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}

	// The harness's failing take: B takes again, in place.
	takeB()
}
