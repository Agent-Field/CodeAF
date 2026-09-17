package plandb

// Tests for the adapted store, brought over from the v1 port's store_test.go
// and retargeted at validateSpec as it now stands
// (docs/design/plandb-cli/DESIGN.md). The v1 file doubled as a governance
// gate's witness: its specs carried roles, deliverables, acceptance and
// resource claims, and its Claim test asserted the resource-conflict refusal.
// The adaptation took those gates off, so the tests asserting them are gone
// with the behaviour; the graph laws the port kept — cycle detection over
// both graphs, the parent-chain readiness rule, descendant and dependent
// cancellation, promotion, composite auto-completion, claim ownership — are
// asserted here against the store as it stands, alongside the behaviour the
// adaptation added: bare adds, notes, fuzzy Resolve, search, critical path,
// bottlenecks, ClaimNext, and the cross-process flock.
//
// Every test carries the wave's TestPlandbCli prefix; every helper is
// plan-prefixed so the cli worker's helpers in the same package never
// collide with these.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// planOpen opens the run's store at path, substituting a scratch file when
// the caller passes an empty one. Every test gets a real path on purpose:
// an in-memory store makes changeTask create a stray .lock sidecar in the
// working directory, and these tests are about the store's laws, not that
// edge case.
func planOpen(t *testing.T, path string) *Store {
	t.Helper()
	if path == "" {
		path = filepath.Join(t.TempDir(), "plan.json")
	}
	store, err := Open(path, "plan-test", "root", "The run", "drive the plan to the ground")
	if err != nil {
		t.Fatalf("open plan store: %v", err)
	}
	return store
}

// planReopen loads an existing store the way a second process picks the file
// up: no project or root named, so whatever the file holds is accepted.
func planReopen(t *testing.T, path string) *Store {
	t.Helper()
	store, err := Open(path, "", "", "", "")
	if err != nil {
		t.Fatalf("reopen plan store: %v", err)
	}
	return store
}

// planSpec is the bare add the doctrine teaches: an id, a title, a work
// order, and nothing the old governance gates used to demand.
func planSpec(id, title string) TaskSpec {
	return TaskSpec{ID: id, Title: title, Description: "do " + title}
}

func planAdd(t *testing.T, store *Store, specs ...TaskSpec) {
	t.Helper()
	if _, err := store.AddMany(specs); err != nil {
		t.Fatalf("add tasks: %v", err)
	}
}

// planFinish claims and completes a leaf the way a worker's taught finish
// does, in one step, because most tests only care that the work landed.
func planFinish(t *testing.T, store *Store, id, agent, result string) {
	t.Helper()
	if _, err := store.Claim(id, agent); err != nil {
		t.Fatalf("claim %s: %v", id, err)
	}
	if _, err := store.Done(id, agent, result, nil, nil); err != nil {
		t.Fatalf("done %s: %v", id, err)
	}
}

func planIDs(tasks []*Task) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
}

func TestPlandbCliOpenCreatesLoadsAndRefusesForeignRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	store := planOpen(t, path)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("open did not create the store file: %v", err)
	}
	if store.Project() != "plan-test" || store.RootID() != "root" {
		t.Fatalf("project/root = %q/%q", store.Project(), store.RootID())
	}
	root := store.Task("root")
	if root == nil || root.Status != StatusRunning || root.ClaimedBy != "runtime" {
		t.Fatalf("root task = %#v", root)
	}
	if root.Parallel != "safe" {
		t.Fatalf("root parallel policy = %q, want the safe default", root.Parallel)
	}
	if store.Task("ghost") != nil {
		t.Fatal("Task answered a task that does not exist")
	}

	reopened := planReopen(t, path)
	if reopened.Project() != "plan-test" || reopened.RootID() != "root" {
		t.Fatalf("reopened project/root = %q/%q", reopened.Project(), reopened.RootID())
	}
	if _, err := reopened.Show("root"); err != nil {
		t.Fatalf("reopened store lost the root: %v", err)
	}

	// A file that belongs to another run is a refusal, not a merge: two
	// sessions sharing one store by accident would each dispatch the other's
	// children.
	if _, err := Open(path, "another-project", "", "", ""); err == nil || !strings.Contains(err.Error(), "different run") {
		t.Fatalf("foreign project accepted: %v", err)
	}
	if _, err := Open(path, "", "another-root", "", ""); err == nil || !strings.Contains(err.Error(), "different run") {
		t.Fatalf("foreign root accepted: %v", err)
	}
	if _, err := Open(filepath.Join(t.TempDir(), "fresh.json"), "", "", "", ""); err == nil {
		t.Fatal("a new store without a project was accepted")
	}
	if _, err := Open(filepath.Join(t.TempDir(), "fresh.json"), "plan-test", "not an id", "", ""); err == nil {
		t.Fatal("a new store with an invalid root id was accepted")
	}
}

// The gates the adaptation removed are asserted by their absence: a bare add
// succeeds, and the task it creates is claimable without any of the old
// eligibility work.
func TestPlandbCliBareAddSucceedsUnderTheSafeDefault(t *testing.T) {
	store := planOpen(t, filepath.Join(t.TempDir(), "plan.json"))
	created, err := store.AddMany([]TaskSpec{{ID: "bare", Title: "Bare"}})
	if err != nil {
		t.Fatalf("a bare add must succeed now that the governance gates are off: %v", err)
	}
	if len(created) != 1 || created[0].ID != "bare" {
		t.Fatalf("created = %#v", created)
	}
	task := store.Task("bare")
	if task.Status != StatusReady {
		t.Fatalf("bare task status = %s, want ready", task.Status)
	}
	if task.Kind != "generic" || task.Effect != EffectObserve || task.Parallel != "safe" || task.Isolation != "shared" {
		t.Fatalf("bare task defaults = %#v", task.TaskSpec)
	}
	if _, err := store.Claim("bare", "worker"); err != nil {
		t.Fatalf("claim bare task: %v", err)
	}
}

func TestPlandbCliAddManyValidatesTheWholeBatch(t *testing.T) {
	store := planOpen(t, "")

	third := planSpec("third", "Third")
	third.Dependencies = []Dependency{{TaskID: "missing"}}
	if _, err := store.AddMany([]TaskSpec{planSpec("ok1", "Ok1"), planSpec("ok2", "Ok2"), third}); err == nil || !strings.Contains(err.Error(), "unknown dependency") {
		t.Fatalf("batch with an unknown dependency accepted: %v", err)
	}
	if store.Summary().Total != 1 {
		t.Fatalf("a refused batch left tasks behind: %#v", store.Summary())
	}

	nameless := planSpec("nameless", " ")
	if _, err := store.AddMany([]TaskSpec{planSpec("ok1", "Ok1"), planSpec("ok2", "Ok2"), nameless}); err == nil || !strings.Contains(err.Error(), "title is required") {
		t.Fatalf("batch with a titleless part accepted: %v", err)
	}
	if store.Summary().Total != 1 {
		t.Fatalf("a refused batch left tasks behind: %#v", store.Summary())
	}

	if _, err := store.AddMany([]TaskSpec{{ID: "twin", Title: "Twin"}, {ID: "twin", Title: "Twin"}}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate ids accepted: %v", err)
	}
	if _, err := store.AddMany([]TaskSpec{{ID: "orphan", Title: "Orphan", ParentID: "ghost"}}); err == nil || !strings.Contains(err.Error(), "unknown parent") {
		t.Fatalf("unknown parent accepted: %v", err)
	}

	// A terminal parent cannot grow children: the composite's bookkeeping is
	// over once it landed.
	planAdd(t, store, planSpec("late", "Late"))
	planFinish(t, store, "late", "worker", "delivered")
	if _, err := store.AddMany([]TaskSpec{{ID: "kid", Title: "Kid", ParentID: "late"}}); err == nil || !strings.Contains(err.Error(), "terminal parent") {
		t.Fatalf("child of a terminal parent accepted: %v", err)
	}
}

func TestPlandbCliTrimsTPrefixesOnIdsParentsAndDeps(t *testing.T) {
	store := planOpen(t, "")
	planAdd(t, store, planSpec("first", "First"))
	created, err := store.AddMany([]TaskSpec{{
		ID:           "t-second",
		Title:        "Second",
		ParentID:     "t-root",
		Dependencies: []Dependency{{TaskID: "t-first"}},
	}})
	if err != nil {
		t.Fatalf("t-prefixed add refused: %v", err)
	}
	if len(created) != 1 || created[0].ID != "second" {
		t.Fatalf("created = %#v, want the bare id", created)
	}
	if store.Task("t-second") != nil {
		t.Fatal("the store kept the t- spelling; the bare id is the only one it keeps")
	}
	task := store.Task("second")
	if task == nil || task.ParentID != "root" || len(task.Dependencies) != 1 || task.Dependencies[0].TaskID != "first" {
		t.Fatalf("trimmed task = %#v", task)
	}
	// Duplicate detection sees through the prefix too: t-second and second
	// are one id.
	if _, err := store.AddMany([]TaskSpec{{ID: "t-second", Title: "Again"}}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("t-prefixed duplicate accepted: %v", err)
	}
}

func TestPlandbCliReadyLadderWalksTheParentChain(t *testing.T) {
	store := planOpen(t, "")
	gate := planSpec("gate", "Gate")
	parent := planSpec("parent", "Parent")
	parent.Dependencies = []Dependency{{TaskID: "gate"}}
	child := planSpec("child", "Child")
	child.ParentID = "parent"
	planAdd(t, store, gate, parent, child)

	// The child's own dependency list is empty, but its parent waits on the
	// gate, and readiness walks the parent chain — a child cannot run out
	// from under an unfinished parent's coordination.
	ready := store.ReadyLeaves()
	if len(ready) != 1 || ready[0].ID != "gate" {
		t.Fatalf("initial ready set = %v, want [gate]", planIDs(ready))
	}
	if store.Task("parent").Status != StatusPending {
		t.Fatalf("parent status = %s, want pending", store.Task("parent").Status)
	}

	planFinish(t, store, "gate", "w-gate", "gate cleared")
	ready = store.ReadyLeaves()
	if len(ready) != 1 || ready[0].ID != "child" {
		t.Fatalf("ready set after the gate cleared = %v, want [child]", planIDs(ready))
	}
	if got := store.Task("parent"); got.Status != StatusReady {
		t.Fatalf("parent status = %s, want ready", got.Status)
	}
	// The parent is ready but composite, so it is never handed out.
	if _, err := store.Claim("parent", "w"); err == nil {
		t.Fatal("a composite parent was claimed")
	}
	if _, err := store.Claim("child", "w-child"); err != nil {
		t.Fatalf("claim child: %v", err)
	}
}

func TestPlandbCliLineageRuleGatesHardEdgesOnly(t *testing.T) {
	store := planOpen(t, "")
	parent := planSpec("p", "P")
	child := planSpec("c", "C")
	child.ParentID = "p"
	child.Dependencies = []Dependency{{TaskID: "p"}}
	if _, err := store.AddMany([]TaskSpec{parent, child}); err == nil || !strings.Contains(err.Error(), "lineage") {
		t.Fatalf("child hard dependency on its parent accepted: %v", err)
	}

	store = planOpen(t, "")
	parent = planSpec("p", "P")
	parent.Dependencies = []Dependency{{TaskID: "c"}}
	child = planSpec("c", "C")
	child.ParentID = "p"
	if _, err := store.AddMany([]TaskSpec{parent, child}); err == nil || !strings.Contains(err.Error(), "lineage") {
		t.Fatalf("parent hard dependency on its child accepted: %v", err)
	}

	// A suggests edge crosses the lineage freely, and it does not gate
	// readiness — the doctrine's reading set never blocks on one.
	store = planOpen(t, "")
	parent = planSpec("p", "P")
	child = planSpec("c", "C")
	child.ParentID = "p"
	child.Dependencies = []Dependency{{TaskID: "p", Kind: DepSuggests}}
	planAdd(t, store, parent, child)
	if got := store.Task("c"); got.Status != StatusReady {
		t.Fatalf("child with a suggests edge on its parent: status = %s, want ready", got.Status)
	}
}

func TestPlandbCliCyclesAreRefusedOnBothGraphs(t *testing.T) {
	store := planOpen(t, "")
	a := planSpec("a", "A")
	a.Dependencies = []Dependency{{TaskID: "b"}}
	b := planSpec("b", "B")
	b.Dependencies = []Dependency{{TaskID: "a"}}
	if _, err := store.AddMany([]TaskSpec{a, b}); err == nil || !strings.Contains(err.Error(), "dependency graph") {
		t.Fatalf("dependency cycle accepted: %v", err)
	}
	if store.Summary().Total != 1 {
		t.Fatalf("a refused batch left tasks behind: %#v", store.Summary())
	}

	other := planOpen(t, "")
	p := planSpec("p", "P")
	p.ParentID = "c"
	c := planSpec("c", "C")
	c.ParentID = "p"
	if _, err := other.AddMany([]TaskSpec{p, c}); err == nil || !strings.Contains(err.Error(), "containment graph") {
		t.Fatalf("containment cycle accepted: %v", err)
	}
	if other.Summary().Total != 1 {
		t.Fatalf("a refused batch left tasks behind: %#v", other.Summary())
	}
}

func TestPlandbCliClaimTakesReadyLeavesOnly(t *testing.T) {
	store := planOpen(t, "")
	planAdd(t, store, planSpec("leaf", "Leaf"))
	composite := planSpec("comp", "Comp")
	kid := planSpec("kid", "Kid")
	kid.ParentID = "comp"
	planAdd(t, store, composite, kid)
	waiter := planSpec("waiter", "Waiter")
	waiter.Dependencies = []Dependency{{TaskID: "leaf"}}
	planAdd(t, store, waiter)

	// waiter: pending behind leaf. comp: composite, so never runnable even
	// though its status says ready.
	ready := store.ReadyLeaves()
	if len(ready) != 2 || ready[0].ID != "leaf" || ready[1].ID != "kid" {
		t.Fatalf("ready set = %v, want [leaf kid]", planIDs(ready))
	}
	if _, err := store.Claim("waiter", "w"); err == nil {
		t.Fatal("a pending task was claimed")
	}
	if _, err := store.Claim("comp", "w"); err == nil {
		t.Fatal("a composite task was claimed")
	}
	if _, err := store.Claim("ghost", "w"); err == nil {
		t.Fatal("a task that does not exist was claimed")
	}
	if _, err := store.Claim("kid", "   "); err == nil {
		t.Fatal("a blank agent name was accepted")
	}
	if _, err := store.Claim("kid", "w-kid"); err != nil {
		t.Fatalf("claim ready leaf: %v", err)
	}
	if _, err := store.Claim("kid", "w-other"); err == nil {
		t.Fatal("a running task was claimed twice")
	}
}

func TestPlandbCliOwnershipGatesDoneFailReleaseAndRetry(t *testing.T) {
	store := planOpen(t, filepath.Join(t.TempDir(), "plan.json"))
	planAdd(t, store, planSpec("job", "Job"))

	if _, err := store.Claim("job", "worker-a"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := store.Done("job", "worker-b", "not mine", nil, nil); err == nil || !strings.Contains(err.Error(), `owned by "worker-a"`) {
		t.Fatalf("wrong owner completed a task: %v", err)
	}
	if _, err := store.Fail("job", "worker-b", "sabotage"); err == nil {
		t.Fatal("wrong owner failed a task")
	}
	if _, err := store.Release("job", "worker-b"); err == nil {
		t.Fatal("wrong owner released a task")
	}

	// Release puts the work back, and promotion makes it ready again at once.
	if _, err := store.Release("job", "worker-a"); err != nil {
		t.Fatalf("release by the owner: %v", err)
	}
	if got := store.Task("job"); got.Status != StatusReady {
		t.Fatalf("released task status = %s, want ready", got.Status)
	}
	if _, err := store.Claim("job", "worker-a"); err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if _, err := store.Fail("job", "worker-a", "boom"); err != nil {
		t.Fatalf("fail by the owner: %v", err)
	}
	if got := store.Task("job"); got.Status != StatusFailed || got.Error != "boom" {
		t.Fatalf("failed task = %#v", got)
	}
	if _, err := store.Retry("job"); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := store.Task("job"); got.Status != StatusReady {
		t.Fatalf("retried task status = %s, want ready", got.Status)
	}
	if _, err := store.Retry("job"); err == nil {
		t.Fatal("retried a task that is not failed")
	}

	if _, err := store.Claim("job", "worker-a"); err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	done, err := store.Done("job", "worker-a", "green", []string{"artifact"}, []string{"receipt"})
	if err != nil {
		t.Fatalf("done by the owner: %v", err)
	}
	if done.Status != StatusDone || done.Result != "green" || len(done.Artifacts) != 1 {
		t.Fatalf("completed task = %#v", done)
	}
	if _, err := store.Done("job", "worker-a", "again", nil, nil); err == nil {
		t.Fatal("a done task completed twice")
	}

	// The root is the run's, and no worker verb may finish it.
	rootID := store.RootID()
	if _, err := store.Done(rootID, "runtime", "res", nil, nil); err == nil {
		t.Fatal("the root was completed by a worker verb")
	}
	if _, err := store.Fail(rootID, "runtime", "reason"); err == nil {
		t.Fatal("the root was failed by a worker verb")
	}
	if _, err := store.Release(rootID, "runtime"); err == nil {
		t.Fatal("the root was released by a worker verb")
	}
	if _, err := store.Retry(rootID); err == nil {
		t.Fatal("the root was retried by a worker verb")
	}
	if _, err := store.Cancel(rootID, "reason"); err == nil {
		t.Fatal("the root was cancelled by a worker verb")
	}
}

func TestPlandbCliDoneDemandsEvidenceOnlyWhenTheTaskAsks(t *testing.T) {
	store := planOpen(t, "")
	tight := planSpec("tight", "Tight")
	tight.EvidenceRequirements = []string{"a receipt"}
	planAdd(t, store, tight)
	if _, err := store.Claim("tight", "worker"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := store.Done("tight", "worker", "res", nil, nil); err == nil || !strings.Contains(err.Error(), "evidence") {
		t.Fatalf("completion without the demanded evidence accepted: %v", err)
	}
	if _, err := store.Done("tight", "worker", "res", nil, []string{"receipt"}); err != nil {
		t.Fatalf("completion with evidence: %v", err)
	}
}

func TestPlandbCliCancelCascadesDescendantsAndDependents(t *testing.T) {
	store := planOpen(t, "")
	parent := planSpec("p", "P")
	c1 := planSpec("c1", "C1")
	c1.ParentID = "p"
	c2 := planSpec("c2", "C2")
	c2.ParentID = "p"
	dependent := planSpec("d", "D")
	dependent.Dependencies = []Dependency{{TaskID: "p"}}
	suggested := planSpec("e", "E")
	suggested.Dependencies = []Dependency{{TaskID: "p", Kind: DepSuggests}}
	transitive := planSpec("f", "F")
	transitive.Dependencies = []Dependency{{TaskID: "d"}}
	planAdd(t, store, parent, c1, c2, dependent, suggested, transitive)

	// Terminal work is terminal: the cascade skips it.
	planFinish(t, store, "c2", "w", "c2 delivered")

	if _, err := store.Cancel("p", "plan changed"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if got := store.Task("p"); got.Status != StatusCancelled || got.ClaimedBy != "" {
		t.Fatalf("cancelled parent = %#v", got)
	}
	if got := store.Task("c1"); got.Status != StatusCancelled {
		t.Fatalf("descendant status = %s, want cancelled", got.Status)
	}
	if got := store.Task("c2"); got.Status != StatusDone {
		t.Fatalf("delivered child status = %s, want done; the cascade must skip terminal work", got.Status)
	}
	if got := store.Task("d"); got.Status != StatusCancelled {
		t.Fatalf("hard dependent status = %s, want cancelled", got.Status)
	}
	if got := store.Task("f"); got.Status != StatusCancelled {
		t.Fatalf("transitive dependent status = %s, want cancelled", got.Status)
	}
	if got := store.Task("e"); got.Status != StatusReady {
		t.Fatalf("suggests-only observer status = %s, want ready; a suggests edge neither gates nor cancels", got.Status)
	}

	if _, err := store.Cancel("p", "again"); err == nil || !strings.Contains(err.Error(), "already terminal") {
		t.Fatalf("cancel of a terminal task accepted: %v", err)
	}
}

func TestPlandbCliAmendPrependsToTheDescription(t *testing.T) {
	store := planOpen(t, "")
	spec := planSpec("job", "Job")
	spec.Description = "first line\nsecond line"
	planAdd(t, store, spec)
	if _, err := store.Amend("job", "NOTE: use jwt"); err != nil {
		t.Fatalf("amend: %v", err)
	}
	if got := store.Task("job"); got.Description != "NOTE: use jwt\n\nfirst line\nsecond line" {
		t.Fatalf("amended description = %q", got.Description)
	}
	if _, err := store.Amend("ghost", "x"); err == nil {
		t.Fatal("amended a task that does not exist")
	}
}

func TestPlandbCliAddDepEnforcesTheGraphLaws(t *testing.T) {
	store := planOpen(t, "")
	planAdd(t, store, planSpec("up", "Up"), planSpec("down", "Down"), planSpec("side", "Side"))

	if _, err := store.AddDep("down", "up", ""); err != nil {
		t.Fatalf("add dep: %v", err)
	}
	down := store.Task("down")
	if len(down.Dependencies) != 1 || down.Dependencies[0].TaskID != "up" || down.Dependencies[0].Kind != DepFeedsInto {
		t.Fatalf("added dependency = %#v, want up/feeds_into", down.Dependencies)
	}

	if _, err := store.AddDep("down", "up", DepBlocks); err == nil || !strings.Contains(err.Error(), "already depends") {
		t.Fatalf("duplicate edge accepted: %v", err)
	}
	if _, err := store.AddDep("down", "ghost", ""); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("edge to an unknown upstream accepted: %v", err)
	}
	if _, err := store.AddDep("ghost", "up", ""); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("edge from an unknown downstream accepted: %v", err)
	}
	// down→up exists; up→down closes the loop, and the whole graph is asked
	// before the edge lands.
	if _, err := store.AddDep("up", "down", ""); err == nil || !strings.Contains(err.Error(), "dependency graph") {
		t.Fatalf("cycling edge accepted: %v", err)
	}

	// The lineage law is asked of the new edge too: a hard edge between an
	// ancestor and its descendant refuses, a suggests edge crosses.
	planAdd(t, store, planSpec("p", "P"))
	kid := planSpec("k", "K")
	kid.ParentID = "p"
	planAdd(t, store, kid)
	if _, err := store.AddDep("p", "k", ""); err == nil || !strings.Contains(err.Error(), "lineage") {
		t.Fatalf("parent hard dependency on its child accepted: %v", err)
	}
	if _, err := store.AddDep("k", "p", ""); err == nil || !strings.Contains(err.Error(), "lineage") {
		t.Fatalf("child hard dependency on its parent accepted: %v", err)
	}
	if _, err := store.AddDep("k", "p", DepSuggests); err != nil {
		t.Fatalf("suggests edge across the lineage refused: %v", err)
	}
}

func TestPlandbCliNotesAreTaskScoped(t *testing.T) {
	store := planOpen(t, "")
	planAdd(t, store, planSpec("job", "Job"))

	first, err := store.AddNote("job", "worker-a", "handing off: the schema is set")
	if err != nil {
		t.Fatalf("add note: %v", err)
	}
	if first.ID == "" || first.TaskID != "job" || first.Agent != "worker-a" {
		t.Fatalf("note = %#v", first)
	}
	if _, err := store.AddNote("job", "worker-b", "picked it up"); err != nil {
		t.Fatalf("add note: %v", err)
	}
	if _, err := store.AddNote("job", "worker-a", "done"); err != nil {
		t.Fatalf("add note: %v", err)
	}

	notes := store.Notes("job", 0)
	if len(notes) != 3 || notes[0].Body != "handing off: the schema is set" {
		t.Fatalf("notes = %#v, want all three oldest first", notes)
	}
	bounded := store.Notes("job", 2)
	if len(bounded) != 2 || bounded[0].ID != notes[0].ID {
		t.Fatalf("bounded notes = %#v, want the first two", bounded)
	}
	// A note about one task is not a note about another.
	planAdd(t, store, planSpec("other", "Other"))
	if _, err := store.AddNote("other", "worker-a", "somewhere else"); err != nil {
		t.Fatalf("add note: %v", err)
	}
	if got := store.Notes("other", 0); len(got) != 1 || got[0].Body != "somewhere else" {
		t.Fatalf("another task's notes = %#v, want only its own", got)
	}
	if _, err := store.AddNote("ghost", "worker-a", "nowhere"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("note on an unknown task accepted: %v", err)
	}
	if _, err := store.AddNote("job", "worker-a", "   "); err == nil {
		t.Fatal("an empty note was accepted")
	}
}

func TestPlandbCliContextEntriesFilterAndPrune(t *testing.T) {
	store := planOpen(t, "")
	planAdd(t, store, planSpec("job", "Job"))

	decision, err := store.AddContext("", "decision", "chose flock for the store")
	if err != nil {
		t.Fatalf("add context: %v", err)
	}
	discovery, err := store.AddContext("", "", "found the flake")
	if err != nil {
		t.Fatalf("add context: %v", err)
	}
	if discovery.Kind != "discovery" {
		t.Fatalf("default kind = %q, want discovery", discovery.Kind)
	}
	scoped, err := store.AddContext("job", "discovery", "scoped to the job")
	if err != nil {
		t.Fatalf("add context: %v", err)
	}

	all := store.Contexts("", "", 0)
	if len(all) != 3 || all[0].ID != scoped.ID {
		t.Fatalf("contexts = %#v, want newest first", all)
	}
	if byKind := store.Contexts("", "decision", 0); len(byKind) != 1 || byKind[0].ID != decision.ID {
		t.Fatalf("contexts by kind = %#v", byKind)
	}
	if byTask := store.Contexts("job", "", 0); len(byTask) != 1 || byTask[0].ID != scoped.ID {
		t.Fatalf("contexts by task = %#v", byTask)
	}

	if _, err := store.AddContext("ghost", "", "x"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("context naming an unknown task accepted: %v", err)
	}
	if _, err := store.AddContext("", "", "   "); err == nil {
		t.Fatal("empty context content accepted")
	}

	if err := store.Prune(discovery.ID); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if left := store.Contexts("", "", 0); len(left) != 2 {
		t.Fatalf("contexts after prune = %#v, want two", left)
	}
	if err := store.Prune(discovery.ID); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("pruning a pruned entry accepted: %v", err)
	}
}

func TestPlandbCliResolveFuzzyIds(t *testing.T) {
	store := planOpen(t, "")
	planAdd(t, store, planSpec("alpha", "Alpha"), planSpec("alphabet", "Alphabet"), planSpec("beta", "Beta"))

	if got, err := store.Resolve("alpha"); err != nil || got.ID != "alpha" {
		t.Fatalf("exact resolve = %#v, %v", got, err)
	}
	if got, err := store.Resolve("t-alpha"); err != nil || got.ID != "alpha" {
		t.Fatalf("t-prefixed resolve = %#v, %v", got, err)
	}
	if got, err := store.Resolve("bet"); err != nil || got.ID != "beta" {
		t.Fatalf("unique prefix resolve = %#v, %v", got, err)
	}
	// A prefix fitting two tasks is a question the caller must not guess at.
	if _, err := store.Resolve("al"); err == nil || !strings.Contains(err.Error(), "matches several") || !strings.Contains(err.Error(), "t-alpha") || !strings.Contains(err.Error(), "t-alphabet") {
		t.Fatalf("ambiguous prefix resolved anyway: %v", err)
	}
	if _, err := store.Resolve("zzz"); err == nil || !strings.Contains(err.Error(), "no task matches") {
		t.Fatalf("unmatched word resolved anyway: %v", err)
	}
	if _, err := store.Resolve("t-zzz"); err == nil {
		t.Fatal("t-prefixed unmatched word resolved anyway")
	}
}

func TestPlandbCliClaimNextTakesTheBestReadyLeaf(t *testing.T) {
	store := planOpen(t, "")
	low := planSpec("low", "Low")
	low.Priority = 1
	mid := planSpec("mid", "Mid")
	mid.Priority = 3
	top := planSpec("top", "Top")
	top.Priority = 5
	planAdd(t, store, low, mid, top)

	// The read and the write happen under the same hold of both locks, so
	// two agents asking at once cannot be handed the same task.
	first, err := store.ClaimNext("agent-1")
	if err != nil {
		t.Fatalf("claim next: %v", err)
	}
	if first == nil || first.ID != "top" || first.Status != StatusRunning || first.ClaimedBy != "agent-1" {
		t.Fatalf("first claim = %#v, want top running for agent-1", first)
	}
	second, err := store.ClaimNext("agent-2")
	if err != nil || second == nil || second.ID != "mid" {
		t.Fatalf("second claim = %#v, %v, want mid", second, err)
	}
	third, err := store.ClaimNext("agent-3")
	if err != nil || third == nil || third.ID != "low" {
		t.Fatalf("third claim = %#v, %v, want low", third, err)
	}
	fourth, err := store.ClaimNext("agent-4")
	if err != nil || fourth != nil {
		t.Fatalf("empty answer = %#v, %v, want nil, nil", fourth, err)
	}
	if _, err := store.ClaimNext("   "); err == nil {
		t.Fatal("a blank agent name was accepted")
	}

	// A plan whose remaining work is pending behind a claimed upstream
	// answers empty rather than handing out pending work.
	behind := planOpen(t, "")
	a := planSpec("a", "A")
	b := planSpec("b", "B")
	b.Dependencies = []Dependency{{TaskID: "a"}}
	planAdd(t, behind, a, b)
	source, err := behind.ClaimNext("agent-4")
	if err != nil || source == nil || source.ID != "a" {
		t.Fatalf("claim from a fresh plan = %#v, %v, want a", source, err)
	}
	empty, err := behind.ClaimNext("agent-5")
	if err != nil || empty != nil {
		t.Fatalf("claim with only pending work left = %#v, %v, want nil, nil", empty, err)
	}
}

func TestPlandbCliSearchRanksMatches(t *testing.T) {
	store := planOpen(t, "")
	design := planSpec("design", "Design schema")
	design.Description = "design the tables for the run"
	prose := planSpec("prose", "Write prose")
	prose.Description = "schema notes and more schema words"
	planAdd(t, store, design, prose)
	if _, err := store.AddNote("design", "worker", "schema settled"); err != nil {
		t.Fatalf("add note: %v", err)
	}
	if _, err := store.AddContext("", "decision", "the schema is set"); err != nil {
		t.Fatalf("add context: %v", err)
	}

	results := store.Search("schema", 0)
	if len(results) != 4 {
		t.Fatalf("search = %#v, want four answers", results)
	}
	// A title match outranks a description-only match, and the notes and
	// context entries the task left behind are searchable too.
	if results[0].Kind != "task" || results[0].ID != "design" {
		t.Fatalf("top result = %#v, want the title match", results[0])
	}
	if results[1].Kind != "task" || results[1].ID != "prose" {
		t.Fatalf("second result = %#v, want the description match", results[1])
	}
	kinds := map[string]bool{}
	for _, result := range results {
		kinds[result.Kind] = true
	}
	if !kinds["note"] || !kinds["context"] {
		t.Fatalf("search missed the note or the context entry: %#v", results)
	}
	if limited := store.Search("schema", 2); len(limited) != 2 {
		t.Fatalf("limited search = %#v, want two", limited)
	}
	if results := store.Search("", 0); len(results) != 0 {
		t.Fatalf("empty query = %#v, want nothing", results)
	}
}

func TestPlandbCliBottlenecksCountTheDownstreamWork(t *testing.T) {
	store := planOpen(t, "")
	a := planSpec("a", "A")
	b := planSpec("b", "B")
	b.Dependencies = []Dependency{{TaskID: "a"}}
	c := planSpec("c", "C")
	c.Dependencies = []Dependency{{TaskID: "b"}}
	side := planSpec("side", "Side")
	planAdd(t, store, a, b, c, side)

	counts := store.Bottlenecks(0)
	if len(counts) != 4 || counts[0].Task.ID != "a" || counts[0].Downstream != 2 || counts[1].Task.ID != "b" || counts[1].Downstream != 1 {
		t.Fatalf("bottlenecks = %#v", counts)
	}

	planFinish(t, store, "a", "worker", "a delivered")
	counts = store.Bottlenecks(0)
	if len(counts) != 3 || counts[0].Task.ID != "b" || counts[0].Downstream != 1 {
		t.Fatalf("bottlenecks after a completed = %#v, want b first with one downstream", counts)
	}
	if one := store.Bottlenecks(1); len(one) != 1 || one[0].Task.ID != "b" {
		t.Fatalf("bounded bottlenecks = %#v, want only b", one)
	}
}

// KNOWN FAILURE AGAINST THE STORE AS IT STANDS — a deliberate failing-shape
// test, not a wrong expectation. The store's own contract says CriticalPath
// answers the longest chain of hard dependencies, upstream first. Its walk
// seeds only at the root, and no task may hold a hard dependency on the root
// (the lineage rule refuses one), so children[root] is always empty and every
// plan answers empty. This test is the shape that proves it; it goes green
// when the walk seeds from tasks that actually have no hard upstream.
func TestPlandbCliCriticalPathAnswersTheLongestChain(t *testing.T) {
	store := planOpen(t, "")
	a := planSpec("a", "A")
	b := planSpec("b", "B")
	b.Dependencies = []Dependency{{TaskID: "a"}}
	c := planSpec("c", "C")
	c.Dependencies = []Dependency{{TaskID: "b"}}
	side := planSpec("side", "Side")
	planAdd(t, store, a, b, c, side)

	path := store.CriticalPath()
	if got := planIDs(path); len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("critical path = %v, want a b c upstream first", got)
	}
}

func TestPlandbCliCanFinalizeAndCompleteRoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	store := planOpen(t, path)
	if ok, reason := store.CanFinalize(); !ok || reason != "" {
		t.Fatalf("an empty plan is not finalizable: %t, %q", ok, reason)
	}
	planAdd(t, store, planSpec("solo", "Solo"))
	if ok, reason := store.CanFinalize(); ok || !strings.Contains(reason, "open plan tasks remain") || !strings.Contains(reason, "solo") {
		t.Fatalf("CanFinalize with open work = %t, %q", ok, reason)
	}
	planFinish(t, store, "solo", "worker", "solo delivered")
	if ok, reason := store.CanFinalize(); !ok || reason != "" {
		t.Fatalf("CanFinalize after the work landed = %t, %q", ok, reason)
	}
	if err := store.CompleteRoot("the run is over"); err != nil {
		t.Fatalf("complete root: %v", err)
	}
	root := store.Task("root")
	if root.Status != StatusDone || root.Result != "the run is over" {
		t.Fatalf("root after completion = %#v", root)
	}
	if err := store.CompleteRoot("again"); err != nil {
		t.Fatalf("a second completion was refused: %v", err)
	}

	// A run that ends with a failed child fails the root, not done.
	other := planOpen(t, filepath.Join(t.TempDir(), "plan.json"))
	planAdd(t, other, planSpec("good", "Good"), planSpec("bad", "Bad"))
	planFinish(t, other, "good", "worker", "good delivered")
	if _, err := other.Claim("bad", "worker-b"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := other.Fail("bad", "worker-b", "it broke"); err != nil {
		t.Fatalf("fail: %v", err)
	}
	if err := other.CompleteRoot("wrap"); err != nil {
		t.Fatalf("complete root with a failed child: %v", err)
	}
	if got := other.Task("root"); got.Status != StatusFailed {
		t.Fatalf("root after a failed child = %s, want failed", got.Status)
	}

	// Completion is the runtime's last step: open work refuses it.
	third := planOpen(t, filepath.Join(t.TempDir(), "plan.json"))
	planAdd(t, third, planSpec("stuck", "Stuck"))
	if err := third.CompleteRoot("too soon"); err == nil {
		t.Fatal("the root completed with open descendants")
	}
}

// The full wave the loop runs: two parallel leaves, one integration behind
// both, the root held back for the runtime throughout, and a reopen that
// finds the whole thing on disk.
func TestPlandbCliPromotesWavesAndLeavesTheRootToTheRuntime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	store := planOpen(t, path)
	created, err := store.AddMany([]TaskSpec{
		planSpec("inspect", "Inspect"),
		planSpec("draft", "Draft"),
		{
			ID:           "integrate",
			Title:        "Integrate",
			Description:  "review and finalize the outcome",
			Dependencies: []Dependency{{TaskID: "inspect"}, {TaskID: "draft"}},
			Parallel:     "serial",
			Isolation:    "exclusive",
		},
	})
	if err != nil {
		t.Fatalf("add the wave: %v", err)
	}
	if len(created) != 3 {
		t.Fatalf("created %d tasks", len(created))
	}
	ready := store.ReadyLeaves()
	if len(ready) != 2 || ready[0].ID != "inspect" || ready[1].ID != "draft" {
		t.Fatalf("initial ready set = %v", planIDs(ready))
	}
	if _, err := store.Claim("inspect", "worker-inspect"); err != nil {
		t.Fatalf("claim inspect: %v", err)
	}
	if _, err := store.Claim("draft", "worker-draft"); err != nil {
		t.Fatalf("claim draft: %v", err)
	}
	// Two plain tasks running at once: the safe default keeps them parallel.
	if _, err := store.Done("inspect", "worker-inspect", "inspection complete", nil, nil); err != nil {
		t.Fatalf("done inspect: %v", err)
	}
	if _, err := store.Done("draft", "worker-draft", "draft complete", nil, nil); err != nil {
		t.Fatalf("done draft: %v", err)
	}
	ready = store.ReadyLeaves()
	if len(ready) != 1 || ready[0].ID != "integrate" {
		t.Fatalf("second ready set = %v, want [integrate]", planIDs(ready))
	}
	if _, err := store.Claim("integrate", "integrator"); err != nil {
		t.Fatalf("claim integrate: %v", err)
	}
	if _, err := store.Done("integrate", "integrator", "green", nil, nil); err != nil {
		t.Fatalf("done integrate: %v", err)
	}
	if got := store.Task("root"); got.Status != StatusRunning {
		t.Fatalf("root status = %s, want running: the run's finish is the runtime's", got.Status)
	}
	if ok, reason := store.CanFinalize(); !ok || reason != "" {
		t.Fatalf("CanFinalize = %t, %q", ok, reason)
	}
	if err := store.CompleteRoot("final response"); err != nil {
		t.Fatalf("complete root: %v", err)
	}
	if summary := store.Summary(); summary.Done != 4 || summary.Total != 4 {
		t.Fatalf("summary = %#v", summary)
	}
	reopened := planReopen(t, path)
	if summary := reopened.Summary(); summary.Done != 4 || summary.Total != 4 {
		t.Fatalf("reopened summary = %#v", summary)
	}
}

func TestPlandbCliParallelSafeDefaultAndTheConflictsThatRemain(t *testing.T) {
	// The default is parallel-unless-declared: two plain tasks run at once,
	// which is the reference loop's whole point.
	store := planOpen(t, filepath.Join(t.TempDir(), "plan.json"))
	planAdd(t, store, planSpec("one", "One"), planSpec("two", "Two"))
	if _, err := store.Claim("one", "worker-1"); err != nil {
		t.Fatalf("claim one: %v", err)
	}
	if _, err := store.Claim("two", "worker-2"); err != nil {
		t.Fatalf("claim two beside one: %v", err)
	}

	// The conflict rules that remain filter the ready set: a serial pair
	// cannot run together, and the ready set says why.
	serialStore := planOpen(t, filepath.Join(t.TempDir(), "plan.json"))
	first := planSpec("s1", "S1")
	first.Parallel = "serial"
	second := planSpec("s2", "S2")
	second.Parallel = "serial"
	planAdd(t, serialStore, first, second)
	if _, err := serialStore.Claim("s1", "worker-1"); err != nil {
		t.Fatalf("claim s1: %v", err)
	}
	ready := serialStore.ReadySet()
	if len(ready.Runnable) != 0 || len(ready.Blocked) != 1 || ready.Blocked[0].Task.ID != "s2" {
		t.Fatalf("ready set = %#v, want s2 blocked", ready)
	}
	if len(ready.Blocked[0].Reasons) == 0 || !strings.Contains(ready.Blocked[0].Reasons[0], "conflicts with active task s1") {
		t.Fatalf("block reasons = %#v", ready.Blocked[0].Reasons)
	}
	if got, err := serialStore.ClaimNext("worker-2"); err != nil || got != nil {
		t.Fatalf("ClaimNext handed out blocked work: %#v, %v", got, err)
	}

	// An exclusive task holds the machine against everything else running.
	exclusiveStore := planOpen(t, filepath.Join(t.TempDir(), "plan.json"))
	exclusive := planSpec("ex", "Ex")
	exclusive.Isolation = "exclusive"
	plain := planSpec("plain", "Plain")
	planAdd(t, exclusiveStore, exclusive, plain)
	if _, err := exclusiveStore.Claim("ex", "worker-1"); err != nil {
		t.Fatalf("claim ex: %v", err)
	}
	ready = exclusiveStore.ReadySet()
	if len(ready.Runnable) != 0 || len(ready.Blocked) != 1 || ready.Blocked[0].Task.ID != "plain" {
		t.Fatalf("ready set beside an exclusive task = %#v, want plain blocked", ready)
	}

	// Overlapping write resources conflict; disjoint ones do not. And the
	// Claim refusal the v1 store made on overlap is gone: the conflict lives
	// in the ready set now, not in the claim.
	resourceStore := planOpen(t, filepath.Join(t.TempDir(), "plan.json"))
	docs := planSpec("first", "First")
	docs.Resources = []ResourceClaim{{URI: "workspace://docs/**", Mode: "write"}}
	report := planSpec("second", "Second")
	report.Resources = []ResourceClaim{{URI: "workspace://docs/report.md", Mode: "write"}}
	assets := planSpec("third", "Third")
	assets.Resources = []ResourceClaim{{URI: "workspace://assets/image.png", Mode: "write"}}
	planAdd(t, resourceStore, docs, report, assets)
	if _, err := resourceStore.Claim("first", "worker-1"); err != nil {
		t.Fatalf("claim first: %v", err)
	}
	ready = resourceStore.ReadySet()
	if len(ready.Runnable) != 1 || ready.Runnable[0].ID != "third" || len(ready.Blocked) != 1 || ready.Blocked[0].Task.ID != "second" {
		t.Fatalf("ready set with overlapping resources = %#v", ready)
	}
	if _, err := resourceStore.Claim("second", "worker-2"); err != nil {
		t.Fatalf("the resource-conflict refusal was supposed to be gone from Claim: %v", err)
	}
}

// KNOWN FAILURES AGAINST THE STORE AS IT STANDS — deliberate failing-shape
// tests, not wrong expectations. DESIGN.md promises that every
// read-modify-write transaction takes the advisory flock and reloads the
// file under it; changeTask and ClaimNext do exactly that, but AddMany,
// AddNote, AddContext and Prune clone the handle's own memory and rename it
// over the file with no lock and no reload. A second handle (or a second
// process — this is the CLI's own road for add, split, note and context)
// that wrote between this handle's load and its write is silently erased.
// Each subtest proves one loss deterministically in process; they go green
// when the writers take the same two locks changeTask takes.
func TestPlandbCliWritesSurviveAnotherHandle(t *testing.T) {
	t.Run("addmany", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "plan.json")
		first := planOpen(t, path)
		second := planReopen(t, path)
		planAdd(t, first, planSpec("job", "Job"))
		if _, err := second.AddMany([]TaskSpec{planSpec("other", "Other")}); err != nil {
			t.Fatalf("add from the second handle: %v", err)
		}
		reopened := planReopen(t, path)
		if reopened.Task("job") == nil {
			t.Fatalf("a stale handle's add dropped a concurrent task: %#v", reopened.Tasks())
		}
	})

	t.Run("note", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "plan.json")
		first := planOpen(t, path)
		planAdd(t, first, planSpec("job", "Job"))
		second := planReopen(t, path)

		if _, err := first.AddNote("job", "worker-a", "first handle's note"); err != nil {
			t.Fatalf("add note: %v", err)
		}
		if _, err := second.AddContext("", "decision", "second handle decides"); err != nil {
			t.Fatalf("add context: %v", err)
		}

		reopened := planReopen(t, path)
		got := reopened.Notes("job", 0)
		if len(got) != 1 || got[0].Body != "first handle's note" {
			t.Fatalf("a stale handle's write dropped a concurrent note: %#v", got)
		}
	})

	t.Run("prune", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "plan.json")
		writer := planOpen(t, path)
		kept, err := writer.AddContext("", "discovery", "kept")
		if err != nil {
			t.Fatalf("add context: %v", err)
		}
		pruner := planReopen(t, path)
		if _, err := writer.AddContext("", "discovery", "added after"); err != nil {
			t.Fatalf("add context: %v", err)
		}
		if err := pruner.Prune(kept.ID); err != nil {
			t.Fatalf("prune: %v", err)
		}
		reopened := planReopen(t, path)
		left := reopened.Contexts("", "", 0)
		if len(left) != 1 || left[0].Content != "added after" {
			t.Fatalf("a stale prune dropped a concurrent context entry: %#v", left)
		}
	})
}

// Two handles on one path: the first holds a real changeTask (its now hook
// sleeps inside the change, with both locks held), the second tries its own
// change and must wait the flock out. Both roads in two goroutines, bounded
// waits throughout — never both locks in one goroutine, which flock on a
// second descriptor would self-deadlock.
func TestPlandbCliFlockSerializesTwoHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	first := planOpen(t, path)
	planAdd(t, first, planSpec("shared", "Shared"))
	second := planReopen(t, path)

	held := make(chan struct{})
	released := make(chan struct{})
	firstErr := make(chan error, 1)
	first.now = func() time.Time {
		close(held)
		time.Sleep(150 * time.Millisecond)
		return time.Now()
	}
	go func() {
		_, err := first.Amend("shared", "first handle wrote while holding the lock")
		firstErr <- err
		close(released)
	}()

	select {
	case <-held:
	case <-time.After(2 * time.Second):
		t.Fatal("the first handle never entered its change")
	}

	secondDone := make(chan error, 1)
	go func() {
		_, err := second.Amend("shared", "second handle wrote after the wait")
		secondDone <- err
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("the second handle's change ran while the first held the lock: %v", err)
	case <-time.After(80 * time.Millisecond):
		// still waiting: the flock is doing its job
	}

	select {
	case <-released:
	case <-time.After(2 * time.Second):
		t.Fatal("the first handle never released the lock")
	}
	if err := <-firstErr; err != nil {
		t.Fatalf("the first handle's change failed: %v", err)
	}
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("the second handle's change failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the second handle's change never completed after the lock freed")
	}

	// The second handle reloaded under the lock, so both writes are in the
	// file — nobody lost anybody's update.
	got := second.Task("shared")
	if got == nil || !strings.Contains(got.Description, "first handle wrote") || !strings.Contains(got.Description, "second handle wrote") {
		t.Fatalf("a write was lost across the two handles: %q", got.Description)
	}
}
