package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// A clean quit requires the shared terminal exit to leave an anchor answering fresh
// reads before the terminal's personal member has stopped.
func TestClusterSurfaceQuitHandsBackBeforeStoppingMember(t *testing.T) {
	celltest.Available(t)
	root, err := os.MkdirTemp("", "l2lfix-surface-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	t.Setenv("CODEAF_HOME", root)
	_, err = identity.Ensure(home.Dir())
	if err != nil {
		t.Fatal(err)
	}
	svc := relayserve.New(relayserve.Config{Store: t.TempDir()})
	defer svc.Close()
	server := httptest.NewServer(svc.Handler)
	defer server.Close()
	t.Setenv(syncsetup.URLVar, server.URL)
	previous := runSurfaceProgram
	t.Cleanup(func() { runSurfaceProgram = previous })
	var leader uint64
	runSurfaceProgram = func(ctx context.Context, options tui3.Options) error {
		s, err := syncOf()
		if err != nil {
			return err
		}
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			sum := sha256.Sum256([]byte(s.Ledger + "/" + s.Device.ID()))
			lock, err := os.Open(filepath.Join(s.Home, "cluster-member", hex.EncodeToString(sum[:]), "lock"))
			owned := false
			if err == nil {
				owned = filelock.IsBusy(filelock.Lock(lock, true, true))
				_ = lock.Close()
			}
			if owned {
				receipt, err := s.LeadCluster(ctx, 0)
				if err == nil {
					leader = receipt.Node
					if options.WaitForRelease == nil || !options.WaitForRelease() {
						t.Fatal("coordinating quit did not join closing releases")
					}
					return nil
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("personal member did not lead while the surface was open")
		return nil
	}
	if err := runSurface(context.Background(), tui3.Options{ProfileDir: root}); err != nil {
		t.Fatal(err)
	}
	s, err := syncOf()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	status, err := s.ClusterStatus(ctx)
	if err != nil || status.Leader == leader || status.Leader == 0 || status.Leader > 3 {
		t.Fatalf("quit returned before an anchor could coordinate: %+v %v", status, err)
	}
}
