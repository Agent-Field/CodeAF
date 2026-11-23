package pair

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
)

// Explicit replacement discards writes made before installation, including
// after a failed identity replacement, and keeps writes made after it.
func TestR3_2_ReplaceRecoveryKeepsVaultReadable(t *testing.T) {
	for _, at := range []string{"prepared", "install-failed", "installed-before-progress"} {
		t.Run(at, func(t *testing.T) {
			home := t.TempDir()
			old, err := identity.EnsureSolo(home)
			if err != nil {
				t.Fatal(err)
			}
			v, err := keys.Open(home)
			if err != nil {
				t.Fatal(err)
			}
			if err := v.Put("before", keys.Entry{Name: "BEFORE", Value: "discard"}); err != nil {
				t.Fatal(err)
			}
			next, err := identity.Mint()
			if err != nil {
				t.Fatal(err)
			}
			stopped := errors.New("process stopped")
			identityPath := filepath.Join(home, identity.File)
			backup := filepath.Join(home, "identity-backup")
			_, err = adopt(Joining{Home: home, Replace: true, After: func(step string) error {
				if step == "staged" && at == "prepared" || step == "installed" && at == "installed-before-progress" {
					return stopped
				}
				if step == "staged" && at == "install-failed" {
					// Force the atomic identity replacement to fail before the
					// switch, as a locked destination can do on Windows.
					if err := os.Rename(identityPath, backup); err != nil {
						return err
					}
					if err := os.Mkdir(identityPath, 0700); err != nil {
						return err
					}
					return os.WriteFile(filepath.Join(identityPath, "blocked"), []byte("blocked"), 0600)
				}
				return nil
			}}, Grant{Identity: next})
			if at == "install-failed" {
				var pathErr *os.LinkError
				if !errors.As(err, &pathErr) || pathErr.Op != "rename" {
					t.Fatalf("expected identity replacement failure: %v", err)
				}
				if err := os.RemoveAll(identityPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(backup, identityPath); err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, stopped) {
				t.Fatalf("stop at %s: %v", at, err)
			}
			if at == "installed-before-progress" {
				// The identity write can succeed before the progress write.
				raw, err := os.ReadFile(filepath.Join(home, mergeFile))
				if err != nil {
					t.Fatal(err)
				}
				var rec mergeRecord
				if err := json.Unmarshal(raw, &rec); err != nil {
					t.Fatal(err)
				}
				rec.Phase = "prepared"
				if err := rec.save(home); err != nil {
					t.Fatal(err)
				}
			}
			current, err := identity.Load(home)
			if err != nil {
				t.Fatal(err)
			}
			wantID := old.ID()
			if at == "installed-before-progress" {
				wantID = next.ID()
			}
			if current.ID() != wantID {
				t.Fatal("test did not stop on the expected side of installation")
			}
			if err := v.Put("during", keys.Entry{Name: "DURING", Value: "between attempts"}); err != nil {
				t.Fatal(err)
			}
			if err := Resume(home, nil); err != nil {
				t.Fatal(err)
			}
			current, err = identity.Load(home)
			if err != nil || current.ID() != next.ID() {
				t.Fatalf("destination after recovery = %s, %v", current.ID(), err)
			}
			if at == "installed-before-progress" {
				if got, err := v.Get("during"); err != nil || got.Value != "between attempts" {
					t.Errorf("post-install key was lost: %+v, %v", got, err)
				}
			} else if _, err := v.Get("during"); !errors.Is(err, keys.ErrNotFound) {
				t.Errorf("pre-install key must be discarded, leaving a readable vault: %v", err)
			}
			if _, err := v.Get("before"); !errors.Is(err, keys.ErrNotFound) {
				t.Errorf("old saved key survived clean slate: %v", err)
			}
			if err := v.Put("after", keys.Entry{Name: "AFTER", Value: "kept"}); err != nil {
				t.Fatalf("vault cannot be written after recovery: %v", err)
			}
		})
	}
}
