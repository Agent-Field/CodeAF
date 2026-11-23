package cellstore

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// TestW3DaemonStartPrunesOnlyDeadSockets proves B3 through a real daemon start:
// abandoned endpoints go; live listeners, held locks and unrelated files stay.
func TestW3DaemonStartPrunesOnlyDeadSockets(t *testing.T) {
	e := realEngine(t)
	d := daemonFor(t, e.Binary).(Daemon)
	e.Transport = d
	dir := filepath.Dir(d.Socket)
	stale := filepath.Join(dir, "e-abandoned.sock")
	listener, err := net.Listen("unix", stale)
	if err != nil {
		t.Fatal(err)
	}
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	livePath := filepath.Join(dir, "e-live.sock")
	live, err := net.Listen("unix", livePath)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	locked := filepath.Join(dir, "e-starting.sock")
	lock, err := os.OpenFile(locked+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, true); err != nil {
		t.Fatal(err)
	}
	defer filelock.Unlock(lock)
	for _, name := range []string{"e-regular.sock", "e-starting.sock", "keep.sock", "e-copy.sock.bak"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.Seal(t.Context(), newCell(t), TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"e-abandoned.sock", "e-regular.sock"} {
		if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("dead endpoint %s remains: %v", name, err)
		}
	}
	for _, name := range []string{"e-live.sock", "e-starting.sock", "e-starting.sock.lock", "keep.sock", "e-copy.sock.bak"} {
		if _, err := os.Lstat(filepath.Join(dir, name)); err != nil {
			t.Errorf("active or unrelated %s removed: %v", name, err)
		}
	}
	conn, err := net.Dial("unix", livePath)
	if err != nil {
		t.Fatalf("cleanup broke a live endpoint: %v", err)
	}
	conn.Close()
}
