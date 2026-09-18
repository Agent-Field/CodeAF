package plandb

// Tests for what the store reaches disk through — the SQLite database and its
// one transaction per write — rather than for the store's laws, which
// store_test.go owns. Every one of these is about a moment the earlier file
// store could get wrong and the database is asked to get right: many writers
// at once, a crash between two calls of a decomposed verb, and a store that
// belongs to somebody else.

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
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

// The crash helper's two modes, named in the child's environment: it runs only
// when the path is set, so the parent's ordinary `go test` never enters it.
const (
	plandbCrashPathEnv = "PLANDB_CRASH_TEST_PATH"
	plandbCrashModeEnv = "PLANDB_CRASH_TEST_MODE"
)

// TestPlandbCliCrashMidWriteLeavesTheLastCommit kills a write half-way. A
// child process opens the store, begins the one write transaction every writer
// takes, lands the first statement of the whole-plan rewrite, and dies with no
// commit — the crash an interrupted process leaves. The next open must read
// the state BEFORE the write, not the half-written one, and it must still be
// resumable: the log replays what committed and discards what did not.
func TestPlandbCliCrashMidWriteLeavesTheLastCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plandb.db")
	seed := planOpen(t, path)
	planAdd(t, seed, planSpec("kept", "Kept"), planSpec("other", "Other"))
	if err := seed.Close(); err != nil {
		t.Fatalf("close the seeded store: %v", err)
	}
	planRunCrashChild(t, path, "half", 17)

	// Open validates every invariant of the loaded plan, so a crash that left
	// the file inconsistent would refuse here rather than answer.
	reopened := planReopen(t, path)
	defer reopened.Close()
	for id, title := range map[string]string{"kept": "Kept", "other": "Other"} {
		if got := reopened.Task(id); got == nil || got.Title != title {
			t.Fatalf("after the crash task %q = %#v, want the committed %q", id, got, title)
		}
	}
	// Consistent also means resumable: the next write applies cleanly.
	if _, err := reopened.AddNote("kept", "w", "after the crash"); err != nil {
		t.Fatalf("write after the crash: %v", err)
	}
}

// TestPlandbCliCrashedCommitIsReplayedFromTheWAL commits a write in a child
// and crashes it with the store open, so the commit sits in the write-ahead
// log and the database file has not been checkpointed yet. The next open must
// replay the log and answer the committed plan: the WAL is replayed, not
// discarded.
func TestPlandbCliCrashedCommitIsReplayedFromTheWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plandb.db")
	seed := planOpen(t, path)
	planAdd(t, seed, planSpec("kept", "Kept"))
	if err := seed.Close(); err != nil {
		t.Fatalf("close the seeded store: %v", err)
	}
	planRunCrashChild(t, path, "commit", 23)

	// The commit is still waiting in the log rather than folded into the file:
	// that is what makes the next open a replay and not a plain read.
	wal, err := os.Stat(path + "-wal")
	if err != nil {
		t.Fatalf("the crashed commit left no write-ahead log: %v", err)
	}
	if wal.Size() == 0 {
		t.Fatal("the write-ahead log is empty — the commit was not waiting in it")
	}

	reopened := planReopen(t, path)
	defer reopened.Close()
	if got := reopened.Task("survivor"); got == nil || got.Title != "Survivor" {
		t.Fatalf("the crashed commit was not replayed: %#v", got)
	}
	// The mode is a property of the file, and the reader recovers from the same
	// log the next writer will append to.
	var mode string
	if err := reopened.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || !strings.EqualFold(mode, "wal") {
		t.Fatalf("journal mode after recovery = %q, %v, want wal", mode, err)
	}
}

// planRunCrashChild runs this test binary as a separate process in one of the
// crash helper's modes and fails unless it exits with wantCode. A separate
// process is the only way to die with a transaction open: an in-process close
// rolls it back cleanly, which is not the crash under test.
func planRunCrashChild(t *testing.T, path, mode string, wantCode int) {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestPlandbCliCrashHelper$", "-test.count=1")
	command.Env = append(os.Environ(), plandbCrashPathEnv+"="+path, plandbCrashModeEnv+"="+mode)
	out, err := command.CombinedOutput()
	var exit *exec.ExitError
	if err == nil || !errors.As(err, &exit) || exit.ExitCode() != wantCode {
		t.Fatalf("crash child (mode %s) exited %v, want code %d:\n%s", mode, err, wantCode, out)
	}
}

// TestPlandbCliCrashHelper is the child half of the two crash tests. It runs
// only when the parent named a path and a mode, and it leaves by os.Exit so no
// deferred rollback or close can tidy up after it — the transaction stays
// exactly as the crash found it.
func TestPlandbCliCrashHelper(t *testing.T) {
	path := os.Getenv(plandbCrashPathEnv)
	mode := os.Getenv(plandbCrashModeEnv)
	if path == "" || mode == "" {
		t.Skip("the crash helper runs only as a child process")
	}
	store, err := Open(path, "", "", "", "")
	if err != nil {
		t.Fatalf("child open: %v", err)
	}
	switch mode {
	case "half":
		// BEGIN IMMEDIATE, the first statement of the whole-plan rewrite, and
		// no commit: the process dies holding an unfinished transaction.
		tx, err := store.beginWrite()
		if err != nil {
			t.Fatalf("child begin: %v", err)
		}
		if _, err := tx.Exec("DELETE FROM tasks"); err != nil {
			t.Fatalf("child first statement: %v", err)
		}
		os.Exit(17)
	case "commit":
		// A committed write, then a crash with the log holding it and no
		// checkpoint: the frames the next open must replay.
		if _, err := store.AddMany([]TaskSpec{planSpec("survivor", "Survivor")}); err != nil {
			t.Fatalf("child commit: %v", err)
		}
		os.Exit(23)
	default:
		t.Fatalf("unknown crash mode %q", mode)
	}
}

// TestPlandbCliWriterWaitsOutTheWriteLock proves the busy handling. The first
// handle holds a write transaction open while the second writes: the second
// meets SQLITE_BUSY, the busy timeout set once at open holds it, and the write
// lands once the first commits instead of failing at once.
func TestPlandbCliWriterWaitsOutTheWriteLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plandb.db")
	seed := planOpen(t, path)
	planAdd(t, seed, planSpec("a", "A"))
	if err := seed.Close(); err != nil {
		t.Fatalf("close the seeded store: %v", err)
	}
	first := planReopen(t, path)
	defer first.Close()
	second := planReopen(t, path)
	defer second.Close()

	// The pragma that makes a writer wait rather than fail is set once, on the
	// write handle, and both handles carry it.
	for _, st := range []*Store{first, second} {
		var timeout int
		if err := st.db.QueryRow("PRAGMA busy_timeout").Scan(&timeout); err != nil || timeout != 5000 {
			t.Fatalf("busy_timeout = %d, %v, want the 5000 set at open", timeout, err)
		}
	}

	// first opens its write transaction and holds it: the commit is gated on a
	// channel, so the write lock stays held while second tries to write.
	held := make(chan struct{})
	release := make(chan struct{})
	first.now = func() time.Time {
		select {
		case <-held:
		default:
			close(held)
		}
		<-release
		return time.Now().UTC()
	}
	firstDone := make(chan error, 1)
	go func() {
		_, err := first.Amend("a", "held open")
		firstDone <- err
	}()
	<-held

	secondDone := make(chan error, 1)
	go func() {
		_, err := second.Amend("a", "waited its turn")
		secondDone <- err
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("the second writer did not wait for the held lock: %v", err)
	case <-time.After(300 * time.Millisecond):
		// Still waiting behind the busy timeout — the handling is doing its job.
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first writer: %v", err)
	}
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second writer once the lock cleared: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the second writer never landed once the lock cleared")
	}

	// Both writes are in the plan: the wait lost nothing.
	reopened := planReopen(t, path)
	defer reopened.Close()
	description := reopened.Task("a").Description
	if !strings.Contains(description, "held open") || !strings.Contains(description, "waited its turn") {
		t.Fatalf("a write was lost across the wait: %q", description)
	}
}

// TestPlandbCliReadsAnswerBesideAnOpenWrite holds a write transaction open on
// one handle while another reads. The reading verbs — the list Tasks renders,
// the show card, the overview Summary counts — must answer at once, on the
// last committed plan, and never queue behind the open write: the reader runs
// its own DEFERRED snapshot beside the writer under WAL rather than waiting
// for a lock the writer is holding.
func TestPlandbCliReadsAnswerBesideAnOpenWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plandb.db")
	seed := planOpen(t, path)
	planAdd(t, seed, planSpec("a", "A"), planSpec("b", "B"))
	if err := seed.Close(); err != nil {
		t.Fatalf("close the seeded store: %v", err)
	}
	writer := planReopen(t, path)
	defer writer.Close()
	reader := planReopen(t, path)
	defer reader.Close()

	// The writer opens its transaction and holds it: the commit is gated on a
	// channel, so the write lock stays held while the reader reads.
	const heldText = "written while the reader reads"
	held := make(chan struct{})
	release := make(chan struct{})
	writer.now = func() time.Time {
		select {
		case <-held:
		default:
			close(held)
		}
		<-release
		return time.Now().UTC()
	}
	writeDone := make(chan error, 1)
	go func() {
		_, err := writer.Amend("a", heldText)
		writeDone <- err
	}()
	<-held

	// list, show and overview, each rendering the plan as the reader sees it.
	readings := []struct {
		name string
		read func() string
	}{
		{"list", func() string {
			ids := make([]string, 0)
			for _, task := range reader.Tasks() {
				ids = append(ids, task.ID)
			}
			return strings.Join(ids, ",")
		}},
		{"show", func() string { return reader.Task("a").Description }},
		{"overview", func() string { return fmt.Sprintf("%+v", reader.Summary()) }},
	}
	for _, reading := range readings {
		reading := reading
		done := make(chan string, 1)
		go func() { done <- reading.read() }()
		select {
		case got := <-done:
			if strings.Contains(got, heldText) {
				t.Fatalf("%s answered a write that has not committed: %q", reading.name, got)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s blocked behind the open write", reading.name)
		}
	}

	// Once the write commits, the reader answers it — the same handle that
	// answered beside the write now sees it.
	close(release)
	if err := <-writeDone; err != nil {
		t.Fatalf("writer: %v", err)
	}
	if got := reader.Task("a").Description; !strings.Contains(got, heldText) {
		t.Fatalf("the reader never saw the committed write: %q", got)
	}
}

// The number of hands that open the same store at once in the schema-race
// test below.
const plandbConcurrentOpens = 20

// TestPlandbCliConcurrentOpensNeverLeaveAReaderWithoutATable drives one store
// file from many hands at once: one store is held open and read in a tight
// loop — Tasks, RoleOf and ReadySet — while twenty further Open calls on the
// same file come and go from other goroutines, one of them the built CLI
// running `list` as a separate process. Not one read fails, and the schema
// cookie barely moves: the schema is created once and every later open
// rewrites it to itself, so no reader is ever handed a database without the
// table it asked for.
func TestPlandbCliConcurrentOpensNeverLeaveAReaderWithoutATable(t *testing.T) {
	if testing.Short() {
		t.Skip("opens twenty stores and runs the CLI against one file")
	}
	path := filepath.Join(t.TempDir(), "plandb.db")
	seed := planOpen(t, path)
	defer seed.Close()
	planAdd(t, seed, planSpec("kept", "Kept"))

	// The schema cookie the run starts from. An open of a store that is already
	// current commits no change, so no number of opens below may move it; over
	// the whole run it advances at most once, which is the one build of the
	// schema and never a second.
	before := planSchemaVersion(t, seed.db)

	// The reader runs until stop closes, two seconds later: every reading verb
	// over the store that is held open, over and over, while the opens come and
	// go beside it.
	stop := make(chan struct{})
	readErrs := make(chan error, 1)
	var reading sync.WaitGroup
	reading.Add(1)
	go func() {
		defer reading.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if seed.Task("kept") == nil {
				readErrs <- errors.New("a read lost the task the store holds")
				return
			}
			if _, err := seed.RoleOf("kept"); err != nil {
				readErrs <- fmt.Errorf("RoleOf failed: %w", err)
				return
			}
			if ready := seed.ReadySet(); len(ready.Runnable)+len(ready.Blocked) == 0 {
				readErrs <- errors.New("ReadySet answered neither runnable nor blocked")
				return
			}
		}
	}()

	binary := planBuildCLI(t)
	openErrs := make(chan error, plandbConcurrentOpens)
	var opening sync.WaitGroup
	for i := 0; i < plandbConcurrentOpens; i++ {
		opening.Add(1)
		go func(i int) {
			defer opening.Done()
			first := true
			for {
				select {
				case <-stop:
					return
				default:
				}
				if first {
					first = false
					// One hand's first open is the built CLI through its own main,
					// so the cross-process road is exercised and not just the
					// in-process one.
					command := exec.Command(binary, "--db", path, "list")
					if output, err := command.CombinedOutput(); err != nil {
						openErrs <- fmt.Errorf("the CLI's list failed: %v\n%s", err, output)
						return
					}
					continue
				}
				store, err := Open(path, "", "", "", "")
				if err != nil {
					openErrs <- fmt.Errorf("open while reading: %w", err)
					return
				}
				if store.Task("kept") == nil {
					openErrs <- errors.New("an open did not see the plan the store holds")
					_ = store.Close()
					return
				}
				if err := store.Close(); err != nil {
					openErrs <- fmt.Errorf("close: %w", err)
					return
				}
			}
		}(i)
	}

	time.Sleep(2 * time.Second)
	close(stop)
	reading.Wait()
	opening.Wait()

	select {
	case err := <-readErrs:
		t.Fatal(err)
	default:
	}
	for i := 0; i < plandbConcurrentOpens; i++ {
		select {
		case err := <-openErrs:
			t.Fatal(err)
		default:
		}
	}

	if after := planSchemaVersion(t, seed.db); after > before+1 {
		t.Fatalf("the schema cookie advanced %d times across the run, want at most once", after-before)
	}
}

// TestPlandbCliOpenFinishesAHalfBuiltSchema opens the file an interrupted
// first build leaves behind: the meta table was committed and the tables
// beside it were not. The open must finish the schema rather than trust that
// meta's presence means the rest is there — one transaction over the whole
// schema is what makes a half-built file impossible to hand a reader, and this
// is the moment that names it. The earlier step checked only for meta, so a
// file in exactly this state opened into a missing table and refused.
func TestPlandbCliOpenFinishesAHalfBuiltSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plandb.db")
	// A lone meta table, the shape an interrupted build leaves: the first
	// commit landed and the tables it creates beside it did not.
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open the half-built file: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE meta (
		id      INTEGER PRIMARY KEY CHECK (id = 1),
		project TEXT    NOT NULL,
		root_id TEXT    NOT NULL,
		next_id INTEGER NOT NULL,
		version INTEGER NOT NULL
	)`); err != nil {
		t.Fatalf("build the half-built file: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close the half-built file: %v", err)
	}

	// The open finishes the schema and seeds the run: the meta table it found
	// held no row, so this is a new store built the same way a fresh one is.
	store := planOpen(t, path)
	defer store.Close()
	planAdd(t, store, planSpec("kept", "Kept"))
	if store.Task("kept") == nil {
		t.Fatal("the finished store does not hold the task it was given")
	}

	// The store it wrote is whole: a second process opens it cleanly.
	reopened := planReopen(t, path)
	defer reopened.Close()
	if reopened.Task("kept") == nil {
		t.Fatalf("the finished store did not survive a reopen: %#v", reopened.Tasks())
	}
}

// TestPlandbCliOpenOfACurrentStoreWritesNothing proves the schema step is a
// no-op on a store that is already current. PRAGMA data_version answers it
// without watching the file's mtime, which a WAL write does not move: a
// connection's counter changes only when ANOTHER connection commits, so a
// watching handle that reads the same number before and after an Open proves
// that Open committed no change.
func TestPlandbCliOpenOfACurrentStoreWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plandb.db")
	seed := planOpen(t, path)
	planAdd(t, seed, planSpec("kept", "Kept"))
	if err := seed.Close(); err != nil {
		t.Fatalf("close the seeded store: %v", err)
	}

	// The watching handle only reads, so its data_version moves only if some
	// other connection commits a change to the file.
	watcher, err := openReadDatabase(path)
	if err != nil {
		t.Fatalf("open the watching handle: %v", err)
	}
	defer watcher.Close()
	before := planDataVersion(t, watcher)

	reopened := planReopen(t, path)
	if err := reopened.Close(); err != nil {
		t.Fatalf("close the reopened store: %v", err)
	}

	if after := planDataVersion(t, watcher); after != before {
		t.Fatalf("opening a current store moved data_version from %d to %d — it wrote", before, after)
	}
}

// planSchemaVersion reads PRAGMA schema_version through a handle: SQLite
// advances it when the schema changes and leaves it when a statement rewrites
// the schema to itself.
func planSchemaVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var version int
	if err := db.QueryRow("PRAGMA schema_version").Scan(&version); err != nil {
		t.Fatalf("read schema_version: %v", err)
	}
	return version
}

// planDataVersion reads PRAGMA data_version through a handle that does not
// write, so the number moves only when another connection commits a change.
func planDataVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var version int
	if err := db.QueryRow("PRAGMA data_version").Scan(&version); err != nil {
		t.Fatalf("read data_version: %v", err)
	}
	return version
}
