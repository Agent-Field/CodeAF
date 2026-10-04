package handoff

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
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

// TestWarmTakeAcrossFilesystemsFallsBackToCopy: when the filesystem refuses
// the hard link (the staging folder is on another device than the held tree)
// nothing is seeded, the engine writes every file, and the take is whole while
// the held tree is untouched.
func TestWarmTakeAcrossFilesystemsFallsBackToCopy(t *testing.T) {
	refused := func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.EXDEV} }
	defer func(old func(string, string) error) { linkFile = old }(linkFile)
	linkFile = refused
	_, a, b := backFromB(t)
	a.materialize(a.cell(chatID), map[string]string{"keep.txt": "same", "edit.txt": "old"})
	b.work(map[string]string{"a.txt": "one", "keep.txt": "same", "edit.txt": "new"})
	held := stat(t, filepath.Join(a.root(chatID), "keep.txt"))
	a.local.dirty = false

	taken, err := a.taker().Take(context.Background(), chatID)
	if err != nil {
		t.Fatal(err)
	}
	got := tree(t, taken.Cell.Root)
	if got["keep.txt"] != "same" || got["edit.txt"] != "new" || got["a.txt"] != "one" {
		t.Fatalf("root = %v, want the new head", got)
	}
	if os.SameFile(held, stat(t, filepath.Join(taken.Cell.Root, "keep.txt"))) {
		t.Fatal("a refused link still produced a shared file")
	}
}

// TestSeededFileChangedInPlaceIsRepairedByTheRestore: a write made in place to
// a held file after seeding changes the staged file too, because they are one
// inode. The restore compares content, never a cache of what was seeded, so it
// replaces that file by a new one: the taken tree holds the head and the held
// tree keeps only its own edit.
func TestSeededFileChangedInPlaceIsRepairedByTheRestore(t *testing.T) {
	_, a, b := backFromB(t)
	a.materialize(a.cell(chatID), map[string]string{"keep.txt": "same"})
	head := b.work(map[string]string{"keep.txt": "same"})
	held := filepath.Join(a.root(chatID), "keep.txt")
	stage := cell.Cell{ID: chatID, Root: stagingOf(a.root(chatID))}
	if err := os.MkdirAll(stage.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	seedFrom(a.root(chatID), stage.Root)
	if err := os.WriteFile(held, []byte("edited in place"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := a.taker().Fetch.Fetch(context.Background(), stage, head); err != nil {
		t.Fatal(err)
	}

	if got := tree(t, stage.Root)["keep.txt"]; got != "same" {
		t.Fatalf("taken keep.txt = %q, the in-place edit leaked through the shared file", got)
	}
	if got := tree(t, a.root(chatID))["keep.txt"]; got != "edited in place" {
		t.Fatalf("held keep.txt = %q, the restore wrote into the held file", got)
	}
}

// TestTakeLeavesNoSecondNameForAnyFile: once the staging folder replaced the
// root, the old root is gone, so no file of the taken tree has another name
// and a later in-place write there cannot reach any other tree.
func TestTakeLeavesNoSecondNameForAnyFile(t *testing.T) {
	_, a, b := backFromB(t)
	a.materialize(a.cell(chatID), map[string]string{"keep.txt": "same"})
	b.work(map[string]string{"a.txt": "one", "keep.txt": "same"})
	a.local.dirty = false

	taken, err := a.taker().Take(context.Background(), chatID)
	if err != nil {
		t.Fatal(err)
	}
	links := stat(t, filepath.Join(taken.Cell.Root, "keep.txt")).Sys().(*syscall.Stat_t).Nlink
	if links != 1 {
		t.Fatalf("keep.txt has %d names after the swap, want 1", links)
	}
	for _, leftover := range []string{taken.Cell.Root + ".replaced", taken.Cell.Root + ".taking"} {
		if exists(leftover) {
			t.Fatalf("%s survived the take", leftover)
		}
	}
}

// TestCrashAfterSeedingLeavesTheHeldTreeWhole: a process that dies once the
// staging folder is seeded and fetched, before the swap, leaves the held tree
// byte for byte as it was (the engine never writes a shared file), and the
// next take discards what the crash left and succeeds.
func TestCrashAfterSeedingLeavesTheHeldTreeWhole(t *testing.T) {
	_, a, b := backFromB(t)
	a.materialize(a.cell(chatID), map[string]string{"keep.txt": "same", "edit.txt": "old"})
	head := b.work(map[string]string{"a.txt": "one", "keep.txt": "same", "edit.txt": "new"})
	before := tree(t, a.root(chatID))
	a.local.dirty = false
	stage := cell.Cell{ID: chatID, Root: stagingOf(a.root(chatID))}

	// The claim runs as far as the staging folder and stops: no install.
	if _, _, _, err := a.taker().fetchAndAcquire(context.Background(), stage, a.root(chatID), head, directory.AcquireOpts{}); err != nil {
		t.Fatal(err)
	}

	if got := tree(t, a.root(chatID)); !reflect.DeepEqual(got, before) {
		t.Fatalf("held tree = %v, was %v", got, before)
	}
	if got := tree(t, stage.Root); got["edit.txt"] != "new" {
		t.Fatalf("the crash left staging = %v, want the new head to be there", got)
	}
	taken, err := a.taker().Take(context.Background(), chatID)
	if err != nil {
		t.Fatalf("the take after the crash: %v", err)
	}
	if got := tree(t, taken.Cell.Root); got["edit.txt"] != "new" || got["a.txt"] != "one" {
		t.Fatalf("root = %v, want the new head", got)
	}
}
