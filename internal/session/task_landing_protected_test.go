package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// C7: a completed task never advances a protected branch, including a remote's
// default branch, and its branch is left as the finished result.
func TestC7AProtectedCheckoutKeepsCompletedWorkOnItsTaskBranch(t *testing.T) {
	for _, branch := range []string{"main", "dev", "ship"} {
		t.Run(branch, func(t *testing.T) {
			repo := newTestRepo(t)
			mustGit(t, repo, "checkout", "-b", branch)
			if branch == "ship" {
				remote := filepath.Join(t.TempDir(), "origin.git")
				if err := os.MkdirAll(remote, 0o755); err != nil {
					t.Fatal(err)
				}
				mustGit(t, remote, "init", "--bare")
				mustGit(t, repo, "remote", "add", "origin", remote)
				mustGit(t, repo, "push", "-u", "origin", branch)
				mustGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/"+branch)
				mustGit(t, repo, "remote", "set-head", "origin", "-a")
			}
			place := Place{Dir: t.TempDir(), Workspace: repo}
			beforeHead := strings.TrimSpace(gitOut(t, repo, "rev-parse", branch))
			beforeStatus := gitOut(t, repo, "status", "--porcelain")

			tree, err := prepareTaskTree(place, repo, "protected", 1, "write the protected case")
			if err != nil {
				t.Fatalf("prepareTaskTree: %v", err)
			}
			writeFile(t, filepath.Join(tree.dir, "protected.txt"), branch+"\n")
			merge, detail, _ := tree.comeHome("write the protected case", []string{"protected.txt"})
			if merge != mergeKept {
				t.Fatalf("merge = %q (%s), want %q", merge, detail, mergeKept)
			}
			wantSentence := "its branch " + tree.branch + " was kept: your checkout is on " + branch +
				", which aforge never writes to — merge it when you are ready"
			if !strings.Contains(detail, wantSentence) {
				t.Fatalf("landing detail does not say why the branch was kept:\n%s", detail)
			}
			if got := strings.TrimSpace(gitOut(t, repo, "rev-parse", branch)); got != beforeHead {
				t.Fatalf("%s moved from %s to %s", branch, beforeHead, got)
			}
			if got := gitOut(t, repo, "status", "--porcelain"); got != beforeStatus {
				t.Fatalf("checkout status changed from %q to %q", beforeStatus, got)
			}
			if got := gitOut(t, repo, "show", tree.branch+":protected.txt"); got != branch+"\n" {
				t.Fatalf("kept branch holds %q", got)
			}
			if got := taskArtifactURI(tree.dir, tree.branch, merge); got != "git:"+tree.branch {
				t.Fatalf("kept task artifact = %q, want its branch", got)
			}
			if list := gitOut(t, repo, "worktree", "list", "--porcelain"); strings.Contains(list, tree.dir) {
				t.Fatalf("the task working copy stayed registered:\n%s", list)
			}

			notice := TaskNotice{ID: 1, Title: "Write the protected case", State: TaskDone,
				Report: detail, Changed: []string{"protected.txt"}, Branch: tree.branch, Merge: merge}
			if notice.State != TaskDone {
				t.Fatalf("state = %q, want done", notice.State)
			}
			note := taskNote(notice, "", TaskSettleAsk, landingAddress{person: true})
			if strings.Count(note, wantSentence) != 1 {
				t.Fatalf("completion note does not carry the protected sentence once:\n%s", note)
			}
		})
	}
}

// C8: a checkout moved to another branch after the cut, or detached before the
// landing, is never chosen as the destination.
func TestC8AMovedOrDetachedCheckoutKeepsTheTaskBranch(t *testing.T) {
	t.Run("moved", func(t *testing.T) {
		repo := newTestRepo(t)
		tree, err := prepareTaskTree(Place{}, repo, "moved", 1, "write after the move")
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(tree.dir, "moved.txt"), "kept\n")
		mustGit(t, repo, "checkout", "-b", "other")
		merge, detail, _ := tree.comeHome("write after the move", []string{"moved.txt"})
		want := "its branch " + tree.branch + " was kept: your checkout has moved from work to other since the work was cut — merge it where you want it"
		if merge != mergeKept || !strings.Contains(detail, want) {
			t.Fatalf("landing = %q, %q; want moved-checkout keep", merge, detail)
		}
		if _, err := os.Stat(filepath.Join(repo, "moved.txt")); !os.IsNotExist(err) {
			t.Fatalf("the moved checkout received the file: %v", err)
		}
	})

	t.Run("detached", func(t *testing.T) {
		repo := newTestRepo(t)
		tree, err := prepareTaskTree(Place{}, repo, "detached", 1, "write while detached")
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(tree.dir, "detached.txt"), "kept\n")
		mustGit(t, repo, "checkout", "--detach")
		merge, detail, _ := tree.comeHome("write while detached", []string{"detached.txt"})
		want := "its branch " + tree.branch + " was kept: your checkout is not on a branch — check one out and merge it"
		if merge != mergeKept || !strings.Contains(detail, want) {
			t.Fatalf("landing = %q, %q; want detached-checkout keep", merge, detail)
		}
		if _, err := os.Stat(filepath.Join(repo, "detached.txt")); !os.IsNotExist(err) {
			t.Fatalf("the detached checkout received the file: %v", err)
		}
	})
}

// C9: the repository owned by a conversation is working material rather than
// the person's checkout, so its default branch still receives finished work.
func TestC9AnOwnedWorkspaceStillMergesOnItsDefaultBranch(t *testing.T) {
	place, work := newOwnedPlace(t)
	tree, err := prepareTaskTree(place, work, "owned", 1, "write in owned work")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(tree.dir, "owned.txt"), "landed\n")
	if merge, detail, _ := tree.comeHome("write in owned work", []string{"owned.txt"}); merge != mergeMerged {
		t.Fatalf("merge = %q (%s), want the owned workspace to merge", merge, detail)
	}
	if got := readFile(t, filepath.Join(work, "owned.txt")); got != "landed\n" {
		t.Fatalf("owned file = %q", got)
	}
}

// C12: a checkpoint written before home was recorded still merges on a feature
// branch and still protects a named trunk branch.
func TestC12ARecordWithoutHomeStillLandsByTheCurrentBranchPolicy(t *testing.T) {
	t.Run("feature branch", func(t *testing.T) {
		repo := newTestRepo(t)
		tree, err := prepareTaskTree(Place{}, repo, "old-feature", 1, "write from an old record")
		if err != nil {
			t.Fatal(err)
		}
		tree.home = ""
		writeFile(t, filepath.Join(tree.dir, "old.txt"), "merged\n")
		if merge, detail, _ := tree.comeHome("write from an old record", []string{"old.txt"}); merge != mergeMerged {
			t.Fatalf("merge = %q (%s), want feature-branch merge", merge, detail)
		}
	})

	t.Run("protected branch", func(t *testing.T) {
		repo := newTestRepo(t)
		mustGit(t, repo, "checkout", "-b", "main")
		tree, err := prepareTaskTree(Place{}, repo, "old-main", 1, "write from an old record")
		if err != nil {
			t.Fatal(err)
		}
		tree.home = ""
		writeFile(t, filepath.Join(tree.dir, "old.txt"), "kept\n")
		if merge, detail, _ := tree.comeHome("write from an old record", []string{"old.txt"}); merge != mergeKept {
			t.Fatalf("merge = %q (%s), want protected-branch keep", merge, detail)
		}
	})
}

// C15: A GROUND THAT IS A SUBDIRECTORY OF THE PERSON'S REPOSITORY IS STILL THE
// PERSON'S REPOSITORY.
//
// [taskTree.landsInThePersonsRepository] used to answer false the moment `root`
// and `ground` differed, and [Agent.Land] builds its tree with
// `ground: tree.Folder` and `root: tree.Root` — so a folder one level inside a
// checkout skipped every protection in this file and merged onto whatever
// branch the person was standing on.
//
// Nothing could reach that: ReferPlace snaps a referred path to the repository
// root, and so does groundRoot on the task ladder. This test exists because
// that invariant is enforced in OTHER files, with nothing pinning it where the
// guard relies on it — and the guard is the piece whose failure costs somebody
// their working tree.
func TestC15ARepositorySubdirectoryGroundIsStillProtected(t *testing.T) {
	repo := newTestRepo(t)
	notes := filepath.Join(repo, "notes")
	if err := os.MkdirAll(notes, 0o755); err != nil {
		t.Fatal(err)
	}

	inside := taskTree{root: repo, ground: notes, home: "work"}
	if !inside.landsInThePersonsRepository() {
		t.Errorf("a ground inside the person's repository (%s under %s) is not read as theirs, "+
			"so a landing there would merge onto their branch with none of this file's protection",
			notes, repo)
	}

	// AND A GROUND THAT IS NOT UNDER THE ROOT AT ALL IS STILL NOT THEIRS, which
	// is the case the old comparison was written for.
	elsewhere := taskTree{root: repo, ground: t.TempDir(), home: "work"}
	if elsewhere.landsInThePersonsRepository() {
		t.Error("a ground outside the root is being read as the person's repository")
	}
}
