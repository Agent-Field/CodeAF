package grade

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// The three files a graded fixture is built from. They are spelled the way
// gofmt would spell them, so a fixture never fails the formatting stage by
// accident and the stage under test is always the one the case is about.
const (
	addGo = `package pkg

// Add answers the sum of the two integers given.
func Add(a, b int) int {
	return a + b
}
`
	addTestGo = `package pkg

import "testing"

func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Fatal("Add does not add")
	}
}
`
	failingTestGo = `package pkg

import "testing"

func TestAdd(t *testing.T) {
	t.Fatal("this seat did not do its job")
}
`
)

// tools skips the test when the machine has no go tool or no gofmt to grade
// with. A grade is a claim about what the go tool says, and a box without one
// can neither make the claim nor refute it.
func tools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"go", "gofmt"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("grade: no %s on PATH, nothing to grade with", tool)
		}
	}
}

// hermeticEnv is the environment every case grades in: the machine's own, with
// the two variables that would let the developer's checkout reach into a
// temporary module cleared. A GOFLAGS carrying -mod=vendor, or a go.work above
// the temporary directory, would fail a fixture that is perfectly good.
func hermeticEnv() []string {
	return append(os.Environ(), "GOFLAGS=", "GOWORK=off")
}

// newModule writes a module root with the files given — paths are slash
// separated and relative to the root — and answers where it is.
func newModule(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, "go.mod", "module example.test\n\ngo 1.22\n")
	for name, body := range files {
		write(t, root, name, body)
	}
	return root
}

func write(t *testing.T, root, name, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("grade: making %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("grade: writing %s: %v", full, err)
	}
}

// grade runs the package over the fixture with the hermetic environment and
// fails the test when the workspace was refused, which every case but the
// first expects to be a module.
func grade(t *testing.T, root string, changed []string, opts Options) Result {
	t.Helper()
	opts.Env = hermeticEnv()
	result, ok := Grade(context.Background(), root, changed, opts)
	if !ok {
		t.Fatalf("grade: %s was refused as a workspace", root)
	}
	return result
}

// A tree with no go.mod is not a tree this grader failed; it is a tree it has
// nothing to say about, and it says nothing rather than recording a zero the
// pool would learn from.
func TestATreeWithNoModuleIsRefusedRatherThanGradedZero(t *testing.T) {
	tools(t)
	root := t.TempDir()
	write(t, root, "pkg/pkg.go", addGo)
	result, ok := Grade(context.Background(), root, []string{"pkg/pkg.go"}, Options{Env: hermeticEnv()})
	if ok {
		t.Fatalf("grade: a tree with no go.mod was graded: %+v", result)
	}
	if !reflect.DeepEqual(result, Result{}) {
		t.Fatalf("grade: a refused workspace carried a result: %+v", result)
	}
}

// The deep grade: everything ran, everything passed, and the source word says
// the tests are behind it.
func TestAPackageThatBuildsAndPassesIsGradedTested(t *testing.T) {
	tools(t)
	root := newModule(t, map[string]string{
		"pkg/pkg.go":      addGo,
		"pkg/pkg_test.go": addTestGo,
	})
	result := grade(t, root, []string{"pkg/pkg.go", "pkg/pkg_test.go"}, Options{})
	if !result.Pass {
		t.Fatalf("grade: a clean package failed at %q: %s", result.Stage, result.Detail)
	}
	if result.Source != SourceTested {
		t.Fatalf("grade: source is %q, want %q", result.Source, SourceTested)
	}
	if result.Stage != "" || result.Detail != "" {
		t.Fatalf("grade: a pass named stage %q and detail %q", result.Stage, result.Detail)
	}
	if want := []string{"./pkg"}; !reflect.DeepEqual(result.Packages, want) {
		t.Fatalf("grade: packages are %v, want %v", result.Packages, want)
	}
}

// A suite that ran and said no is the one failure that carries the deep source
// word: the tests are behind this grade, and what they said is that the work is
// not good.
func TestAFailingSuiteIsGradedTestedAndNamesTheTestStage(t *testing.T) {
	tools(t)
	root := newModule(t, map[string]string{
		"pkg/pkg.go":      addGo,
		"pkg/pkg_test.go": failingTestGo,
	})
	result := grade(t, root, []string{"pkg/pkg_test.go"}, Options{})
	if result.Pass {
		t.Fatal("grade: a failing suite passed")
	}
	if result.Stage != "test" {
		t.Fatalf("grade: stage is %q, want %q", result.Stage, "test")
	}
	if result.Source != SourceTested {
		t.Fatalf("grade: source is %q, want %q", result.Source, SourceTested)
	}
	if result.Detail == "" {
		t.Fatal("grade: a failing suite carried no detail")
	}
}

// A tree that does not build has nothing to say about its tests, so the run
// stops at the build and the source word says nothing was tested.
func TestATreeThatDoesNotBuildStopsAtTheBuildStage(t *testing.T) {
	tools(t)
	root := newModule(t, map[string]string{
		"pkg/pkg.go": `package pkg

func Add(a, b int) int {
	return missingEntirely
}
`,
	})
	result := grade(t, root, []string{"pkg/pkg.go"}, Options{})
	if result.Pass {
		t.Fatal("grade: a tree that does not build passed")
	}
	if result.Stage != "build" {
		t.Fatalf("grade: stage is %q, want %q (detail: %s)", result.Stage, "build", result.Detail)
	}
	if result.Source != SourceBuilt {
		t.Fatalf("grade: source is %q, want %q", result.Source, SourceBuilt)
	}
	if result.Detail == "" {
		t.Fatal("grade: a build failure carried no detail")
	}
}

// The formatting check is first, so an unformatted file is named as itself
// rather than as whatever the later stages would have made of it.
func TestAnUnformattedFileStopsAtTheFmtStage(t *testing.T) {
	tools(t)
	root := newModule(t, map[string]string{
		"pkg/pkg.go": "package pkg\n\nfunc  Add(a, b int) int { return a+b }\n",
	})
	result := grade(t, root, []string{"pkg/pkg.go"}, Options{})
	if result.Pass {
		t.Fatal("grade: an unformatted file passed")
	}
	if result.Stage != "fmt" {
		t.Fatalf("grade: stage is %q, want %q (detail: %s)", result.Stage, "fmt", result.Detail)
	}
	if result.Source != SourceBuilt {
		t.Fatalf("grade: source is %q, want %q", result.Source, SourceBuilt)
	}
}

// A suite that could not finish inside its budget is an unknown and not a
// failure: the grade keeps what the earlier stages proved, names no stage, and
// says with its source word that the tests are not behind it.
func TestASuiteThatOutlastsItsBudgetIsAnUnknownAndNotAFailure(t *testing.T) {
	tools(t)
	root := newModule(t, map[string]string{
		"pkg/pkg.go": addGo,
		"pkg/pkg_test.go": `package pkg

import (
	"testing"
	"time"
)

func TestAdd(t *testing.T) {
	time.Sleep(20 * time.Second)
}
`,
	})
	result := grade(t, root, []string{"pkg/pkg_test.go"}, Options{TestBudget: 3 * time.Second})
	if !result.Pass {
		t.Fatalf("grade: an unfinished suite was read as a failure at %q: %s", result.Stage, result.Detail)
	}
	if result.Source != SourceBuilt {
		t.Fatalf("grade: source is %q, want %q", result.Source, SourceBuilt)
	}
	if result.Stage != "" {
		t.Fatalf("grade: an unfinished suite named stage %q", result.Stage)
	}
	if result.Elapsed > time.Minute {
		t.Fatalf("grade: the budget was not honoured, grading took %s", result.Elapsed)
	}
}

// The caller's list of what a task changed is the task's own account of
// itself, and a path outside the workspace is ignored rather than argued with.
func TestAChangedPathOutsideTheWorkspaceIsIgnored(t *testing.T) {
	tools(t)
	root := newModule(t, map[string]string{
		"pkg/pkg.go":      addGo,
		"pkg/pkg_test.go": addTestGo,
	})
	changed := []string{
		"pkg/pkg.go",
		"../elsewhere/other.go",
		filepath.Join(t.TempDir(), "another.go"),
		"",
	}
	result := grade(t, root, changed, Options{})
	if !result.Pass {
		t.Fatalf("grade: a stray path broke the grade at %q: %s", result.Stage, result.Detail)
	}
	if want := []string{"./pkg"}; !reflect.DeepEqual(result.Packages, want) {
		t.Fatalf("grade: packages are %v, want %v", result.Packages, want)
	}
	if result.Source != SourceTested {
		t.Fatalf("grade: source is %q, want %q", result.Source, SourceTested)
	}
}

// A landing that changed nothing is failed at the landing stage before any
// tool runs: the unchanged tree building says nothing about work never done,
// and a pass here taught the pool that doing nothing is acceptable.
func TestALandingThatChangedNothingFailsAtTheLandingStage(t *testing.T) {
	root := newModule(t, map[string]string{"pkg/pkg.go": addGo})
	result := grade(t, root, nil, Options{})
	if result.Pass {
		t.Fatalf("grade: an empty changed list passed")
	}
	if result.Stage != stageLanding || result.Detail != detailNoLanding {
		t.Fatalf("grade: stopped at %q with %q, want %q with %q", result.Stage, result.Detail, stageLanding, detailNoLanding)
	}
	if result.Source != SourceBuilt {
		t.Fatalf("grade: source is %q, want %q", result.Source, SourceBuilt)
	}
	if result.Version != Version {
		t.Fatalf("grade: version is %d, want %d", result.Version, Version)
	}
}

// A landing that removed a test file fails at the landing stage and names
// the file, because deleting the suite is the cheapest way to make it pass.
func TestALandingThatRemovedATestFileFailsByName(t *testing.T) {
	root := newModule(t, map[string]string{"pkg/pkg.go": addGo})
	result := grade(t, root, []string{"pkg/pkg.go", "pkg/pkg_test.go"}, Options{})
	if result.Pass {
		t.Fatalf("grade: a removed test file passed")
	}
	if result.Stage != stageLanding || result.Detail != detailTestRemoved+"pkg/pkg_test.go" {
		t.Fatalf("grade: stopped at %q with %q", result.Stage, result.Detail)
	}
}

// A landing that changed files but no Go file is not graded at all: this
// grader reads Go, and a build of the tree it did not touch is not a grade of
// what it did.
func TestALandingWithNoGoFileIsNotGraded(t *testing.T) {
	root := newModule(t, map[string]string{"pkg/pkg.go": addGo, "README.md": "# a module\n"})
	if _, ok := Grade(context.Background(), root, []string{"README.md"}, Options{Env: hermeticEnv()}); ok {
		t.Fatalf("grade: a landing with no Go file was graded")
	}
}

// The detail a grade carries is cut to the head of the log, so one suite's
// output cannot arrive in a pool row whole.
func TestDetailIsCutToTheHeadOfTheLog(t *testing.T) {
	var long string
	for i := 0; i < 200; i++ {
		long += "this is one line of a very long and repetitive failure log\n"
	}
	got := detail(long, nil)
	if len(got) > detailBytes {
		t.Fatalf("grade: detail is %d bytes, want at most %d", len(got), detailBytes)
	}
	if got == "" {
		t.Fatal("grade: a long log was cut to nothing")
	}
	if escaped := detail("\x1b[31mred\x1b[0m", nil); escaped != "red" {
		t.Fatalf("grade: detail kept an escape sequence: %q", escaped)
	}
}
