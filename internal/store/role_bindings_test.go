package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// spliceRoleTree is the shape every role test needs: one job with a stage and a
// leaf so "nearest governing ancestor" has more than one answer, a sibling job
// nothing binds, and a pinned job whose nodes carry a promised work model.
func spliceRoleTree(t *testing.T, graph *Store) {
	t.Helper()
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "deliver the thing", Stage: 3},
		{ID: "stage", Parent: "job", Brief: "one stage of it", Stage: 2},
		{ID: "leaf", Parent: "stage", Brief: "the actual work", Stage: 1},
	}}, Provenance{Origin: OriginUser, SessionID: "roles", Intent: "bind some roles"}); err != nil {
		t.Fatalf("splice job: %v", err)
	}
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "other-job", Brief: "an unrelated job", Stage: 1},
	}}, Provenance{Origin: OriginUser, SessionID: "roles", Intent: "another task"}); err != nil {
		t.Fatalf("splice sibling: %v", err)
	}
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "pinned-job", Brief: "a job somebody pointed at a model", Stage: 2},
		{ID: "pinned-leaf", Parent: "pinned-job", Brief: "its work", Stage: 1},
	}}, Provenance{
		Origin: OriginUser, SessionID: "roles", Intent: "a pinned task",
		WorkModel: "pinned/work", PlanModel: "pinned/plan",
	}); err != nil {
		t.Fatalf("splice pinned job: %v", err)
	}
}

func roleEventCount(t *testing.T, graph *Store) int {
	t.Helper()
	var count int
	if err := graph.db.QueryRow(`SELECT COUNT(*) FROM events WHERE kind = ?`, EventRoleBindingSet).Scan(&count); err != nil {
		t.Fatalf("count role events: %v", err)
	}
	return count
}

// TestRoleResolutionIsSilentUntilSomethingIsBound is the default-path contract,
// and the one every other wave has to keep: with nothing bound and no defaults
// installed, no role resolves to anything anywhere — so a caller wired to this
// table does precisely what it did before the table existed. The one thing that
// does answer is a pin, because a pin already answered yesterday.
func TestRoleResolutionIsSilentUntilSomethingIsBound(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "unbound.db"))
	spliceRoleTree(t, graph)

	for _, role := range ModelRoles() {
		for _, id := range []string{"", RootID, "job", "stage", "leaf", "other-job"} {
			resolved, err := graph.ResolveRole(role, id)
			if err != nil {
				t.Fatalf("resolve %s at %q: %v", role, id, err)
			}
			if resolved.Bound() || resolved.Source != RoleUnbound || resolved.Scope != "" {
				t.Fatalf("resolve %s at %q = %+v, want unbound", role, id, resolved)
			}
		}
	}
	// A node that is not in the graph at all degrades to the scopes above it
	// rather than refusing: resolution never panics and never errors on a node
	// that folded away underneath its caller.
	resolved, err := graph.ResolveRole(RoleWork, "no-such-node")
	if err != nil {
		t.Fatalf("resolve at a missing node: %v", err)
	}
	if resolved.Bound() {
		t.Fatalf("missing node resolved to %+v, want unbound", resolved)
	}

	// The pinned job answers with its promise, on the two roles the graph has
	// always recorded per node and on neither of the three it has not.
	work, err := graph.ResolveRole(RoleWork, "pinned-leaf")
	if err != nil {
		t.Fatal(err)
	}
	if work.Model != "pinned/work" || work.Source != RoleFromPin {
		t.Fatalf("pinned work = %+v, want the pin", work)
	}
	plan, err := graph.ResolveRole(RolePlan, "pinned-leaf")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Model != "pinned/plan" || plan.Source != RoleFromPin {
		t.Fatalf("pinned plan = %+v, want the pin", plan)
	}
	scribe, err := graph.ResolveRole(RoleScribe, "pinned-leaf")
	if err != nil {
		t.Fatal(err)
	}
	if scribe.Bound() {
		t.Fatalf("scribe at a pinned node = %+v, want unbound: only work and plan have pins", scribe)
	}

	if bindings, err := graph.RoleBindings(); err != nil || len(bindings) != 0 {
		t.Fatalf("RoleBindings() = %v, %v, want none", bindings, err)
	}
	if count := roleEventCount(t, graph); count != 0 {
		t.Fatalf("silent resolution journaled %d events, want none", count)
	}
}

// TestRoleBindingPrecedence walks the whole ladder down one node, adding one
// rung at a time, and checks the answer after every addition. The order is the
// contract every consumer cites: pin, node, task (nearest), global, default,
// unbound.
func TestRoleBindingPrecedence(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "precedence.db"))
	spliceRoleTree(t, graph)

	assert := func(step string, id string, wantModel string, wantSource RoleSource) {
		t.Helper()
		resolved, err := graph.ResolveRole(RoleWork, id)
		if err != nil {
			t.Fatalf("%s: resolve %q: %v", step, id, err)
		}
		if resolved.Model != wantModel || resolved.Source != wantSource {
			t.Fatalf("%s: resolve %q = %+v, want %q from %s", step, id, resolved, wantModel, wantSource)
		}
	}

	// 6. nothing at all.
	assert("unbound", "leaf", "", RoleUnbound)

	// 5. the compiled-in default this process installed.
	graph.InstallRoleDefaults(NewRoleDefaults("orchestrate/model", "plan/model", "work/model", "cheap/model"))
	assert("default", "leaf", "work/model", RoleFromDefault)

	// 4. global outranks the default.
	if changed, err := graph.SetRoleBinding(RoleWork, ScopeGlobal, "global/model", "test:user"); err != nil || !changed {
		t.Fatalf("bind global: changed=%t err=%v", changed, err)
	}
	assert("global", "leaf", "global/model", RoleFromGlobal)
	assert("global reaches every node", "other-job", "global/model", RoleFromGlobal)

	// 3. a task binding outranks global for its own subtree and nothing else.
	if _, err := graph.SetRoleBinding(RoleWork, TaskScope("job"), "job/model", "test:user"); err != nil {
		t.Fatal(err)
	}
	assert("task", "leaf", "job/model", RoleFromTask)
	assert("task governs its root too", "job", "job/model", RoleFromTask)
	assert("a sibling task is untouched", "other-job", "global/model", RoleFromGlobal)

	// 3b. the NEAREST governing ancestor wins, exactly as a ceiling does.
	if _, err := graph.SetRoleBinding(RoleWork, TaskScope("stage"), "stage/model", "test:user"); err != nil {
		t.Fatal(err)
	}
	assert("nearest task", "leaf", "stage/model", RoleFromTask)
	assert("above the nearer one", "job", "job/model", RoleFromTask)

	// 2. a node binding outranks every task binding above it.
	if _, err := graph.SetRoleBinding(RoleWork, NodeScope("leaf"), "leaf/model", "test:user"); err != nil {
		t.Fatal(err)
	}
	assert("node", "leaf", "leaf/model", RoleFromNode)
	assert("a node binding reaches no other node", "stage", "stage/model", RoleFromTask)

	// 1. the pin outranks all of it. SetSubtreeWorkModel is the verb that
	// writes one, and it is the same word CommandSetModel journals.
	if _, err := graph.SetSubtreeWorkModel("leaf", "pinned/by-hand", "the user said so"); err != nil {
		t.Fatal(err)
	}
	assert("pin", "leaf", "pinned/by-hand", RoleFromPin)

	// And the ladder unwinds in the same order it was built.
	if _, err := graph.SetSubtreeWorkModel("leaf", "", ""); err == nil {
		t.Fatal("an empty model cleared a pin, want a refusal")
	}
	if changed, err := graph.ClearRoleBinding(RoleWork, NodeScope("leaf"), "test:user"); err != nil || !changed {
		t.Fatalf("clear node binding: changed=%t err=%v", changed, err)
	}
	assert("pin survives the node binding", "leaf", "pinned/by-hand", RoleFromPin)
	assert("unpinned nodes fall back to the nearest task", "stage", "stage/model", RoleFromTask)
	if _, err := graph.ClearRoleBinding(RoleWork, TaskScope("stage"), "test:user"); err != nil {
		t.Fatal(err)
	}
	assert("an outer task resumes", "stage", "job/model", RoleFromTask)
	if _, err := graph.ClearRoleBinding(RoleWork, TaskScope("job"), "test:user"); err != nil {
		t.Fatal(err)
	}
	assert("global resumes", "stage", "global/model", RoleFromGlobal)
	if _, err := graph.ClearRoleBinding(RoleWork, ScopeGlobal, "test:user"); err != nil {
		t.Fatal(err)
	}
	assert("the default resumes", "stage", "work/model", RoleFromDefault)
	graph.InstallRoleDefaults(nil)
	assert("and the machine is silent again", "stage", "", RoleUnbound)
}

// TestSetModelAndRoleBindingsTellOneStory is the composition Wave 0's subtree
// sweep and this table have to agree on: SetModel is a node-level pin sweep
// over the live subtree, a binding is the inheritable default, and pins win —
// including for work admitted after the sweep, which carries no pin and
// therefore inherits.
func TestSetModelAndRoleBindingsTellOneStory(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "compose.db"))
	spliceRoleTree(t, graph)

	if _, err := graph.SetRoleBinding(RoleWork, TaskScope("job"), "binding/model", "test:chip"); err != nil {
		t.Fatal(err)
	}
	rebinding, err := graph.SetSubtreeWorkModel("job", "swept/model", "the user moved the hands")
	if err != nil {
		t.Fatal(err)
	}
	if len(rebinding.Nodes) != 3 {
		t.Fatalf("sweep touched %v, want the job and both descendants", rebinding.Nodes)
	}
	for _, id := range []string{"job", "stage", "leaf"} {
		resolved, err := graph.ResolveRole(RoleWork, id)
		if err != nil {
			t.Fatal(err)
		}
		if resolved.Model != "swept/model" || resolved.Source != RoleFromPin {
			t.Fatalf("%s after the sweep = %+v, want the pin", id, resolved)
		}
	}

	// New work spliced under the same task was never swept, so it has no pin
	// and inherits the binding — which is exactly what a binding is for.
	if err := graph.Splice("stage", Subtree{Nodes: []NodeSpec{
		{ID: "fresh-leaf", Brief: "work admitted after the sweep", Stage: 1},
	}}, Provenance{Origin: OriginUser, SessionID: "roles", Intent: "more work"}); err != nil {
		t.Fatal(err)
	}
	resolved, err := graph.ResolveRole(RoleWork, "fresh-leaf")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Model != "binding/model" || resolved.Source != RoleFromTask {
		t.Fatalf("unswept work = %+v, want the task binding", resolved)
	}

	// And the same answer costs no query when the caller already holds the row.
	node, found, err := graph.Node("leaf")
	if err != nil || !found {
		t.Fatalf("read leaf: %v", err)
	}
	fromRow, err := graph.ResolveRoleForNode(RoleWork, node)
	if err != nil {
		t.Fatal(err)
	}
	if fromRow.Model != "swept/model" || fromRow.Source != RoleFromPin {
		t.Fatalf("ResolveRoleForNode = %+v, want the pin", fromRow)
	}
}

// TestRoleBindingWritesAreIdempotent covers the journal's shape: the same
// binding twice writes once, a different value writes again, clearing writes
// once and clearing nothing writes nothing at all.
func TestRoleBindingWritesAreIdempotent(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "idempotent.db"))
	spliceRoleTree(t, graph)

	if changed, err := graph.SetRoleBinding(RoleScribe, ScopeGlobal, "tiny/model", "test:user"); err != nil || !changed {
		t.Fatalf("first bind: changed=%t err=%v", changed, err)
	}
	if changed, err := graph.SetRoleBinding(RoleScribe, ScopeGlobal, " tiny/model ", "test:someone-else"); err != nil || changed {
		t.Fatalf("re-bind: changed=%t err=%v, want no second event", changed, err)
	}
	if count := roleEventCount(t, graph); count != 1 {
		t.Fatalf("re-binding journaled %d events, want 1", count)
	}
	if changed, err := graph.SetRoleBinding(RoleScribe, ScopeGlobal, "tinier/model", "test:user"); err != nil || !changed {
		t.Fatalf("re-point: changed=%t err=%v", changed, err)
	}
	if changed, err := graph.ClearRoleBinding(RoleScribe, ScopeGlobal, "test:user"); err != nil || !changed {
		t.Fatalf("clear: changed=%t err=%v", changed, err)
	}
	if changed, err := graph.ClearRoleBinding(RoleScribe, ScopeGlobal, "test:user"); err != nil || changed {
		t.Fatalf("clearing nothing: changed=%t err=%v, want a quiet no", changed, err)
	}
	if count := roleEventCount(t, graph); count != 3 {
		t.Fatalf("journal holds %d events, want set, re-point, clear", count)
	}
	if _, found, err := graph.RoleBindingAt(RoleScribe, ScopeGlobal); err != nil || found {
		t.Fatalf("cleared binding still reads back: found=%t err=%v", found, err)
	}
}

// TestSeedingNeverSpamsAndNeverClobbers is the environment bridge's whole
// contract: AFORGE_PLAN_MODEL seeds an unbound role, re-seeds itself when the
// environment changes, writes nothing on an unchanged boot, and stops speaking
// the moment a person has bound the role by hand.
func TestSeedingNeverSpamsAndNeverClobbers(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "seed.db"))
	spliceRoleTree(t, graph)
	const origin = RoleSeedOriginPrefix + "AFORGE_PLAN_MODEL"

	if changed, err := graph.SeedRoleBinding(RolePlan, ScopeGlobal, "env/plan", origin); err != nil || !changed {
		t.Fatalf("first boot: changed=%t err=%v", changed, err)
	}
	for boot := 0; boot < 3; boot++ {
		if changed, err := graph.SeedRoleBinding(RolePlan, ScopeGlobal, "env/plan", origin); err != nil || changed {
			t.Fatalf("boot %d: changed=%t err=%v, want silence", boot, changed, err)
		}
	}
	if count := roleEventCount(t, graph); count != 1 {
		t.Fatalf("four boots journaled %d events, want 1", count)
	}
	if changed, err := graph.SeedRoleBinding(RolePlan, ScopeGlobal, "env/other-plan", origin); err != nil || !changed {
		t.Fatalf("changed environment: changed=%t err=%v", changed, err)
	}

	// A hand on the palette outranks the environment from then on, restart or
	// no restart.
	if _, err := graph.SetRoleBinding(RolePlan, ScopeGlobal, "chosen/plan", "user:chip"); err != nil {
		t.Fatal(err)
	}
	if changed, err := graph.SeedRoleBinding(RolePlan, ScopeGlobal, "env/other-plan", origin); err != nil || changed {
		t.Fatalf("seed over a hand: changed=%t err=%v, want refusal to clobber", changed, err)
	}
	binding, found, err := graph.RoleBindingAt(RolePlan, ScopeGlobal)
	if err != nil || !found || binding.Value != "chosen/plan" {
		t.Fatalf("binding = %+v found=%t err=%v, want the user's choice", binding, found, err)
	}
	if _, err := graph.SeedRoleBinding(RolePlan, ScopeGlobal, "env/plan", "user:chip"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("seeding under a non-seed origin = %v, want ErrInvalid", err)
	}
}

// TestRoleBindingsAreRebuiltFromTheJournal is journal-is-truth: the projection
// is discarded and reconstructed by replay, and the store reopens on the same
// answers.
func TestRoleBindingsAreRebuiltFromTheJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rebuild.db")
	graph := openTestStore(t, path)
	spliceRoleTree(t, graph)

	if _, err := graph.SetRoleBinding(RoleWork, ScopeGlobal, "global/work", "test:user"); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.SetRoleBinding(RoleWork, TaskScope("job"), "job/work", "test:user"); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.SetRoleBinding(RoleScribe, NodeScope("leaf"), "leaf/scribe", "test:user"); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.SetRoleBinding(RoleVerify, ScopeGlobal, "gone/verify", "test:user"); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.ClearRoleBinding(RoleVerify, ScopeGlobal, "test:user"); err != nil {
		t.Fatal(err)
	}
	before, err := graph.RoleBindings()
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 3 {
		t.Fatalf("bindings = %+v, want three surviving", before)
	}
	// Ladder order, then scope: the settings view reads five rows in one order.
	if before[0].Role != RoleWork || before[1].Role != RoleWork || before[2].Role != RoleScribe {
		t.Fatalf("bindings out of ladder order: %+v", before)
	}
	if before[0].Scope != ScopeGlobal || before[1].Scope != TaskScope("job") {
		t.Fatalf("scopes out of order: %+v", before)
	}

	if err := graph.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	after, err := graph.RoleBindings()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("rebuild = %+v, want %+v", after, before)
	}
	for i := range after {
		if after[i].Role != before[i].Role || after[i].Scope != before[i].Scope ||
			after[i].Value != before[i].Value || after[i].Origin != before[i].Origin {
			t.Fatalf("rebuild[%d] = %+v, want %+v", i, after[i], before[i])
		}
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTestStore(t, path)
	resolved, err := reopened.ResolveRole(RoleWork, "leaf")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Model != "job/work" || resolved.Source != RoleFromTask {
		t.Fatalf("after reopen = %+v, want the task binding", resolved)
	}
}

// TestRoleBindingsRefuseWhatTheyCannotInterpret is the no-panic bar: an
// unknown role, a malformed scope, an empty model, and a scope naming a node
// that is not there are all refusals, and none of them is a crash.
func TestRoleBindingsRefuseWhatTheyCannotInterpret(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "refusals.db"))
	spliceRoleTree(t, graph)

	if _, err := graph.SetRoleBinding("judge", ScopeGlobal, "some/model", "test:user"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("sixth role = %v, want ErrInvalid", err)
	}
	if _, err := graph.SetRoleBinding("", ScopeGlobal, "some/model", "test:user"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty role = %v, want ErrInvalid", err)
	}
	if _, err := graph.ResolveRole("labeler", "leaf"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("resolve an unknown role = %v, want ErrInvalid", err)
	}
	if _, err := graph.ResolveRoleForNode("labeler", Node{ID: "leaf"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("resolve-for-node an unknown role = %v, want ErrInvalid", err)
	}
	for _, scope := range []BindingScope{"", "task:", "node:", "task: ", "session:x", "leaf"} {
		if _, err := graph.SetRoleBinding(RoleWork, scope, "some/model", "test:user"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("scope %q = %v, want ErrInvalid", scope, err)
		}
	}
	if _, err := graph.SetRoleBinding(RoleWork, ScopeGlobal, "   ", "test:user"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty model = %v, want ErrInvalid", err)
	}
	if _, err := graph.SetRoleBinding(RoleWork, TaskScope("no-such-node"), "some/model", "test:user"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("scope on a missing node = %v, want ErrNotFound", err)
	}
	if _, err := graph.ClearRoleBinding("judge", ScopeGlobal, "test:user"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("clear an unknown role = %v, want ErrInvalid", err)
	}
	if _, _, err := graph.RoleBindingAt(RoleWork, "task:"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("read a malformed scope = %v, want ErrInvalid", err)
	}
	if _, err := ParseModelRole("Scribe "); err != nil {
		t.Fatalf("parse a typed role: %v", err)
	}
	if _, err := ParseModelRole("clerk"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("parse a role word = %v, want ErrInvalid: the word is not the key", err)
	}
	if scope, err := ParseBindingScope(" task:job "); err != nil || scope != TaskScope("job") {
		t.Fatalf("parse a scope = %q, %v", scope, err)
	}
	if count := roleEventCount(t, graph); count != 0 {
		t.Fatalf("refusals journaled %d events, want none", count)
	}
}

// TestTheLadderIsTheDocsLadder locks the five and their words to 5.23. A sixth
// role needs the doc amended before it needs code, and this is where the
// amendment gets noticed.
func TestTheLadderIsTheDocsLadder(t *testing.T) {
	roles := ModelRoles()
	if len(roles) != 5 {
		t.Fatalf("the ladder has %d rungs, want five", len(roles))
	}
	words := map[ModelRole]string{
		RoleOrchestrate: "voice", RolePlan: "architect", RoleWork: "hands",
		RoleVerify: "skeptic", RoleScribe: "clerk",
	}
	for _, role := range roles {
		if role.Word() != words[role] {
			t.Fatalf("%s reads %q, want %q", role, role.Word(), words[role])
		}
	}
	if ModelRole("judge").Word() != "" || ModelRole("judge").Valid() {
		t.Fatal("an unknown role has a word or is valid")
	}
	// Verify and scribe fall to the cheap model when configuration names one,
	// and to the work model when it does not — the ordering law's floor, not a
	// silent upgrade.
	cheap := NewRoleDefaults("o/model", "p/model", "w/model", "c/model")
	if cheap[RoleVerify] != "c/model" || cheap[RoleScribe] != "c/model" {
		t.Fatalf("cheap defaults = %v", cheap)
	}
	unnamed := NewRoleDefaults("o/model", "", "w/model", "")
	if unnamed[RoleVerify] != "w/model" || unnamed[RoleScribe] != "w/model" {
		t.Fatalf("fallback defaults = %v", unnamed)
	}
	if _, named := unnamed[RolePlan]; named {
		t.Fatalf("an empty default was installed anyway: %v", unnamed)
	}
}

// TestRoleResolutionCostsIndexedLookups is the performance contract stated as a
// plan, not a stopwatch: the existence probe is one index probe of
// role_bindings and touches nothing else, and the ancestry walk resolves both
// the hop and the binding by index rather than scanning either table.
func TestRoleResolutionCostsIndexedLookups(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "plans.db"))
	spliceRoleTree(t, graph)
	if _, err := graph.SetRoleBinding(RoleWork, TaskScope("job"), "job/model", "test:user"); err != nil {
		t.Fatal(err)
	}

	plan := func(query string, args ...any) string {
		t.Helper()
		rows, err := graph.db.Query("EXPLAIN QUERY PLAN "+query, args...)
		if err != nil {
			t.Fatalf("explain: %v", err)
		}
		defer rows.Close()
		var lines []string
		for rows.Next() {
			var id, parent, notUsed int
			var detail string
			if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
				t.Fatalf("explain: %v", err)
			}
			lines = append(lines, detail)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("explain: %v", err)
		}
		return strings.Join(lines, "\n")
	}

	// "SCAN CONSTANT ROW" is the EXISTS wrapper's own one-row outer query and
	// reads no table; what matters is that the only table touched is
	// role_bindings, by a covering index, with nodes never opened at all.
	probe := plan(roleBindingsExistQuery, RoleWork)
	if !strings.Contains(probe, "SEARCH role_bindings USING COVERING INDEX") {
		t.Fatalf("the unbound-path probe is not a covering index probe:\n%s", probe)
	}
	if strings.Contains(probe, "SCAN role_bindings") || strings.Contains(probe, "nodes") {
		t.Fatalf("the probe reads more than the binding index:\n%s", probe)
	}

	walk := plan(nearestTaskRoleBindingQuery, "leaf", maxAncestryDepth, RoleWork)
	if !strings.Contains(walk, "SEARCH role_bindings") {
		t.Fatalf("the ancestry walk does not resolve bindings by index:\n%s", walk)
	}
	if !strings.Contains(walk, "SEARCH nodes") {
		t.Fatalf("the ancestry walk does not resolve hops by index:\n%s", walk)
	}
	if strings.Contains(walk, "SCAN nodes") || strings.Contains(walk, "SCAN role_bindings") {
		t.Fatalf("the ancestry walk scans a table:\n%s", walk)
	}
}
