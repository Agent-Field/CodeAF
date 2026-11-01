package syncsetup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
)

// C4.1: a late .env import must leave a removed home absent.
func TestGuardLeavesRemovedHomeAbsent(t *testing.T) {
	h := filepath.Join(t.TempDir(), "home")
	if _, err := identity.Ensure(h); err != nil {
		t.Fatal(err)
	}
	c, err := cell.Create(h, cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Root, ".env"), []byte("L6_FIXTURE=local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deleted := false
	g := cellstore.Guard{Home: h, Kept: func(string) {
		if !deleted {
			deleted = true
			if err := os.RemoveAll(h); err != nil {
				t.Fatal(err)
			}
		}
	}}
	// Screening can refuse the import or keep going without it; neither may create keys.
	_ = g.Screen(c, c.Root, filepath.Join(c.Root, cell.StateDir), []string{".env"})
	if !deleted {
		t.Fatal("fixture never found .env")
	}
	if _, err := os.Stat(h); !os.IsNotExist(err) {
		t.Fatalf("removed home was recreated: %v", err)
	}
}

// C4.1: capture refuses a replacement root before creating even its lock file.
func TestVaultCaptureLeavesReplacedHomeUntouched(t *testing.T) {
	h := filepath.Join(t.TempDir(), "home")
	if _, err := identity.Ensure(h); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(h, h+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(h, 0o700); err != nil {
		t.Fatal(err)
	}
	s := &Sync{Home: h}
	err := s.withVault(func(vaultsync.Syncer) error { t.Fatal("capture reached replacement home"); return nil })
	if !errors.Is(err, identity.ErrHomeRemoved) {
		t.Fatalf("capture = %v", err)
	}
	entries, err := os.ReadDir(h)
	if err != nil || len(entries) != 0 {
		t.Fatalf("capture changed replacement: %v, %v", entries, err)
	}
}

// SPEC-3 item 2: writing or retiring a send hold cannot recreate a lost root.
func TestFixRound2SendHoldRefusesRemovedHome(t *testing.T) {
	for _, replaced := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "replaced"}[replaced], func(t *testing.T) {
			h := filepath.Join(t.TempDir(), "home")
			id, err := identity.Ensure(h)
			if err != nil {
				t.Fatal(err)
			}
			dev, err := identity.Device(h)
			if err != nil {
				t.Fatal(err)
			}
			s := &Sync{Home: h, Identity: id, Device: dev, Ledger: "local"}
			if err := os.Rename(h, h+".old"); err != nil {
				t.Fatal(err)
			}
			if replaced {
				if err := os.Mkdir(h, 0700); err != nil {
					t.Fatal(err)
				}
			}
			held := sendLease{Cell: "chat", Fence: 1}
			if err := s.recordSendLease("chat", held); err == nil {
				t.Fatal("wrote hold into removed home")
			}
			_ = s.forgetSendLease("chat", held)
			if replaced {
				entries, err := os.ReadDir(h)
				if err != nil || len(entries) != 0 {
					t.Fatalf("replacement changed: %v %v", entries, err)
				}
			} else if _, err := os.Stat(h); !os.IsNotExist(err) {
				t.Fatalf("home recreated: %v", err)
			}
		})
	}
}
