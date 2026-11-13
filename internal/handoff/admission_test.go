package handoff

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// Contract A1 postpones the only ordinary whole-tree restore until acquisition.
func TestOrdinaryAdmissionRestoresOnlyAfterAcquiring(t *testing.T) {
	for _, inPlace := range []bool{false, true} {
		t.Run(map[bool]string{false: "owned", true: "borrowed"}[inPlace], func(t *testing.T) {
			w, a, _ := backFromB(t)
			take := a.taker()
			take.InPlace = func(cell.Cell) bool { return inPlace }
			take.Prepare = func(context.Context, cell.Cell, string) error { return nil }
			take.Preflight = func(context.Context, cell.Cell) error { t.Error("ordinary chat ran workspace admission"); return nil }
			started := time.Now()
			acquired := false
			restores := 0
			take.OnPhase = func(p Phase) {
				if !p.Ended.IsZero() {
					t.Logf("%s: %s", p.Name, p.Ended.Sub(p.Started))
				} else {
					t.Logf("%s after %s", p.Name, p.Started.Sub(started))
				}
				if p.Name == "first acquire" {
					acquired = true
				}
			}
			take.Fetch = admissionFetch{inner: take.Fetch, restore: func() {
				restores++
				if !acquired {
					t.Error("whole tree restored before acquiring")
				}
			}}
			got, err := take.Take(t.Context(), chatID)
			if err != nil {
				t.Fatal(err)
			}
			if restores != 1 || got.Kept != "" || w.cellRec(chatID).Lease.Device != devA {
				t.Fatalf("restores=%d kept=%q", restores, got.Kept)
			}
			if tree(t, got.Cell.Root)["b.txt"] != "from b" {
				t.Fatal("latest files lost")
			}
		})
	}
}

type admissionFetch struct {
	inner   Fetch
	restore func()
}

func (f admissionFetch) Fetch(ctx context.Context, c cell.Cell, head string) error {
	f.restore()
	return f.inner.Fetch(ctx, c, head)
}

func (f admissionFetch) Complete(ctx context.Context, c cell.Cell, head string) error {
	return f.inner.Complete(ctx, c, head)
}

// Contract A1 keeps incoming workspace admission ahead of the lease and local files.
func TestIncomingWorkspaceChecksAdmissionBeforeAcquire(t *testing.T) {
	w, a, _ := backFromB(t)
	before := w.cellRec(chatID).Lease
	take := a.taker()
	denied := errors.New("choose local folders for this chat's working roots")
	checked := false
	take.Prepare = func(context.Context, cell.Cell, string) error { checked = true; return denied }
	take.OnPhase = func(p Phase) {
		if p.Name == "first acquire" {
			t.Error("incoming workspace acquired before admission")
		}
	}
	_, err := take.Take(t.Context(), chatID)
	if !errors.Is(err, denied) || !checked {
		t.Fatalf("incoming workspace skipped its admission check: %v", err)
	}
	if w.cellRec(chatID).Lease != before || tree(t, a.root(chatID))["b.txt"] != "" {
		t.Fatal("rejected workspace changed the lease or local files")
	}
}
