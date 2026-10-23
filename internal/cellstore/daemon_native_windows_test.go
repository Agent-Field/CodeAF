package cellstore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/furrowbin"
)

// TestK2WindowsDaemonLifecycle proves K2 with the native engine: short profile
// socket, a stale endpoint, repeated AF_UNIX calls, and reaping only our child.
func TestK2WindowsDaemonLifecycle(t *testing.T) {
	bin, configured, err := furrowbin.ConfiguredBinary()
	if err != nil || !configured {
		t.Fatalf("K2 needs the runner's native engine: %v", err)
	}
	dir, err := os.MkdirTemp("", "cw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { StopSpawned(); os.RemoveAll(dir) })
	t.Setenv("LOCALAPPDATA", dir)
	socket := socketFor(bin)
	if len([]byte(socket)) >= 108 || !strings.Contains(socket, "codeaf") {
		t.Fatalf("K2 socket path %q (%d bytes)", socket, len(socket))
	}
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(socket, []byte("stale endpoint"), 0600); err != nil {
		t.Fatal(err)
	}
	d := Daemon{Binary: bin, Socket: socket}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := time.Now()
	conn, err := d.connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	t.Logf("K2 cold daemon connect: %s; socket %d bytes", time.Since(start), len(socket))
	for i := 0; i < 3; i++ {
		start = time.Now()
		conn, err := d.connect(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.verify(conn); err != nil {
			t.Fatal(err)
		}
		conn.Close()
		t.Logf("K2 warm health %d: %s", i, time.Since(start))
	}
	spawnedMu.Lock()
	all := append([]spawnedDaemon(nil), spawned...)
	spawnedMu.Unlock()
	StopSpawned()
	for _, child := range all {
		select {
		case <-child.exited:
		default:
			t.Fatal("K2 child was not reaped")
		}
	}
	if conn, err := dial(ctx, socket); err == nil {
		conn.Close()
		t.Fatal("K2 stopped child still listens")
	}
}
