package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

func checkoutT(t *testing.T, dir string, q appwire.CheckoutQuery) (appwire.CheckoutStatus, error) {
	t.Helper()
	if q.Limit == 0 {
		q.Limit = appwire.CheckoutPageDefault
	}
	ctx, cancel := context.WithTimeout(context.Background(), appwire.CheckoutSeconds*time.Second)
	defer cancel()
	return appCheckout(ctx, appCanonical(dir), q)
}

// A CLEAN CHECKOUT IS PROVEN CLEAN: counts are present and zero because a
// complete read showed it, and the branch facts are git's own.
func TestCheckoutStatusClean(t *testing.T) {
	repo := diffRepo(t)
	got, err := checkoutT(t, repo, appwire.CheckoutQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Repo || got.Branch != "main" || len(got.Head) != 40 || got.Detached || got.Unborn || got.Operation != "" {
		t.Fatalf("branch facts: %+v", got)
	}
	if got.Counts == nil || *got.Counts != (appwire.CheckoutCounts{}) || len(got.Entries) != 0 || got.Next != "" || got.Incomplete {
		t.Fatalf("a clean checkout: %+v", got)
	}
	if got.Upstream != "" || got.Ahead != nil || got.Behind != nil {
		t.Fatalf("an upstream nobody configured must be absent, got %+v", got)
	}
}

// STAGED, UNSTAGED, UNTRACKED AND RENAMED PATHS ARE EACH THEIR OWN FACT, and a
// path both staged and edited again is counted on both sides.
func TestCheckoutStatusFacts(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "old.txt"), "keep this line\nand this one\nplus a third line\n")
	gitT(t, repo, "add", "old.txt")
	gitT(t, repo, "commit", "-q", "-m", "second")
	gitT(t, repo, "mv", "old.txt", "new.txt")
	write(t, filepath.Join(repo, "a.txt"), "one\ntwo\nthree\nfour\n")
	gitT(t, repo, "add", "a.txt")
	write(t, filepath.Join(repo, "a.txt"), "one\ntwo\nthree\nfour\nfive\n")
	write(t, filepath.Join(repo, "free.txt"), "x\n")

	got, err := checkoutT(t, repo, appwire.CheckoutQuery{})
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]appwire.CheckoutEntry{}
	for _, e := range got.Entries {
		by[e.Path] = e
	}
	if e := by["a.txt"]; e.Kind != appwire.CheckoutChanged || e.Index != "modified" || e.Worktree != "modified" {
		t.Fatalf("a.txt: %+v", e)
	}
	if e := by["new.txt"]; e.Kind != appwire.CheckoutRenamed || e.OldPath != "old.txt" || e.Index != "renamed" {
		t.Fatalf("new.txt: %+v", e)
	}
	if e := by["free.txt"]; e.Kind != appwire.CheckoutUntracked || e.Index != "" || e.Worktree != "" {
		t.Fatalf("free.txt: %+v", e)
	}
	want := appwire.CheckoutCounts{Staged: 2, Unstaged: 1, Untracked: 1}
	if got.Counts == nil || *got.Counts != want {
		t.Fatalf("counts %+v, want %+v", got.Counts, want)
	}
}

// A MERGE THE PERSON LEFT HALF-DONE IS NAMED, and its paths are conflicts.
func TestCheckoutStatusConflictAndOperation(t *testing.T) {
	repo := diffRepo(t)
	gitT(t, repo, "checkout", "-q", "-b", "side")
	write(t, filepath.Join(repo, "a.txt"), "side\ntwo\nthree\n")
	gitT(t, repo, "commit", "-q", "-am", "side")
	gitT(t, repo, "checkout", "-q", "main")
	write(t, filepath.Join(repo, "a.txt"), "main\ntwo\nthree\n")
	gitT(t, repo, "commit", "-q", "-am", "main")
	// The conflict is the point, so the failure is expected.
	_ = exec.Command("git", "-c", "user.name=t", "-c", "user.email=t@t", "-C", repo, "merge", "side").Run()

	got, err := checkoutT(t, repo, appwire.CheckoutQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Operation != appwire.CheckoutOpMerge {
		t.Fatalf("operation %q", got.Operation)
	}
	if len(got.Entries) != 1 || got.Entries[0].Kind != appwire.CheckoutConflicted || got.Entries[0].Conflict != "both_modified" {
		t.Fatalf("entries %+v", got.Entries)
	}
	if got.Counts == nil || got.Counts.Conflicted != 1 {
		t.Fatalf("counts %+v", got.Counts)
	}
}

// A REPOSITORY WITH NO COMMIT YET IS UNBORN, not empty and not an error.
func TestCheckoutStatusUnborn(t *testing.T) {
	diffRepo(t) // skips without git, pins the hermetic configuration
	fresh := t.TempDir()
	gitT(t, fresh, "init", "-q", "-b", "trunk")
	write(t, filepath.Join(fresh, "x"), "x\n")
	got, err := checkoutT(t, fresh, appwire.CheckoutQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Repo || !got.Unborn || got.Head != "" || got.Branch != "trunk" || len(got.Entries) != 1 {
		t.Fatalf("unborn: %+v", got)
	}
}

// A FOLDER THAT IS NOT A CHECKOUT IS repo:false, KNOWN, AND NOT AN ERROR.
func TestCheckoutStatusNotARepository(t *testing.T) {
	diffRepo(t) // skips without git, pins the hermetic configuration
	got, err := checkoutT(t, t.TempDir(), appwire.CheckoutQuery{})
	if err != nil || got.Repo || got.Entries == nil || len(got.Entries) != 0 {
		t.Fatalf("not a repo: %+v %v", got, err)
	}
}

// PAGING NEVER SKIPS AND NEVER RE-AIMS: every entry arrives exactly once across
// pages, and a cursor from a status that has since changed is refused.
func TestCheckoutStatusPaging(t *testing.T) {
	repo := diffRepo(t)
	for i := 0; i < 5; i++ {
		write(t, filepath.Join(repo, "u"+strconv.Itoa(i)+".txt"), "x\n")
	}
	seen := map[string]int{}
	cursor, pages := "", 0
	for {
		got, err := checkoutT(t, repo, appwire.CheckoutQuery{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, e := range got.Entries {
			seen[e.Path]++
		}
		if got.Next == "" {
			break
		}
		cursor = got.Next
	}
	if pages != 3 || len(seen) != 5 {
		t.Fatalf("pages %d seen %v", pages, seen)
	}
	for path, n := range seen {
		if n != 1 {
			t.Fatalf("%s arrived %d times", path, n)
		}
	}

	first, _ := checkoutT(t, repo, appwire.CheckoutQuery{Limit: 2})
	write(t, filepath.Join(repo, "another.txt"), "x\n")
	if _, err := checkoutT(t, repo, appwire.CheckoutQuery{Limit: 2, Cursor: first.Next}); wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("a stale cursor answered %v", err)
	}
	for _, bad := range []string{"nope", "abc:1", first.Revision + ":99", first.Revision + ":-1"} {
		if _, err := checkoutT(t, repo, appwire.CheckoutQuery{Cursor: bad}); wireCode(err) != appwire.CodeBadParams {
			t.Fatalf("cursor %q answered %v", bad, err)
		}
	}
}

// A STATUS LONGER THAN THE ENGINE READS IS SAID, NOT PASSED OFF AS WHOLE: it is
// incomplete, carries no counts, and issues no cursor past the part read.
func TestCheckoutStatusOverflowIsIncomplete(t *testing.T) {
	repo := diffRepo(t)
	for i := 0; i < 40; i++ {
		write(t, filepath.Join(repo, "untracked-file-"+strconv.Itoa(i)+".txt"), "x\n")
	}
	was := appCheckoutOutMax
	appCheckoutOutMax = 600
	defer func() { appCheckoutOutMax = was }()
	got, err := checkoutT(t, repo, appwire.CheckoutQuery{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Incomplete || got.Counts != nil || got.Next != "" || len(got.Entries) == 0 || len(got.Entries) >= 40 {
		t.Fatalf("overflow: incomplete=%v counts=%v next=%q entries=%d", got.Incomplete, got.Counts, got.Next, len(got.Entries))
	}
	for _, e := range got.Entries {
		if e.Path == "" || filepath.Ext(e.Path) != ".txt" {
			t.Fatalf("a record cut mid-way was read as an entry: %+v", e)
		}
	}
}

// A FILTER FROM REPOSITORY CONFIGURATION IS NEVER RUN. The file's stat data is
// made stale on purpose, which is what makes plain git run the clean filter.
func TestCheckoutStatusRunsNoFilter(t *testing.T) {
	repo := diffRepo(t)
	marker := filepath.Join(t.TempDir(), "ran")
	write(t, filepath.Join(repo, ".gitattributes"), "*.dat filter=spy\n")
	write(t, filepath.Join(repo, "f.dat"), "a\n")
	gitT(t, repo, "add", ".gitattributes", "f.dat")
	gitT(t, repo, "commit", "-q", "-m", "filtered")
	gitT(t, repo, "config", "filter.spy.clean", "touch '"+marker+"'; cat")
	write(t, filepath.Join(repo, "f.dat"), "b\n")
	time.Sleep(1100 * time.Millisecond)
	if err := os.Chtimes(filepath.Join(repo, "f.dat"), time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := checkoutT(t, repo, appwire.CheckoutQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the repository's clean filter ran")
	}
	if len(got.Entries) != 1 || got.Entries[0].Path != "f.dat" || got.Entries[0].Worktree != "modified" {
		t.Fatalf("entries %+v", got.Entries)
	}
}

// ASKING WRITES NOTHING: the index is the same bytes afterwards.
func TestCheckoutStatusWritesNothing(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "a.txt"), "ONE\ntwo\nthree\n")
	index := filepath.Join(repo, ".git", "index")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := checkoutT(t, repo, appwire.CheckoutQuery{}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(index)
	if !bytes.Equal(before, after) {
		t.Fatal("checkout.status changed the index")
	}
}

// A CLOCK THAT RAN OUT IS busy, NEVER repo:false OR AN EMPTY LIST.
func TestCheckoutStatusTimeoutIsBusy(t *testing.T) {
	repo := diffRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := appCheckout(ctx, appCanonical(repo), appwire.CheckoutQuery{Limit: 10}); wireCode(err) != appwire.CodeBusy {
		t.Fatalf("an expired deadline answered %v", err)
	}
}

// PATHS ARE GIT'S BYTES: one that is not UTF-8 is counted and carries no path,
// and a rename record's second half is its original and not an entry.
func TestAppParseStatus(t *testing.T) {
	text := "# branch.oid " + string(bytes.Repeat([]byte("a"), 40)) + "\x00# branch.head (detached)\x00" +
		"# branch.upstream origin/main\x00# branch.ab +2 -1\x00" +
		"1 .M N... 100644 100644 100644 h h bad\xff.txt\x00" +
		"2 R. N... 100644 100644 100644 h h R100 to.txt\x00from.txt\x00" +
		"1 .M S... 160000 160000 160000 h h sub\x00? plain.txt\x00"
	out, entries := appParseStatus(text)
	if !out.Detached || out.Branch != "" || out.Upstream != "origin/main" || out.Ahead == nil || *out.Ahead != 2 || *out.Behind != 1 {
		t.Fatalf("header: %+v", out)
	}
	if len(entries) != 4 {
		t.Fatalf("entries %+v", entries)
	}
	if !entries[0].PathUnreadable || entries[0].Path != "" {
		t.Fatalf("non-UTF-8 path: %+v", entries[0])
	}
	if entries[1].Kind != appwire.CheckoutRenamed || entries[1].Path != "to.txt" || entries[1].OldPath != "from.txt" {
		t.Fatalf("rename: %+v", entries[1])
	}
	if !entries[2].Submodule || entries[3].Kind != appwire.CheckoutUntracked || entries[3].Path != "plain.txt" {
		t.Fatalf("rest: %+v", entries[2:])
	}
}

// A FILTER MARKED required (git-lfs does this) MUST NOT FAIL THE STATUS: the
// emptied command does nothing, which git would call a failure of a required
// filter, so required is set false beside it. The edit keeps the file's size so
// git has to hash it.
func TestCheckoutStatusRequiredFilterDoesNotFail(t *testing.T) {
	repo := diffRepo(t)
	marker := filepath.Join(t.TempDir(), "ran")
	write(t, filepath.Join(repo, ".gitattributes"), "*.dat filter=lfsish\n")
	write(t, filepath.Join(repo, "a.dat"), "a\n")
	gitT(t, repo, "add", ".gitattributes", "a.dat")
	gitT(t, repo, "commit", "-q", "-m", "filtered")
	gitT(t, repo, "config", "filter.lfsish.clean", "touch '"+marker+"'; cat")
	gitT(t, repo, "config", "filter.lfsish.required", "true")
	write(t, filepath.Join(repo, "a.dat"), "b\n")
	time.Sleep(1100 * time.Millisecond)
	if err := os.Chtimes(filepath.Join(repo, "a.dat"), time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := checkoutT(t, repo, appwire.CheckoutQuery{})
	if err != nil {
		t.Fatalf("a required filter failed the status: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the repository's clean filter ran")
	}
	if len(got.Entries) != 1 || got.Entries[0].Path != "a.dat" || got.Entries[0].Worktree != "modified" {
		t.Fatalf("entries %+v", got.Entries)
	}
}

// A STATUS CUT WHERE A RENAME'S ORIGINAL PATH WOULD BEGIN claims no rename.
func TestAppCheckoutCutRenameClaimsNothing(t *testing.T) {
	whole := "2 R. N... 100644 100644 100644 h h R100 to.txt\x00from.txt\x00"
	cut := "? first.txt\x002 R. N... 100644 100644 100644 h h R100 to.txt\x00"
	_, entries := appParseStatus(whole)
	if len(entries) != 1 || entries[0].OldPath != "from.txt" {
		t.Fatalf("whole rename: %+v", entries)
	}
	// What appCheckout hands the parser after an overflow: the last terminator is gone.
	_, entries = appParseStatus(cut[:len(cut)-1])
	if len(entries) != 1 || entries[0].Path != "first.txt" {
		t.Fatalf("a rename without its original path was read: %+v", entries)
	}
}

// THROUGH THE REAL CUT: a rename whose original path lies past the byte budget
// is not reported as a rename with no old_path.
func TestCheckoutStatusOverflowNeverHalfARename(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "old-name.txt"), "keep this line\nand this one\nplus a third line\n")
	gitT(t, repo, "add", "old-name.txt")
	gitT(t, repo, "commit", "-q", "-m", "second")
	gitT(t, repo, "mv", "old-name.txt", "new-name.txt")
	full, err := checkoutT(t, repo, appwire.CheckoutQuery{})
	if err != nil || len(full.Entries) != 1 || full.Entries[0].OldPath != "old-name.txt" {
		t.Fatalf("whole status: %+v %v", full.Entries, err)
	}
	was := appCheckoutOutMax
	defer func() { appCheckoutOutMax = was }()
	// Every budget from nothing up to the whole text: a rename is complete or absent.
	for max := 1; max < 400; max++ {
		appCheckoutOutMax = max
		got, err := checkoutT(t, repo, appwire.CheckoutQuery{})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range got.Entries {
			if e.Kind == appwire.CheckoutRenamed && e.OldPath == "" {
				t.Fatalf("budget %d reported a rename from nothing: %+v", max, e)
			}
		}
	}
}

// UNTRACKED FILES ARE LISTED ONE BY ONE, never summarised as their directory.
func TestCheckoutStatusListsUntrackedFilesInDirectories(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "dir", "one.txt"), "1\n")
	write(t, filepath.Join(repo, "dir", "deep", "two.txt"), "2\n")
	got, err := checkoutT(t, repo, appwire.CheckoutQuery{})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, e := range got.Entries {
		paths = append(paths, e.Path)
	}
	if len(paths) != 2 || paths[0] != "dir/deep/two.txt" || paths[1] != "dir/one.txt" || got.Counts.Untracked != 2 {
		t.Fatalf("paths %v counts %+v", paths, got.Counts)
	}
}

func entryOf(t *testing.T, st appwire.CheckoutStatus, path string) appwire.CheckoutEntry {
	t.Helper()
	for _, e := range st.Entries {
		if e.Path == path {
			return e
		}
	}
	t.Fatalf("no entry %s in %+v", path, st.Entries)
	return appwire.CheckoutEntry{}
}

func wantLines(t *testing.T, e appwire.CheckoutEntry, added, removed int) {
	t.Helper()
	if e.Added == nil || e.Removed == nil || *e.Added != added || *e.Removed != removed || e.Binary {
		t.Fatalf("%s: want +%d -%d, got %+v", e.Path, added, removed, e)
	}
}

func wantUnknown(t *testing.T, e appwire.CheckoutEntry) {
	t.Helper()
	if e.Added != nil || e.Removed != nil {
		t.Fatalf("%s: counts must be unknown, got %+v", e.Path, e)
	}
}

// COUNTS ARE THE NET CHANGE OF THE WORK TREE AGAINST HEAD: an edit staged and
// then reverted on disk cancels to 0 and 0, and one staged then edited again is
// one net count, not two.
func TestCheckoutStatusLinesNetAgainstHead(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "b.txt"), "1\n2\n3\n")
	gitT(t, repo, "add", "b.txt")
	gitT(t, repo, "commit", "-q", "-m", "b")
	write(t, filepath.Join(repo, "a.txt"), "one\ntwo\nthree\nfour\n") // +1
	write(t, filepath.Join(repo, "b.txt"), "1\nTWO\n3\n")             // +1 -1
	gitT(t, repo, "add", "a.txt", "b.txt")
	write(t, filepath.Join(repo, "b.txt"), "1\n2\n3\n") // reverted on disk: net nothing
	write(t, filepath.Join(repo, "a.txt"), "one\ntwo\nthree\nfour\nfive\n")
	got, err := checkoutT(t, repo, appwire.CheckoutQuery{})
	if err != nil || got.LinesUnavailable != "" {
		t.Fatalf("%v %q", err, got.LinesUnavailable)
	}
	wantLines(t, entryOf(t, got, "a.txt"), 2, 0)
	b := entryOf(t, got, "b.txt")
	if b.Index == "" || b.Worktree == "" {
		t.Fatalf("b.txt should be staged and edited back: %+v", b)
	}
	wantLines(t, b, 0, 0)
}

func TestCheckoutStatusLinesModifiedDeletedAndBinary(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "bin.dat"), "a\x00b\n")
	write(t, filepath.Join(repo, "gone.txt"), "x\ny\n")
	gitT(t, repo, "add", "bin.dat", "gone.txt")
	gitT(t, repo, "commit", "-q", "-m", "more")
	write(t, filepath.Join(repo, "bin.dat"), "a\x00c\x00\n")
	if err := os.Remove(filepath.Join(repo, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(repo, "a.txt"), "one\nTWO\nthree\n")
	got, err := checkoutT(t, repo, appwire.CheckoutQuery{})
	if err != nil {
		t.Fatal(err)
	}
	wantLines(t, entryOf(t, got, "a.txt"), 1, 1)
	wantLines(t, entryOf(t, got, "gone.txt"), 0, 2)
	if b := entryOf(t, got, "bin.dat"); !b.Binary || b.Added != nil || b.Removed != nil {
		t.Fatalf("binary: %+v", b)
	}
}

// A RENAME COUNTS ITS EDITS SINCE THE ORIGINAL PATH, and a pure rename is 0/0.
func TestCheckoutStatusLinesRename(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "r1.txt"), "l1\nl2\nl3\nl4\nl5\nl6\n")
	write(t, filepath.Join(repo, "r2.txt"), "m1\nm2\nm3\nm4\nm5\nm6\n")
	gitT(t, repo, "add", "r1.txt", "r2.txt")
	gitT(t, repo, "commit", "-q", "-m", "r")
	gitT(t, repo, "mv", "r1.txt", "n1.txt")
	gitT(t, repo, "mv", "r2.txt", "n2.txt")
	write(t, filepath.Join(repo, "n2.txt"), "m1\nm2\nm3\nm4\nm5\nm6\nm7\n")
	got, err := checkoutT(t, repo, appwire.CheckoutQuery{})
	if err != nil || got.LinesUnavailable != "" {
		t.Fatalf("%v %q", err, got.LinesUnavailable)
	}
	wantLines(t, entryOf(t, got, "n1.txt"), 0, 0)
	wantLines(t, entryOf(t, got, "n2.txt"), 1, 0)
	if entryOf(t, got, "n2.txt").OldPath != "r2.txt" {
		t.Fatal("pairing lost")
	}
}

// UNBORN HAS NO HEAD: staged files count against the empty tree, and untracked
// ones are unknown. Nothing is called zero for want of a commit.
func TestCheckoutStatusLinesUnborn(t *testing.T) {
	diffRepo(t)
	fresh := t.TempDir()
	gitT(t, fresh, "init", "-q", "-b", "trunk")
	write(t, filepath.Join(fresh, "s.txt"), "1\n2\n3\n")
	gitT(t, fresh, "add", "s.txt")
	write(t, filepath.Join(fresh, "u.txt"), "x\ny")
	got, err := checkoutT(t, fresh, appwire.CheckoutQuery{})
	if err != nil || !got.Unborn || got.LinesUnavailable != "" {
		t.Fatalf("%v %+v", err, got)
	}
	wantLines(t, entryOf(t, got, "s.txt"), 3, 0)
	wantUnknown(t, entryOf(t, got, "u.txt"))
}

// UNTRACKED FILES ARE NEVER OPENED: a pipe or a link swapped in for one cannot
// hang the read or leave the checkout, because there is no read. Their counts
// are unknown, and the call returns at once.
func TestCheckoutStatusLinesUntrackedAreNeverOpened(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "d", "t.txt"), "1\n2\n")
	outside := filepath.Join(t.TempDir(), "secret.txt")
	write(t, outside, "s\n")
	if err := os.Symlink(outside, filepath.Join(repo, "link.txt")); err != nil {
		t.Skip("no symlinks")
	}
	if err := syscall.Mkfifo(filepath.Join(repo, "pipe"), 0o644); err != nil {
		t.Skip("no fifos")
	}
	done := make(chan appwire.CheckoutStatus, 1)
	go func() {
		got, _ := checkoutT(t, repo, appwire.CheckoutQuery{})
		done <- got
	}()
	select {
	case got := <-done:
		for _, e := range got.Entries {
			wantUnknown(t, e)
			if e.Binary {
				t.Fatalf("%s claimed binary", e.Path)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a fifo in the checkout hung checkout.status")
	}
}

// A CUT STATUS ATTACHES NO COUNTS AND SAYS WHY.
func TestCheckoutStatusLinesCutStatus(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "a.txt"), "changed\n")
	for i := 0; i < 40; i++ {
		write(t, filepath.Join(repo, "untracked-file-"+strconv.Itoa(i)+".txt"), "x\n")
	}
	was := appCheckoutOutMax
	appCheckoutOutMax = 700
	defer func() { appCheckoutOutMax = was }()
	got, err := checkoutT(t, repo, appwire.CheckoutQuery{Limit: 200})
	if err != nil || !got.Incomplete || got.LinesUnavailable != appwire.LinesIncomplete {
		t.Fatalf("%v incomplete=%v lines=%q", err, got.Incomplete, got.LinesUnavailable)
	}
	for _, e := range got.Entries {
		wantUnknown(t, e)
	}
}

// THE STATUS MUST AGREE BEFORE AND AFTER THE COUNTS: when the checkout changes
// between, no count from the earlier diff is attached to the later page.
func TestCheckoutStatusLinesChangedDuringRead(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "a.txt"), "changed\n")
	appLinesBetween = func() { write(t, filepath.Join(repo, "late.txt"), "x\n") }
	defer func() { appLinesBetween = func() {} }()
	got, err := checkoutT(t, repo, appwire.CheckoutQuery{})
	if err != nil || got.LinesUnavailable != appwire.LinesChanged {
		t.Fatalf("%v %q", err, got.LinesUnavailable)
	}
	wantUnknown(t, entryOf(t, got, "a.txt"))
}

// A required filter still cannot fail or run, and a file it governs has no
// count rather than a count of bytes it would have rewritten.
func TestCheckoutStatusLinesFilteredPathIsUnknown(t *testing.T) {
	repo := diffRepo(t)
	marker := filepath.Join(t.TempDir(), "ran")
	write(t, filepath.Join(repo, ".gitattributes"), "*.dat filter=lfsish\n")
	write(t, filepath.Join(repo, "a.dat"), "a\n")
	gitT(t, repo, "add", ".gitattributes", "a.dat")
	gitT(t, repo, "commit", "-q", "-m", "filtered")
	gitT(t, repo, "config", "filter.lfsish.clean", "touch '"+marker+"'; cat")
	gitT(t, repo, "config", "filter.lfsish.required", "true")
	write(t, filepath.Join(repo, "a.dat"), "b\n")
	write(t, filepath.Join(repo, "a.txt"), "one\ntwo\nthree\nfour\n")
	time.Sleep(1100 * time.Millisecond)
	_ = os.Chtimes(filepath.Join(repo, "a.dat"), time.Now(), time.Now())
	got, err := checkoutT(t, repo, appwire.CheckoutQuery{})
	if err != nil || got.LinesUnavailable != "" {
		t.Fatalf("%v %q", err, got.LinesUnavailable)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the clean filter ran")
	}
	wantUnknown(t, entryOf(t, got, "a.dat"))
	wantLines(t, entryOf(t, got, "a.txt"), 1, 0)
}

// THE STATUS TEXT CAN STAY THE SAME WHILE THE CONTENT DOES NOT: a modified file
// edited again between the two reads is still " M", so the counts of the first
// diff must not be attached to it.
func TestCheckoutStatusLinesContentEditWithSameStatus(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "a.txt"), "one\ntwo\nthree\nfour\n")
	appLinesBetween = func() { write(t, filepath.Join(repo, "a.txt"), "one\ntwo\nthree\nfour\nfive\nsix\n") }
	defer func() { appLinesBetween = func() {} }()
	got, err := checkoutT(t, repo, appwire.CheckoutQuery{})
	if err != nil || got.LinesUnavailable != appwire.LinesChanged {
		t.Fatalf("%v %q", err, got.LinesUnavailable)
	}
	wantUnknown(t, entryOf(t, got, "a.txt"))
}

// A STAGING CHANGE WITH THE SAME STATUS TEXT STILL MOVES THE INDEX STAMP.
func TestCheckoutStatusLinesIndexRewrittenBetweenReads(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "a.txt"), "one\ntwo\nthree\nfour\n")
	gitT(t, repo, "add", "a.txt")
	appLinesBetween = func() {
		write(t, filepath.Join(repo, "a.txt"), "one\ntwo\nthree\nfour\n") // same bytes, new mtime
		gitT(t, repo, "add", "a.txt")
	}
	defer func() { appLinesBetween = func() {} }()
	got, err := checkoutT(t, repo, appwire.CheckoutQuery{})
	if err != nil || got.LinesUnavailable != appwire.LinesChanged {
		t.Fatalf("%v %q", err, got.LinesUnavailable)
	}
}

// ONE BYTE BUDGET COVERS THE WHOLE CALL: a status that fits once but not twice
// is read, and its counts are too_large, not confirmed against a short re-read.
func TestCheckoutStatusAggregateBudget(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "a.txt"), "changed\n")
	for i := 0; i < 5; i++ {
		write(t, filepath.Join(repo, "untracked-"+strconv.Itoa(i)+".txt"), "x\n")
	}
	was := appCheckoutTotalMax
	defer func() { appCheckoutTotalMax = was }()
	// First measure what the whole call spends.
	appCheckoutTotalMax = 1 << 20
	ok, err := checkoutT(t, repo, appwire.CheckoutQuery{})
	if err != nil || ok.LinesUnavailable != "" || ok.Incomplete {
		t.Fatalf("roomy budget: %v %q", err, ok.LinesUnavailable)
	}
	// Room for one status and the small reads, not for the second status.
	for budget := 330; budget < 700; budget += 10 {
		appCheckoutTotalMax = budget
		got, err := checkoutT(t, repo, appwire.CheckoutQuery{})
		if err != nil {
			continue
		}
		if got.LinesUnavailable == appwire.LinesTooLarge {
			wantUnknown(t, entryOf(t, got, "a.txt"))
			return
		}
	}
	t.Fatal("no budget made the second status read overflow the shared pool")
}

// COUNTING WRITES NOTHING EITHER: `git diff` refreshes the index's stat cache
// even with optional locks off, so the counts use diff-index and the index file
// keeps its bytes and its modification time.
func TestCheckoutStatusLinesLeaveTheIndexAlone(t *testing.T) {
	repo := diffRepo(t)
	write(t, filepath.Join(repo, "a.txt"), "one\nTWO\nthree\n")
	gitT(t, repo, "add", "a.txt")
	write(t, filepath.Join(repo, "a.txt"), "one\ntwo\nthree\n") // staged edit reverted on disk
	index := filepath.Join(repo, ".git", "index")
	before, _ := os.ReadFile(index)
	was, _ := os.Stat(index)
	got, err := checkoutT(t, repo, appwire.CheckoutQuery{})
	if err != nil || got.LinesUnavailable != "" {
		t.Fatalf("%v %q", err, got.LinesUnavailable)
	}
	wantLines(t, entryOf(t, got, "a.txt"), 0, 0)
	after, _ := os.ReadFile(index)
	now, _ := os.Stat(index)
	if !bytes.Equal(before, after) || !was.ModTime().Equal(now.ModTime()) {
		t.Fatal("counting the lines rewrote the index")
	}
}

// A WORKSPACE THAT IS A SUBFOLDER OF THE REPOSITORY gets the same counts as the
// root does: status paths are root-relative, so the count and attribute commands
// run at the root. Run from the subfolder they named nothing and answered 0/0.
func TestCheckoutStatusLinesFromSubfolderWorkspace(t *testing.T) {
	repo := diffRepo(t)
	marker := filepath.Join(t.TempDir(), "ran")
	write(t, filepath.Join(repo, ".gitattributes"), "*.dat filter=lfsish\n")
	write(t, filepath.Join(repo, "sub", "inner.txt"), "i1\ni2\ni3\ni4\ni5\ni6\ni7\ni8\n")
	write(t, filepath.Join(repo, "sub", "f.dat"), "a\n")
	gitT(t, repo, "add", ".gitattributes", "sub")
	gitT(t, repo, "commit", "-q", "-m", "sub")
	write(t, filepath.Join(repo, "a.txt"), "one\ntwo\nthree\nfour\nfive\n")                         // root file: +2
	write(t, filepath.Join(repo, "sub", "inner.txt"), "i1\ni2\ni3\ni4\ni5\ni6\ni7\ni8\ni9\nj\nk\n") // nested: +3
	write(t, filepath.Join(repo, "sub", "f.dat"), "b\n")                                            // filter-governed: unknown
	gitT(t, repo, "mv", "sub/inner.txt", "sub/moved.txt")
	// The filter is configured LAST: the setup's own git commands refresh the
	// index and would run it, and this test is about what checkout.status runs.
	gitT(t, repo, "config", "filter.lfsish.clean", "touch '"+marker+"'; cat")
	gitT(t, repo, "config", "filter.lfsish.required", "true")

	for _, workspace := range []string{repo, filepath.Join(repo, "sub")} {
		got, err := checkoutT(t, workspace, appwire.CheckoutQuery{})
		if err != nil || got.LinesUnavailable != "" {
			t.Fatalf("%s: %v %q", workspace, err, got.LinesUnavailable)
		}
		wantLines(t, entryOf(t, got, "a.txt"), 2, 0)
		wantLines(t, entryOf(t, got, "sub/moved.txt"), 3, 0)
		wantUnknown(t, entryOf(t, got, "sub/f.dat"))
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the clean filter ran")
	}
}
