package taskstorage_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/taskstorage"
)

type vault struct {
	contents []byte
	broken   bool
}

func (v *vault) Capture(_ context.Context, path string) (string, error) {
	b, e := os.ReadFile(filepath.Join(path, "work.txt"))
	v.contents = b
	return "saved", e
}
func (v *vault) Restore(_ context.Context, id, path string) error {
	if v.broken {
		return errors.New("saved object unavailable")
	}
	if e := os.Mkdir(path, 0700); e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(path, "work.txt"), v.contents, 0640)
}
func (v *vault) Verify(_ context.Context, id, path string) error {
	if v.broken {
		return errors.New("saved object corrupt")
	}
	b, e := os.ReadFile(filepath.Join(path, "work.txt"))
	if e != nil {
		return e
	}
	if string(b) != string(v.contents) {
		return errors.New("contents differ")
	}
	return nil
}
func fixture(t *testing.T) (taskstorage.Manager, string, *vault) {
	t.Helper()
	root := t.TempDir()
	trees := filepath.Join(root, "trees")
	path := filepath.Join(trees, "task-a")
	if e := os.MkdirAll(path, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(path, "work.txt"), []byte("original uncommitted work\n"), 0640); e != nil {
		t.Fatal(e)
	}
	v := &vault{}
	return taskstorage.Manager{Root: filepath.Join(root, "storage"), Trees: trees, Vault: v, Busy: func(string) bool { return false }}, path, v
}
func TestParkAndReopenKeepsExactWorkAndIdentity(t *testing.T) {
	m, path, _ := fixture(t)
	ctx := context.Background()
	if e := m.Park(ctx, path); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatalf("expanded copy retained: %v", e)
	}
	state, e := m.Inspect(ctx, path)
	if e != nil || state.Phase != taskstorage.Cold || state.Snapshot != "saved" {
		t.Fatalf("cold state: %+v %v", state, e)
	}
	paths, e := m.Paths(ctx)
	if e != nil || len(paths) != 1 || paths[0] != path {
		t.Fatalf("retained identities: %v %v", paths, e)
	}
	if e := m.Ensure(ctx, path); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(path, "work.txt"))
	if e != nil || string(b) != "original uncommitted work\n" {
		t.Fatalf("restored: %q %v", b, e)
	}
	if e := m.Ensure(ctx, path); e != nil {
		t.Fatal(e)
	}
	if e := m.Park(ctx, path); e != nil {
		t.Fatal(e)
	}
	if e := m.Park(ctx, path); e != nil {
		t.Fatal(e)
	}
}
func TestRunningOrForeignWorkspaceNeverRetires(t *testing.T) {
	m, path, _ := fixture(t)
	m.Busy = func(string) bool { return true }
	if e := m.Park(context.Background(), path); e == nil {
		t.Fatal("running copy retired")
	}
	if _, e := os.Stat(filepath.Join(path, "work.txt")); e != nil {
		t.Fatal(e)
	}
	m.Busy = func(string) bool { return false }
	if e := m.Park(context.Background(), t.TempDir()); e == nil {
		t.Fatal("foreign workspace accepted")
	}
}
func TestMissingSavedBytesCannotBecomeFreshCopy(t *testing.T) {
	m, path, v := fixture(t)
	ctx := context.Background()
	if e := m.Park(ctx, path); e != nil {
		t.Fatal(e)
	}
	v.broken = true
	if e := m.Ensure(ctx, path); e == nil {
		t.Fatal("missing archive accepted")
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatalf("published partial or fresh copy: %v", e)
	}
	state, e := m.Inspect(ctx, path)
	if e != nil || state.Snapshot != "saved" {
		t.Fatalf("recovery pointer lost: %+v %v", state, e)
	}
	v.broken = false
	if e := m.Ensure(ctx, path); e != nil {
		t.Fatal(e)
	}
}
func TestCaptureFailureKeepsOriginal(t *testing.T) {
	m, path, v := fixture(t)
	v.broken = true
	if e := m.Park(context.Background(), path); e == nil {
		t.Fatal("corrupt capture retired")
	}
	if _, e := os.Stat(filepath.Join(path, "work.txt")); e != nil {
		t.Fatal(e)
	}
}
func TestRecreatedWorkspaceIsNeverOverwritten(t *testing.T) {
	m, path, _ := fixture(t)
	ctx := context.Background()
	if e := m.Park(ctx, path); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(path, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(path, "work.txt"), []byte("new external work"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := m.Ensure(ctx, path); e == nil {
		t.Fatal("conflicting copy accepted")
	}
	b, _ := os.ReadFile(filepath.Join(path, "work.txt"))
	if string(b) != "new external work" {
		t.Fatal("external work overwritten")
	}
}

func TestSymlinkAboveOwnedRootCannotRetireForeignData(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if e := os.MkdirAll(filepath.Join(outside, "trees", "task"), 0700); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(outside, "trees", "task", "work.txt")
	if e := os.WriteFile(path, []byte("foreign"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, filepath.Join(root, "redirect")); e != nil {
		t.Skip(e)
	}
	m := taskstorage.Manager{Root: filepath.Join(root, "storage"), Trees: filepath.Join(root, "redirect", "trees"), Vault: &vault{}, Busy: func(string) bool { return false }}
	if e := m.Park(context.Background(), filepath.Join(m.Trees, "task")); e == nil {
		t.Fatal("retired foreign data through an ancestor symlink")
	}
	if b, e := os.ReadFile(path); e != nil || string(b) != "foreign" {
		t.Fatalf("foreign data changed: %q %v", b, e)
	}
}

type interruptedVault struct {
	vault
	detach  bool
	changed bool
}

func (v *interruptedVault) Verify(ctx context.Context, id, path string) error {
	if v.detach && strings.Contains(filepath.Base(path), ".storage-retiring-") {
		v.detach = false
		if v.changed {
			_ = os.WriteFile(filepath.Join(path, "work.txt"), []byte("new racing bytes"), 0600)
		} else {
			_ = os.Remove(filepath.Join(path, "work.txt"))
		}
		panic("simulated process interruption")
	}
	return v.vault.Verify(ctx, id, path)
}
func (v *interruptedVault) VerifyRemainder(_ context.Context, id, path string) error {
	b, e := os.ReadFile(filepath.Join(path, "work.txt"))
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if string(b) != string(v.contents) {
		return errors.New("remaining work changed")
	}
	return nil
}
func TestInterruptedRetirementRestoresMissingFiles(t *testing.T) {
	m, path, _ := fixture(t)
	v := &interruptedVault{detach: true}
	m.Vault = v
	func() {
		defer func() {
			if recover() == nil {
				t.Error("interruption not reached")
			}
		}()
		_ = m.Park(context.Background(), path)
	}()
	if e := m.Ensure(context.Background(), path); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(path, "work.txt"))
	if e != nil || string(b) != "original uncommitted work\n" {
		t.Fatalf("recovery lost saved bytes: %q %v", b, e)
	}
}
func TestInterruptedRetirementPreservesUnexpectedNewBytes(t *testing.T) {
	m, path, _ := fixture(t)
	v := &interruptedVault{detach: true, changed: true}
	m.Vault = v
	func() { defer func() { _ = recover() }(); _ = m.Park(context.Background(), path) }()
	if e := m.Ensure(context.Background(), path); e == nil {
		t.Fatal("accepted newer unknown retirement data")
	}
	matches, e := filepath.Glob(filepath.Join(filepath.Dir(path), ".task-a.storage-retiring-*", "work.txt"))
	if e != nil || len(matches) != 1 {
		t.Fatalf("changed retired copy disappeared: %v %v", matches, e)
	}
	b, e := os.ReadFile(matches[0])
	if e != nil || string(b) != "new racing bytes" {
		t.Fatalf("new bytes lost: %q %v", b, e)
	}
}
