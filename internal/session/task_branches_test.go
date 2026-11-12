package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestBranchListRefusesCorruptBareHead(t *testing.T) {
	repo, _, _ := sideRepo(t)
	bare := filepath.Join(t.TempDir(), "bare.git")
	gitT(t, t.TempDir(), "clone", "-q", "--bare", repo, bare)
	if err := os.Remove(filepath.Join(bare, "packed-refs")); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bare, "refs", "heads", "main"), []byte(strings.Repeat("cd", 20)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	place := newPlace(t, false)
	writeRuns(t, place, []string{"task/a-1"})
	if _, err := ListTaskBranches(context.Background(), place, bare, TaskBranchQuery{}); !errors.Is(err, ErrTaskBranchGit) {
		t.Fatalf("corrupt bare HEAD must be an error, got %v", err)
	}
}

func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

// sideRepo is a repository on main with one base commit, and a side commit
// HEAD does not reach; it answers (repo, base, side).
func sideRepo(t *testing.T) (string, string, string) {
	t.Helper()
	repo := t.TempDir()
	gitT(t, repo, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644)
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-q", "-m", "base")
	base := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	gitT(t, repo, "checkout", "-q", "-b", "side")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\nb\n"), 0o644)
	gitT(t, repo, "commit", "-q", "-am", "side")
	side := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	gitT(t, repo, "checkout", "-q", "main")
	gitT(t, repo, "branch", "-q", "-D", "side")
	return repo, base, side
}

// pointRefs makes every name a branch at oid, in one git call.
func pointRefs(t *testing.T, repo, oid string, names []string) {
	t.Helper()
	var in strings.Builder
	for _, name := range names {
		fmt.Fprintf(&in, "create refs/heads/%s %s\n", name, oid)
	}
	cmd := exec.Command("git", "update-ref", "--stdin")
	cmd.Dir = repo
	cmd.Stdin = strings.NewReader(in.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("update-ref: %v %s", err, out)
	}
}

// writeRuns is a task checkpoint of run rows naming branches, ids from 1.
func writeRuns(t *testing.T, place Place, branches []string) {
	t.Helper()
	var rows []string
	for i, branch := range branches {
		rows = append(rows, fmt.Sprintf(`{"id":%d,"title":"T%d","state":"done","branch":%q}`, i+1, i+1, branch))
	}
	doc := fmt.Sprintf(`{"type":"tasks","version":1,"seq":%d,"nodes":[],"runs":[%s]}`, len(branches), strings.Join(rows, ","))
	if err := os.WriteFile(place.join(placeTasks), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
}

func listBranches(t *testing.T, place Place, repo string, q TaskBranchQuery) TaskBranchPage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	page, err := ListTaskBranches(ctx, place, repo, q)
	if err != nil {
		t.Fatalf("list %+v: %v", q, err)
	}
	return page
}

// stallGit puts a git behind the listing that never answers the commands
// stall picks, until its context ends.
func stallGit(t *testing.T, stall func(args []string) bool) {
	t.Helper()
	real := taskBranchCommand
	t.Cleanup(func() { taskBranchCommand = real })
	taskBranchCommand = func(ctx context.Context, dir string, args ...string) *exec.Cmd {
		if stall(args) {
			return exec.CommandContext(ctx, "sleep", "60")
		}
		return real(ctx, dir, args...)
	}
}

func TestUnmergedTaskBranchesComeFromGitAndTheSessionsOwnRecords(t *testing.T) {
	repo, _, _ := sideRepo(t)
	gitT(t, repo, "checkout", "-q", "-b", "task/paint-abc123")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\nb\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "n.md"), []byte("x\ny\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "pic.bin"), []byte{0, 1, 2, 0, 3}, 0o644)
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-q", "-m", "work")
	gitT(t, repo, "checkout", "-q", "main")
	gitT(t, repo, "branch", "task/merged-111111")
	// A task/ branch the records never name, unmerged: somebody else's.
	gitT(t, repo, "branch", "task/stranger-222222", "task/paint-abc123")
	place := newPlace(t, false)
	writeRuns(t, place, []string{"task/paint-abc123", "task/merged-111111", "task/deleted-333333", "task/../main"})
	page := listBranches(t, place, repo, TaskBranchQuery{})
	if !page.Repo || page.Head != strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")) || page.Recorded != 3 || page.Next != "" || page.Incomplete || len(page.Branches) != 1 {
		t.Fatalf("page %+v", page)
	}
	b := page.Branches[0]
	// a.txt +1, n.md +2, and a binary file that counts as a file with no lines.
	if b.Name != "task/paint-abc123" || b.Task != "1" || b.Title != "T1" || b.Files != 3 || b.Added != 3 || b.Removed != 0 || !b.CountsKnown || b.Capped || b.Base == "" || b.Head == "" {
		t.Fatalf("fact %+v", b)
	}
	// Not a repository: git's own no, not none.
	if page := listBranches(t, place, t.TempDir(), TaskBranchQuery{}); page.Repo || page.Branches != nil {
		t.Fatalf("non-repo answered %+v", page)
	}
	// No checkpoint ever written: a repository with nothing recorded.
	if page := listBranches(t, newPlace(t, false), repo, TaskBranchQuery{}); !page.Repo || page.Recorded != 0 || len(page.Branches) != 0 || page.Next != "" {
		t.Fatalf("no records answered %+v", page)
	}
}

// Every one of more than two hundred recorded branches is reached by
// following next, each once, in record order.
func TestEveryRecordedBranchIsReachedByContinuation(t *testing.T) {
	repo, _, side := sideRepo(t)
	var names []string
	for i := range 205 {
		names = append(names, fmt.Sprintf("task/b-%03d", i))
	}
	pointRefs(t, repo, side, names)
	place := newPlace(t, false)
	writeRuns(t, place, names)
	var got []string
	cursor, pages := "", 0
	for {
		page := listBranches(t, place, repo, TaskBranchQuery{Cursor: cursor, Limit: 1000})
		pages++
		if page.Recorded != 205 || page.Incomplete || len(page.Branches) > TaskBranchPageMax {
			t.Fatalf("page %d: recorded %d incomplete %v rows %d", pages, page.Recorded, page.Incomplete, len(page.Branches))
		}
		for _, b := range page.Branches {
			if !b.CountsKnown || b.Files != 1 || b.Added != 1 {
				t.Fatalf("row %+v", b)
			}
			got = append(got, b.Name)
		}
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	if pages != 3 || strings.Join(got, ",") != strings.Join(names, ",") {
		t.Fatalf("%d pages, %d rows", pages, len(got))
	}
}

// A long run of merged names costs no rows, and a page that ran out of time
// part-way says so with a next that resumes where it stopped.
func TestASparseScanProgressesHonestly(t *testing.T) {
	repo, base, side := sideRepo(t)
	var names, merged []string
	for i := range 200 {
		names = append(names, fmt.Sprintf("task/m-%03d", i))
	}
	merged = append(merged, names...)
	names = append(names, "task/live-a", "task/live-b")
	pointRefs(t, repo, base, merged)
	pointRefs(t, repo, side, []string{"task/live-a", "task/live-b"})
	place := newPlace(t, false)
	writeRuns(t, place, names)

	// The second batch never answers: the first page examined one batch.
	asked := 0
	stallGit(t, func(args []string) bool {
		if args[0] != "for-each-ref" {
			return false
		}
		asked++
		return asked > 1
	})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	page, err := ListTaskBranches(ctx, place, repo, TaskBranchQuery{Limit: 1})
	cancel()
	if err != nil || len(page.Branches) != 0 || page.Next == "" || !page.Incomplete {
		t.Fatalf("stalled sparse page %+v %v", page, err)
	}
	held, _ := decodeTaskBranchCursor(page.Next)
	if held.Offset != taskBranchBatch {
		t.Fatalf("resumes at %d, want %d", held.Offset, taskBranchBatch)
	}
	asked = -1000 // git answers again
	var got []string
	for cursor := page.Next; cursor != ""; {
		next := listBranches(t, place, repo, TaskBranchQuery{Cursor: cursor, Limit: 1})
		for _, b := range next.Branches {
			got = append(got, b.Name)
		}
		cursor = next.Next
	}
	if strings.Join(got, ",") != "task/live-a,task/live-b" {
		t.Fatalf("continued to %v", got)
	}
}

// A git that answers nothing is busy - never "not a repository", never none -
// and is stopped with the request rather than left running.
func TestStalledGitIsBusyNotNone(t *testing.T) {
	repo, _, side := sideRepo(t)
	pointRefs(t, repo, side, []string{"task/x-1"})
	place := newPlace(t, false)
	writeRuns(t, place, []string{"task/x-1"})
	for name, stall := range map[string]func([]string) bool{
		"head":   func(args []string) bool { return args[0] == "rev-parse" },
		"refs":   func(args []string) bool { return args[0] == "for-each-ref" },
		"always": func([]string) bool { return true },
	} {
		t.Run(name, func(t *testing.T) {
			stallGit(t, stall)
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			began := time.Now()
			page, err := ListTaskBranches(ctx, place, repo, TaskBranchQuery{})
			if !errors.Is(err, ErrTaskBranchBusy) || page.Repo || page.Branches != nil {
				t.Fatalf("answered %+v %v", page, err)
			}
			if took := time.Since(began); took > 5*time.Second {
				t.Fatalf("a stalled git held the request %v", took)
			}
		})
	}
	// The diff stalls on the page's only row: the row stands, its counts are
	// absent, and the page says it was cut short.
	stallGit(t, func(args []string) bool { return args[0] == "diff" })
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	page, err := ListTaskBranches(ctx, place, repo, TaskBranchQuery{})
	if err != nil || len(page.Branches) != 1 || page.Branches[0].CountsKnown || page.Branches[0].Files != 0 || !page.Incomplete || page.Next != "" {
		t.Fatalf("stalled diff %+v %v", page, err)
	}
}

// A cursor is refused once the recorded list or HEAD it was issued for has
// changed, and one this engine never issued is refused too.
func TestAStaleCursorIsRefusedNotReaimed(t *testing.T) {
	repo, _, side := sideRepo(t)
	names := []string{"task/a-1", "task/a-2", "task/a-3"}
	pointRefs(t, repo, side, names)
	place := newPlace(t, false)
	writeRuns(t, place, names)
	page := listBranches(t, place, repo, TaskBranchQuery{Limit: 1})
	if page.Next == "" {
		t.Fatal("no next")
	}
	refused := func(why string) {
		t.Helper()
		if _, err := ListTaskBranches(context.Background(), place, repo, TaskBranchQuery{Cursor: page.Next}); !errors.Is(err, ErrTaskBranchCursor) {
			t.Fatalf("%s: %v", why, err)
		}
	}
	for _, bad := range []string{"b1.", "d1.e30", "b1.!!!", "b1." + strings.Repeat("A", 600)} {
		if _, err := ListTaskBranches(context.Background(), place, repo, TaskBranchQuery{Cursor: bad}); !errors.Is(err, ErrTaskBranchCursor) {
			t.Fatalf("forged %q: %v", bad, err)
		}
	}
	// The records changed: a name added at the front moves every index.
	writeRuns(t, place, append([]string{"task/a-0"}, names...))
	refused("records changed")
	writeRuns(t, place, names)
	if _, err := ListTaskBranches(context.Background(), place, repo, TaskBranchQuery{Cursor: page.Next}); err != nil {
		t.Fatalf("the same records again: %v", err)
	}
	// HEAD moved.
	os.WriteFile(filepath.Join(repo, "c.txt"), []byte("c\n"), 0o644)
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-q", "-m", "more")
	refused("head moved")
}

// A diff longer than the engine reads is capped with lower-bound counts; a
// diff git fails is unknown, never zero.
func TestCountsAreCappedOrUnknownNeverFalseZero(t *testing.T) {
	repo, base, _ := sideRepo(t)
	gitT(t, repo, "checkout", "-q", "-b", "task/wide-1")
	for i := range 5 {
		os.WriteFile(filepath.Join(repo, fmt.Sprintf("f%d.txt", i)), []byte("1\n2\n"), 0o644)
	}
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-q", "-m", "wide")
	gitT(t, repo, "checkout", "-q", "main")
	place := newPlace(t, false)
	writeRuns(t, place, []string{"task/wide-1"})

	saved := taskBranchNumstatMax
	taskBranchNumstatMax = 3
	page := listBranches(t, place, repo, TaskBranchQuery{})
	taskBranchNumstatMax = saved
	if b := page.Branches[0]; !b.CountsKnown || !b.Capped || b.Files != 3 || b.Added != 6 || b.Base != base {
		t.Fatalf("capped %+v", b)
	}
	stallGit(t, func(args []string) bool { return false })
	real := taskBranchCommand
	taskBranchCommand = func(ctx context.Context, dir string, args ...string) *exec.Cmd {
		if args[0] == "diff" {
			return exec.CommandContext(ctx, "false")
		}
		return real(ctx, dir, args...)
	}
	page = listBranches(t, place, repo, TaskBranchQuery{})
	if b := page.Branches[0]; b.CountsKnown || b.Files != 0 || b.Base == "" || page.Incomplete {
		t.Fatalf("failed diff %+v", page)
	}
}

// Records that cannot be read, tombstones that cannot be, and records past
// the bound are errors, never an empty list.
func TestUnreadableRecordsAreNotNone(t *testing.T) {
	repo, _, _ := sideRepo(t)
	place := newPlace(t, false)
	os.WriteFile(place.join(placeTasks), []byte(`{"type":"tasks"`), 0o600)
	if _, err := ListTaskBranches(context.Background(), place, repo, TaskBranchQuery{}); !errors.Is(err, ErrTaskBranchRecords) {
		t.Fatalf("corrupt records: %v", err)
	}
	writeRuns(t, place, []string{"task/a-1"})
	os.WriteFile(place.join(taskDeletedFile), []byte(`{`), 0o600)
	if _, err := ListTaskBranches(context.Background(), place, repo, TaskBranchQuery{}); !errors.Is(err, ErrTaskBranchRecords) {
		t.Fatalf("corrupt tombstones: %v", err)
	}
	os.Remove(place.join(taskDeletedFile))
	saved := taskBranchRecordsMax
	taskBranchRecordsMax = 16
	defer func() { taskBranchRecordsMax = saved }()
	if _, err := ListTaskBranches(context.Background(), place, repo, TaskBranchQuery{}); !errors.Is(err, ErrTaskBranchRecordsTooLarge) {
		t.Fatalf("oversized records: %v", err)
	}
}

// A tombstoned task's whole family is left out - exactly the rows
// filterDeletedDocument removes - and a chain a hundred thousand deep costs
// its length, not its square.
func TestTombstonedFamiliesAreLeftOutInLinearCost(t *testing.T) {
	// The small tree: the set matches the store's own filter.
	doc := taskDocument{Runs: []runRecord{
		{ID: 1}, {ID: 2, Parent: 1}, {ID: 3, Parent: 2}, {ID: 4}, {ID: 5, Parent: 4}, {ID: 6, Parent: 3}, {ID: 7, Parent: 9},
	}}
	deleted := map[string]bool{"2": true, "4": false, "9": true}
	excluded, _, _ := tombstonedTasks(context.Background(), doc, deleted)
	kept := filterDeletedDocument(doc, map[string]bool{"2": true, "4": false, "9": true})
	for _, run := range doc.Runs {
		id := fmt.Sprint(run.ID)
		inKept := false
		for _, k := range kept.Runs {
			inKept = inKept || k.ID == run.ID
		}
		if excluded[id] == inKept {
			t.Fatalf("row %s: excluded %v, filter kept %v", id, excluded[id], inKept)
		}
	}

	// The deep chain, through the real reader.
	const deep = 100_000
	var rows strings.Builder
	for i := 1; i <= deep; i++ {
		parent := ""
		if i > 1 {
			parent = fmt.Sprintf(`,"parent":%d`, i-1)
		}
		fmt.Fprintf(&rows, `{"id":%d,"title":"c","state":"done","branch":"task/c-%d"%s},`, i, i, parent)
	}
	fmt.Fprintf(&rows, `{"id":%d,"title":"survivor","state":"done","branch":"task/keep-1"}`, deep+1)
	place := newPlace(t, false)
	os.WriteFile(place.join(placeTasks), []byte(fmt.Sprintf(`{"type":"tasks","version":1,"seq":%d,"nodes":[],"runs":[%s]}`, deep+1, rows.String())), 0o600)
	os.WriteFile(place.join(taskDeletedFile), []byte(`{"1":true}`), 0o600)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	names, err := ownedTaskBranches(ctx, place)
	if err != nil || len(names) != 1 || names[0].branch != "task/keep-1" {
		t.Fatalf("names %v %v", names, err)
	}
	raw, _ := os.ReadFile(place.join(placeTasks))
	document, _ := decodeTasks(raw)
	if excluded, steps, err := tombstonedTasks(ctx, document, map[string]bool{"1": true}); err != nil || len(excluded) != deep || steps > deep {
		t.Fatalf("excluded %d in %d steps: %v", len(excluded), steps, err)
	}
	// Every linear pass over the records looks at the clock as it goes: a
	// request that ended part-way is not answered from half a pass.
	spentPass, stopPass := context.WithCancel(context.Background())
	stopPass()
	if _, _, err := tombstonedTasks(spentPass, document, map[string]bool{"1": true}); !errors.Is(err, errTaskBranchTime) {
		t.Fatalf("tombstone pass after the deadline: %v", err)
	}
	if _, err := recordedTaskBranches(spentPass, document, nil); !errors.Is(err, errTaskBranchTime) {
		t.Fatalf("name pass after the deadline: %v", err)
	}
	many := make([]namedBranch, deep)
	if _, err := taskBranchGeneration(spentPass, place, "/w", many); !errors.Is(err, errTaskBranchTime) {
		t.Fatalf("generation pass after the deadline: %v", err)
	}
	// A request whose time is already spent says so; it is not an empty list.
	spent, stop := context.WithCancel(context.Background())
	stop()
	if _, err := ownedTaskBranches(spent, place); !errors.Is(err, errTaskBranchTime) {
		t.Fatalf("spent request: %v", err)
	}
}

// A title past the bound is cut at a character boundary and says so.
func TestALongTitleIsCutAndSaysSo(t *testing.T) {
	repo, _, side := sideRepo(t)
	pointRefs(t, repo, side, []string{"task/t-1"})
	place := newPlace(t, false)
	title := strings.Repeat("é", 400)
	doc := fmt.Sprintf(`{"type":"tasks","version":1,"seq":1,"nodes":[],"runs":[{"id":1,"title":%q,"state":"done","branch":"task/t-1"}]}`, title)
	os.WriteFile(place.join(placeTasks), []byte(doc), 0o600)
	b := listBranches(t, place, repo, TaskBranchQuery{}).Branches[0]
	if !b.TitleTruncated || len(b.Title) > taskBranchTitleMax || !utf8.ValidString(b.Title) || !strings.HasPrefix(title, b.Title) {
		t.Fatalf("title %d bytes, truncated %v", len(b.Title), b.TitleTruncated)
	}
}

// fakeGit answers every git the listing starts with sh running script.
func fakeGit(t *testing.T, script func(args []string) string) {
	t.Helper()
	real := taskBranchCommand
	t.Cleanup(func() { taskBranchCommand = real })
	taskBranchCommand = func(ctx context.Context, dir string, args ...string) *exec.Cmd {
		if body := script(args); body != "" {
			command := exec.CommandContext(ctx, "sh", "-c", body)
			command.Dir = dir
			return command
		}
		return real(ctx, dir, args...)
	}
}

// "Not a repository" is git's own exact answer, or no folder at all, and
// nothing else: a git that fails any other way is an error, never repo false.
func TestOnlyAKnownNoHeadIsRepoFalse(t *testing.T) {
	place := newPlace(t, false)
	writeRuns(t, place, []string{"task/a-1"})
	noRepo := func(name, workspace string) {
		t.Helper()
		page, err := ListTaskBranches(context.Background(), place, workspace, TaskBranchQuery{})
		if err != nil || page.Repo || page.Branches != nil {
			t.Fatalf("%s: %+v %v", name, page, err)
		}
	}
	failed := func(name, workspace string) {
		t.Helper()
		page, err := ListTaskBranches(context.Background(), place, workspace, TaskBranchQuery{})
		if !errors.Is(err, ErrTaskBranchGit) || page.Repo || page.Branches != nil {
			t.Fatalf("%s answered %+v %v; want ErrTaskBranchGit", name, page, err)
		}
	}
	noRepo("no git", t.TempDir())
	noRepo("no folder", filepath.Join(t.TempDir(), "gone"))
	unborn := t.TempDir()
	gitT(t, unborn, "init", "-q", "-b", "main")
	noRepo("unborn", unborn)

	// HEAD detached at a commit that is not there: broken, not unborn.
	broken := t.TempDir()
	gitT(t, broken, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(broken, ".git", "HEAD"), []byte(strings.Repeat("ab", 20)+"\n"), 0o644)
	failed("detached at nothing", broken)

	// A folder that cannot be looked at is not a folder that is not there.
	if os.Geteuid() != 0 {
		shut := filepath.Join(t.TempDir(), "shut")
		os.MkdirAll(filepath.Join(shut, "w"), 0o755)
		os.Chmod(shut, 0)
		t.Cleanup(func() { os.Chmod(shut, 0o755) })
		failed("unreadable folder", filepath.Join(shut, "w"))
	}

	repo, _, _ := sideRepo(t)
	for name, script := range map[string]string{
		"exit 7":          "exit 7",
		"unknown fatal":   "echo 'fatal: detected dubious ownership in repository' >&2; exit 128",
		"lookalike fatal": "echo \"fatal: not a git repository: '/x'\" >&2; exit 128",
		"padded fatal":    "echo 'fatal: not a git repository (or any of the parent directories): .git (and more)' >&2; exit 128",
		"right words, 1":  "echo 'fatal: not a git repository (or any of the parent directories): .git' >&2; exit 1",
		"malformed HEAD":  "echo not-a-commit-id; exit 0",
	} {
		t.Run(name, func(t *testing.T) {
			fakeGit(t, func([]string) string { return script })
			failed(name, repo)
		})
	}
	// HEAD fails and the work-tree question then fails oddly too.
	fakeGit(t, func(args []string) string {
		if args[0] == "rev-parse" && args[1] == "--is-inside-work-tree" {
			return "exit 7"
		}
		if args[0] == "rev-parse" {
			return "exit 1"
		}
		return ""
	})
	failed("work tree unknown", repo)
	if n := len(taskBranchSlots); n != 0 {
		t.Fatalf("%d slots still held", n)
	}
}

// At most two listings run in the process at once: a third is busy at once,
// without starting git or waiting, and every slot comes back however the
// listing ended.
func TestAThirdListingIsBusyAndSlotsComeBack(t *testing.T) {
	repo, _, side := sideRepo(t)
	pointRefs(t, repo, side, []string{"task/a-1"})
	place := newPlace(t, false)
	writeRuns(t, place, []string{"task/a-1"})

	started := make(chan struct{}, 8)
	calls := 0
	var mu sync.Mutex
	real := taskBranchCommand
	t.Cleanup(func() { taskBranchCommand = real })
	taskBranchCommand = func(ctx context.Context, dir string, args ...string) *exec.Cmd {
		mu.Lock()
		calls++
		mu.Unlock()
		started <- struct{}{}
		return exec.CommandContext(ctx, "sleep", "60")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := ListTaskBranches(ctx, place, repo, TaskBranchQuery{})
			done <- err
		}()
	}
	<-started
	<-started
	mu.Lock()
	before := calls
	mu.Unlock()
	if _, err := ListTaskBranches(context.Background(), place, repo, TaskBranchQuery{}); !errors.Is(err, ErrTaskBranchBusy) {
		t.Fatalf("third listing: %v", err)
	}
	mu.Lock()
	after := calls
	mu.Unlock()
	if after != before || len(taskBranchSlots) != 2 {
		t.Fatalf("the third started git (%d -> %d) or slots are %d", before, after, len(taskBranchSlots))
	}
	cancel()
	for range 2 {
		if err := <-done; !errors.Is(err, ErrTaskBranchBusy) {
			t.Fatalf("a cancelled listing answered %v", err)
		}
	}
	if n := len(taskBranchSlots); n != 0 {
		t.Fatalf("%d slots held after the stalled listings ended", n)
	}
	taskBranchCommand = real
	if page := listBranches(t, place, repo, TaskBranchQuery{}); len(page.Branches) != 1 {
		t.Fatalf("after release %+v", page)
	}
	listBranches(t, place, t.TempDir(), TaskBranchQuery{})
	if _, err := ListTaskBranches(context.Background(), place, repo, TaskBranchQuery{Cursor: "b1.x"}); !errors.Is(err, ErrTaskBranchCursor) {
		t.Fatalf("bad cursor %v", err)
	}
	if n := len(taskBranchSlots); n != 0 {
		t.Fatalf("%d slots held after success, no repo and refusal", n)
	}
}
