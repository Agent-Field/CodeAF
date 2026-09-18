// Package grade answers what a task left in a repository, with no language
// model anywhere in the answer.
//
// THE MECHANISM. [Grade] runs four stages over the workspace a task changed —
// gofmt over the changed Go files, "go build ./..." over the whole module,
// "go vet" over the packages the change touched, and those same packages'
// tests with the cache defeated — and answers whether everything that ran
// passed. The grade is a function of the landing and the repository alone. No
// model is consulted, so no model grades its own work, and no model's opinion
// of a landing can move it: that is the whole point of the grader of the
// pareto-crewing design's section on the reward, and it is what the model pool
// learns from in place of a judge's score.
//
// THE GRADE SAYS HOW DEEP IT WENT. A landing whose tests ran is evidence of a
// different weight from one whose tests did not, so the two are never folded
// into one word: [SourceTested] and [SourceBuilt] ride in the `judge` column
// of a pool row and say which of the two this grade is. A test stage that runs
// out of budget is an UNKNOWN, NOT A FAILURE — it answers [SourceBuilt] with
// what the earlier stages proved and names no failing stage, because a suite
// that could not finish has said nothing about the work.
//
// This package touches nothing but the workspace it is pointed at: it runs the
// go tool the caller names, in the environment the caller hands it, and holds
// no credentials, no network and no state of its own.
package grade

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// The two source words, which are the `judge` column of a pool row. The relay
// demands <vendor>/<id> of that column, so the grader spells itself the way a
// model id is spelled — the harness is the vendor and the grade's depth is the
// id. They are constants rather than a formatted pair because a row already
// written carries the bytes it was written with, and a spelling that drifts
// splits one metric into two.
const (
	// SourceTested is the deep grade: fmt, build, vet AND the touched
	// packages' tests all ran.
	SourceTested = "codeaf/grader"
	// SourceBuilt is the shallow grade: fmt, build and vet ran; the tests
	// either did not finish inside their budget or there were none to run.
	SourceBuilt = "codeaf/grader-build"
)

// The four stage words a [Result] can name. They are the vocabulary a caller
// reads [Result.Stage] against, and they are written once here so the string
// in the result and the string in the doc comment cannot drift apart.
const (
	stageFmt   = "fmt"
	stageBuild = "build"
	stageVet   = "vet"
	stageTest  = "test"
)

// The budgets a caller that names none gets. Two minutes covers gofmt, a whole
// module's build and a vet of the touched packages on a cold cache; three
// minutes covers the touched packages' own suites, which is the stage whose
// length is the change's own doing rather than the repository's.
const (
	defaultBuildBudget = 2 * time.Minute
	defaultTestBudget  = 3 * time.Minute
)

// How much of a failing stage's output [Result.Detail] carries. A grade is
// stored and read back beside a hundred others, so it keeps the head of the
// output — where the go tool puts the first thing that went wrong — and drops
// the rest rather than carrying a whole suite's log into a pool row.
const (
	detailLines = 30
	detailBytes = 2000
)

// waitDelay is how long a killed command is given to let go of the pipes this
// package reads it through. A cancelled "go test" is killed as one process
// while the test binary it started is not, and the child holds the write end
// of the pipe open; without this delay the read would block on an orphan that
// is still sleeping out its own test.
const waitDelay = 2 * time.Second

// Options is everything the caller decides about how a grade is taken.
type Options struct {
	// Go is the go tool to run; empty means "go" on PATH.
	Go string
	// BuildBudget bounds gofmt + go build + go vet together; zero means 2 minutes.
	BuildBudget time.Duration
	// TestBudget bounds go test of the touched packages; zero means 3 minutes.
	TestBudget time.Duration
	// Env is the environment for every command; nil means os.Environ().
	Env []string
}

// Result is one grade: how deep it went, what it says, and what it read.
type Result struct {
	// Source is SourceTested or SourceBuilt: how deep the grade went.
	Source string
	// Pass is the grade: everything that ran passed.
	Pass bool
	// Stage names the stage that failed — "fmt", "build", "vet" or "test" —
	// and is empty on a pass.
	Stage string
	// Detail is the first few lines of the failing stage's output, trimmed,
	// empty on a pass. Never more than ~2000 bytes.
	Detail string
	// Packages is the touched packages the vet and test stages were run on, as
	// go tool patterns, sorted. It is the set the change touched whatever the
	// grade did with it, so a landing that failed its build still says which
	// packages it would have been tested on.
	Packages []string
	// Elapsed is wall time spent grading.
	Elapsed time.Duration
}

// Grade grades the workspace after a task changed the paths given (relative to
// workspace or absolute under it).
//
// It answers ok=false, and no Result, when the workspace has no go.mod at its
// root — A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN — so the caller
// records nothing rather than a fake grade. A tree with no module is not a
// tree this grader failed; it is a tree it has nothing to say about, and a
// recorded zero there would be a lie the pool would learn from.
//
// The stages run in order and the first failure stops the run: a tree that
// does not build has nothing to say about its tests, and running them anyway
// would spend the budget to learn what the build already said. A changed path
// that falls outside the workspace is ignored rather than refused, and an
// empty changed list still builds the module — the four stages are the grade,
// and a landing that named no files is graded on the three that do not need
// them.
func Grade(ctx context.Context, workspace string, changed []string, opts Options) (Result, bool) {
	// A caller that hands no context is handed the background one rather than
	// a panic: this function is called from the end of a run, where the thing
	// that went wrong is the run and not the grade.
	if ctx == nil {
		ctx = context.Background()
	}
	root, err := filepath.Abs(workspace)
	if err != nil {
		return Result{}, false
	}
	if info, err := os.Stat(filepath.Join(root, "go.mod")); err != nil || info.IsDir() {
		return Result{}, false
	}

	started := time.Now()
	goTool := opts.Go
	if goTool == "" {
		goTool = "go"
	}
	env := opts.Env
	if env == nil {
		env = os.Environ()
	}
	buildBudget := opts.BuildBudget
	if buildBudget <= 0 {
		buildBudget = defaultBuildBudget
	}
	testBudget := opts.TestBudget
	if testBudget <= 0 {
		testBudget = defaultTestBudget
	}

	files, dirs := changedGo(root, changed)
	packages := touchedPackages(root, dirs)

	// The result starts as the shallowest passing grade there is, and every
	// stage either deepens it or ends it. A run that reaches the end without
	// testing anything is exactly this.
	result := Result{Source: SourceBuilt, Pass: true, Packages: packages}
	fail := func(stage, out string, err error) (Result, bool) {
		result.Pass = false
		result.Stage = stage
		result.Detail = detail(out, err)
		result.Elapsed = time.Since(started)
		return result, true
	}

	// The three stages that read the tree rather than run it share one budget,
	// because together they are the one question "does this tree hold up".
	buildCtx, cancelBuild := context.WithTimeout(ctx, buildBudget)
	defer cancelBuild()

	if len(files) > 0 {
		out, err := run(buildCtx, root, env, gofmtBeside(goTool), append([]string{"-l"}, files...)...)
		// gofmt -l NAMES THE FILES THAT ARE NOT FORMATTED AND IS SILENT
		// OTHERWISE, so any output at all is the failure and the output is
		// already the detail a person wants: the list of paths to run it over.
		if strings.TrimSpace(out) != "" || err != nil {
			return fail(stageFmt, out, err)
		}
	}

	// The build is over the whole module and not only the touched packages: a
	// change is allowed to break a caller it never opened, and the grade is a
	// statement about the repository the task left behind.
	if out, err := run(buildCtx, root, env, goTool, "build", "./..."); err != nil {
		return fail(stageBuild, out, err)
	}

	if len(packages) > 0 {
		if out, err := run(buildCtx, root, env, goTool, append([]string{"vet"}, packages...)...); err != nil {
			return fail(stageVet, out, err)
		}
	}

	// Nothing the change touched holds a package, so there is nothing to test
	// and the grade stays the shallow one. This is not a failure: it is the
	// honest depth of a landing that moved no Go.
	if len(packages) == 0 {
		result.Elapsed = time.Since(started)
		return result, true
	}

	testCtx, cancelTest := context.WithTimeout(ctx, testBudget)
	defer cancelTest()
	args := append([]string{"test", "-count=1", "-timeout", testBudget.String()}, packages...)
	out, err := run(testCtx, root, env, goTool, args...)
	if err == nil {
		result.Source = SourceTested
		result.Elapsed = time.Since(started)
		return result, true
	}
	// A SUITE THAT COULD NOT FINISH IS AN UNKNOWN, NOT A FAILURE. The budget
	// ran out, or the caller's own context did, and neither says anything about
	// the work: the grade keeps what the earlier stages proved and the source
	// word says the tests are not behind it. The context is read before the
	// exit status because a killed command always exits badly.
	if testCtx.Err() != nil {
		result.Elapsed = time.Since(started)
		return result, true
	}
	result.Source = SourceTested
	return fail(stageTest, out, err)
}

// run executes one command in the workspace with the caller's environment and
// returns everything it wrote to either stream. Both streams land in one buffer
// because the go tool splits a single failure across them — the compiler's
// complaint on one and the tool's summary on the other — and a detail that
// carries half of it is a detail nobody can act on.
func run(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.WaitDelay = waitDelay
	err := cmd.Run()
	return out.String(), err
}

// gofmtBeside is the gofmt belonging to the go tool given.
//
// GOFMT LIVES BESIDE THE GO IT SHIPPED WITH, and the two move together: a tree
// formatted by one toolchain's gofmt and checked by another's can fail a
// formatting check nobody broke. So when the caller names a go tool by path,
// the check is run by the gofmt in that same directory; a bare name, or none,
// leaves both to PATH.
func gofmtBeside(goTool string) string {
	dir := filepath.Dir(goTool)
	if dir == "" || dir == "." {
		return "gofmt"
	}
	return filepath.Join(dir, "gofmt")
}

// changedGo reads the changed paths into the two lists the stages want: the
// Go files that are there now, which are what gofmt is run over, and the
// directories of every changed Go path whether or not the file survived, which
// are what the touched packages are derived from. A deleted file still names a
// package the change touched.
//
// Both lists are sorted and carry no duplicate, so a grade is a property of
// the landing and never of the order the paths arrived in.
func changedGo(root string, changed []string) (files, dirs []string) {
	realRoot := root
	// A workspace reached through a symlink — every temporary directory on a
	// Mac is one — makes an absolute changed path look like it is somewhere
	// else entirely, so the real path is kept as a second way in.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		realRoot = resolved
	}
	seenFile := make(map[string]bool, len(changed))
	seenDir := make(map[string]bool, len(changed))
	for _, raw := range changed {
		rel, ok := inside(root, realRoot, raw)
		if !ok || !strings.HasSuffix(rel, ".go") {
			continue
		}
		if dir := path.Dir(rel); !seenDir[dir] {
			seenDir[dir] = true
			dirs = append(dirs, dir)
		}
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil || info.IsDir() {
			continue
		}
		if !seenFile[rel] {
			seenFile[rel] = true
			files = append(files, rel)
		}
	}
	sort.Strings(files)
	sort.Strings(dirs)
	return files, dirs
}

// inside answers a changed path as a slash-separated path relative to the
// workspace, and false when it names something outside it. A path outside the
// workspace is ignored rather than refused: the caller's list of what a task
// changed is the task's own account of itself, and a grade is not the place to
// argue with it.
func inside(root, realRoot, raw string) (string, bool) {
	if strings.TrimSpace(raw) == "" {
		return "", false
	}
	abs := raw
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, abs)
	}
	abs = filepath.Clean(abs)
	for _, base := range []string{root, realRoot} {
		rel, err := filepath.Rel(base, abs)
		if err != nil {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		return filepath.ToSlash(rel), true
	}
	return "", false
}

// touchedPackages reads the changed directories into the go tool patterns the
// vet and test stages run on: a directory holding at least one Go file is a
// package the change touched, and one holding none — because the change
// emptied it, or because the path named a directory that is gone — is nothing
// the go tool can be pointed at. The workspace root is spelled "." and every
// other directory "./dir", which is what a pattern relative to the workspace
// looks like to the go tool run there.
func touchedPackages(root string, dirs []string) []string {
	var packages []string
	for _, dir := range dirs {
		if !holdsGo(filepath.Join(root, filepath.FromSlash(dir))) {
			continue
		}
		if dir == "." {
			packages = append(packages, ".")
			continue
		}
		packages = append(packages, "./"+dir)
	}
	sort.Strings(packages)
	return packages
}

// holdsGo reports whether the directory holds at least one Go file of its own.
// A directory that cannot be read holds nothing as far as the grade is
// concerned, which keeps an unreadable path out of the patterns rather than
// turning it into a stage failure the change did not cause.
func holdsGo(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			return true
		}
	}
	return false
}

// ansiEscape matches one terminal control sequence. The go tool writes none
// into a pipe, but a test binary is free to, and a detail is stored as text
// and read back somewhere that is not a terminal — so the escapes are cut
// rather than carried into a pool row.
var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")

// detail is a failing stage's output as a [Result] carries it: trimmed, free
// of escapes, and cut to the head of the log. When the command never ran at
// all — no such tool, no permission — the output is empty and the error itself
// is the detail, because "the stage failed and said nothing" is the one detail
// a person cannot act on.
func detail(out string, err error) string {
	text := strings.TrimSpace(ansiEscape.ReplaceAllString(out, ""))
	if text == "" && err != nil {
		text = err.Error()
	}
	if lines := strings.Split(text, "\n"); len(lines) > detailLines {
		text = strings.Join(lines[:detailLines], "\n")
	}
	text = strings.TrimSpace(text)
	if len(text) > detailBytes {
		cut := text[:detailBytes]
		// The cut lands wherever the byte count lands, which may be inside a
		// rune; the broken tail is dropped so the detail stays valid UTF-8.
		for len(cut) > 0 {
			r, size := utf8.DecodeLastRuneInString(cut)
			if r != utf8.RuneError || size != 1 {
				break
			}
			cut = cut[:len(cut)-1]
		}
		text = strings.TrimSpace(cut)
	}
	return text
}
