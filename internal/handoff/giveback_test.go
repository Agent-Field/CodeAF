package handoff

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// deadReleaseDir refuses a Release whose context has ended, as the HTTP
// directory does: the request never leaves.
type deadReleaseDir struct{ *tracedDir }

func (d deadReleaseDir) Release(ctx context.Context, id string, fence uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.tracedDir.Release(ctx, id, fence)
}

// A take cancelled once the lease is ours (the person pressed Cancel while the
// chat was opening) still hands the lease back, so the chat is takeable at once
// instead of fenced to this computer until the lease lapses.
func TestACancelledTakeHandsTheLeaseBackAfterAcquire(t *testing.T) {
	w := newWorld(t)
	a, b := w.device(devA), w.device(devB)
	w.bare = true
	a.release(a.start(nil))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.hooks = []func(context.Context, cell.Cell) error{
		func(context.Context, cell.Cell) error {
			cancel()
			return context.Canceled
		},
	}
	tk := b.taker()
	tk.Dir = deadReleaseDir{b.dir}
	_, err := tk.Take(ctx, chatID)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancelled take", err)
	}
	if got := w.cellRec(chatID).Lease.Expires; got != 0 {
		t.Fatalf("lease expires %d after a take cancelled past acquire, want it handed back (err %v)", got, err)
	}
}
