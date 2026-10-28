package taskstorage

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

var unavailableRecovery = errors.New("synthetic unavailable native writer lease")

type unavailableRecoveryVault struct{ calls int }

func (*unavailableRecoveryVault) archiveRecoveryReady() error { return unavailableRecovery }
func (v *unavailableRecoveryVault) Capture(context.Context, string) (string, error) {
	v.calls++
	return "", unavailableRecovery
}
func (v *unavailableRecoveryVault) Restore(context.Context, string, string) error {
	v.calls++
	return unavailableRecovery
}
func (v *unavailableRecoveryVault) Verify(context.Context, string, string) error {
	v.calls++
	return unavailableRecovery
}

// The public Manager seam must consult a backend's unavailable lease capability
// before it acquires a receipt or interprets an old journal as disposable data.
func TestUnavailableRecoveryLeasePreservesJournalBeforeAnyMutation(t *testing.T) {
	for _, operation := range []string{"park", "ensure"} {
		t.Run(operation, func(t *testing.T) {
			root := t.TempDir()
			trees := filepath.Join(root, "trees")
			path := filepath.Join(trees, "task")
			storage := filepath.Join(root, "storage")
			scope := filepath.Join(storage, "receipts", digest(trees))
			check := digest("task") + ".check-" + strings.Repeat("a", 32)
			container := filepath.Join(scope, check)
			for _, file := range []string{filepath.Join(path, "work.txt"), filepath.Join(container, "verified", "partial")} {
				if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte("keep mock bytes exactly"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			state := State{Version: 1, Path: "task", Snapshot: strings.Repeat("b", 64), Phase: Resident, Check: check}
			raw, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(scope, digest("task")+".json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			snapshot := func() map[string]string {
				out := map[string]string{}
				if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					rel, err := filepath.Rel(root, path)
					if err != nil {
						return err
					}
					if entry.IsDir() {
						out[rel+"/"] = ""
						return nil
					}
					bytes, err := os.ReadFile(path)
					out[rel] = string(bytes)
					return err
				}); err != nil {
					t.Fatal(err)
				}
				return out
			}
			before := snapshot()
			backend := &unavailableRecoveryVault{}
			manager := Manager{Root: storage, Trees: trees, Vault: backend, Busy: func(string) bool { return false }}
			if operation == "park" {
				err = manager.Park(context.Background(), path)
			} else {
				err = manager.Ensure(context.Background(), path)
			}
			if !errors.Is(err, unavailableRecovery) {
				t.Fatalf("unavailable recovery capability ignored: %v", err)
			}
			if backend.calls != 0 {
				t.Fatal("native operation launched despite unavailable lease capability")
			}
			if !reflect.DeepEqual(before, snapshot()) {
				t.Fatal("recovery changed journal, staging or receipt files before capability guard")
			}
		})
	}
}

func TestWindowsReceiptLeasedNativeCallRefusesBeforeLaunch(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows runtime; cross-compilation is not execution proof")
	}
	lease, err := os.CreateTemp(t.TempDir(), "receipt-lock")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	ctx := context.WithValue(context.Background(), receiptLeaseKey{}, lease)
	backend := CLIBackend{Binary: filepath.Join(t.TempDir(), "must-not-launch.exe"), Store: t.TempDir()}
	if _, err := backend.Capture(ctx, "mock-workspace"); err == nil || !strings.Contains(err.Error(), "writer lease") {
		t.Fatalf("unsupported native recovery did not fail closed: %v", err)
	}
	// A missing executable still gives the ordinary launch error without a
	// receipt lease: this guard does not disable unrelated direct capture.
	if _, err := backend.Capture(context.Background(), "mock-workspace"); err == nil || strings.Contains(err.Error(), "writer lease") {
		t.Fatalf("ordinary capture was incorrectly subject to recovery policy: %v", err)
	}
}
