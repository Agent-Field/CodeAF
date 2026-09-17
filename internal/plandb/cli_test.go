package plandb

// The CLI's own tests: every ported verb driven through Main exactly as a
// shell would drive it — argv in, exit code and streams out — against a
// temp store per case. The helpers are cli-prefixed because the store's
// tests live in this package too and own their own names.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cliHarness is one test's store and streams. Main's two writers sit behind
// package variables so a test reads exactly what a shell would have seen.
type cliHarness struct {
	t    *testing.T
	db   string
	out  *bytes.Buffer
	errb *bytes.Buffer
}

func cliNewHarness(t *testing.T) *cliHarness {
	t.Helper()
	return &cliHarness{
		t:    t,
		db:   filepath.Join(t.TempDir(), "plan", "plandb.db"),
		out:  &bytes.Buffer{},
		errb: &bytes.Buffer{},
	}
}

// cliRun runs one argv and answers the exit code, with the runner's streams
// captured for the assertions that follow.
func (h *cliHarness) run(argv ...string) int {
	h.t.Helper()
	h.out.Reset()
	h.errb.Reset()
	savedOut, savedErr := cliOut, cliErr
	cliOut, cliErr = h.out, h.errb
	defer func() { cliOut, cliErr = savedOut, savedErr }()
	return Main(argv)
}

// cliInitFresh creates the harness's store and refuses anything but a clean
// exit, so every later assertion starts from a run that exists.
func (h *cliHarness) cliInitFresh() {
	h.t.Helper()
	if code := h.run("--db", h.db, "init", "demo", "--description", "the shape run"); code != 0 {
		h.t.Fatalf("init exited %d: %s%s", code, h.out.String(), h.errb.String())
	}
}

// cliAdd adds one task the doctrine's way and answers its t- id.
func (h *cliHarness) cliAdd(title, as string, extra ...string) string {
	h.t.Helper()
	argv := []string{"--db", h.db, "add", title}
	if as != "" {
		argv = append(argv, "--as", as)
	}
	argv = append(argv, extra...)
	if code := h.run(argv...); code != 0 {
		h.t.Fatalf("add %q exited %d: %s%s", title, code, h.out.String(), h.errb.String())
	}
	line := h.out.String()
	start := strings.Index(line, "created task t-")
	if start < 0 {
		h.t.Fatalf("add %q printed no created line: %q", title, line)
	}
	id := line[start+len("created task "):]
	if end := strings.Index(id, " "); end >= 0 {
		id = id[:end]
	}
	return strings.TrimSuffix(id, "\n")
}

func (h *cliHarness) cliJSON(t *testing.T) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(h.out.Bytes(), &value); err != nil {
		t.Fatalf("--json output is not an object: %v\n%s", err, h.out.String())
	}
	return value
}

func cliWantCode(t *testing.T, code, want int) {
	t.Helper()
	if code != want {
		t.Fatalf("exit %d, want %d", code, want)
	}
}

// cliWantErr asserts the one error shape: `error: <sentence>` on stderr and
// nothing on stdout.
func cliWantError(t *testing.T, h *cliHarness, code int, sentence string) {
	t.Helper()
	cliWantCode(t, code, 1)
	if got := h.errb.String(); !strings.HasPrefix(got, "error: ") || !strings.Contains(got, sentence) {
		t.Fatalf("stderr %q does not carry %q", got, sentence)
	}
	if h.out.String() != "" {
		t.Fatalf("stdout %q should be empty on an error", h.out.String())
	}
}

func TestPlandbCliInitAndAddCapturedShapes(t *testing.T) {
	h := cliNewHarness(t)
	code := h.run("--db", h.db, "init", "demo", "--description", "the shape run")
	cliWantCode(t, code, 0)
	for _, want := range []string{
		"created p-demo (demo)",
		`next: plandb add "title" --description "detailed spec" [--dep t-upstream] [--as custom-id]`,
		"tip:  create tasks in dependency order. use --dep to chain them.",
		`plandb add "A" --as a && plandb add "B" --dep t-a --as b`,
		"plandb go → work → plandb done --next → repeat",
	} {
		if !strings.Contains(h.out.String(), want) {
			t.Fatalf("init output misses %q:\n%s", want, h.out.String())
		}
	}
	// A second init in the same place is a refusal, not a merge: two runs on
	// one store would dispatch each other's children.
	code = h.run("--db", h.db, "init", "other")
	cliWantError(t, h, code, "already exists")

	code = h.run("--db", h.db, "add", "A", "--description", "part a", "--as", "a")
	cliWantCode(t, code, 0)
	if got := h.out.String(); got != "created task t-a (A)\n" {
		t.Fatalf("add shape %q, want %q", got, "created task t-a (A)\n")
	}
	code = h.run("--db", h.db, "add", "B", "--description", "part b", "--dep", "t-a", "--as", "b")
	cliWantCode(t, code, 0)
	if got := h.out.String(); got != "created task t-b (B)\n" {
		t.Fatalf("add shape %q, want \"created task t-b (B)\\n\"", got)
	}
	// The grammar allows flags after positionals, --flag=value, and repeatable --dep.
	code = h.run("--db", h.db, "add", "C", "--as=c", "--kind=research", "--dep", "t-a", "--dep", "t-b:blocks")
	cliWantCode(t, code, 0)
	code = h.run("--db", h.db, "show", "t-c")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "depends on: t-a (feeds_into), t-b (blocks)") {
		t.Fatalf("show misses both dep kinds:\n%s", h.out.String())
	}
	if !strings.Contains(h.out.String(), "kind: research") {
		t.Fatalf("show misses the kind:\n%s", h.out.String())
	}
	// An unknown dependency is named in the refusal, which says what to do
	// about it.
	code = h.run("--db", h.db, "add", "D", "--dep", "t-nope")
	cliWantError(t, h, code, "dependency task 't-nope' not found. Create it first, then add the dependency.")
	// A bad priority is the cause and the shape it wants.
	code = h.run("--db", h.db, "add", "E", "--priority", "high")
	cliWantError(t, h, code, "--priority needs a whole number")
}

func TestPlandbCliShowCardAndJSON(t *testing.T) {
	h := cliNewHarness(t)
	h.cliInitFresh()
	h.cliAdd("Design schema", "n1", "--description", "design the tables")
	code := h.run("--db", h.db, "show", "t-n1")
	cliWantCode(t, code, 0)
	for _, want := range []string{
		"id: t-n1",
		"project: p-demo",
		"title: Design schema",
		"kind: generic",
		"priority: 0",
		"description: design the tables",
	} {
		if !strings.Contains(h.out.String(), want) {
			t.Fatalf("show card misses %q:\n%s", want, h.out.String())
		}
	}
	// task get is the same door under its longer spelling, fuzzy ids included.
	code = h.run("--db", h.db, "task", "get", "n1")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "title: Design schema") {
		t.Fatalf("task get missed the card:\n%s", h.out.String())
	}
	code = h.run("--db", h.db, "--json", "show", "n1")
	cliWantCode(t, code, 0)
	value := h.cliJSON(t)
	if value["id"] != "t-n1" || value["project_id"] != "p-demo" || value["status"] != "ready" || value["is_composite"] != false {
		t.Fatalf("json task object wrong: %#v", value)
	}
	if _, ok := value["created_at"]; !ok {
		t.Fatalf("json task object carries no created_at: %#v", value)
	}
	// Not found, as one plain sentence.
	code = h.run("--db", h.db, "show", "t-missing")
	cliWantError(t, h, code, "not found: task t-missing")
}

func TestPlandbCliSplitShapes(t *testing.T) {
	h := cliNewHarness(t)
	h.cliInitFresh()
	h.cliAdd("A", "a", "--description", "part a")
	code := h.run("--db", h.db, "--json", "split", "t-a", "--into", `[{"title":"P","description":"p"},{"title":"Q","description":"q","deps_on":["P"]}]`)
	cliWantCode(t, code, 0)
	value := h.cliJSON(t)
	created, _ := value["created"].([]any)
	if len(created) != 2 {
		t.Fatalf("split created %v, want two ids", value["created"])
	}
	titleMap, _ := value["title_to_id"].(map[string]any)
	if titleMap["P"] != created[0] || titleMap["Q"] != created[1] {
		t.Fatalf("title_to_id %v does not match created %v", titleMap, created)
	}
	effect, _ := value["effect"].(map[string]any)
	if effect == nil {
		t.Fatalf("split answer carries no effect: %#v", value)
	}
	readyNow, _ := effect["ready_now"].([]any)
	if len(readyNow) != 1 || readyNow[0] != created[0] {
		t.Fatalf("effect.ready_now %v, want [%v] — P has no blockers, Q waits on P", readyNow, created[0])
	}
	state, _ := value["project_state"].(map[string]any)
	// A itself is ready, P is ready, Q waits on P.
	if state["total"] != float64(3) || state["ready"] != float64(2) || state["pending"] != float64(1) {
		t.Fatalf("project_state %v, want total 3 with two ready", state)
	}
	if value["parent_task_id"] != "t-a" {
		t.Fatalf("parent_task_id %v, want t-a", value["parent_task_id"])
	}
	// A bad third part creates nothing: the batch is validated whole.
	before := h.cliReadStore(t)
	code = h.run("--db", h.db, "split", "t-a", "--into", `[{"title":"OK"},{"title":""}]`)
	cliWantError(t, h, code, "has no title")
	if h.cliReadStore(t) != before {
		t.Fatalf("a refused split wrote to the store")
	}
	// Comma titles and the > chain.
	code = h.run("--db", h.db, "split", "t-a", "--into", "X, Y")
	cliWantCode(t, code, 0)
	if got := h.out.String(); got != "split t-a\n" {
		t.Fatalf("split text shape %q, want \"split t-a\\n\"", got)
	}
	code = h.run("--db", h.db, "--json", "split", "t-a", "--into", "F > G > H")
	cliWantCode(t, code, 0)
	// The store resolves ids and id prefixes, not titles, so the chain's tail
	// is shown through the id the split answer mapped H to.
	value = h.cliJSON(t)
	chainIDs, _ := value["title_to_id"].(map[string]any)
	hID, _ := chainIDs["H"].(string)
	code = h.run("--db", h.db, "--json", "show", hID)
	cliWantCode(t, code, 0)
	value = h.cliJSON(t)
	deps, _ := value["dependencies"].([]any)
	if len(deps) != 1 {
		t.Fatalf("chain tail %v carries %d deps, want 1", value, len(deps))
	}
	// deps_on naming a stranger is refused with the cause and the way out.
	code = h.run("--db", h.db, "split", "t-a", "--into", `[{"title":"L","deps_on":["nope"]}]`)
	cliWantError(t, h, code, "is not one of the split's own parts")
}

func (h *cliHarness) cliReadStore(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(h.db)
	if err != nil {
		t.Fatalf("read store: %v", err)
	}
	return string(data)
}

func TestPlandbCliCoordinationVerbs(t *testing.T) {
	h := cliNewHarness(t)
	h.cliInitFresh()
	h.cliAdd("A", "a", "--description", "first")
	h.cliAdd("B", "b", "--description", "second")
	h.cliAdd("C", "c", "--description", "third")

	code := h.run("--db", h.db, "task", "add-dep", "t-c", "--after", "t-a", "--kind", "suggests")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "added t-c ← t-a (suggests)") {
		t.Fatalf("add-dep shape:\n%s", h.out.String())
	}
	// The same pair twice is refused: the store never carries a duplicate edge.
	code = h.run("--db", h.db, "task", "add-dep", "t-c", "--after", "t-a")
	cliWantError(t, h, code, "already depends on")
	code = h.run("--db", h.db, "task", "add-dep", "t-c", "--after", "t-b")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "added t-c ← t-b (feeds_into)") {
		t.Fatalf("default-kind add-dep shape:\n%s", h.out.String())
	}
	// A cycle is refused by the graph laws, with the cause: the law follows
	// hard edges only — c waits on b through feeds_into, so making b wait on c
	// closes a hard loop.
	code = h.run("--db", h.db, "task", "add-dep", "t-b", "--after", "t-c")
	cliWantError(t, h, code, "dependency graph")

	code = h.run("--db", h.db, "task", "amend", "t-b", "--prepend", "NOTE: use jwt")
	cliWantCode(t, code, 0)
	if got := h.out.String(); got != "amended t-b\n" {
		t.Fatalf("amend shape %q, want \"amended t-b\\n\"", got)
	}
	code = h.run("--db", h.db, "show", "t-b")
	if !strings.Contains(h.out.String(), "description: NOTE: use jwt\n\nthird") &&
		!strings.HasPrefix(strings.SplitN(h.out.String(), "description: ", 2)[1], "NOTE: use jwt") {
		t.Fatalf("amend did not prepend:\n%s", h.out.String())
	}

	// insert with --before wires the new task between the two.
	code = h.run("--db", h.db, "task", "insert", "--after", "t-a", "--before", "t-b", "--title", "validate A", "--description", "check a")
	cliWantCode(t, code, 0)
	line := strings.TrimSpace(h.out.String())
	insertedID := strings.TrimPrefix(line, "inserted ")
	if !strings.HasPrefix(insertedID, "t-") || len(insertedID) != 8 {
		t.Fatalf("insert shape %q, want a t- id of six base-36 characters", h.out.String())
	}
	code = h.run("--db", h.db, "--json", "show", insertedID)
	cliWantCode(t, code, 0)
	value := h.cliJSON(t)
	if value["title"] != "validate A" {
		t.Fatalf("inserted task is %v", value["title"])
	}
	code = h.run("--db", h.db, "--json", "show", "t-b")
	cliWantCode(t, code, 0)
	value = h.cliJSON(t)
	deps, _ := value["dependencies"].([]any)
	// The insert took --after's place: t-b waits on the inserted task alone,
	// and the old direct t-a → t-b edge is gone rather than doubling up.
	if len(deps) != 1 || deps[0].(map[string]any)["task_id"] != insertedID {
		t.Fatalf("t-b should wait on the inserted task alone, got %v", deps)
	}
	if deps[0].(map[string]any)["task_id"] == "t-a" {
		t.Fatalf("the moved t-a → t-b edge is still on t-b: %v", deps)
	}

	// Notes and context, and the reading set over both.
	code = h.run("--db", h.db, "task", "note", "t-a", "the parser is done, reuse it")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "noted t-a (n-") {
		t.Fatalf("note shape:\n%s", h.out.String())
	}
	code = h.run("--db", h.db, "task", "notes", "t-a")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "the parser is done, reuse it") {
		t.Fatalf("notes missed the body:\n%s", h.out.String())
	}
	code = h.run("--db", h.db, "context", "chose sqlite for simplicity", "--kind", "decision")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "[decision]") {
		t.Fatalf("context shape:\n%s", h.out.String())
	}
	code = h.run("--db", h.db, "contexts")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "chose sqlite for simplicity") {
		t.Fatalf("contexts missed the entry:\n%s", h.out.String())
	}
	entryID := strings.Fields(h.out.String())[0]
	code = h.run("--db", h.db, "prune", entryID)
	cliWantCode(t, code, 0)
	code = h.run("--db", h.db, "contexts")
	if !strings.Contains(h.out.String(), "(no rows)") {
		t.Fatalf("prune left the entry behind:\n%s", h.out.String())
	}

	code = h.run("--db", h.db, "search", "parser")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "[note] n-") {
		t.Fatalf("search misses the note row:\n%s", h.out.String())
	}
	code = h.run("--db", h.db, "search", "first")
	if !strings.Contains(h.out.String(), "  [task] t-a generic A: first") {
		t.Fatalf("search task row shape:\n%s", h.out.String())
	}

	code = h.run("--db", h.db, "list")
	cliWantCode(t, code, 0)
	// The ready ladder reads truth: t-a has no hard upstream and is ready;
	// t-b, t-c and the inserted validator each wait on a hard dependency and
	// are pending. AddDep now demotes a ready task that gains a dependency,
	// so a row's bracket is the state its edges actually describe.
	if !strings.Contains(h.out.String(), "  t-a A [ready]") ||
		!strings.Contains(h.out.String(), "  t-b B [pending]") ||
		!strings.Contains(h.out.String(), "  t-c C [pending]") {
		t.Fatalf("list rows:\n%s", h.out.String())
	}
	code = h.run("--db", h.db, "list", "--kind", "research")
	if !strings.Contains(h.out.String(), "(no rows)") {
		t.Fatalf("list --kind filter:\n%s", h.out.String())
	}
	code = h.run("--db", h.db, "status")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "p-demo demo: 0/4 done (0%)") ||
		!strings.Contains(h.out.String(), "| ready: t-a") ||
		!strings.Contains(h.out.String(), "| blocked: 3") {
		t.Fatalf("status line:\n%s", h.out.String())
	}
	code = h.run("--db", h.db, "status", "--full")
	if !strings.Contains(h.out.String(), "├─ ○ t-a A [ready]") || !strings.Contains(h.out.String(), "dependencies:") {
		t.Fatalf("status --full tree:\n%s", h.out.String())
	}
	code = h.run("--db", h.db, "task", "overview")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "Project overview: 4 tasks") {
		t.Fatalf("overview header:\n%s", h.out.String())
	}
	code = h.run("--db", h.db, "critical-path")
	cliWantCode(t, code, 0)
	// CriticalPath seeds from every task with no hard upstream and follows
	// the edges: a → the inserted validator → b → c is this graph's longest
	// chain, four tasks long. An empty answer is for a plan with no chain,
	// and this one has one.
	if !strings.Contains(h.out.String(), "Critical path (4 tasks):") ||
		!strings.Contains(h.out.String(), "○ t-a A [ready]") {
		t.Fatalf("critical path:\n%s", h.out.String())
	}
	code = h.run("--db", h.db, "bottlenecks", "--limit", "1")
	cliWantCode(t, code, 0)
	// The count is direct dependents: the inserted validator holds up t-b, and
	// that is the one row the limit keeps.
	if !strings.Contains(h.out.String(), "Bottlenecks (tasks blocking the most downstream work):") ||
		!strings.Contains(h.out.String(), "— blocks 1 tasks [") {
		t.Fatalf("bottlenecks:\n%s", h.out.String())
	}
}

func TestPlandbCliInsertShapeAndRewrite(t *testing.T) {
	h := cliNewHarness(t)
	h.cliInitFresh()
	h.cliAdd("A", "a", "--description", "part a")
	h.cliAdd("B", "b", "--description", "part b", "--dep", "t-a")
	code := h.run("--db", h.db, "--json", "task", "insert", "--after", "t-a", "--before", "t-b", "--title", "validate A", "--description", "check a")
	cliWantCode(t, code, 0)
	value := h.cliJSON(t)
	id, _ := value["id"].(string)
	if !strings.HasPrefix(id, "t-") || value["title"] != "validate A" || value["status"] != "pending" {
		t.Fatalf("insert json task shape: %#v", value)
	}
	effect, _ := value["effect"].(map[string]any)
	if effect == nil {
		t.Fatalf("insert json carries no effect: %#v", value)
	}
	path, _ := effect["critical_path"].([]any)
	if len(path) != 3 || path[0] != "t-a" || path[1] != id || path[2] != "t-b" || effect["depth"] != float64(3) {
		t.Fatalf("insert effect critical path: %#v", effect)
	}
	if _, ok := value["project_state"].(map[string]any); !ok {
		t.Fatalf("insert json carries no project_state: %#v", value)
	}
	// The new task took --after's place: t-b waits on it alone, the old
	// direct t-a → t-b edge lifted rather than left doubled up.
	code = h.run("--db", h.db, "--json", "show", "t-b")
	cliWantCode(t, code, 0)
	value = h.cliJSON(t)
	deps, _ := value["dependencies"].([]any)
	if len(deps) != 1 || deps[0].(map[string]any)["task_id"] != id {
		t.Fatalf("t-b waits on %v, want only the inserted task", value["dependencies"])
	}
}

func TestPlandbCliReadingVerbJSONShapes(t *testing.T) {
	h := cliNewHarness(t)
	h.cliInitFresh()
	h.cliAdd("A", "a", "--description", "part a")
	h.cliAdd("B", "b", "--description", "part b", "--dep", "t-a")

	// critical-path --json: the chain as a length and a " > " path.
	code := h.run("--db", h.db, "--json", "critical-path")
	cliWantCode(t, code, 0)
	value := h.cliJSON(t)
	if value["length"] != float64(2) || value["path"] != "t-a > t-b" {
		t.Fatalf("critical-path json: %#v", value)
	}

	// bottlenecks --json: flat rows of {task_id,title,status,downstream_count}.
	code = h.run("--db", h.db, "--json", "bottlenecks")
	cliWantCode(t, code, 0)
	var rows []map[string]any
	if err := json.Unmarshal(h.out.Bytes(), &rows); err != nil {
		t.Fatalf("bottlenecks json is not an array: %v\n%s", err, h.out.String())
	}
	if len(rows) != 1 {
		t.Fatalf("bottlenecks rows %v, want the one task holding up work", rows)
	}
	row := rows[0]
	if row["task_id"] != "t-a" || row["title"] != "A" || row["status"] != "ready" || row["downstream_count"] != float64(1) {
		t.Fatalf("bottleneck row shape: %#v", row)
	}
	if _, nested := row["task"]; nested {
		t.Fatalf("bottleneck row should not nest the task: %#v", row)
	}

	// task overview --json: tasks, the dependency edges and a total.
	code = h.run("--db", h.db, "--json", "task", "overview")
	cliWantCode(t, code, 0)
	value = h.cliJSON(t)
	if value["total"] != float64(2) {
		t.Fatalf("overview total %v, want 2", value["total"])
	}
	tasks, _ := value["tasks"].([]any)
	if len(tasks) != 2 {
		t.Fatalf("overview tasks %v, want two", value["tasks"])
	}
	deps, _ := value["dependencies"].([]any)
	if len(deps) != 1 {
		t.Fatalf("overview dependencies %v, want one edge", value["dependencies"])
	}
	edge := deps[0].(map[string]any)
	if edge["from_task"] != "t-a" || edge["to_task"] != "t-b" || edge["kind"] != "feeds_into" || edge["condition"] != "All" || edge["id"] != float64(1) {
		t.Fatalf("overview edge shape: %#v", edge)
	}
	if _, present := edge["metadata"]; !present {
		t.Fatalf("overview edge carries no metadata key: %#v", edge)
	}

	// task note/notes --json name the task with its t- prefix.
	code = h.run("--db", h.db, "--json", "task", "note", "t-a", "the parser is done", "--agent", "w1")
	cliWantCode(t, code, 0)
	var note map[string]any
	if err := json.Unmarshal(h.out.Bytes(), &note); err != nil {
		t.Fatalf("note json is not an object: %v\n%s", err, h.out.String())
	}
	if note["task_id"] != "t-a" || note["content"] != "the parser is done" || note["agent_id"] != "w1" || !strings.HasPrefix(note["id"].(string), "n-") {
		t.Fatalf("note json shape: %#v", note)
	}
	code = h.run("--db", h.db, "--json", "task", "notes", "t-a")
	cliWantCode(t, code, 0)
	var notes []map[string]any
	if err := json.Unmarshal(h.out.Bytes(), &notes); err != nil {
		t.Fatalf("notes json is not an array: %v\n%s", err, h.out.String())
	}
	if len(notes) != 1 || notes[0]["task_id"] != "t-a" || notes[0]["content"] != "the parser is done" {
		t.Fatalf("notes json shape: %#v", notes)
	}
}

func TestPlandbCliGoAndDone(t *testing.T) {
	h := cliNewHarness(t)
	h.cliInitFresh()
	h.cliAdd("A", "a", "--description", "part a")
	h.cliAdd("B", "b", "--description", "part b", "--dep", "t-a")
	h.cliAdd("C", "c", "--description", "part c", "--dep", "t-b")

	// go claims the highest-priority ready leaf for the caller.
	code := h.run("--db", h.db, "--agent", "w1", "go")
	cliWantCode(t, code, 0)
	for _, want := range []string{
		// The bracket counts AFTER the claim: t-a is running now, so nothing is
		// ready and the two dependents are what is blocked.
		`→ t-a "A" [0/3 · 0 ready · 2 blocked]`,
		`downstream: t-b "B" (receives YOUR result)`,
		`actions: context "..." --kind discovery | search "query" | split --into "A, B" | done --next`,
	} {
		if !strings.Contains(h.out.String(), want) {
			t.Fatalf("go output misses %q:\n%s", want, h.out.String())
		}
	}
	// A second agent cannot take the claimed task: nothing else is ready.
	code = h.run("--db", h.db, "--agent", "w2", "go")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "nothing ready to claim") {
		t.Fatalf("second go took work it should not:\n%s", h.out.String())
	}
	// done under a stranger's name is refused by ownership.
	code = h.run("--db", h.db, "done", "t-a", "--agent", "w2", "--result", "x")
	cliWantError(t, h, code, "is owned by")
	// The owner finishes; the dependent is promoted and named downstream.
	code = h.run("--db", h.db, "done", "t-a", "--agent", "w1", "--result", `{"tables":"ok"}`)
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "✓ t-a done [1/3 · 1 ready · 1 blocked]") {
		t.Fatalf("done line:\n%s", h.out.String())
	}
	// Bare done with no running task says so, and names the agent it looked for.
	code = h.run("--db", h.db, "done", "--result", "x")
	cliWantError(t, h, code, "no running task found for agent 'default'. Specify task ID explicitly.")
	// done needs a result: that record is what the next worker reads.
	code = h.run("--db", h.db, "done", "t-b", "--agent", "w1")
	cliWantError(t, h, code, "done needs --result")
	// go for the next agent, then done --next completes and claims the next.
	code = h.run("--db", h.db, "--agent", "w2", "go")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), `→ t-b "B"`) {
		t.Fatalf("second claim:\n%s", h.out.String())
	}
	code = h.run("--db", h.db, "done", "--next", "--agent", "w2", "--result", "b done")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "✓ t-b done [2/3 · 1 ready · 0 blocked]") ||
		!strings.Contains(h.out.String(), `→ t-c "C" [2/3 · 0 ready · 0 blocked]`) {
		t.Fatalf("done --next output:\n%s", h.out.String())
	}
	// Finishing the last task closes the run.
	code = h.run("--db", h.db, "done", "t-c", "--agent", "w2", "--result", "c done")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "✓ t-c done [3/3 · 0 ready · 0 blocked]") ||
		!strings.Contains(h.out.String(), "all tasks complete!") {
		t.Fatalf("final done output:\n%s", h.out.String())
	}
}

func TestPlandbCliGoAndDoneJSON(t *testing.T) {
	h := cliNewHarness(t)
	h.cliInitFresh()
	h.cliAdd("A", "a", "--description", "part a")
	code := h.run("--db", h.db, "--json", "--agent", "w1", "go")
	cliWantCode(t, code, 0)
	value := h.cliJSON(t)
	if value["id"] != "t-a" || value["status"] != "running" || value["claimed_by"] != "w1" {
		t.Fatalf("go json: %#v", value)
	}
	code = h.run("--db", h.db, "--json", "done", "t-a", "--agent", "w1", "--result", "done a")
	cliWantCode(t, code, 0)
	value = h.cliJSON(t)
	if value["task"] == nil {
		t.Fatalf("done json carries no task: %#v", value)
	}
	task, _ := value["task"].(map[string]any)
	if task["status"] != "done" || task["result"] != "done a" {
		t.Fatalf("done json task: %#v", task)
	}
	// --json go with nothing ready answers null, the JSON of no claim.
	code = h.run("--db", h.db, "--json", "go")
	cliWantCode(t, code, 0)
	if strings.TrimSpace(h.out.String()) != "null" {
		t.Fatalf("empty go json %q, want null", h.out.String())
	}
}

func TestPlandbCliCancelAndWhatIf(t *testing.T) {
	h := cliNewHarness(t)
	h.cliInitFresh()
	h.cliAdd("A", "a", "--description", "first")
	h.cliAdd("B", "b", "--description", "second", "--dep", "t-a")
	h.cliAdd("C", "c", "--description", "third", "--dep", "t-b")
	h.cliAdd("D", "d", "--description", "free", "--dep", "t-a:suggests")

	// The preview names the cascade and writes nothing.
	code := h.run("--db", h.db, "what-if", "cancel", "t-a")
	cliWantCode(t, code, 0)
	for _, want := range []string{
		"what-if cancel t-a",
		`would cancel: t-a "A" [ready]`,
		"hard dependents:",
		`  t-b "B" [pending]`,
	} {
		if !strings.Contains(h.out.String(), want) {
			t.Fatalf("what-if output misses %q:\n%s", want, h.out.String())
		}
	}
	before := h.cliReadStore(t)
	code = h.run("--db", h.db, "--json", "what-if", "cancel", "t-a")
	cliWantCode(t, code, 0)
	value := h.cliJSON(t)
	dependents, _ := value["dependents"].([]any)
	// The preview is a fixpoint: cancelling t-a dooms t-b, and t-c waits on
	// t-b, so t-c is doomed with them.
	if len(dependents) != 2 || dependents[0] != "t-b" || dependents[1] != "t-c" {
		t.Fatalf("what-if json dependents %v, want [t-b t-c]", value["dependents"])
	}
	if value["total"] != float64(3) {
		t.Fatalf("what-if json total %v, want 3", value["total"])
	}
	if h.cliReadStore(t) != before {
		t.Fatalf("what-if wrote to the store")
	}
	// A suggests edge is not a hard dependent: D survives the cancel.
	code = h.run("--db", h.db, "task", "cancel", "t-a")
	cliWantCode(t, code, 0)
	if got := h.out.String(); got != "cancelled rows=3\n" {
		t.Fatalf("cancel shape %q, want \"cancelled rows=3\\n\"", got)
	}
	code = h.run("--db", h.db, "--json", "list", "--status", "cancelled")
	cliWantCode(t, code, 0)
	var rows []map[string]any
	if err := json.Unmarshal(h.out.Bytes(), &rows); err != nil {
		t.Fatalf("cancelled list is not a json array: %v\n%s", err, h.out.String())
	}
	if len(rows) != 3 {
		t.Fatalf("cancelled rows %v, want t-a, t-b and t-c", rows)
	}
	ids := map[string]bool{}
	for _, row := range rows {
		ids[row["id"].(string)] = true
	}
	if !ids["t-a"] || !ids["t-b"] || !ids["t-c"] {
		t.Fatalf("cancelled set %v, want t-a, t-b and t-c", ids)
	}
	// The suggests-dependent is untouched and still ready.
	code = h.run("--db", h.db, "show", "t-d")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "status: ○ ready") {
		t.Fatalf("t-d should be untouched:\n%s", h.out.String())
	}
	// The run itself is nobody's to cancel.
	code = h.run("--db", h.db, "task", "cancel", "root")
	cliWantError(t, h, code, "the harness owns the root task")
}

func TestPlandbCliPivot(t *testing.T) {
	h := cliNewHarness(t)
	h.cliInitFresh()
	h.cliAdd("parent", "p", "--description", "to be pivoted")
	h.cliAdd("old one", "old1", "--parent", "t-p")
	h.cliAdd("keeper", "keep1", "--parent", "t-p")
	// Complete one child through the store the way the runtime does, so
	// --keep-done has something to keep.
	st, err := Open(h.db, "", "", "", "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := st.Claim("keep1", "runtime"); err != nil {
		t.Fatalf("claim keeper: %v", err)
	}
	if _, err := st.Done("keep1", "runtime", "kept", nil, nil); err != nil {
		t.Fatalf("complete keeper: %v", err)
	}
	code := h.run("--db", h.db, "--json", "task", "pivot", "t-p", "--subtasks", `[{"title":"N1"},{"title":"N2","deps_on":["N1"]}]`)
	cliWantCode(t, code, 0)
	value := h.cliJSON(t)
	created, _ := value["created"].([]any)
	cancelled, _ := value["cancelled"].([]any)
	if len(created) != 2 || len(cancelled) != 1 || cancelled[0] != "t-old1" {
		t.Fatalf("pivot json created %v cancelled %v, want two created and t-old1 cancelled", value["created"], value["cancelled"])
	}
	if value["parent_task_id"] != "t-p" {
		t.Fatalf("pivot parent %v", value["parent_task_id"])
	}
	if _, ok := value["effect"].(map[string]any); !ok {
		t.Fatalf("pivot json carries no effect: %#v", value)
	}
	if kept, _ := value["kept"].([]any); len(kept) != 0 {
		t.Fatalf("pivot without --keep-done kept %v, want none", value["kept"])
	}
	// The completed child survives; the new children hang under the parent.
	code = h.run("--db", h.db, "show", "t-keep1")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "status: ✓ done") {
		t.Fatalf("keeper lost:\n%s", h.out.String())
	}
	code = h.run("--db", h.db, "task", "overview")
	cliWantCode(t, code, 0)
	if strings.Count(h.out.String(), "⊘") != 1 || strings.Count(h.out.String(), "·") != 1 {
		t.Fatalf("overview after pivot:\n%s", h.out.String())
	}
	// --keep-done leaves claimed and running work standing too.
	h.cliAdd("runner", "run1", "--parent", "t-p")
	if _, err := st.Claim("run1", "busy-agent"); err != nil {
		t.Fatalf("claim runner: %v", err)
	}
	code = h.run("--db", h.db, "--json", "task", "pivot", "t-p", "--subtasks", `[{"title":"M1"}]`, "--keep-done")
	cliWantCode(t, code, 0)
	value = h.cliJSON(t)
	cancelled, _ = value["cancelled"].([]any)
	for _, id := range cancelled {
		if id == "t-run1" {
			t.Fatalf("--keep-done cancelled the claimed task: %v", cancelled)
		}
	}
	kept, _ := value["kept"].([]any)
	hasKeeper := false
	for _, id := range kept {
		if id == "t-keep1" {
			hasKeeper = true
		}
	}
	if !hasKeeper {
		t.Fatalf("--keep-done did not report the kept task: %v", value["kept"])
	}
	// And without it, the claimed work is swept with the subtree.
	code = h.run("--db", h.db, "--json", "task", "pivot", "t-p", "--subtasks", `[{"title":"M3"}]`)
	cliWantCode(t, code, 0)
	value = h.cliJSON(t)
	cancelled, _ = value["cancelled"].([]any)
	swept := false
	for _, id := range cancelled {
		if id == "t-run1" {
			swept = true
		}
	}
	if !swept {
		t.Fatalf("pivot without --keep-done left the claimed task: %v", cancelled)
	}
}

func TestPlandbCliRefusals(t *testing.T) {
	h := cliNewHarness(t)
	h.cliInitFresh()
	before := h.cliReadStore(t)
	refused := [][]string{
		{"claim"}, {"start"}, {"fail"}, {"pause"}, {"next"}, {"heartbeat"}, {"progress"}, {"approve"},
		{"task", "claim"}, {"task", "start"}, {"task", "fail"}, {"task", "pause"},
		{"task", "next"}, {"task", "heartbeat"}, {"task", "progress"}, {"task", "approve"},
		{"use", "t-a"}, {"project", "list"}, {"project", "create", "x"},
		{"mcp"}, {"serve"}, {"watch"}, {"events"}, {"ahead"}, {"artifact", "put", "t-a"},
		{"export"}, {"import"},
	}
	for _, argv := range refused {
		// The refusal names the VERB — the word, and for the task and what-if
		// families the subcommand — never its arguments.
		joined := argv[0]
		if len(argv) >= 2 && (argv[0] == "task" || argv[0] == "what-if") {
			joined = argv[0] + " " + argv[1]
		}
		code := h.run(append([]string{"--db", h.db}, argv...)...)
		cliWantError(t, h, code, "Task lifecycle and scope are managed by the supervisor")
		if !strings.HasPrefix(h.errb.String(), "error: "+joined+":") {
			t.Fatalf("refusal does not name the verb %q: %s", joined, h.errb.String())
		}
	}
	// init is NOT refused — it is the one scope verb the CLI owns — and the
	// refusal pass left the store byte for byte.
	if h.cliReadStore(t) != before {
		t.Fatalf("a refused verb changed the store")
	}
	code := h.run("--db", t.TempDir()+"/fresh.json", "init", "fresh")
	cliWantCode(t, code, 0)
}

func TestPlandbCliStoreDiscovery(t *testing.T) {
	root := t.TempDir()
	db := filepath.Join(root, "plandb.db")
	if code := Main([]string{"--db", db, "init", "demo"}); code != 0 {
		t.Fatalf("seed init exited %d", code)
	}
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(deep)
	h := cliNewHarness(t)
	code := h.run("add", "found by the walk", "--as", "walked")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "created task t-walked") {
		t.Fatalf("walk-up add:\n%s%s", h.out.String(), h.errb.String())
	}
	t.Setenv("PLANDB_DB", filepath.Join(t.TempDir(), "missing.json"))
	code = h.run("add", "wrong store")
	cliWantError(t, h, code, "no plan store at")
	elsewhere := filepath.Join(t.TempDir(), "run", "plandb.db")
	if code := Main([]string{"--db", elsewhere, "init", "elsewhere"}); code != 0 {
		t.Fatalf("second init exited %d", code)
	}
	t.Setenv("PLANDB_DB", elsewhere)
	code = h.run("status")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "p-elsewhere") {
		t.Fatalf("status under PLANDB_DB found the wrong store:\n%s", h.out.String())
	}
	t.Setenv("PLANDB_DB", "")
	code = h.run("status")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "p-demo") {
		t.Fatalf("status after env reset found the wrong store:\n%s", h.out.String())
	}
	code = h.run("--project", "other", "status")
	cliWantError(t, h, code, "belongs to project")
}

func TestPlandbCliErrorsAndHelp(t *testing.T) {
	h := cliNewHarness(t)
	cliWantCode(t, h.run("--version"), 0)
	if got := strings.TrimSpace(h.out.String()); got != "plandb 0.3.0" {
		t.Fatalf("--version %q, want \"plandb 0.3.0\"", got)
	}
	cliWantCode(t, h.run("--help"), 0)
	if !strings.Contains(h.out.String(), "init NAME") || !strings.Contains(h.out.String(), "critical-path") {
		t.Fatalf("help misses the surface:\n%s", h.out.String())
	}
	cliWantCode(t, h.run(), 0)
	cliWantCode(t, h.run("help", "add"), 0)
	if !strings.Contains(h.out.String(), "plandb add TITLE") {
		t.Fatalf("help add:\n%s", h.out.String())
	}
	h9 := cliNewHarness(t)
	h9.cliInitFresh()
	cliWantError(t, h9, h9.run("--db", h9.db, "bogus-verb"), "unknown command")
	h2 := cliNewHarness(t)
	cliWantError(t, h2, h2.run("--db", h2.db, "add", "X", "--nope"), "unknown flag")
	h3 := cliNewHarness(t)
	cliWantError(t, h3, h3.run("--db", h3.db, "add", "X", "--description"), "needs a value")
	h4 := cliNewHarness(t)
	cliWantError(t, h4, h4.run("--db", filepath.Join(t.TempDir(), "gone.json"), "status"), "no plan store at")
	if !strings.Contains(h4.errb.String(), `plandb init`) {
		t.Fatalf("missing-store error does not say what to do:\n%s", h4.errb.String())
	}
}

func TestPlandbCliScanShapes(t *testing.T) {
	// The scanner itself: the doctrine's own sentences parse, and stdlib flag
	// shapes that cannot parse them stay unable to pretend otherwise.
	p, err := cliScan([]string{"add", "t", "--description", "d", "--dep", "a", "--dep", "b:suggests", "-c", "--json"})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(p.pos) != 2 || p.pos[0] != "add" || p.pos[1] != "t" {
		t.Fatalf("scan positionals %v", p.pos)
	}
	if p.vals["description"] != "d" || p.vals["dep"] != "" {
		t.Fatalf("scan values %#v", p.vals)
	}
	if len(p.lists["dep"]) != 2 || p.lists["dep"][1] != "b:suggests" {
		t.Fatalf("scan dep list %#v", p.lists["dep"])
	}
	if !p.bools["compact"] || !p.bools["json"] {
		t.Fatalf("scan bools %#v", p.bools)
	}
	if _, err := cliScan([]string{"--flag-without-value"}); err == nil {
		t.Fatalf("unknown flag accepted")
	}
	if _, err := cliScan([]string{"--db"}); err == nil {
		t.Fatalf("valueless --db accepted")
	}
	p, err = cliScan([]string{"show", "t-1", "--db", "x.json"})
	if err != nil || p.pos[0] != "show" || p.vals["db"] != "x.json" {
		t.Fatalf("flags after positionals: %#v err %v", p.pos, err)
	}
}
