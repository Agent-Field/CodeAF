package handoff

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// stat reads a file of the device's root, failing the test when it is absent.
func stat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// TestWarmTakeWritesOnlyTheChangedFile: a device that holds the chat at an
// older head keeps every file the new head did not change as the very same
// file, and only the changed path is a new file.
func TestWarmTakeWritesOnlyTheChangedFile(t *testing.T) {
	_, a, b := backFromB(t)
	a.materialize(a.cell(chatID), map[string]string{"keep.txt": "same", "edit.txt": "old"})
	b.work(map[string]string{"a.txt": "one", "keep.txt": "same", "edit.txt": "new"})
	keep, edit := stat(t, filepath.Join(a.root(chatID), "keep.txt")), stat(t, filepath.Join(a.root(chatID), "edit.txt"))
	a.local.dirty = false

	taken, err := a.taker().Take(context.Background(), chatID)
	if err != nil {
		t.Fatal(err)
	}
	if got := tree(t, taken.Cell.Root); got["edit.txt"] != "new" || got["keep.txt"] != "same" {
		t.Fatalf("root = %v, want the new head", got)
	}
	if !os.SameFile(keep, stat(t, filepath.Join(taken.Cell.Root, "keep.txt"))) {
		t.Fatal("an unchanged file was rewritten")
	}
	if os.SameFile(edit, stat(t, filepath.Join(taken.Cell.Root, "edit.txt"))) {
		t.Fatal("the changed file is still the old file")
	}
}

// TestWarmTakeRemovesWhatTheHeadDropped: a path the new head no longer holds
// is gone from the taken tree, though the staging folder started as a copy.
func TestWarmTakeRemovesWhatTheHeadDropped(t *testing.T) {
	_, a, b := backFromB(t)
	a.materialize(a.cell(chatID), map[string]string{"gone.txt": "x", "old/name.txt": "moved"})
	b.work(map[string]string{"a.txt": "one", "new/name.txt": "moved"})

	taken, err := a.taker().Take(context.Background(), chatID)
	if err != nil {
		t.Fatal(err)
	}
	got := tree(t, taken.Cell.Root)
	if _, ok := got["gone.txt"]; ok || got["new/name.txt"] != "moved" {
		t.Fatalf("root = %v, want gone.txt removed and the rename applied", got)
	}
	if _, ok := got[filepath.Join("old", "name.txt")]; ok {
		t.Fatalf("root = %v, the old name of a renamed file is still there", got)
	}
}

// TestTakeBackOfAnUnchangedChatWritesNothing: taking a chat back at the head
// this device already holds leaves every file as it was.
func TestTakeBackOfAnUnchangedChatWritesNothing(t *testing.T) {
	_, a, _ := backFromB(t)
	a.materialize(a.cell(chatID), map[string]string{"a.txt": "one", "b.txt": "from b"})
	before := map[string]os.FileInfo{}
	for name := range tree(t, a.root(chatID)) {
		before[name] = stat(t, filepath.Join(a.root(chatID), name))
	}

	taken, err := a.taker().Take(context.Background(), chatID)
	if err != nil {
		t.Fatal(err)
	}
	for name, info := range before {
		if !os.SameFile(info, stat(t, filepath.Join(taken.Cell.Root, name))) {
			t.Fatalf("%s was rewritten by a take that changes nothing", name)
		}
	}
}

// TestWarmTakeThatFailsLeavesTheOldRootWhole: a take that dies after the
// staging folder was filled must not have touched a byte of the root, though
// the two folders share their unchanged files.
func TestWarmTakeThatFailsLeavesTheOldRootWhole(t *testing.T) {
	_, a, b := backFromB(t)
	a.materialize(a.cell(chatID), map[string]string{"edit.txt": "old"})
	b.work(map[string]string{"a.txt": "one", "b.txt": "from b", "edit.txt": "new"})
	before := tree(t, a.root(chatID))
	a.onFetc = func() {
		if _, err := b.dir.Client.Acquire(context.Background(), chatID, directory.AcquireOpts{}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := a.taker().Take(context.Background(), chatID); !errors.Is(err, directory.ErrLeaseHeld) {
		t.Fatalf("err = %v, want ErrLeaseHeld", err)
	}
	if got := tree(t, a.root(chatID)); !reflect.DeepEqual(got, before) {
		t.Fatalf("root = %v, was %v: the failed take reached the root", got, before)
	}
}

// TestSeedKeepsLinksModesAndSymlinks is the copy a warm take starts from.
func TestSeedKeepsLinksModesAndSymlinks(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "stage")
	write := func(rel, body string, mode os.FileMode) {
		p := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("d/run.sh", "#!/bin/sh", 0o755)
	write(identityDir+"/workspace", "id", 0o600)
	if err := os.Symlink("d/run.sh", filepath.Join(src, "ln")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dst, 0o700); err != nil {
		t.Fatal(err)
	}

	seedFrom(src, dst)

	if !os.SameFile(stat(t, filepath.Join(src, "d/run.sh")), stat(t, filepath.Join(dst, "d/run.sh"))) {
		t.Fatal("a regular file is not a hard link")
	}
	if got, err := os.Readlink(filepath.Join(dst, "ln")); err != nil || got != "d/run.sh" {
		t.Fatalf("symlink = %q, %v", got, err)
	}
	if stat(t, filepath.Join(dst, "d/run.sh")).Mode() != 0o755 {
		t.Fatal("file mode changed")
	}
	if _, err := os.Stat(filepath.Join(dst, identityDir)); !os.IsNotExist(err) {
		t.Fatalf("the engine's identity folder was copied (%v)", err)
	}
	if !stat(t, filepath.Join(dst, "d")).ModTime().Equal(stat(t, filepath.Join(src, "d")).ModTime()) {
		t.Fatal("folder time not kept")
	}
}

// TestSeedFromNothingLeavesTheFolderEmpty is the cold take.
func TestSeedFromNothingLeavesTheFolderEmpty(t *testing.T) {
	dst := t.TempDir()
	seedFrom(filepath.Join(t.TempDir(), "absent"), dst)
	if entries, _ := os.ReadDir(dst); len(entries) != 0 {
		t.Fatalf("staging holds %d entries after a cold seed", len(entries))
	}
}
