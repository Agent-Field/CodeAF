package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

func cdiffT(t *testing.T, workspace, path, revision string) (appwire.CheckoutDiffResult, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), appwire.CheckoutSeconds*time.Second)
	defer cancel()
	return appCheckoutDiff(ctx, appCanonical(workspace), appwire.CheckoutPathQuery{Path: path, Revision: revision})
}

func clocT(t *testing.T, workspace, path, revision string) (appwire.CheckoutLocation, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), appwire.CheckoutSeconds*time.Second)
	defer cancel()
	return appCheckoutLocate(ctx, appCanonical(workspace), appwire.CheckoutPathQuery{Path: path, Revision: revision})
}

// THE REVIEW OF A ROOT ROW FROM A SUBFOLDER WORKSPACE IS THE ROOT FILE: root.txt
// and sub/root.txt both exist and both changed, differently. The path is the
// status row's, never joined to the workspace.
func TestCheckoutDiffRootRowFromSubWorkspace(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "root.txt"), "r1\nr2\n")
	write(t, filepath.Join(repo, "sub", "root.txt"), "s1\ns2\n")
	gitT(t, repo, "add", "root.txt", "sub")
	gitT(t, repo, "commit", "-q", "-m", "two")
	write(t, filepath.Join(repo, "root.txt"), "r1\nr2\nROOT-ADDED\n")
	write(t, filepath.Join(repo, "sub", "root.txt"), "s1\nSUB-ADDED\n")
	sub := filepath.Join(repo, "sub")

	got, err := cdiffT(t, sub, "root.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Entry.Path != "root.txt" || got.Diff.State != appwire.DiffChanged || !strings.Contains(got.Diff.Text, "+ROOT-ADDED") ||
		strings.Contains(got.Diff.Text, "SUB-ADDED") || got.WorkspacePath != "" || !got.Diff.Outside || got.Revision == "" {
		t.Fatalf("root row: %+v", got)
	}
	if got.Repo != appCanonical(repo) || got.Open != filepath.Join(appCanonical(repo), "root.txt") {
		t.Fatalf("repo/open: %q %q", got.Repo, got.Open)
	}
	nested, err := cdiffT(t, sub, "sub/root.txt", got.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(nested.Diff.Text, "+SUB-ADDED") || strings.Contains(nested.Diff.Text, "ROOT-ADDED") ||
		nested.WorkspacePath != "root.txt" || nested.Diff.Outside {
		t.Fatalf("nested row: %+v", nested)
	}
	// And the same rows from the root workspace.
	rootSide, err := cdiffT(t, repo, "sub/root.txt", "")
	if err != nil || rootSide.WorkspacePath != "sub/root.txt" || !strings.Contains(rootSide.Diff.Text, "+SUB-ADDED") {
		t.Fatalf("%v %+v", err, rootSide)
	}
}

// ONLY A STATUS ROW CAN BE ASKED ABOUT: not a path outside, not an absolute
// one, not a tracked file nobody changed, not an ignored one.
func TestCheckoutDiffRefusesWhatIsNotARow(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, ".gitignore"), "ign.txt\n")
	write(t, filepath.Join(repo, "ign.txt"), "x\n")
	write(t, filepath.Join(repo, "a.txt"), "changed\n")
	outside := filepath.Join(t.TempDir(), "secret")
	write(t, outside, "s\n")
	for _, path := range []string{"ign.txt", "nope.txt", ".gitignore2", "sub/missing.txt"} {
		if _, err := cdiffT(t, repo, path, ""); wireCode(err) != appwire.CodeNotFound {
			t.Fatalf("%s answered %v", path, err)
		}
	}
	for _, path := range []string{"", "/etc/passwd", outside, "../x", "a/../b", "./a.txt", "a//b", "a\\b", "a.txt/"} {
		if appwire.ValidCheckoutPath(path) {
			t.Fatalf("%q accepted", path)
		}
	}
	if _, err := cdiffT(t, repo, "a.txt", ""); err != nil {
		t.Fatal(err)
	}
}

// A MOVED CHECKOUT REFUSES THE DISPLAYED REVISION and diffs nothing; an omitted
// revision is a fresh lookup that says which revision it used.
func TestCheckoutDiffStaleRevision(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "a.txt"), "one\ntwo\nthree\nfour\n")
	shown, err := cdiffT(t, repo, "a.txt", "")
	if err != nil || shown.Revision == "" {
		t.Fatalf("%v %+v", err, shown)
	}
	write(t, filepath.Join(repo, "b.txt"), "new\n")
	_, err = cdiffT(t, repo, "a.txt", shown.Revision)
	var wire *appwire.Error
	if wireCode(err) != appwire.CodeConflict {
		t.Fatalf("stale answered %v", err)
	}
	wire = err.(*appwire.Error)
	if d, ok := wire.Detail.(appwire.ConflictDetail); !ok || d.Reason != appwire.ConflictStaleRevision {
		t.Fatalf("detail %+v", wire.Detail)
	}
	fresh, err := cdiffT(t, repo, "a.txt", "")
	if err != nil || fresh.Revision == shown.Revision {
		t.Fatalf("fresh %v %q", err, fresh.Revision)
	}
	if _, err := clocT(t, repo, "a.txt", shown.Revision); wireCode(err) != appwire.CodeConflict {
		t.Fatalf("locate stale answered %v", err)
	}
}

func TestCheckoutDiffKinds(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "old.txt"), "keep this line\nand this one\nplus a third line\nand a fourth\n")
	write(t, filepath.Join(repo, "gone.txt"), "x\ny\n")
	gitT(t, repo, "add", "old.txt", "gone.txt")
	gitT(t, repo, "commit", "-q", "-m", "more")
	gitT(t, repo, "mv", "old.txt", "new.txt")
	write(t, filepath.Join(repo, "new.txt"), "keep this line\nand this one\nplus a third line\nand a fourth\nfifth\n")
	if err := os.Remove(filepath.Join(repo, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(repo, "fresh.txt"), "u1\nu2")
	write(t, filepath.Join(repo, "a.txt"), "one\ntwo\nthree\nfour\n")
	gitT(t, repo, "add", "a.txt")
	write(t, filepath.Join(repo, "a.txt"), "one\ntwo\nthree\nfour\nfive\n")

	ren, err := cdiffT(t, repo, "new.txt", "")
	if err != nil || ren.Diff.Change != "renamed" || ren.Diff.OldPath != "old.txt" || ren.Diff.Add == nil || *ren.Diff.Add != 1 {
		t.Fatalf("rename: %v %+v", err, ren.Diff)
	}
	del, err := cdiffT(t, repo, "gone.txt", "")
	if err != nil || del.Diff.Change != "deleted" || *del.Diff.Del != 2 || del.Open != "" {
		t.Fatalf("deleted: %v %+v open=%q", err, del.Diff, del.Open)
	}
	unt, err := cdiffT(t, repo, "fresh.txt", "")
	if err != nil || unt.Diff.State != appwire.DiffUntracked || *unt.Diff.Add != 2 || !strings.Contains(unt.Diff.Text, "No newline at end of file") {
		t.Fatalf("untracked: %v %+v", err, unt.Diff)
	}
	both, err := cdiffT(t, repo, "a.txt", "")
	if err != nil || both.Diff.Change != "modified" || *both.Diff.Add != 2 {
		t.Fatalf("staged+unstaged is one net diff: %v %+v", err, both.Diff)
	}
}

// STAGED EDITS THAT CANCEL ARE SAID TO BE UNCHANGED, NOT DRAWN AS EMPTY HUNKS.
func TestCheckoutDiffCancelledEditIsUnchanged(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "a.txt"), "ONE\ntwo\nthree\n")
	gitT(t, repo, "add", "a.txt")
	write(t, filepath.Join(repo, "a.txt"), "one\ntwo\nthree\n")
	got, err := cdiffT(t, repo, "a.txt", "")
	if err != nil || got.Diff.State != appwire.DiffUnchanged || got.Diff.Text != "" {
		t.Fatalf("%v %+v", err, got.Diff)
	}
}

func TestCheckoutDiffUnbornAndDetached(t *testing.T) {
	diffRepo(t)
	fresh := t.TempDir()
	gitT(t, fresh, "init", "-q", "-b", "trunk")
	write(t, filepath.Join(fresh, "s.txt"), "1\n2\n3\n")
	gitT(t, fresh, "add", "s.txt")
	got, err := cdiffT(t, fresh, "s.txt", "")
	if err != nil || got.Diff.State != appwire.DiffChanged || got.Diff.Change != "added" || *got.Diff.Add != 3 {
		t.Fatalf("unborn: %v %+v", err, got.Diff)
	}

	repo := diffRepo(t)
	head := gitT(t, repo, "rev-parse", "HEAD")
	gitT(t, repo, "checkout", "-q", "--detach", head)
	write(t, filepath.Join(repo, "a.txt"), "one\ntwo\nthree\nfour\n")
	det, err := cdiffT(t, repo, "a.txt", "")
	if err != nil || det.Diff.Commit != head || *det.Diff.Add != 1 {
		t.Fatalf("detached: %v %+v", err, det.Diff)
	}
}

// COPIED, CONFLICTED AND SUBMODULE ROWS ARE SAID UNSUPPORTED; never an empty diff.
func TestCheckoutDiffUnsupportedRows(t *testing.T) {
	repo := diffRepo(t)
	gitT(t, repo, "checkout", "-q", "-b", "side")
	write(t, filepath.Join(repo, "a.txt"), "side\ntwo\nthree\n")
	gitT(t, repo, "commit", "-q", "-am", "side")
	gitT(t, repo, "checkout", "-q", "main")
	write(t, filepath.Join(repo, "a.txt"), "main\ntwo\nthree\n")
	gitT(t, repo, "commit", "-q", "-am", "main")
	// The conflict is the point, so the failure is expected.
	_ = exec.Command("git", "-c", "user.name=t", "-c", "user.email=t@t", "-C", repo, "merge", "side").Run()
	got, err := cdiffT(t, repo, "a.txt", "")
	if err != nil || got.Diff.State != appwire.DiffUnavailable || got.Diff.Reason != appwire.DiffReasonUnsupportedRow ||
		got.Diff.Text != "" || got.Diff.Detail != appwire.CheckoutConflicted {
		t.Fatalf("conflicted: %v %+v", err, got.Diff)
	}
}

// NOTHING IN THE REPOSITORY IS WRITTEN OR RUN: the index keeps its bytes and
// mtime, and a required clean filter never fires (for a file it governs the diff
// is unavailable, for a file it does not, the diff is real).
func TestCheckoutDiffWritesNothingAndRunsNoFilter(t *testing.T) {
	repo := diffRepo(t)
	marker := filepath.Join(t.TempDir(), "ran")
	write(t, filepath.Join(repo, ".gitattributes"), "*.dat filter=lfsish\n")
	write(t, filepath.Join(repo, "f.dat"), "a\n")
	gitT(t, repo, "add", ".gitattributes", "f.dat")
	gitT(t, repo, "commit", "-q", "-m", "filtered")
	write(t, filepath.Join(repo, "f.dat"), "b\n")
	// A staged edit reverted on disk leaves the index's stat cache stale, which is
	// exactly what `git diff` (not diff-index) would rewrite.
	write(t, filepath.Join(repo, "c.txt"), "c1\nc2\nc3\n")
	gitT(t, repo, "add", "c.txt")
	gitT(t, repo, "commit", "-q", "-m", "c")
	write(t, filepath.Join(repo, "c.txt"), "c1\nCC\nc3\n")
	gitT(t, repo, "add", "c.txt")
	write(t, filepath.Join(repo, "c.txt"), "c1\nc2\nc3\n")
	write(t, filepath.Join(repo, "a.txt"), "one\nTWO\nthree\n")
	gitT(t, repo, "add", "a.txt")
	write(t, filepath.Join(repo, "a.txt"), "one\ntwo\nthree\nfour\n")
	gitT(t, repo, "config", "filter.lfsish.clean", "touch '"+marker+"'; cat")
	gitT(t, repo, "config", "filter.lfsish.required", "true")
	gitT(t, repo, "config", "diff.external", "touch '"+marker+"'; false")
	index := filepath.Join(repo, ".git", "index")
	before, _ := os.ReadFile(index)
	was, _ := os.Stat(index)
	filtered, err := cdiffT(t, repo, "f.dat", "")
	if err != nil || filtered.Diff.State != appwire.DiffUnavailable || filtered.Diff.Reason != appwire.DiffReasonFilter {
		t.Fatalf("filtered: %v %+v", err, filtered.Diff)
	}
	plain, err := cdiffT(t, repo, "a.txt", "")
	if err != nil || plain.Diff.State != appwire.DiffChanged {
		t.Fatalf("plain: %v %+v", err, plain.Diff)
	}
	if cancelled, err := cdiffT(t, repo, "c.txt", ""); err != nil || cancelled.Diff.State != appwire.DiffUnchanged {
		t.Fatalf("cancelled: %v %+v", err, cancelled.Diff)
	}
	after, _ := os.ReadFile(index)
	now, _ := os.Stat(index)
	if !bytes.Equal(before, after) || !was.ModTime().Equal(now.ModTime()) {
		t.Fatal("the index was written")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a filter or external diff ran")
	}
}

// AN UNTRACKED PATH SWAPPED FOR A LINK, A PIPE OR A LINKED PARENT IS NEVER READ.
func TestCheckoutDiffUntrackedUnsafeShapes(t *testing.T) {
	repo := diffRepo(t)
	secret := filepath.Join(t.TempDir(), "secret.txt")
	write(t, secret, "TOP-SECRET\n")
	if err := os.Symlink(secret, filepath.Join(repo, "link.txt")); err != nil {
		t.Skip("no symlinks")
	}
	if err := syscall.Mkfifo(filepath.Join(repo, "pipe"), 0o644); err != nil {
		t.Skip("no fifos")
	}
	write(t, filepath.Join(repo, "real", "in.txt"), "inside\n")
	for _, path := range []string{"link.txt", "pipe"} {
		done := make(chan appwire.CheckoutDiffResult, 1)
		go func() {
			got, _ := cdiffT(t, repo, path, "")
			done <- got
		}()
		select {
		case got := <-done:
			if got.Diff.Text != "" || strings.Contains(got.Diff.Text, "TOP-SECRET") || got.Diff.State == appwire.DiffUntracked && got.Diff.Add != nil {
				t.Fatalf("%s was read: %+v", path, got.Diff)
			}
			if got.Open != "" {
				t.Fatalf("%s offered for open: %q", path, got.Open)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s hung the call", path)
		}
	}
	// The helpers themselves, directly: a symlink PARENT inside the root, a
	// leaf swapped to a link after the fact, a file growing past the limit.
	if err := os.Symlink(filepath.Join(repo, "real"), filepath.Join(repo, "viaLink")); err != nil {
		t.Skip("no symlinks")
	}
	if data, reason := appReadContained(repo, "viaLink/in.txt", 1<<10); reason != "unreadable" || data != nil {
		t.Fatalf("a linked parent was read: %q %q", data, reason)
	}
	if appReachable(repo, "viaLink/in.txt") || appReachable(repo, "link.txt") || appReachable(repo, "pipe") {
		t.Fatal("an unsafe path was offered")
	}
	if !appReachable(repo, "real/in.txt") {
		t.Fatal("a real path was not offered")
	}
	if _, reason := appReadContained(repo, "real/in.txt", 3); reason != "too_large" {
		t.Fatalf("a file over the limit: %q", reason)
	}
}

// LOCATE OFFERS NO OPEN FOR A NON-UTF-8 OR DELETED ROW, AND NEVER BY A PATH THE
// CLIENT MADE; its facts match diff's.
func TestCheckoutLocateFacts(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "sub", "n.txt"), "n\n")
	write(t, filepath.Join(repo, "a.txt"), "one\ntwo\nthree\nfour\n")
	loc, err := clocT(t, filepath.Join(repo, "sub"), "sub/n.txt", "")
	if err != nil || loc.WorkspacePath != "n.txt" || loc.Open != filepath.Join(appCanonical(repo), "sub", "n.txt") || loc.Entry.Kind != appwire.CheckoutUntracked {
		t.Fatalf("%v %+v", err, loc)
	}
	out, err := clocT(t, filepath.Join(repo, "sub"), "a.txt", loc.Revision)
	if err != nil || out.WorkspacePath != "" || out.Open == "" || out.Revision != loc.Revision {
		t.Fatalf("%v %+v", err, out)
	}
	if err := os.Remove(filepath.Join(repo, "a.txt")); err != nil {
		t.Fatal(err)
	}
	gone, err := clocT(t, repo, "a.txt", "")
	if err != nil || gone.Open != "" {
		t.Fatalf("deleted: %v %+v", err, gone)
	}
}

// A CUT STATUS: a path inside the part read is answered, one outside it is
// too_large and not "not found".
func TestCheckoutDiffCutStatus(t *testing.T) {
	repo := diffRepo(t)
	for _, name := range []string{"aaa-first.txt", "zzz-last.txt"} {
		write(t, filepath.Join(repo, name), "x\n")
	}
	for i := 0; i < 40; i++ {
		write(t, filepath.Join(repo, "m-untracked-file-"+string(rune('a'+i%26))+string(rune('a'+i/26))+".txt"), "x\n")
	}
	was := appCheckoutOutMax
	appCheckoutOutMax = 700
	defer func() { appCheckoutOutMax = was }()
	if got, err := cdiffT(t, repo, "aaa-first.txt", ""); err != nil || got.Diff.State != appwire.DiffUntracked {
		t.Fatalf("inside the part read: %v %+v", err, got.Diff)
	}
	if _, err := cdiffT(t, repo, "zzz-last.txt", ""); wireCode(err) != appwire.CodeTooLarge {
		t.Fatalf("outside the part read answered %v", err)
	}
}

// THE CHECKOUT MOVING DURING THE COMPARISON IS SAID, NOT DRAWN.
func TestCheckoutDiffChangedDuringRead(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "a.txt"), "one\ntwo\nthree\nfour\n")
	appLinesBetween = func() {}
	hook := func() { write(t, filepath.Join(repo, "a.txt"), "one\ntwo\nthree\nfour\nfive\nsix\n") }
	appDiffBetween = hook
	defer func() { appDiffBetween = func() {} }()
	got, err := cdiffT(t, repo, "a.txt", "")
	if err != nil || got.Diff.State != appwire.DiffUnavailable || got.Diff.Reason != appwire.DiffReasonChanged || got.Diff.Text != "" {
		t.Fatalf("%v %+v", err, got.Diff)
	}
}

// THE POOL IS THE CALL'S: with room for one status and not a second, the diff is
// refused as too_large rather than confirmed against a short re-read.
func TestCheckoutDiffSharesTheBytePool(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "a.txt"), "one\ntwo\nthree\nfour\n")
	was := appCheckoutTotalMax
	defer func() { appCheckoutTotalMax = was }()
	for budget := 200; budget < 900; budget += 10 {
		appCheckoutTotalMax = budget
		got, err := cdiffT(t, repo, "a.txt", "")
		if err == nil && got.Diff.State == appwire.DiffUnavailable && got.Diff.Reason == appwire.DiffReasonTooLarge &&
			strings.Contains(got.Diff.Detail, "byte budget") {
			if got.Diff.Text != "" {
				t.Fatal("hunks survived an unconfirmed read")
			}
			return
		}
	}
	t.Fatal("no budget made the confirming status read overflow the shared pool")
}
