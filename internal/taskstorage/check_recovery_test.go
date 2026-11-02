package taskstorage_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func recoveryReceipt(t *testing.T, root string) (string, map[string]any) {
	t.Helper()
	files, e := filepath.Glob(filepath.Join(root, "receipts", "*", "*.json"))
	if e != nil || len(files) != 1 {
		t.Fatalf("receipt: %v %v", files, e)
	}
	raw, e := os.ReadFile(files[0])
	if e != nil {
		t.Fatal(e)
	}
	var state map[string]any
	if e = json.Unmarshal(raw, &state); e != nil {
		t.Fatal(e)
	}
	return files[0], state
}
func writeRecoveryReceipt(t *testing.T, path string, state map[string]any) {
	t.Helper()
	raw, e := json.Marshal(state)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestEnsureRecoversOnlyJournaledVerificationContainer(t *testing.T) {
	m, path, _ := fixture(t)
	ctx := context.Background()
	if e := m.Park(ctx, path); e != nil {
		t.Fatal(e)
	}
	if e := m.Ensure(ctx, path); e != nil {
		t.Fatal(e)
	}
	receipt, state := recoveryReceipt(t, m.Root)
	name := strings.TrimSuffix(filepath.Base(receipt), ".json") + ".check-" + strings.Repeat("a", 32)
	container := filepath.Join(filepath.Dir(receipt), name)
	for _, child := range []string{"verified/work.txt", ".furrow-archive-interrupted/partial"} {
		file := filepath.Join(container, child)
		if e := os.MkdirAll(filepath.Dir(file), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(file, []byte("disposable restore bytes"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	unrelated := filepath.Join(filepath.Dir(receipt), ".check-legacy")
	if e := os.Mkdir(unrelated, 0700); e != nil {
		t.Fatal(e)
	}
	state["check"] = name
	writeRecoveryReceipt(t, receipt, state)
	if e := m.Ensure(ctx, path); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(container); !os.IsNotExist(e) {
		t.Fatalf("journaled verification container remains: %v", e)
	}
	if _, e := os.Stat(unrelated); e != nil {
		t.Fatalf("unrelated legacy directory changed: %v", e)
	}
	_, after := recoveryReceipt(t, m.Root)
	if after["check"] != nil && after["check"] != "" {
		t.Fatalf("recovery journal not cleared: %v", after)
	}
	data, e := os.ReadFile(filepath.Join(path, "work.txt"))
	if e != nil || string(data) != "original uncommitted work\n" {
		t.Fatalf("resident bytes changed: %q %v", data, e)
	}
}

func TestEnsureRejectsForgedVerificationContainer(t *testing.T) {
	for _, name := range []string{"../valuable", "/tmp/valuable", "valuable", strings.Repeat("b", 64) + ".check-" + strings.Repeat("a", 32)} {
		t.Run(name, func(t *testing.T) {
			m, path, _ := fixture(t)
			ctx := context.Background()
			if e := m.Park(ctx, path); e != nil {
				t.Fatal(e)
			}
			if e := m.Ensure(ctx, path); e != nil {
				t.Fatal(e)
			}
			receipt, state := recoveryReceipt(t, m.Root)
			sentinel := filepath.Join(filepath.Dir(receipt), "valuable")
			if e := os.WriteFile(sentinel, []byte("unrelated"), 0600); e != nil {
				t.Fatal(e)
			}
			state["check"] = name
			writeRecoveryReceipt(t, receipt, state)
			if e := m.Ensure(ctx, path); e == nil {
				t.Fatal("forged verification container accepted")
			}
			data, e := os.ReadFile(sentinel)
			if e != nil || string(data) != "unrelated" {
				t.Fatalf("unrelated file changed: %q %v", data, e)
			}
			_, after := recoveryReceipt(t, m.Root)
			if after["check"] != name {
				t.Fatalf("invalid recovery journal was silently changed: %v", after)
			}
		})
	}
}

func TestEnsureRefusesVerificationSymlinkWithoutClearingJournal(t *testing.T) {
	m, path, _ := fixture(t)
	ctx := context.Background()
	if e := m.Park(ctx, path); e != nil {
		t.Fatal(e)
	}
	if e := m.Ensure(ctx, path); e != nil {
		t.Fatal(e)
	}
	receipt, state := recoveryReceipt(t, m.Root)
	name := strings.TrimSuffix(filepath.Base(receipt), ".json") + ".check-" + strings.Repeat("a", 32)
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "valuable")
	if e := os.WriteFile(sentinel, []byte("unrelated"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, filepath.Join(filepath.Dir(receipt), name)); e != nil {
		t.Fatal(e)
	}
	state["check"] = name
	writeRecoveryReceipt(t, receipt, state)
	if e := m.Ensure(ctx, path); e == nil {
		t.Fatal("verification symlink accepted")
	}
	_, after := recoveryReceipt(t, m.Root)
	if after["check"] != name {
		t.Fatal("failed cleanup cleared journal")
	}
	data, e := os.ReadFile(sentinel)
	if e != nil || string(data) != "unrelated" {
		t.Fatalf("external file changed: %q %v", data, e)
	}
}

func TestCleanupFailureRetainsVerificationJournalForRetry(t *testing.T) {
	if os.Geteuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("directory permission refusal requires ordinary user")
	}
	m, path, _ := fixture(t)
	ctx := context.Background()
	if e := m.Park(ctx, path); e != nil {
		t.Fatal(e)
	}
	if e := m.Ensure(ctx, path); e != nil {
		t.Fatal(e)
	}
	receipt, state := recoveryReceipt(t, m.Root)
	scope := filepath.Dir(receipt)
	name := strings.TrimSuffix(filepath.Base(receipt), ".json") + ".check-" + strings.Repeat("a", 32)
	if e := os.Mkdir(filepath.Join(scope, name), 0700); e != nil {
		t.Fatal(e)
	}
	state["check"] = name
	writeRecoveryReceipt(t, receipt, state)
	if e := os.Chmod(scope, 0500); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.Chmod(scope, 0700) })
	if e := m.Ensure(ctx, path); e == nil {
		t.Fatal("cleanup without writable owner directory succeeded")
	}
	_, after := recoveryReceipt(t, m.Root)
	if after["check"] != name {
		t.Fatal("cleanup error discarded verification journal")
	}
	if e := os.Chmod(scope, 0700); e != nil {
		t.Fatal(e)
	}
	if e := m.Ensure(ctx, path); e != nil {
		t.Fatalf("cleanup retry failed: %v", e)
	}
	_, after = recoveryReceipt(t, m.Root)
	if after["check"] != nil && after["check"] != "" {
		t.Fatal("successful cleanup retry kept journal")
	}
}

func TestEnsureClearsUncreatedVerificationReservation(t *testing.T) {
	m, path, _ := fixture(t)
	ctx := context.Background()
	if err := m.Park(ctx, path); err != nil {
		t.Fatal(err)
	}
	if err := m.Ensure(ctx, path); err != nil {
		t.Fatal(err)
	}
	receipt, state := recoveryReceipt(t, m.Root)
	name := strings.TrimSuffix(filepath.Base(receipt), ".json") + ".check-" + strings.Repeat("a", 32)
	state["check"] = name
	writeRecoveryReceipt(t, receipt, state)
	if err := m.Ensure(ctx, path); err != nil {
		t.Fatal(err)
	}
	_, after := recoveryReceipt(t, m.Root)
	if after["check"] != nil && after["check"] != "" {
		t.Fatal("uncreated reservation not cleared")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(receipt), name)); !os.IsNotExist(err) {
		t.Fatalf("recovery created verification directory: %v", err)
	}
}
