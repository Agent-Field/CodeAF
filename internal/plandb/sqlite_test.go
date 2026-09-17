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
	"time"
)

// The population this test throws at one store: eight writers, each a
// goroutine driving its own separate process of the built CLI, each adding
// this many tasks. So 8*25 = 200 tasks land in one database while eight
// processes contend for its write lock.
const (
	plandbConcurrentWriters = 8
	plandbTasksPerWriter    = 25
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
// contention: eight goroutines, each driving its own separate process of the
// built CLI, add twenty-five tasks apiece to one store, and all two hundred
// land with not one writer refused. The database's write lock — BEGIN
// IMMEDIATE plus the busy timeout — is what makes a writer wait for the one
// ahead of it instead of failing.
func TestPlandbCliConcurrentWritersLandEveryTask(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the CLI and runs eight concurrent writer processes")
	}
	binary := planBuildCLI(t)
	path := filepath.Join(t.TempDir(), "plandb.db")
	seed := planOpen(t, path)
	if err := seed.Close(); err != nil {
		t.Fatalf("close the seeded store: %v", err)
	}

	var writers sync.WaitGroup
	failures := make(chan error, plandbConcurrentWriters)
	for writer := 0; writer < plandbConcurrentWriters; writer++ {
		writers.Add(1)
		go func(writer int) {
			defer writers.Done()
			// Every one of the writer's tasks is added by the binary a
			// bash-belt worker runs, so the contention is at the database and
			// is never serialized away by one handle's mutex.
			for i := 0; i < plandbTasksPerWriter; i++ {
				id := planWriterTaskID(writer, i)
				command := exec.Command(binary, "--db", path, "add", id, "--as", id)
				if output, err := command.CombinedOutput(); err != nil {
					failures <- fmt.Errorf("writer %d task %d: %v\n%s", writer, i, err, output)
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
	var missing []string
	for writer := 0; writer < plandbConcurrentWriters; writer++ {
		for i := 0; i < plandbTasksPerWriter; i++ {
			if id := planWriterTaskID(writer, i); reopened.Task(id) == nil {
				missing = append(missing, id)
			}
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d writers' tasks did not land: %v", len(missing), missing)
	}
	want := plandbConcurrentWriters * plandbTasksPerWriter
	if got := len(reopened.Tasks()); got != want+1 {
		t.Fatalf("the store holds %d tasks, want the %d the writers added plus the root", got, want)
	}
	if reopened.Task("root") == nil {
		t.Fatal("the seed's root did not survive the writers")
	}
}

// planWriterTaskID is one writer's task id, spelled so no two writers can ask
// the store for the same one: a task missing afterwards is a lost write and
// never a collision two processes fought over.
func planWriterTaskID(writer, task int) string {
	return fmt.Sprintf("w%02d-%02d", writer, task)
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

// The population two claiming handles race over: this many ready leaves are
// seeded, and the two handles claim from that one set until it is empty.
const plandbClaimLeaves = 12

// TestPlandbCliTwoHandlesClaimOneReadySet drives two Store values — the two
// processes two codeaf workers would be — against one plandb.db. Both claim
// from the same ready set at once, so three things must hold afterwards: no
// leaf is claimed twice, no claim is lost, and each handle's Changed names
// the leaves the OTHER handle claimed. The last one is the cross-process
// promise the store owes a waiting worker: a handle's reads answer the last
// committed plan, not the plan it last wrote itself.
func TestPlandbCliTwoHandlesClaimOneReadySet(t *testing.T) {
	if testing.Short() {
		t.Skip("two handles claim the same ready set in parallel")
	}
	path := filepath.Join(t.TempDir(), "plandb.db")
	seed := planOpen(t, path)
	leaves := make([]TaskSpec, 0, plandbClaimLeaves)
	for i := 0; i < plandbClaimLeaves; i++ {
		leaves = append(leaves, TaskSpec{ID: fmt.Sprintf("leaf-%02d", i), Title: fmt.Sprintf("Leaf %d", i)})
	}
	planAdd(t, seed, leaves...)
	if err := seed.Close(); err != nil {
		t.Fatalf("close the seeded store: %v", err)
	}
	since := time.Now().UTC().Add(-time.Second)

	first := planReopen(t, path)
	defer first.Close()
	second := planReopen(t, path)
	defer second.Close()

	var mu sync.Mutex
	var claimedTasks []*Task
	var claimErrs []error
	var claimers sync.WaitGroup
	for i, st := range []*Store{first, second} {
		claimers.Add(1)
		go func(st *Store, agent string) {
			defer claimers.Done()
			// Each handle claims until the ready set is empty, so every leaf
			// must be claimed exactly once between the two of them, and neither
			// may be handed a leaf the other already took.
			for {
				task, err := st.ClaimNext(agent)
				if err != nil {
					mu.Lock()
					claimErrs = append(claimErrs, err)
					mu.Unlock()
					return
				}
				if task == nil {
					return
				}
				mu.Lock()
				claimedTasks = append(claimedTasks, task)
				mu.Unlock()
			}
		}(st, fmt.Sprintf("agent-%d", i))
	}
	claimers.Wait()
	for _, err := range claimErrs {
		t.Fatalf("a claim failed: %v", err)
	}

	owners := map[string]string{}
	for _, task := range claimedTasks {
		if prev, dup := owners[task.ID]; dup {
			t.Fatalf("leaf %q was claimed twice: by %q and %q", task.ID, prev, task.ClaimedBy)
		}
		owners[task.ID] = task.ClaimedBy
	}
	for i := 0; i < plandbClaimLeaves; i++ {
		if id := fmt.Sprintf("leaf-%02d", i); owners[id] == "" {
			t.Fatalf("leaf %q was never claimed — a claim was lost", id)
		}
	}

	// Every leaf is running on disk, owned by the one agent that took it.
	reopened := planReopen(t, path)
	defer reopened.Close()
	for id, agent := range owners {
		if task := reopened.Task(id); task == nil || task.Status != StatusRunning || task.ClaimedBy != agent {
			t.Fatalf("leaf %q on disk = %#v, want running for %q", id, task, agent)
		}
	}

	// A fresh reader sees every claim the race committed: a wake that reads
	// the store after the fact must name all the leaves that moved.
	reader := planReopen(t, path)
	defer reader.Close()
	quiet := map[string]bool{}
	for _, id := range reader.Changed(since) {
		quiet[id] = true
	}
	for id := range owners {
		if !quiet[id] {
			t.Fatalf("a fresh reader's Changed missed leaf %q the race committed: %v", id, reader.Changed(since))
		}
	}

	// And a handle's own Changed names what the OTHER handle wrote: first
	// leaves a note, second must see that leaf move, and then the mirror —
	// the cross-process promise a waiting worker reads, hold as long as the
	// read answers the last committed plan and not the plan this handle last
	// wrote itself.
	moment := time.Now().UTC()
	if _, err := first.AddNote("leaf-01", "agent-0", "handoff for the next worker"); err != nil {
		t.Fatalf("first note: %v", err)
	}
	if !named(second.Changed(moment), "leaf-01") {
		t.Fatalf("second handle missed the leaf first noted: %v", second.Changed(moment))
	}
	moment = time.Now().UTC()
	if _, err := second.AddNote("leaf-02", "agent-1", "handoff for the next worker"); err != nil {
		t.Fatalf("second note: %v", err)
	}
	if !named(first.Changed(moment), "leaf-02") {
		t.Fatalf("first handle missed the leaf second noted: %v", first.Changed(moment))
	}
}

// named reports whether ids carries id.
func named(ids []string, id string) bool {
	for _, got := range ids {
		if got == id {
			return true
		}
	}
	return false
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
