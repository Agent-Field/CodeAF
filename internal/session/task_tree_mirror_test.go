package session

// THE FOLDER FAMILY TREE, AS TESTS (issue #230).
//
// The claim under test is one sentence: a family whose ground is a plain folder
// isolates and lands exactly like a family whose ground is a repository. So
// these drive the real doors — the same [prepareTaskTreeOn] a running node
// calls, the same stand a part is grounded on, the same [taskTree.comeHome] a
// part lands through — and assert the four things the issue asks for: a mirror
// that is a repository, a part that works somewhere its parent is not, parts
// that cannot see each other's work, and both parts' work sitting in the mirror
// before the family lands.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// familyOnAFolder is the shape every test here starts from: a person's folder
// with something in it, and the mirrored parent standing on it.
func familyOnAFolder(t *testing.T) (folder string, place Place, parent taskTree) {
	t.Helper()
	folder = t.TempDir()
	writeFile(t, filepath.Join(folder, "notes.md"), "what the parent gathered\n")
	place = Place{Dir: t.TempDir(), Workspace: folder}
	parent, err := prepareTaskTreeOn(context.Background(), place, folder, "aaaa1111bbbb2222", 7,
		"write the report", taskStand{dir: folder, mode: TaskModeMirror})
	if err != nil {
		t.Fatalf("prepareTaskTreeOn for the parent: %v", err)
	}
	return folder, place, parent
}

// partOf grounds one part the way a running family grounds one: on the stand its
// parent's worker resolves from the directory it is standing in.
func partOf(t *testing.T, place Place, parent taskTree, id uint64, title string) taskTree {
	t.Helper()
	stand := (&Agent{config: Config{Workspace: parent.dir}}).resolveTaskGround(
		taskSpec{parent: 7, title: title, brief: "write " + title + ".md", deliverable: "d", acceptance: "a"})
	if stand.mode != TaskModeWorktree || canonicalPath(stand.dir) != canonicalPath(parent.dir) {
		t.Fatalf("a part of a folder family stands %q as %q, want a worktree of the family tree %q",
			stand.dir, stand.mode, parent.dir)
	}
	tree, err := prepareTaskTreeOn(context.Background(), place, parent.dir, "aaaa1111bbbb2222", id, title, stand)
	if err != nil {
		t.Fatalf("prepareTaskTreeOn for %s: %v", title, err)
	}
	return tree
}

// THE MIRROR IS THE FAMILY TREE. It holds the folder as it stood, in a
// repository of its own, with a commit a part can be cut from — and none of that
// reaches the person's folder.
func TestAFolderFamilyOpensItsMirrorAsTheFamilyTree(t *testing.T) {
	folder, _, parent := familyOnAFolder(t)

	if parent.dir == folder {
		t.Fatalf("the family tree is the person's own folder %q", folder)
	}
	if !familyTreeIsOpen(parent.dir) {
		t.Fatalf("the mirror at %q is not a repository its parts could branch from", parent.dir)
	}
	if parent.note != "" {
		t.Fatalf("a family tree that opened cleanly says %q, want nothing", parent.note)
	}
	if tracked := gitOut(t, parent.dir, "ls-files"); !strings.Contains(tracked, "notes.md") {
		t.Fatalf("the baseline holds %q, want the folder as it stood", tracked)
	}
	if log := gitOut(t, parent.dir, "log", "--oneline"); !strings.Contains(log, familyTreeCommitMessage) {
		t.Fatalf("the family tree's history is %q, want its baseline", log)
	}
	// NOTHING OF OURS LIVES IN THE PERSON'S FOLDER. The mirror is harness-owned
	// space; the folder it was copied from is left exactly as it was found.
	if _, err := os.Stat(filepath.Join(folder, ".git")); err == nil {
		t.Fatalf("the person's folder %q was made into a repository", folder)
	}
}

// THE ISSUE'S ACCEPTANCE, END TO END: two parts, two directories, neither
// standing where its parent stands, neither able to see the other's work, and
// both landed in the family tree before the family itself lands.
func TestPartsOfAFolderFamilyWorkApartAndLandInTheFamilyTree(t *testing.T) {
	_, place, parent := familyOnAFolder(t)

	first := partOf(t, place, parent, 8, "the first section")
	second := partOf(t, place, parent, 9, "the second section")

	if first.dir == parent.dir || second.dir == parent.dir {
		t.Fatalf("a part works in its parent's own directory %q", parent.dir)
	}
	if first.dir == second.dir {
		t.Fatalf("both parts work in %q: the siblings are on top of each other", first.dir)
	}
	if canonicalPath(first.root) != canonicalPath(parent.dir) {
		t.Fatalf("the first part's branch merges into %q, want the family tree %q", first.root, parent.dir)
	}
	if first.branch == "" || second.branch == "" {
		t.Fatal("a part of a folder family has no branch to come home on")
	}

	writeFile(t, filepath.Join(first.dir, "one.md"), "the first section\n")
	writeFile(t, filepath.Join(second.dir, "two.md"), "the second section\n")
	// ISOLATION IS THE WHOLE POINT: what one part wrote is not on the other's
	// disk, and neither is on the parent's.
	for _, absent := range []string{
		filepath.Join(first.dir, "two.md"),
		filepath.Join(second.dir, "one.md"),
		filepath.Join(parent.dir, "one.md"),
	} {
		if _, err := os.Stat(absent); err == nil {
			t.Fatalf("%q is on a disk it was never written to: the parts are sharing a tree", absent)
		}
	}
	// And each part woke up holding the family's material.
	if got := readFile(t, filepath.Join(first.dir, "notes.md")); !strings.Contains(got, "what the parent gathered") {
		t.Fatalf("the first part's notes.md is %q; the family's material did not come with it", got)
	}

	for _, part := range []struct {
		tree taskTree
		file string
	}{{first, "one.md"}, {second, "two.md"}} {
		merge, detail := part.tree.comeHome("the section", []string{part.file})
		if merge != mergeMerged {
			t.Fatalf("%s came home as %q (%s), want it merged into the family tree", part.file, merge, detail)
		}
	}

	// BOTH PARTS' WORK IS IN THE FAMILY TREE BEFORE THE FAMILY LANDS, which is
	// what makes the parent's own landing able to ship the whole family product.
	for _, file := range []string{"one.md", "two.md"} {
		if _, err := os.Stat(filepath.Join(parent.dir, file)); err != nil {
			t.Fatalf("%s never reached the family tree: %v", file, err)
		}
	}
	if log := gitOut(t, parent.dir, "log", "--oneline"); !strings.Contains(log, "the section") {
		t.Fatalf("the family tree's history is %q, want the parts' work in it", log)
	}
}

// THE BOUNDARY THE ISSUE DRAWS. A person who said "here" gets "here": an in
// place or a folder family works in the person's own directory, its parts share
// it, and nothing turns that directory into a repository behind their back.
func TestAFamilyToldToWorkHereIsLeftWhereItWasPut(t *testing.T) {
	for _, mode := range []TaskMode{TaskModeInPlace, TaskModeFolder} {
		folder := t.TempDir()
		writeFile(t, filepath.Join(folder, "notes.md"), "the person's own notes\n")
		place := Place{Dir: t.TempDir(), Workspace: folder}

		tree, err := prepareTaskTreeOn(context.Background(), place, folder, "cccc3333dddd4444", 3,
			"work here", taskStand{dir: folder, mode: mode})
		if err != nil {
			t.Fatalf("prepareTaskTreeOn (%s): %v", mode, err)
		}
		if canonicalPath(tree.dir) != canonicalPath(folder) {
			t.Fatalf("a %s task works in %q, want the folder it was told to work in", mode, tree.dir)
		}
		if _, err := os.Stat(filepath.Join(folder, ".git")); err == nil {
			t.Fatalf("a %s task made the person's folder into a repository", mode)
		}
	}
}

// AND THE DEGRADATION IS LOUD. A family tree that could not be opened is still a
// family tree — the work runs, the parts share the directory — but the parent is
// told so in a sentence it can act on, rather than finding out by having its
// siblings write over each other.
func TestAFamilyTreeThatCannotBeOpenedSaysSo(t *testing.T) {
	// A directory that is not there is every reason git init fails, spelled in
	// the one way a test can rely on: no git on the machine, a read-only disk, a
	// folder somebody moved.
	tree := openFamilyTree(taskTree{
		dir:   filepath.Join(t.TempDir(), "gone"),
		mode:  TaskModeMirror,
		merge: mergeInPlace,
	})
	if tree.note == "" {
		t.Fatal("a family that lost its tree says nothing about it")
	}
	if !strings.Contains(tree.note, "in this same folder beside you") {
		t.Fatalf("the sentence is %q; it does not say where the parts will be working", tree.note)
	}
	if !strings.Contains(tree.note, "write different files") {
		t.Fatalf("the sentence is %q; it does not say what to do about it", tree.note)
	}
}
