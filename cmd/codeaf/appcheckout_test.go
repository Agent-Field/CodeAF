package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
