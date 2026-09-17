package plandb

// Tests for what the store reaches disk through — the SQLite database and its
// one transaction per write — rather than for the store's laws, which
// store_test.go owns. Every one of these is about a moment the earlier file
// store could get wrong and the database is asked to get right: many writers
// at once, a crash between two calls of a decomposed verb, and a store that
// belongs to somebody else.

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The two populations this test throws at one store: goroutines writing
// through their own handle in this process, and separate processes of the
// built CLI. Each adds this many tasks, so a whole population is 8*25 = 200
// and the store holds both.
const (
	plandbWriterGoroutines = 8
	plandbWriterProcesses  = 8
	plandbTasksPerWriter   = 25
)

// planBuildCLI builds the plandb command into the test's own directory, so the
// store's cross-process road is driven by the binary a worker would run and
// not by a second handle pretending to be one.
func planBuildCLI(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "plandb")
	build := exec.Command("go", "build", "-o", binary, "github.com/Agent-Field/codeaf/cmd/plandb")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the plandb command: %v\n%s", err, output)
	}
	return binary
}

// TestPlandbCliConcurrentWritersLandEveryTask is the store's real promise under
// contention: eight goroutines and eight separate CLI processes each add
// twenty-five tasks to one store, and not one of the four hundred is lost or
// refused. The database's write lock — BEGIN IMMEDIATE plus the busy timeout
// — is what makes a writer wait for the one ahead of it instead of failing.
func TestPlandbCliConcurrentWritersLandEveryTask(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the CLI and runs sixteen concurrent writers")
	}
	binary := planBuildCLI(t)
	path := filepath.Join(t.TempDir(), "plandb.db")
	seed := planOpen(t, path)
	if err := seed.Close(); err != nil {
		t.Fatalf("close the seeded store: %v", err)
	}

	var writers sync.WaitGroup
	failures := make(chan error, plandbWriterGoroutines+plandbWriterProcesses)

	// The in-process population: each goroutine opens its own handle, so the
	// contention is real at the database and not serialized away by one
	// store's mutex.
	for writer := 0; writer < plandbWriterGoroutines; writer++ {
		writers.Add(1)
		go func(writer int) {
			defer writers.Done()
			store, err := Open(path, "", "", "", "")
			if err != nil {
				failures <- fmt.Errorf("open writer %d: %w", writer, err)
				return
			}
			defer store.Close()
			for i := 0; i < plandbTasksPerWriter; i++ {
				id := fmt.Sprintf("g%02d-%02d", writer, i)
				if _, err := store.AddMany([]TaskSpec{{ID: id, Title: id}}); err != nil {
					failures <- fmt.Errorf("goroutine %d task %d: %w", writer, i, err)
					return
				}
			}
		}(writer)
	}

	// The cross-process population: each goroutine drives one process at a
	// time, twenty-five times, through the binary a bash-belt worker runs.
	for writer := 0; writer < plandbWriterProcesses; writer++ {
		writers.Add(1)
		go func(writer int) {
			defer writers.Done()
			for i := 0; i < plandbTasksPerWriter; i++ {
				id := fmt.Sprintf("p%02d-%02d", writer, i)
				command := exec.Command(binary, "--db", path, "add", id, "--as", id)
				if output, err := command.CombinedOutput(); err != nil {
					failures <- fmt.Errorf("process %d task %d: %v\n%s", writer, i, err, output)
					return
				}
			}
		}(writer)
	}

	writers.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}

	reopened := planReopen(t, path)
	defer reopened.Close()
	landed := map[byte]int{}
	for _, task := range reopened.Tasks() {
		if task.ID != "" {
			landed[task.ID[0]]++
		}
	}
	if got := landed['g']; got != plandbWriterGoroutines*plandbTasksPerWriter {
		t.Fatalf("the goroutines landed %d tasks, want %d", got, plandbWriterGoroutines*plandbTasksPerWriter)
	}
	if got := landed['p']; got != plandbWriterProcesses*plandbTasksPerWriter {
		t.Fatalf("the processes landed %d tasks, want %d", got, plandbWriterProcesses*plandbTasksPerWriter)
	}
	want := (plandbWriterGoroutines + plandbWriterProcesses) * plandbTasksPerWriter
	if got := len(reopened.Tasks()); got != want+1 {
		t.Fatalf("the store holds %d tasks, want %d writers plus the root", got, want)
	}
	if reopened.Task("root") == nil {
		t.Fatal("the seed's root did not survive the writers")
	}
}

// TestPlandbCliCrashBetweenDecomposedVerbsLeavesConsistentStore models a
// compound verb the CLI leaves decomposed — a sequence of store calls, each
// its own transaction — interrupted between two of them. The crash is a closed
// handle, which is what an interrupted process leaves behind. What a reader
// must find is a plan that committed the call before the crash and none of the
// half-written one: it opens (which validates every invariant) and still
// resumes.
func TestPlandbCliCrashBetweenDecomposedVerbsLeavesConsistentStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plandb.db")
	store := planOpen(t, path)
	planAdd(t, store, planSpec("a", "A"), planSpec("b", "B"))

	// The first call of the decomposition: a new task that waits on a, which
	// is one transaction all by itself.
	if _, err := store.AddMany([]TaskSpec{{
		ID: "fresh", Title: "Fresh", Dependencies: []Dependency{{TaskID: "a"}},
	}}); err != nil {
		t.Fatalf("first verb: %v", err)
	}
	// Crash: the handle is dropped with the rewire's second call — the edge
	// from b — never run.
	if err := store.Close(); err != nil {
		t.Fatalf("close mid-sequence: %v", err)
	}

	// The database a reader finds is whole: it opens — Open validates the
	// loaded plan — and holds exactly the first call's work.
	reopened := planReopen(t, path)
	for _, id := range []string{"a", "b", "fresh"} {
		if reopened.Task(id) == nil {
			t.Fatalf("the crash lost committed task %q: %#v", id, reopened.Tasks())
		}
	}
	if got := reopened.Task("fresh"); len(got.Dependencies) != 1 || got.Dependencies[0].TaskID != "a" {
		t.Fatalf("the first verb's edge did not survive: %#v", got.Dependencies)
	}
	if got := reopened.Task("fresh"); got.Status != StatusPending {
		t.Fatalf("fresh status = %s, want pending behind a", got.Status)
	}
	if got := reopened.Task("b"); len(got.Dependencies) != 0 {
		t.Fatalf("the unrun verb's edge was written anyway: %#v", got.Dependencies)
	}

	// Consistent also means resumable: the second call applies cleanly now.
	if _, err := reopened.AddDep("b", "fresh", ""); err != nil {
		t.Fatalf("second verb after the crash: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	again := planReopen(t, path)
	defer again.Close()
	if got := again.Task("b"); len(got.Dependencies) != 1 || got.Dependencies[0].TaskID != "fresh" {
		t.Fatalf("the completed rewire did not land: %#v", got.Dependencies)
	}
}

// TestPlandbCliForeignProjectRefusedOnAWrittenStore proves the refusal across
// a real database: a plandb.db this code wrote belongs to the run that created
// it, and an open naming another project is refused rather than merged, while
// the store's own project still loads.
func TestPlandbCliForeignProjectRefusedOnAWrittenStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plandb.db")
	store := planOpen(t, path)
	planAdd(t, store, planSpec("job", "Job"))
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := Open(path, "another-project", "", "", ""); err == nil || !strings.Contains(err.Error(), "different run") {
		t.Fatalf("a store written by this code accepted another project: %v", err)
	}
	reopened := planReopen(t, path)
	defer reopened.Close()
	if project := reopened.Project(); project != "plan-test" {
		t.Fatalf("the store's own project did not load back: %q", project)
	}
	if reopened.Task("job") == nil {
		t.Fatalf("the refused open damaged the store: %#v", reopened.Tasks())
	}
}
