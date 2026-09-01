package session

// A LANDING THAT COULD NOT SAVE THE WORK, AS TESTS (issues #255 and #256).
//
// Both defects were one shape: the failing road and the succeeding road answered
// the same outcome, so every caller read "done" off a landing that had saved
// nothing. On a repository ground the node's commit was thrown away, an empty
// branch merged cleanly, and the working copy holding the only copy of the work
// was removed — the file was gone from both places. On a folder ground the copy
// stopped at the first path it could not place, leaving the person's folder with
// half a deliverable under a row that read finished.
//
// So every test here asserts the same two things about a landing that failed:
// THE WORK IS STILL THERE, and the node says somebody has to look.

import (
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readOnlyGitDir makes the one directory git has to write to stage anything
// unwritable, which is what a full disk, a read-only mount or a permission
// somebody changed looks like from in here.
func readOnlyGitDir(t *testing.T, tree taskTree) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root writes through a read-only directory, so this cannot be staged")
	}
	gitdir := strings.TrimSpace(gitOut(t, tree.dir, "rev-parse", "--absolute-git-dir"))
	if err := os.Chmod(gitdir, 0o555); err != nil {
		t.Fatalf("making %s read-only: %v", gitdir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(gitdir, 0o755) })
}

// ISSUE #255, THE REPLICATION. One repository ground, one file the worker wrote,
// and a worktree that cannot be committed into. Nothing merges, nothing is
// released, and the file is still on disk where the sentence says it is.
func TestALandingThatCouldNotSaveTheWorkKeepsIt(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "ffff6666ffff6666", 17, "add the parser")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "parser.py"), "def parse():\n    return 1\n")
	readOnlyGitDir(t, tree)

	merge, detail := tree.comeHome("add the parser", []string{"parser.py"})

	if cameHome(merge) {
		t.Fatalf("merge = %q (%s), want a landing that saved nothing to refuse", merge, detail)
	}
	if merge != mergeAborted {
		t.Fatalf("merge = %q, want %q", merge, mergeAborted)
	}
	// THE SENTENCE NAMES THE DIRECTORY, because that directory holds the only
	// copy of the work there is, and it quotes git rather than paraphrasing it.
	if !strings.Contains(detail, tree.dir) || !strings.Contains(detail, "could not be saved") {
		t.Fatalf("the landing said %q, want it to name where the work is", detail)
	}
	if !strings.Contains(detail, "Permission denied") {
		t.Fatalf("the landing said %q, want git's own account of what went wrong", detail)
	}
	// THE WORK IS STILL THERE. This is the whole defect: it used to be in neither
	// the repository nor the worktree.
	if _, err := os.Stat(filepath.Join(tree.dir, "parser.py")); err != nil {
		t.Fatalf("the landing destroyed the only copy of the work: %v", err)
	}
	// AND SO IS THE WORKING COPY IT IS IN, still registered with the repository
	// rather than left as a directory a sweep may take away.
	worktrees := gitOut(t, repo, "worktree", "list")
	if !strings.Contains(worktrees, tree.dir) {
		t.Fatalf("the worktree was released by a landing that saved nothing:\n%s", worktrees)
	}
	if strings.Contains(worktrees, "prunable") {
		t.Fatalf("the working copy was removed under its own registration:\n%s", worktrees)
	}
	if _, err := git(repo, "rev-parse", "--verify", tree.branch); err != nil {
		t.Fatalf("the branch was deleted by a landing that saved nothing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "parser.py")); err == nil {
		t.Fatal("an empty branch was merged onto the person's own")
	}
}

// THE CONTROL. Nothing about the ordinary road moves: the same task with a
// writable worktree still merges, the file reaches the person's branch, and the
// working copy and the branch are both given back.
func TestALandingThatCouldSaveTheWorkStillComesHome(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "ffff6666ffff7777", 18, "add the parser")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "parser.py"), "def parse():\n    return 1\n")

	merge, detail := tree.comeHome("add the parser", []string{"parser.py"})

	if merge != mergeMerged {
		t.Fatalf("merge = %q (%s), want it merged", merge, detail)
	}
	if detail != "" {
		t.Fatalf("an ordinary landing said %q, want nothing said twice", detail)
	}
	if _, err := os.Stat(filepath.Join(repo, "parser.py")); err != nil {
		t.Fatalf("the work did not reach the person's branch: %v", err)
	}
	if _, err := os.Stat(tree.dir); err == nil {
		t.Fatal("the working copy was kept after its work went in")
	}
	if _, err := git(repo, "rev-parse", "--verify", tree.branch); err == nil {
		t.Fatalf("branch %s was kept after its work merged", tree.branch)
	}
}

// AND THE NODE SAYS SOMEBODY HAS TO LOOK, on the road a person takes hours
// later: an accept over a working copy that cannot be committed into settles
// needing a look with the directory named, not done.
func TestAcceptingWorkThatCannotBeSavedNeedsALook(t *testing.T) {
	agent, node := unverifiedNode(t, nil)
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "ffff6666ffff8888", 19, "add the parser")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "parser.py"), "def parse():\n    return 1\n")
	node.setTree(tree)
	node.finish("wrote the parser", []string{"parser.py"}, tree.branch, tree.merge)
	readOnlyGitDir(t, tree)

	if err := agent.acceptTask(node, "I read it myself"); err != nil {
		t.Fatalf("acceptTask: %v", err)
	}

	if state := node.stateNow(); state != TaskUnverified {
		t.Fatalf("state = %q, want it to need a look: nobody could put the work away", state)
	}
	report, _, _, merge := node.leavings()
	if !strings.HasPrefix(report, needsLookLead) {
		t.Fatalf("the report leads with %q, want the words a person reads for a landing nobody could finish", report)
	}
	if !strings.Contains(report, tree.dir) {
		t.Fatalf("the report never says where the work is:\n%s", report)
	}
	if merge != mergeAborted {
		t.Fatalf("merge = %q, want %q", merge, mergeAborted)
	}
	if _, err := os.Stat(filepath.Join(tree.dir, "parser.py")); err != nil {
		t.Fatalf("the accept destroyed the only copy of the work: %v", err)
	}
	// AND THE NOTE DOES NOT OFFER A BRANCH THAT HOLDS NOTHING. The mark it wears
	// is the one a kept branch wears, and the note tells the two apart by the
	// sentence the report leads with.
	note := taskNote(node.notice(), "", TaskSettleAsk, landingAddress{person: true})
	if strings.Contains(note, "merge that branch to take the work") {
		t.Fatalf("the note offers a branch nothing was committed to:\n%s", note)
	}
	if !strings.Contains(note, tree.dir) {
		t.Fatalf("the note never says where the work is:\n%s", note)
	}
}

// ISSUE #256, THE REPLICATION. A folder family whose ledger cannot be laid in
// full lays NONE of it: the person's folder is as it was, and everything the
// family made is still in the copy the sentence names.
func TestAFolderLandingThatCannotBeLaidInFullLaysNothing(t *testing.T) {
	folder, _, parent := familyOnAFolder(t)
	writeFile(t, filepath.Join(parent.dir, "a.md"), "the first half\n")
	writeFile(t, filepath.Join(parent.dir, "sub", "b.md"), "the second half\n")
	// The person's folder has a FILE where the second half needs a directory.
	writeFile(t, filepath.Join(folder, "sub"), "not a directory\n")
	before := folderContents(t, folder)

	merge, detail := parent.comeHome("write the report", []string{"a.md", "sub/b.md"})

	if cameHome(merge) {
		t.Fatalf("merge = %q (%s), want a half-lay to refuse", merge, detail)
	}
	if merge != mergeAborted {
		t.Fatalf("merge = %q, want %q", merge, mergeAborted)
	}
	if !strings.Contains(detail, parent.dir) || !strings.Contains(detail, folder) {
		t.Fatalf("the landing said %q, want it to name the copy and the folder", detail)
	}
	// THE FOLDER IS AS IT WAS — not one of the ledger's files reached it, and
	// nothing of the harness's own was left lying in it either.
	if after := folderContents(t, folder); after != before {
		t.Fatalf("the folder changed under a landing that refused:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	// AND THE WORK IS ALL STILL IN THE COPY.
	for _, path := range []string{"a.md", filepath.Join("sub", "b.md")} {
		if _, err := os.Stat(filepath.Join(parent.dir, path)); err != nil {
			t.Fatalf("%s went missing from the family's own copy: %v", path, err)
		}
	}
}

// THE CONTROL. An ordinary folder family still lays every file, answers the
// in-place mark, and says nothing extra.
func TestAnOrdinaryFolderLandingStillLaysEveryFile(t *testing.T) {
	folder, _, parent := familyOnAFolder(t)
	writeFile(t, filepath.Join(parent.dir, "a.md"), "the first half\n")
	writeFile(t, filepath.Join(parent.dir, "sub", "b.md"), "the second half\n")

	merge, detail := parent.comeHome("write the report", []string{"a.md", "sub/b.md"})

	if merge != mergeInPlace || detail != "" {
		t.Fatalf("merge = %q (%s), want an ordinary lay saying nothing", merge, detail)
	}
	if got := readFile(t, filepath.Join(folder, "a.md")); got != "the first half\n" {
		t.Fatalf("a.md in the folder is %q", got)
	}
	if got := readFile(t, filepath.Join(folder, "sub", "b.md")); got != "the second half\n" {
		t.Fatalf("sub/b.md in the folder is %q", got)
	}
}

// AND THE LATE ROAD REFUSES IT TOO. A folder family accepted by hand hours
// later, over a folder that cannot take the lay, settles needing a look rather
// than done — with the folder still untouched.
func TestAcceptingAFolderFamilyThatCannotBeLaidNeedsALook(t *testing.T) {
	agent, node := unverifiedNode(t, nil)
	folder, _, parent := familyOnAFolder(t)
	writeFile(t, filepath.Join(parent.dir, "a.md"), "the first half\n")
	writeFile(t, filepath.Join(parent.dir, "sub", "b.md"), "the second half\n")
	writeFile(t, filepath.Join(folder, "sub"), "not a directory\n")
	before := folderContents(t, folder)
	node.setTree(parent)
	node.finish("wrote the report", []string{"a.md", "sub/b.md"}, parent.branch, parent.merge)

	if err := agent.acceptTask(node, "I read it myself"); err != nil {
		t.Fatalf("acceptTask: %v", err)
	}

	if state := node.stateNow(); state != TaskUnverified {
		t.Fatalf("state = %q, want it to need a look", state)
	}
	report, _, _, _ := node.leavings()
	if !strings.HasPrefix(report, needsLookLead) {
		t.Fatalf("the report leads with %q", report)
	}
	if after := folderContents(t, folder); after != before {
		t.Fatalf("the accept laid half a deliverable into the folder:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// A LAY THAT FAILS MOVES NOTHING, and one that succeeds replaces each file
// whole. The second half is what a rename buys over a write in place: a file the
// person has made read-only is REPLACED rather than opened and truncated, and
// there is no moment in which it holds half of each version.
func TestALayIsAllOfTheLedgerOrNoneOfIt(t *testing.T) {
	from, to := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(from, "keep.md"), "the new line\n")
	writeFile(t, filepath.Join(from, "sub", "b.md"), "the second half\n")
	writeFile(t, filepath.Join(to, "keep.md"), "what the person had\n")
	writeFile(t, filepath.Join(to, "sub"), "not a directory\n")

	if problem := layWork(from, to, []string{"keep.md", "sub/b.md"}); problem == "" {
		t.Fatal("a lay that cannot place the whole ledger reported no problem")
	}
	if got := readFile(t, filepath.Join(to, "keep.md")); got != "what the person had\n" {
		t.Fatalf("keep.md is %q, want the person's own file untouched", got)
	}
	if left, err := os.ReadDir(to); err == nil {
		for _, entry := range left {
			if strings.Contains(entry.Name(), layingSuffix) {
				t.Fatalf("a refused lay left %s behind", entry.Name())
			}
		}
	}

	// The way is clear now, and the read-only file the person left is replaced
	// rather than written into.
	if err := os.Remove(filepath.Join(to, "sub")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(to, "keep.md"), 0o444); err != nil {
		t.Fatal(err)
	}
	if problem := layWork(from, to, []string{"keep.md", "sub/b.md"}); problem != "" {
		t.Fatalf("the lay refused a ledger that fits: %s", problem)
	}
	if got := readFile(t, filepath.Join(to, "keep.md")); got != "the new line\n" {
		t.Fatalf("keep.md is %q, want the work over it", got)
	}
	if got := readFile(t, filepath.Join(to, "sub", "b.md")); got != "the second half\n" {
		t.Fatalf("sub/b.md is %q", got)
	}
}

// folderContents is every path under a folder with what is in it, as one string:
// the cheapest honest reading of "the folder is as it was".
func folderContents(t *testing.T, folder string) string {
	t.Helper()
	var seen strings.Builder
	err := filepath.Walk(folder, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(folder, path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			seen.WriteString(relative + "/\n")
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		seen.WriteString(relative + ": " + string(content) + "\n")
		return nil
	})
	if err != nil {
		t.Fatalf("reading %s: %v", folder, err)
	}
	return seen.String()
}

// savedWorkAnswerDropped names the functions allowed to throw away what
// [stageTaskWork] or [commitTaskWork] answered, and says why each may.
//
// EVERY OTHER CALL SITE READS IT. That is the whole of #255: three silent
// endings, one dropped answer, and a landing that merged an empty branch over
// the work and then removed the directory it was in. A new caller that ignores
// the answer fails this test, and the entry it has to write is the place its
// author answers the question in words.
var savedWorkAnswerDropped = map[string]string{
	"auditNode": "the check's staging is preparation and not a landing. A tree that cannot be " +
		"staged into is caught by [taskTree.comeHome], which refuses to land it and writes the " +
		"person the sentence naming where the work is; stopping the check here would spend that " +
		"ending on a duller one and answer nothing about the work.",
}

// TestNoLandingDropsWhatASaveAnswered fails when a call to either save discards
// the failure without an entry above.
func TestNoLandingDropsWhatASaveAnswered(t *testing.T) {
	saves := map[string]int{"stageTaskWork": 0, "commitTaskWork": 1}
	found := map[string]bool{}
	forEachPackageFile(t, func(path string, file *ast.File, fset *token.FileSet) {
		var enclosing string
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.FuncDecl:
				enclosing = typed.Name.Name
			case *ast.ExprStmt:
				// A bare call throws away everything it answered.
				name, _, ok := savedWorkCall(typed.X, saves)
				if !ok {
					return true
				}
				reportDroppedSave(t, path, fset, name, enclosing, typed.Pos(), found)
			case *ast.AssignStmt:
				if len(typed.Rhs) != 1 {
					return true
				}
				name, at, ok := savedWorkCall(typed.Rhs[0], saves)
				if !ok || at >= len(typed.Lhs) {
					return true
				}
				if blank, is := typed.Lhs[at].(*ast.Ident); !is || blank.Name != "_" {
					return true
				}
				reportDroppedSave(t, path, fset, name, enclosing, typed.Pos(), found)
			}
			return true
		})
	})
	for road := range savedWorkAnswerDropped {
		if !found[road] {
			t.Errorf("savedWorkAnswerDropped names %q and nothing there drops a save's answer any "+
				"more — remove the entry", road)
		}
	}
}

// savedWorkCall reads a call to one of the two saves and answers WHICH of its
// results carries the failure, so the assignment shape can look at that one
// alone.
func savedWorkCall(expression ast.Expr, saves map[string]int) (string, int, bool) {
	call, ok := expression.(*ast.CallExpr)
	if !ok {
		return "", 0, false
	}
	name, ok := call.Fun.(*ast.Ident)
	if !ok {
		return "", 0, false
	}
	at, ok := saves[name.Name]
	return name.Name, at, ok
}

// reportDroppedSave is the failure itself, written once for the two shapes of
// discarded answer.
func reportDroppedSave(t *testing.T, path string, fset *token.FileSet, name, enclosing string, at token.Pos, found map[string]bool) {
	t.Helper()
	if _, known := savedWorkAnswerDropped[enclosing]; !known {
		t.Errorf("%s:%d: %s throws away what %s answered, and task_unsaved_test.go's "+
			"savedWorkAnswerDropped does not say why it may.\n"+
			"A save that failed and a node that only read look identical from here, and a landing "+
			"that cannot tell them apart merges an empty branch over the work (#255). Read the "+
			"answer, or add the entry saying why this one need not.",
			filepath.Base(path), fset.Position(at).Line, enclosing, name)
		return
	}
	found[enclosing] = true
}

// ONE FILE LEFT OUT IS ENOUGH. The batch fails over a path git cannot read, the
// retry gets the rest of the ledger in, and the landing still refuses — because
// merging now would put the branch in and take away the only copy of the file
// that did not make it.
func TestALandingRefusesWhenOnePathOfTheLedgerCouldNotBeStaged(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads through a file with no permissions, so nothing can be refused")
	}
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "ffff6666ffff9999", 20, "write both halves")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "open.md"), "the half that fits\n")
	writeFile(t, filepath.Join(tree.dir, "closed.md"), "the half that does not\n")
	if err := os.Chmod(filepath.Join(tree.dir, "closed.md"), 0o000); err != nil {
		t.Fatal(err)
	}

	merge, detail := tree.comeHome("write both halves", []string{"open.md", "closed.md"})

	if merge != mergeAborted {
		t.Fatalf("merge = %q (%s), want the landing to refuse over the path it could not take", merge, detail)
	}
	for _, name := range []string{"open.md", "closed.md"} {
		if _, err := os.Lstat(filepath.Join(tree.dir, name)); err != nil {
			t.Fatalf("%s went missing from the working copy: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(repo, name)); err == nil {
			t.Fatalf("%s was merged onto the person's branch by a landing that refused", name)
		}
	}
}

// A NODE THAT SETTLES WITHOUT MERGING KEEPS ITS WORKING COPY when the branch
// could not take the work either. The sentence it is settled with offers a
// branch; the directory is the only place the work actually is, so it stays
// registered rather than being handed back to a later sweep.
func TestAKeptBranchThatCouldNotBeCommittedKeepsItsWorkingCopy(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "ffff6666ffffaaaa", 21, "build it")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "main.go"), "package main\n")
	readOnlyGitDir(t, tree)

	merge, _ := keptWork(tree, "build it", []string{"main.go"})

	if merge != mergeAborted {
		t.Fatalf("merge = %q, want the branch kept", merge)
	}
	if _, err := os.Stat(filepath.Join(tree.dir, "main.go")); err != nil {
		t.Fatalf("the work was destroyed by a settlement that saved nothing: %v", err)
	}
	worktrees := gitOut(t, repo, "worktree", "list")
	if !strings.Contains(worktrees, tree.dir) || strings.Contains(worktrees, "prunable") {
		t.Fatalf("the working copy holding the only copy of the work was given back:\n%s", worktrees)
	}
}

// AND THE JOB LOG SAYS IT DID NOT MERGE, with the mark the landing handed over
// rather than the conflict this settler used to assume.
func TestASettledLandingThatSavedNothingSaysSoInTheJobLog(t *testing.T) {
	agent := &Agent{}
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	node := &TaskNode{graph: graph, id: 1, spec: taskSpec{title: "add the parser"}, state: TaskRunning}
	graph.mu.Lock()
	graph.nodes[node.id] = node
	graph.order = append(graph.order, node.id)
	graph.mu.Unlock()
	tree := taskTree{dir: "/somewhere/trees/1", root: "/somewhere", branch: "task/add-the-parser"}
	detail := unsavedSentence(tree.dir, "fatal: Unable to create index.lock: Permission denied")

	var log strings.Builder
	state := agent.landConflicted(node, tree, []string{"parser.py"},
		"it wrote the parser", mergeAborted, detail, &log)

	if state != TaskUnverified {
		t.Fatalf("state = %q, want it to need a look", state)
	}
	if !strings.Contains(log.String(), "not merged: "+detail) {
		t.Fatalf("the job log says %q", log.String())
	}
	_, _, _, merge := node.leavings()
	if merge != mergeAborted {
		t.Fatalf("merge = %q, want the mark the landing handed over", merge)
	}
}
