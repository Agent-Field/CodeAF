package enginehost

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
)

func TestReviewRunRefusesStartingLegacyLock(t *testing.T) {
	shortHome(t)
	target := t.TempDir()
	alias := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(filepath.Clean(alias)))
	dir := home.Join("v3", "hosts", hex.EncodeToString(sum[:8]))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, placeName), []byte(alias), 0600); err != nil {
		t.Fatal(err)
	}
	oldLock, err := takeLock(filepath.Join(dir, lockName))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if oldLock != nil {
			releaseLock(oldLock)
		}
	}()
	done := make(chan error, 1)
	go func() {
		done <- Run(alias, Options{Boot: func(remote.Hello) (*remote.Engine, error) { return nil, errors.New("fixture never opens an agent") }})
	}()
	socket, err := SocketPath(alias)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			if !errors.Is(err, ErrHostRunning) {
				t.Fatalf("Run returned %v; want ErrHostRunning while legacy lock held", err)
			}
			return
		case <-tick.C:
			conn, err := net.DialTimeout("unix", socket, 20*time.Millisecond)
			if err != nil {
				continue
			}
			conn.Close()
			// Leave a clean host behind even when the assertion fails.
			releaseLock(oldLock)
			oldLock = nil
			if stopped, err := Stop(alias); err != nil || !stopped {
				t.Errorf("cleanup Stop=%v,%v", stopped, err)
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("new host did not shut down")
			}
			t.Fatal("Run opened a second host socket while the legacy lifetime lock was still held")
		case <-deadline.C:
			t.Fatal("Run neither refused the legacy lock nor opened a socket")
		}
	}
}
