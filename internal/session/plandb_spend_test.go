package session

// THE RUN'S SPEND LEDGER, AS TESTS.
//
// The plan store keeps a spend table (internal/plandb's AddSpend and its
// per-project rollups), and the bash belt's wiring feeds it two ways: every
// model call a plan-driven worker makes writes one row beside the usage
// ledger row it already wrote, and the per-step frame reads the rollup back
// against the conversation's spend rail. What is scripted is only the
// provider; the store, the rows and the frame are the real instruments.
//
// THE NODES ARE HAND-BUILT, the way [putChild] builds them, and never
// admitted: an admit starts the frontier's own runner beside the one the test
// drives, and the runner's worker would charge the same store. One node, one
// runner, one store per scenario.

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// spendNode grafts one node onto the graph by hand, in the state a runner
// reads, with the plan id the scenario asks for. Nothing admits it, so
// nothing but the caller's own worker ever runs it.
func spendNode(t *testing.T, graph *TaskGraph, planID string, parent uint64) *TaskNode {
	t.Helper()
	id := graph.reserve()
	if parent == 0 {
		parent = id
	}
	node := &TaskNode{graph: graph, id: id, spec: taskSpec{title: "spend the work", brief: "b", acceptance: "a", depth: 1, planID: planID}, state: TaskQueued, done: make(chan struct{})}
	if parent != id {
		node.parent = parent
	}
	graph.mu.Lock()
	graph.nodes[id] = node
	graph.order = append(graph.order, id)
	graph.mu.Unlock()
	return node
}

// spendWorker is the agent that IS a node's worker — the
// [bashBeltFrameFixture] shape — scripted, priced by its own wrapper, and
// built on the belt the flag names.
func spendWorker(t *testing.T, graph *TaskGraph, node *TaskNode, belt bool, steps []step) *Agent {
	t.Helper()
	child, err := newAgent(Config{
		Workspace: t.TempDir(),
		Model:     "test/model",
		System:    "SYSTEM",
		InTask:    true,
		tasker:    graph,
		taskID:    node.id,
		taskDepth: 1,
		// The allowance the real runner arms on every worker
		// ([Agent.newTaskAgentOn]); a scripted worker's bash calls are real
		// calls and stand under the same floor.
		ApprovalPolicy: &approval.Policy{Default: approval.ActionAllow},
		AskConsent:     false,
		bashBelt:       belt,
	}, pricedAt(&scriptedCompleter{steps: steps}, 0.05))
	if err != nil {
		t.Fatalf("newAgent for the spend worker: %v", err)
	}
	t.Cleanup(func() { _ = child.Close() })
	return child
}

// armSpendStore opens the run's store whole and puts it where the graph's
// pulse roads find it, the way [planSeed] leaves the graph armed — with the
// experiment's switch set, since the switch is what arms the wiring whole.
func armSpendStore(t *testing.T, graph *TaskGraph, project string) *plandb.Store {
	t.Helper()
	t.Setenv("CODEAF_TASK_BELT", "bash")
	path := filepath.Join(t.TempDir(), planStoreFilename)
	store, err := plandb.Open(path, project, planRootID, project, "")
	if err != nil {
		t.Fatalf("plan store for the run: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	graph.planMu.Lock()
	graph.plan = &planState{path: store.Path()}
	graph.planMu.Unlock()
	return store
}

// spendRow is one row of the store's spend table, read back raw so the
// assertions name the fields the wiring is meant to fill: the task, the
// model, the role, the money and the tokens.
type spendRow struct {
	taskID string
	model  string
	role   string
	usd    float64
	inTok  int
	outTok int
}

// readSpendRows reads the store's spend table whole, oldest first. The read
// is its own read-only connection, so it never races the writer for the
// store's own handle — WAL lets a reader run while a writer holds the lock.
func readSpendRows(t *testing.T, store *plandb.Store) []spendRow {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+store.Path()+"?mode=ro")
	if err != nil {
		t.Fatalf("open the store read-only: %v", err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT task_id, model, role, usd, in_tokens, out_tokens FROM spend ORDER BY at, rowid`)
	if err != nil {
		t.Fatalf("read the spend table: %v", err)
	}
	defer rows.Close()
	var ledger []spendRow
	for rows.Next() {
		var row spendRow
		if err := rows.Scan(&row.taskID, &row.model, &row.role, &row.usd, &row.inTok, &row.outTok); err != nil {
			t.Fatalf("scan a spend row: %v", err)
		}
		ledger = append(ledger, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the spend table: %v", err)
	}
	return ledger
}

// waitSpendRows waits for the writes to land: recordPlanSpend is best-effort
// and off the call's road, so a row arrives on a goroutine the test waits
// for rather than counts behind the call that made it.
func waitSpendRows(t *testing.T, store *plandb.Store, want int) []spendRow {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		rows := readSpendRows(t, store)
		if len(rows) >= want {
			return rows
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d spend rows landed, want %d (last read: %#v)", len(rows), want, rows)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitNoSpendRows holds the same patience against the silence: a worker that
// must write nothing is given the same window a worker writing rows gets,
// because absence is only proved by waiting for the writes not to come.
func waitNoSpendRows(t *testing.T, store *plandb.Store) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if rows := readSpendRows(t, store); len(rows) != 0 {
			t.Fatalf("a worker that writes nothing wrote %#v", rows)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A WORKER WITH A PLAN TASK CHARGES EVERY CALL TO IT, and the role word is
// what the node was doing at that instant: 'work' while it is a leaf, 'plan'
// from the moment it has plan children of its own. The task id is the plan
// task's, the model is the call's, and the money and tokens are the call's
// own figures. A worker without a plan task writes nothing, and so does a
// flag-off worker, whatever its node carries.
func TestSpendRowsFollowThePlanNode(t *testing.T) {
	// TWO CALLS: one bash step and the text that ends the turn, each priced
	// by the scripted model's own wrapper.
	steps := bashSteps([]string{"true"})
	run := func(node *TaskNode, child *Agent) {
		t.Helper()
		room := node.openRoom()
		room.speaking(child)
		if _, _, err := runTaskChild(context.Background(), child, node, "do the work", child.config.Workspace,
			taskLimits{maxSteps: taskMaxSteps, noProgress: taskNoProgress}, room, io.Discard); err != nil {
			t.Fatalf("runTaskChild: %v", err)
		}
	}

	// THE LEAF'S ROLE IS 'WORK'.
	session, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := session.graph()
	store := armSpendStore(t, graph, "spend the work")
	node := spendNode(t, graph, planRootID, 0)
	child := spendWorker(t, graph, node, true, steps)
	run(node, child)
	rows := waitSpendRows(t, store, 2)
	if len(rows) != 2 {
		t.Fatalf("%d spend rows for two calls, want exactly 2", len(rows))
	}
	for index, row := range rows {
		if row.taskID != planRootID {
			t.Errorf("row %d charged %q, want the plan task %q", index, row.taskID, planRootID)
		}
		if row.model != "test/model" {
			t.Errorf("row %d names the model %q, want test/model", index, row.model)
		}
		if row.role != "work" {
			t.Errorf("row %d carries the role %q, want work from a leaf", index, row.role)
		}
		if row.usd != 0.05 || row.inTok <= 0 || row.outTok <= 0 {
			t.Errorf("row %d carries %#v, want the call's own $0.05 and its own token counts", index, row)
		}
	}

	// THE ROLE OF A NODE WITH PLAN CHILDREN IS 'PLAN', read at the moment of
	// the call — the child is on the graph before the worker's first call
	// here, and the rows know it. The child is hand-built settled with its
	// news already handed over (noted), so the runner's fold reads it as
	// taken news and never parks on it.
	psession, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	pgraph := psession.graph()
	pstore := armSpendStore(t, pgraph, "spend the work")
	pnode := spendNode(t, pgraph, planRootID, 0)
	planChild := spendNode(t, pgraph, "child-1", pnode.id)
	pnode.graph.mu.Lock()
	planChild.state = TaskDone
	planChild.noted = true
	close(planChild.done)
	pnode.graph.mu.Unlock()
	pchild := spendWorker(t, pgraph, pnode, true, steps)
	run(pnode, pchild)
	for index, row := range waitSpendRows(t, pstore, 2) {
		if row.taskID != planRootID {
			t.Errorf("row %d charged %q, want the plan task %q", index, row.taskID, planRootID)
		}
		if row.role != "plan" {
			t.Errorf("row %d carries the role %q, want plan from a node with plan children", index, row.role)
		}
	}

	// AND THE ROLLUP JOINS: the rows carry the root's project tag, so the
	// store's per-project total is the run's whole bill.
	if total := store.Summary().ProjectSpend["spend the work"]; total.Calls != 2 || total.USD != 0.1 {
		t.Fatalf("the run's project rollup = %#v, want 2 calls and $0.10", total)
	}

	// A WORKER WITHOUT A PLAN TASK WRITES NOTHING: the belt is on and the
	// store is armed, and the node it is has nothing to charge.
	nsession, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	ngraph := nsession.graph()
	nstore := armSpendStore(t, ngraph, "spend the work")
	nnode := spendNode(t, ngraph, "", 0)
	nchild := spendWorker(t, ngraph, nnode, true, steps)
	run(nnode, nchild)
	waitNoSpendRows(t, nstore)

	// AND SO DOES THE FLAG-OFF WORKER, whatever its node carries: the switch
	// gates the wiring whole, so with it off nothing is armed and not one row
	// is written anywhere — not by this worker, and not by the store road.
	t.Setenv("CODEAF_TASK_BELT", "node")
	osession, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	ograph := osession.graph()
	path := filepath.Join(t.TempDir(), planStoreFilename)
	ostore, err := plandb.Open(path, "spend the work", planRootID, "spend the work", "")
	if err != nil {
		t.Fatalf("plan store for the flag-off run: %v", err)
	}
	t.Cleanup(func() { _ = ostore.Close() })
	onode := spendNode(t, ograph, planRootID, 0)
	ochild := spendWorker(t, ograph, onode, false, steps)
	run(onode, ochild)
	waitNoSpendRows(t, ostore)
}

// PLAN SPEND ROLLS THE RUN'S LEDGER UP BY SEAT: one line per role, dearest
// first, each naming the model that seat spent most of its money through. A
// row priced at nothing is an unpriced one and not a free call, a store's rows
// are this chat's own, and a conversation with no store answers nil.
func TestPlanSpendRollsUpBySeat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	store, err := plandb.Open(path, "the run", planRootID, "The run", "drive the plan", "chat-a")
	if err != nil {
		t.Fatalf("open the plan store: %v", err)
	}
	if _, err := store.AddMany([]plandb.TaskSpec{
		{ID: "alpha", Title: "Alpha"},
		{ID: "beta", Title: "Beta"},
	}); err != nil {
		t.Fatalf("seed the store: %v", err)
	}
	charge := func(id, model, role string, usd float64) {
		t.Helper()
		if err := store.AddSpend(id, model, role, usd, 10, 5); err != nil {
			t.Fatalf("charge %s: %v", id, err)
		}
	}
	// THE PLAN SEAT THROUGH TWO MODELS, so the model the line names is the one
	// most of that seat's money went through; the work seat through one.
	charge("alpha", "vendor/deep", "plan", 1.00)
	charge("beta", "vendor/deep", "plan", 0.25)
	charge("alpha", "vendor/wide", "plan", 0.10)
	charge("beta", "vendor/deep", "work", 0.40)
	// AND A ROW PRICED AT NOTHING IS LEFT OUT OF THE ROLLUP WHOLE.
	charge("beta", "vendor/deep", "work", 0)
	if err := store.Close(); err != nil {
		t.Fatalf("close the seeding handle: %v", err)
	}

	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	lines := agent.PlanSpend(time.Time{})
	if len(lines) != 2 {
		t.Fatalf("PlanSpend = %d lines, want 2 (%#v)", len(lines), lines)
	}
	plan, work := lines[0], lines[1]
	if plan.Seat != "plan" || plan.USD != 1.35 || plan.Calls != 3 {
		t.Errorf("the dearest seat = %#v, want plan with 3 calls and $1.35", plan)
	}
	if plan.Model != "vendor/deep" {
		t.Errorf("the plan seat names %q, want the model most of its money went through (vendor/deep)", plan.Model)
	}
	if work.Seat != "work" || work.USD != 0.40 || work.Calls != 1 {
		t.Errorf("the second seat = %#v, want work with 1 call and $0.40", work)
	}

	// A CONVERSATION WITH NO PLAN ANSWERS NIL, and so does a store whose rows
	// are another chat's: the emptiness is "nothing of mine", never a zero line.
	blank, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	t.Setenv("CODEAF_TASK_BELT", "bash")
	if lines := blank.PlanSpend(time.Time{}); lines != nil {
		t.Fatalf("PlanSpend with no store = %#v, want nil", lines)
	}
	other, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, other, path, "chat-b")
	if lines := other.PlanSpend(time.Time{}); lines != nil {
		t.Fatalf("PlanSpend for another chat = %#v, want nil", lines)
	}

	// AND A WINDOW CUTS EVERY ROW THAT BEGAN BEFORE IT. A window that opened an
	// hour ago is before every charge here and so keeps both seats — which is the
	// half that proves the ledger's written moment was PARSED, since a moment that
	// failed to parse is cut by the same branch and would answer nothing here too.
	// A window opening an hour from now is after every charge, so the rollup
	// answers nothing rather than a stale figure.
	if lines := agent.PlanSpend(time.Now().Add(-time.Hour)); len(lines) != 2 {
		t.Fatalf("PlanSpend over a window that opened an hour ago = %d lines, want the ledger's own 2 (%#v)", len(lines), lines)
	}
	if lines := agent.PlanSpend(time.Now().Add(time.Hour)); lines != nil {
		t.Fatalf("PlanSpend over a future window = %#v, want nil", lines)
	}
}

// THE FRAME READS THE RUN'S ROLLUP AGAINST THE CONVERSATION'S RAIL when a
// limit is set — in place of the session-only figure — and keeps the figure
// it has always drawn when no limit was ever given. The rollup is the store's
// per-project total, the same reading the CLI's status makes.
func TestSpendFrameChargesTheRunAgainstTheLimit(t *testing.T) {
	// THE OWNER CARRIES THE RAIL: the conversation that owns the run is where
	// a person set one, and the frame reads it from there.
	session, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SpendRailUSD = 2
	})
	graph := session.graph()
	store := armSpendStore(t, graph, "frame the work")
	if err := store.AddSpend(planRootID, "model-x", "work", 1.25, 10, 5); err != nil {
		t.Fatalf("charge the run: %v", err)
	}
	node := spendNode(t, graph, planRootID, 0)
	child := spendWorker(t, graph, node, true, nil)
	room := node.openRoom()
	room.speaking(child)
	room.live.steps = 5
	child.mu.Lock()
	child.usage = Usage{CostUSD: 0.183}
	child.mu.Unlock()

	// THE ROLLUP WINS: $1.25 is the run's bill from the store, not this
	// worker's $0.18, and the limit is the rail the conversation carries.
	if frame := child.bashBeltFrame(nil); !strings.Contains(frame, "spent $1.25 of $2.00") || strings.Contains(frame, "so far") {
		t.Fatalf("the frame does not charge the run against the limit: %q", frame)
	}

	// AND A RUN THE STORE HAS NEVER CHARGED falls back to the session-only
	// figure rather than drawing a limit without a number behind it.
	uncharged := armSpendStore(t, graph, "frame the work")
	if total := uncharged.Summary().ProjectSpend; len(total) != 0 {
		t.Fatalf("the uncharged store carried %#v", total)
	}
	if frame := child.bashBeltFrame(nil); !strings.Contains(frame, "spent $0.18 of $2.00") {
		t.Fatalf("an uncharged run does not fall back to the session figure: %q", frame)
	}

	// AND WITH NO LIMIT SET the clause is the one it has always been: this
	// node's own spend, so far.
	graph.planMu.Lock()
	graph.plan = nil
	graph.planMu.Unlock()
	session.mu.Lock()
	session.config.SpendRailUSD = 0
	session.mu.Unlock()
	wantSpend := fmt.Sprintf(" · $%.2f so far", node.spend())
	if frame := child.bashBeltFrame(nil); !strings.Contains(frame, wantSpend) || strings.Contains(frame, "of $") {
		t.Fatalf("a frame without a limit is not the clause it has always been: %q", frame)
	}
}
