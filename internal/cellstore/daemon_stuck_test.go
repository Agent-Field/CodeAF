package cellstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/furrow"
)

// Contract A3 bounds a missing socket behind a held storage lock.
func TestHeldStoreLockWithoutSocketReturnsSavingStuck(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "engine.sock")
	lock, err := os.OpenFile(socket+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, true); err != nil {
		t.Fatal(err)
	}
	defer filelock.Unlock(lock)
	started := time.Now()
	_, err = (Daemon{Socket: socket, Binary: filepath.Join(t.TempDir(), "no-daemon")}).Do(context.Background(), Target{}, snapOp{})
	var stuck *SavingStuckError
	if !errors.As(err, &stuck) {
		t.Fatalf("held lock without socket = %v, want saving-stuck error", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("held lock waited %s", elapsed)
	}
	if !strings.Contains(err.Error(), "saving is stuck") || !strings.Contains(err.Error(), idleMinutes+" minutes") {
		t.Fatalf("no recovery sentence: %v", err)
	}
	// A repeated verb returns the cached diagnosis instead of restarting or spinning.
	started = time.Now()
	_, err = startAndDial(context.Background(), Daemon{Socket: socket})
	if !errors.As(err, &stuck) || time.Since(started) > 100*time.Millisecond {
		t.Fatalf("repeated stuck start: %v", err)
	}
}

// Contract A3 permits recovery only for the same user's exact storage command.
func TestStorageProcessProofRequiresExactSocketAndUser(t *testing.T) {
	for _, tc := range []struct {
		name, uid, program, socket string
		want                       bool
	}{
		{"exact", "1000", "/owned/furrow", "/temporary/engine.sock", true},
		{"near path", "1000", "/owned/furrow", "/temporary/engine.sock.old", false},
		{"other user", "1001", "/owned/furrow", "/temporary/engine.sock", false},
		{"other program", "1000", "/owned/other", "/temporary/engine.sock", false},
		{"helper program", "1000", "/owned/furrow-helper", "/temporary/engine.sock", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := daemonServesSocket(tc.uid, "1000", []string{tc.program, "serve", "--socket", tc.socket, "--idle-minutes", idleMinutes}, "/temporary/engine.sock")
			if got != tc.want {
				t.Fatalf("process proof = %v, want %v", got, tc.want)
			}
		})
	}
}

// Contract A3 never falls through to another writer behind an unreachable store.
func TestSavingStuckDoesNotFallBackToAnotherStorageWriter(t *testing.T) {
	primary := &fixedTransport{err: unavailable(&SavingStuckError{Socket: "temporary.sock"})}
	secondary := &fixedTransport{out: "unexpected writer"}
	_, err := (Fallback{Primary: primary, Secondary: secondary}).Do(t.Context(), Target{}, snapOp{})
	var stuck *SavingStuckError
	if secondary.calls != 0 || !errors.As(err, &stuck) {
		t.Fatalf("stuck storage fell back: calls=%d err=%v", secondary.calls, err)
	}
}

// Contract A3 replaces only an owned storage child whose listener disappeared.
func TestOwnedStoreWithoutSocketRestartsAfterProof(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("process identity is unavailable")
	}
	bin, err := furrow.ResolveOwned()
	if err != nil {
		t.Skipf("no test storage binary: %v", err)
	}
	daemon := daemonFor(t, bin).(Daemon)
	t.Cleanup(StopSpawned)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, err := daemon.connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if err := os.Remove(daemon.Socket); err != nil {
		t.Fatal(err)
	}
	conn, err = daemon.connect(ctx)
	if err != nil {
		t.Fatalf("owned unreachable storage did not recover: %v", err)
	}
	defer conn.Close()
	if err := daemon.verify(conn); err != nil {
		t.Fatal(err)
	}
}
