package exec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The engine verifies with the commands it discovers, spelled bare. A cell
// that carries its own venv must have that venv answer for `pytest`, or the
// verification judges the machine's Python and not the project — the smoke
// run failed four audit cycles in a row on a jiwer import the venv had and
// the system interpreter did not.
func TestChildPathLeadsWithTheProjectVenv(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, ".venv", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	worker := NewSWE(nil, "vendor/model", "key", "", 0)
	path := childPath(t, worker.environ(dir))
	prefix := bin + string(os.PathListSeparator)
	if !strings.HasPrefix(path, prefix) {
		t.Fatalf("child PATH does not lead with the project venv:\n%s", path)
	}
}

// A workspace without a venv changes nothing: the child inherits PATH as-is.
func TestChildPathUntouchedWithoutAVenv(t *testing.T) {
	worker := NewSWE(nil, "vendor/model", "key", "", 0)
	if got, want := childPath(t, worker.environ(t.TempDir())), os.Getenv("PATH"); got != want {
		t.Fatalf("child PATH rewritten with no venv present:\ngot  %s\nwant %s", got, want)
	}
}

func childPath(t *testing.T, environ []string) string {
	t.Helper()
	for _, entry := range environ {
		if value, ok := strings.CutPrefix(entry, "PATH="); ok {
			return value
		}
	}
	t.Fatal("child environment carries no PATH")
	return ""
}

// The flight recorder never appears in the deliverable's diff. The auditor
// proved the need: it refused a change set whose only addition was a
// 47,245-line trace of the run that produced it.
func TestObsIsExcludedFromTheWorkspaceRepository(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	excludeFromGit(t.Context(), dir, obsDir+"/")
	excludeFromGit(t.Context(), dir, obsDir+"/") // twice writes once
	raw, err := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Count(string(raw), obsDir+"/"), 1; got != want {
		t.Fatalf("exclude carries the pattern %d times, want %d:\n%s", got, want, raw)
	}
}

// Ordering is the whole of it, and the previous arrangement had it backwards.
//
// The recorder is opened before the repository work starts — it has to be, or
// that work goes unrecorded — so the trace file already exists when `add -A`
// runs. Excluding it afterwards is too late twice: it is in the baseline
// commit, and an exclusion never applies to a file git is already tracking, so
// every later append shows up as a modification the auditor is entitled to
// refuse. The exclusion has to land between the repository existing and
// anything being staged.
func TestTheBaselineCommitCarriesNoFlightRecorder(t *testing.T) {
	space, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	directory := space.Root()
	if err := os.WriteFile(filepath.Join(directory, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Exactly the order SWE.Run uses: recorder first, repository second.
	trace := newTracer(space, "7")
	trace.note("engine: started")
	trace.flush()
	if _, err := os.Stat(tracePath(t, space, "7")); err != nil {
		t.Fatalf("the recorder was never opened, so this proves nothing: %v", err)
	}

	initialized, err := ensureGitRepository(t.Context(), directory, obsDir+"/", traceDir+"/")
	if err != nil {
		t.Fatal(err)
	}
	if !initialized {
		t.Fatal("a fresh workspace reported an existing repository")
	}
	trace.note("engine: still going")
	trace.close()

	for _, path := range gitLines(t.Context(), directory, "ls-files") {
		if strings.Contains(path, "trace.log") || strings.HasPrefix(path, traceDir) {
			t.Errorf("the baseline commit tracks the flight recorder: %s", path)
		}
	}
	// Tracked is the failure that outlives the commit: an excluded file that is
	// already in the index goes on reporting its modifications forever.
	for _, line := range gitLines(t.Context(), directory, "status", "--porcelain") {
		if strings.Contains(line, "trace.log") {
			t.Errorf("git still reports the recorder after the baseline: %q", line)
		}
	}
	if lines := gitLines(t.Context(), directory, "ls-files"); len(lines) == 0 {
		t.Error("the baseline committed nothing at all, so the exclusion proves nothing")
	}

	// The control arm, so the assertions above are not passing for some other
	// reason: the same sequence with the patterns withheld commits the recorder,
	// which is precisely what excluding after the baseline used to produce.
	bare, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	unguarded := newTracer(bare, "7")
	unguarded.note("engine: started")
	unguarded.close()
	if _, err := ensureGitRepository(t.Context(), bare.Root()); err != nil {
		t.Fatal(err)
	}
	tracked := false
	for _, path := range gitLines(t.Context(), bare.Root(), "ls-files") {
		if strings.Contains(path, "trace.log") {
			tracked = true
		}
	}
	if !tracked {
		t.Error("without the patterns the recorder was not committed either; this test no longer discriminates")
	}
}

// The layout the old implementation quietly did nothing on. `.git` is a file in
// a worktree, and stat'ing it for a directory refused every write — on exactly
// the layout a person is most likely to hand a coding worker.
func TestTheExclusionLandsInAWorktreeToo(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "main")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, main)
	if err := os.WriteFile(filepath.Join(main, "readme.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, main, "add", "-A")
	mustGit(t, main, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false",
		"commit", "-m", "base")

	linked := filepath.Join(root, "linked")
	mustGit(t, main, "worktree", "add", "-b", "side", linked)
	if info, err := os.Stat(filepath.Join(linked, ".git")); err != nil || info.IsDir() {
		t.Fatalf("the worktree layout is not what this test is about: err=%v", err)
	}

	excludeFromGit(t.Context(), linked, traceDir+"/")
	if err := os.MkdirAll(filepath.Join(linked, traceDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linked, traceName("3")), []byte("a trace\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, line := range gitLines(t.Context(), linked, "status", "--porcelain") {
		if strings.Contains(line, "trace") {
			t.Fatalf("a worktree's recorder is still visible to git: %q", line)
		}
	}
}

func gitInit(t *testing.T, directory string) {
	t.Helper()
	mustGit(t, directory, "init")
}

func mustGit(t *testing.T, directory string, args ...string) {
	t.Helper()
	if err := gitQuiet(t.Context(), directory, args...); err != nil {
		t.Fatalf("git %s in %s: %v", strings.Join(args, " "), directory, err)
	}
}

// Siblings share one workspace, so they bootstrap it together.
//
// A job's leaves all run in the job's directory and the scheduler starts them
// at once. Before this was serialised, four concurrent swe leaves raced on
// git's own locks — `.git/index.lock: File exists` from `add -A` and from
// `commit`, `templates/info/exclude: File exists` from a second `init` — and
// git reports a lost race as exit 128. On the live run that found this, three
// of the four leaves died at the starting line with "could not stage the
// workspace" / "could not commit a baseline"; the fourth did the work.
func TestSiblingLeavesBootstrapOneWorkspaceWithoutRacing(t *testing.T) {
	directory := t.TempDir()
	for i := range 200 {
		name := filepath.Join(directory, fmt.Sprintf("file-%d.txt", i))
		if err := os.WriteFile(name, []byte(strings.Repeat("x", 512)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	const leaves = 4
	var wait sync.WaitGroup
	errs := make([]error, leaves)
	initialised := make([]bool, leaves)
	for leaf := range leaves {
		wait.Add(1)
		go func() {
			defer wait.Done()
			initialised[leaf], errs[leaf] = ensureGitRepository(t.Context(), directory, obsDir+"/", traceDir+"/")
		}()
	}
	wait.Wait()

	made := 0
	for leaf, err := range errs {
		if err != nil {
			t.Errorf("leaf %d: %v", leaf, err)
		}
		if initialised[leaf] {
			made++
		}
	}
	// Exactly one leaf makes the repository; the others find it made.
	if made != 1 {
		t.Errorf("%d leaves reported initialising the workspace, want 1", made)
	}
	if lines := gitLines(t.Context(), directory, "log", "--oneline"); len(lines) != 1 {
		t.Errorf("the workspace carries %d baseline commits, want 1", len(lines))
	}
	raw, err := os.ReadFile(filepath.Join(directory, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(raw), obsDir+"/"); got != 1 {
		t.Errorf("exclude carries the recorder pattern %d times, want 1:\n%s", got, raw)
	}
}
