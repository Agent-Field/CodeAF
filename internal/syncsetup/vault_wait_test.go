package syncsetup

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/handoff"
)

// TestAContinueWaitsForAnotherWindowsVaultExchange is D4 from the two-network
// test, reproduced the way it happened: a chat running in another window holds
// state-sync.lock while it pushes the vault over a slow relay, and the continue
// reaches its vault pull meanwhile. The pull took the lock without waiting, so
// the whole move failed with "resource temporarily unavailable" and the chat was
// handed back. The continue must wait for the exchange and arrive.
func TestAContinueWaitsForAnotherWindowsVaultExchange(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	a := h.openA()
	a.mustSay("before moving")
	h.durable(h.cell.ID, h.cell)
	mustNil(t, a.drive.Close(ctx))

	// Another window's exchange holds the lock through its own open file, which
	// is exactly how a second process holds it.
	held, err := os.OpenFile(filepath.Join(h.b.Home, "state-sync.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	mustNil(t, err)
	defer held.Close()
	mustNil(t, filelock.Lock(held, true, true))

	// The lock is let go only after the move has restored its files, so the
	// vault pull that follows meets it held.
	var once sync.Once
	released := make(chan struct{})
	b := h.continuerB()
	b.opt.OnPhase = func(p handoff.Phase) {
		if p.Name == "restore" {
			once.Do(func() {
				time.AfterFunc(time.Second, func() {
					_ = filelock.Unlock(held)
					close(released)
				})
			})
		}
	}
	got, err := b.Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatalf("the continue failed while another window held the vault: %v", err)
	}
	select {
	case <-released:
	default:
		t.Fatal("the continue arrived without meeting the held vault lock, so it proved nothing")
	}
	if got.Taken.Cell.ID != h.cell.ID {
		t.Fatalf("a different chat arrived: %s", got.Taken.Cell.ID)
	}
}

// TestAVaultWaitIsBoundedAndBackgroundExchangesNeverWait pins both sides of the
// rule: a background exchange (no wait) is refused at once as before, and a
// continue's wait ends at its bound with the same busy refusal.
func TestAVaultWaitIsBoundedAndBackgroundExchangesNeverWait(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state-sync.lock")
	held, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	mustNil(t, err)
	defer held.Close()
	mustNil(t, filelock.Lock(held, true, true))
	mine, err := os.OpenFile(path, os.O_RDWR, 0o600)
	mustNil(t, err)
	defer mine.Close()

	started := time.Now()
	if err := lockState(t.Context(), mine, 0); !filelock.IsBusy(err) {
		t.Fatalf("a background exchange did not get the busy refusal at once: %v", err)
	}
	if waited := time.Since(started); waited > vaultBusyRetry {
		t.Fatalf("a background exchange waited %s", waited)
	}
	started = time.Now()
	if err := lockState(t.Context(), mine, 3*vaultBusyRetry); !filelock.IsBusy(err) {
		t.Fatalf("a bounded wait did not end in the busy refusal: %v", err)
	}
	if waited := time.Since(started); waited < 3*vaultBusyRetry {
		t.Fatalf("the bounded wait gave up after %s", waited)
	}
}
