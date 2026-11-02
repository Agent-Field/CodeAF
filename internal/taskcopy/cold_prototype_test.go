package taskcopy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/taskstorage"
)

type coldVault struct{ text []byte }

func (v *coldVault) Capture(_ context.Context, path string) (string, error) {
	b, e := os.ReadFile(filepath.Join(path, "README.md"))
	v.text = b
	return "saved", e
}
func (v *coldVault) Restore(_ context.Context, _ string, path string) error {
	if e := os.Mkdir(path, 0700); e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(path, "README.md"), v.text, 0644)
}
func (v *coldVault) Verify(_ context.Context, _ string, path string) error {
	b, e := os.ReadFile(filepath.Join(path, "README.md"))
	if e != nil {
		return e
	}
	if string(b) != string(v.text) {
		return errors.New("different fixture")
	}
	return nil
}

func TestSealDoesNotDiscardUnpreparedColdTaskCarry(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_TASK_STORAGE", "1")
	t.Setenv("CODEAF_PROTOTYPE_ARCHIVE_BINARY", filepath.Join(t.TempDir(), "unavailable"))
	a := newMachine(t)
	tree := a.task("one")
	a.seal()
	manager, enabled := taskstorage.Prototype(liveRoot(a.cell))
	if !enabled {
		t.Fatal("prototype disabled")
	}
	manager.Vault = &coldVault{}
	manager.Busy = func(string) bool { return false }
	if e := manager.Park(context.Background(), tree); e != nil {
		t.Fatal(e)
	}
	t.Setenv("CODEAF_PROTOTYPE_TASK_STORAGE", "")
	if e := (Carry{}).Compose(a.cell); e == nil {
		t.Fatal("disabled storage silently discarded an unmarked cold task")
	}
	t.Setenv("CODEAF_PROTOTYPE_TASK_STORAGE", "1")
	if e := (Carry{}).Compose(a.cell); e == nil {
		t.Fatal("seal silently accepted a cold copy with no prepared relay cache")
	}
	if _, e := readRecord(filepath.Join(carriedRoot(a.cell), "one")); e != nil {
		t.Fatalf("seal deleted the last relay artifact: %v", e)
	}
}

func TestColdTaskSealsAndTravelsWithoutExpandingItsArchive(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_TASK_STORAGE", "1")
	t.Setenv("CODEAF_PROTOTYPE_ARCHIVE_BINARY", filepath.Join(t.TempDir(), "unavailable"))
	a := newMachine(t)
	tree := a.task("one")
	write(t, filepath.Join(tree, "note.txt"), "uncommitted task note\n")
	write(t, filepath.Join(tree, "build", "cache.txt"), "local dependency bytes\n")
	carry := Carry{}
	if e := carry.PrepareCold(a.cell, tree); e != nil {
		t.Fatal(e)
	}
	manager, _ := taskstorage.Prototype(liveRoot(a.cell))
	manager.Vault = &coldVault{}
	manager.Busy = func(string) bool { return false }
	manager.BeforeRetire = func(_ context.Context, snapshot, path string) error { return carry.MarkCold(a.cell, path, snapshot) }
	if e := manager.Park(context.Background(), tree); e != nil {
		t.Fatal(e)
	}
	// Restarting without the experimental capability cannot reinterpret a
	// retired directory as a deleted task and sweep its portable recovery.
	artifact := filepath.Join(carriedRoot(a.cell), "one", recordName)
	before, e := os.ReadFile(artifact)
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("CODEAF_PROTOTYPE_TASK_STORAGE", "")
	if e := carry.Compose(a.cell); e == nil {
		t.Fatal("disabled storage silently accepted a cold task")
	}
	after, e := os.ReadFile(artifact)
	if e != nil || string(after) != string(before) {
		t.Fatalf("disabled storage changed the cold relay artifact: %v", e)
	}
	state, e := manager.Inspect(context.Background(), tree)
	if e != nil || state.Phase != taskstorage.Cold || state.Snapshot == "" {
		t.Fatalf("disabled storage lost local archive recovery: %+v %v", state, e)
	}
	t.Setenv("CODEAF_PROTOTYPE_TASK_STORAGE", "1")
	for i := 0; i < 2; i++ {
		a.seal()
	}
	if _, e := os.Stat(tree); !os.IsNotExist(e) {
		t.Fatalf("sealing expanded the cold task: %v", e)
	}
	b := newMachineNoRepo(t)
	a.moveTo(b)
	names, e := restorer.Restore(b.cell, b.project)
	if e != nil || len(names) != 1 || names[0] != "one" {
		t.Fatalf("cold task takeover: %v %v", names, e)
	}
	if body, ok := b.read("one", "note.txt"); !ok || body != "uncommitted task note\n" {
		t.Fatalf("restored note: %q %v", body, ok)
	}
	if _, ok := b.read("one", "build/cache.txt"); ok {
		t.Fatal("local-only ignored data was broadened into relay")
	}
}
