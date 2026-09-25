package session

// THE RUN ROAD'S OWN HALF OF THE SEAT LAW. [NewBeltWorker] builds the worker
// the run engine hosts each claimed store task in (internal/run's BashWorker),
// and unlike the /task road's spawn — which reads the belt switch and sets the
// seat beside it (task_run.go's workerSeat) — it had no posture of its own: a
// run worker's every call went out with no reasoning field at all.
//
// The seat it now takes is [effort.RoleWork], the same one the /task road
// chooses with its belt on. What these tests pin is the RESOLUTION and not a
// field: the constructor sets the role, and [effort.Resolve]'s own order —
// turn beats conversation beats task beats role beats the install's default —
// decides the rung, so nothing here special-cases a scope.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// TestBeltWorkerBuiltByNewBeltWorkerThinksFromTheWorkSeat is the run road's
// seat, read at the constructor: a worker built through [NewBeltWorker] answers
// low when nothing above the role spoke, and answers the rung the work carries
// when the work set one — the task scope, which [effort.Resolve] reads before
// the role and therefore returns ahead of the floor.
func TestBeltWorkerBuiltByNewBeltWorkerThinksFromTheWorkSeat(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	// The plandb override wins unprobed (plandb_plan.go's resolvePlanCLI), so
	// the constructor's shim arms against any path and no CLI is run.
	t.Setenv(planCLIBinEnv, filepath.Join(t.TempDir(), "stub-codeaf"))

	cases := []struct {
		name string
		rung effort.Rung
		want string
	}{
		{"nothing above the seat", effort.None, "low"},
		{"a rung on the work", effort.High, "high"},
		{"the seat is a floor, not a cap", effort.Max, "max"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agent := newRunBeltWorker(t, tc.rung)
			if got := agent.ResolvedEffort(); got != tc.want {
				t.Fatalf("the run worker resolves to %q, want %q", got, tc.want)
			}
		})
	}
}

// newRunBeltWorker builds one run worker through [NewBeltWorker] against a real
// store at a real path, the way internal/run's BashWorker does — the root task
// of a fresh store, the seat's model, and the rung handed in as the work's own
// when the case set one. A scripted completer stands in for the seat's
// provider, because the resolution under test is read off the agent and no call
// is made.
func newRunBeltWorker(t *testing.T, taskRung effort.Rung) *Agent {
	t.Helper()
	dir := t.TempDir()
	store, err := plandb.Open(filepath.Join(dir, planStoreFilename), "the work", planRootID, "", "")
	if err != nil {
		t.Fatalf("open the plan store: %v", err)
	}
	task := store.Task(planRootID)
	if task == nil {
		t.Fatal("the fresh store carries no root task")
	}
	config := Config{Workspace: t.TempDir(), Model: "test/model"}
	if taskRung.Valid() {
		config.Effort = taskRung
	}
	agent, err := NewBeltWorker(config, &scriptedCompleter{}, task, store.Path(), store.RootID())
	if err != nil {
		t.Fatalf("NewBeltWorker: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	return agent
}

// ASK LAW. [BeltWorkerBrief] composes every run worker's opening document, and
// A LEAF OWNS A PART OF THE ASK, AND READS THE ASK: a leaf's document carries
// the run root's description verbatim under one heading after its own work
// order, the root's own document carries nothing extra (its work order IS the
// ask), an ask over the cap is cut with the marker, and a store with no root
// row to read renders no section at all.

// askStore opens a real store whose root carries the person's ask and adds one
// leaf under it, so the brief's new section can be read off a leaf and its
// absence off the root.
func askStore(t *testing.T, rootDescription string) *plandb.Store {
	t.Helper()
	store, err := plandb.Open(filepath.Join(t.TempDir(), planStoreFilename), "the run", planRootID, "The run", rootDescription)
	if err != nil {
		t.Fatalf("open the plan store: %v", err)
	}
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "leaf", Title: "the leaf", Description: "the leaf's own work order", ParentID: planRootID}}); err != nil {
		t.Fatalf("add the leaf: %v", err)
	}
	return store
}

// TestBeltWorkerBriefCarriesTheRunsAskToALeaf is the law's positive half: a
// leaf's document holds the root's description VERBATIM — the exact bytes,
// under the heading and its one rule — so the leaf that owns a part reads the
// whole ask, omissions and all.
func TestBeltWorkerBriefCarriesTheRunsAskToALeaf(t *testing.T) {
	ask := "Scoped containers can be initialized independently; the parent container's singletons are not reinitialized."
	store := askStore(t, ask)
	doc := BeltWorkerBrief(store, store.Task("leaf"), false, false, "")
	want := askSectionHeading + "\n" + askSectionRule + "\n\n" + ask
	if !strings.Contains(doc, want) {
		t.Fatalf("a leaf's document does not carry the run's ask verbatim under %q:\n%s", askSectionHeading, doc)
	}
}

// TestBeltWorkerBriefLeavesTheRootsOwnDocumentAlone is the emptiness law: the
// root's work order IS the ask, so its document gains no section and prints
// its own words exactly once — under THE WORK it was composed into.
func TestBeltWorkerBriefLeavesTheRootsOwnDocumentAlone(t *testing.T) {
	ask := "add a rate limiter to the upload route"
	store := askStore(t, ask)
	doc := BeltWorkerBrief(store, store.Task(planRootID), true, false, "")
	if strings.Contains(doc, askSectionHeading) {
		t.Fatalf("the root's document grew the ask section:\n%s", doc)
	}
	if n := strings.Count(doc, ask); n != 1 {
		t.Fatalf("the root's own words appear %d times in its document, want once as its work order:\n%s", n, doc)
	}
}

// TestBeltWorkerBriefBoundsTheAskWithAMarkedCut: the ask is verbatim only up to
// the cap, and what the cap takes is marked — a leaf handed a truncated ask can
// see that it was.
func TestBeltWorkerBriefBoundsTheAskWithAMarkedCut(t *testing.T) {
	store := askStore(t, strings.Repeat("z", askSectionLimit+4096))
	doc := BeltWorkerBrief(store, store.Task("leaf"), false, false, "")
	if !strings.Contains(doc, askSectionHeading) {
		t.Fatalf("the leaf's document dropped the ask section entirely:\n%s", doc)
	}
	if !strings.Contains(doc, strings.Repeat("z", askSectionLimit-len("…"))+"…") {
		t.Fatalf("the ask over the %d-byte cap was not cut with the marker:\n%s", askSectionLimit, doc)
	}
	if strings.Contains(doc, strings.Repeat("z", askSectionLimit+1)) {
		t.Fatalf("the ask was not bounded at the %d-byte cap", askSectionLimit)
	}
}

// TestBeltWorkerBriefRendersNoAskSectionWithoutARoot is the defensive half:
// with no root row to read — a nil handle, or a root carrying no description —
// the section is absent rather than empty, never a heading over nothing.
func TestBeltWorkerBriefRendersNoAskSectionWithoutARoot(t *testing.T) {
	leaf := &plandb.Task{TaskSpec: plandb.TaskSpec{ID: "leaf", Description: "the leaf's own work order"}}
	if doc := BeltWorkerBrief(nil, leaf, false, false, ""); strings.Contains(doc, askSectionHeading) {
		t.Fatalf("a nil store grew an ask section:\n%s", doc)
	}
	store := askStore(t, "")
	if doc := BeltWorkerBrief(store, store.Task("leaf"), false, false, ""); strings.Contains(doc, askSectionHeading) {
		t.Fatalf("a root with no description grew an ask section:\n%s", doc)
	}
}

// TestCheckSectionDirectsDeclaredChecks pins the checker contract to the
// store-backed declaration the review description renders under Checks:.
func TestCheckSectionDirectsDeclaredChecks(t *testing.T) {
	if !strings.Contains(checkSection, "Checks:") {
		t.Fatalf("the check section does not direct the worker to the declared Checks: block:\n%s", checkSection)
	}
	if !strings.Contains(checkSection, "exactly as spelled") {
		t.Fatalf("the check section does not preserve declared commands verbatim:\n%s", checkSection)
	}
	if !strings.Contains(checkSection, "check: <one sentence>") {
		t.Fatalf("the check section has no ending when a declared command cannot run:\n%s", checkSection)
	}
}

// TestBashWorkerPageRequiresChecksForDelegatedTasks pins the task author's
// half of the Checks: contract before the check seat begins.
func TestBashWorkerPageRequiresChecksForDelegatedTasks(t *testing.T) {
	doc := bashWorkerPage()
	if !strings.Contains(doc, "Every delegated task has at least one `--check`.") {
		t.Fatalf("the worker page permits a delegated task without Checks:\n%s", doc)
	}
}
