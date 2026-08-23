package session

// WHAT THE WORKERS UNDER A RUN ARE CALLED, and what the run's own row says in
// the minute before there are any.
//
// Both come off one screen. A run divided into nine workers drew nine rows, and
// every one of them read "You are a" — three words off the front of the brief
// that worker opens on, because nothing on a planned node was a NAME. And above
// them the run's own row sat blank for the whole opening planner call, so the
// interval where the shape of the work was being decided looked exactly like an
// interval where nothing was happening.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// plannerOnce puts one set of workers on the frontier and then says nothing,
// which is the shape of a real opening call followed by NOOPs.
type plannerOnce struct {
	mu   sync.Mutex
	add  []orchestrate.Node
	said bool
}

func (p *plannerOnce) Plan(context.Context, orchestrate.View) (orchestrate.Amendment, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.said {
		return orchestrate.Amendment{}, nil
	}
	p.said = true
	return orchestrate.Amendment{Add: p.add}, nil
}

// heldExec starts every worker and finishes none, so the tree can be read with
// the run genuinely in flight rather than raced against its own ending.
type heldExec struct{ held chan struct{} }

func (e heldExec) Exec(ctx context.Context, _ orchestrate.Node, _ []orchestrate.NodeStatus) (string, float64, error) {
	select {
	case <-e.held:
	case <-ctx.Done():
	}
	return "", 0, ctx.Err()
}

// planned is one node of a run as its snapshot carries it: a slug, the name the
// planner gave the slice, and the second-person brief its worker opens on.
//
// THE TITLE IS SETTLED THE WAY THE FRONTIER SETTLES IT, because that is the only
// way a node ever reaches a family: internal/orchestrate's apply is the one
// writer of the frontier and it fills the field there, once, so a snapshot with
// a raw empty title is a shape that exists nowhere but a test that built one.
func planned(id, title, goal string, state orchestrate.State) orchestrate.NodeStatus {
	node := orchestrate.Node{ID: id, Title: title, Goal: goal}
	node.Title = orchestrate.NodeTitle(node)
	return orchestrate.NodeStatus{Node: node, State: state}
}

// THE ROW IS THE NAME THE PLANNER WROTE. Every surface that draws a worker reads
// the title this publishes, so this is the one assertion the reported defect
// turns on.
func TestAWorkerUnderARunIsCalledWhatItWasNamedAndNeverItsBrief(t *testing.T) {
	agent, updates := familyAgent(t)
	family := agent.newOrchestrateFamily("competitive intelligence on the three vendors", "cheap/model", "run-1")

	family.upsert([]orchestrate.NodeStatus{
		planned("pricing-sheet", "pricing sheet",
			"You are a competitive intelligence analyst. Collect every published price for the three vendors.",
			orchestrate.Running),
		planned("feature-matrix", "feature matrix",
			"You are assembling a feature matrix across the three vendors.",
			orchestrate.Running),
	})

	var workers []TaskNotice
	for _, notice := range familyNotices(t, updates) {
		if notice.Node != "" {
			workers = append(workers, notice)
		}
	}
	if len(workers) != 2 {
		t.Fatalf("%d worker rows published: %+v", len(workers), workers)
	}
	want := map[string]string{"pricing-sheet": "pricing sheet", "feature-matrix": "feature matrix"}
	for _, worker := range workers {
		if worker.Title != want[worker.Node] {
			t.Errorf("worker %q is called %q, want %q", worker.Node, worker.Title, want[worker.Node])
		}
		if strings.HasPrefix(worker.Title, "You are") {
			t.Errorf("worker %q was named out of its brief: %q", worker.Node, worker.Title)
		}
	}
}

// AND A WORKER NOBODY NAMED IS NAMED MECHANICALLY, from the slug it already had.
// This is the path an older planner, or a reply that lost the key, takes — and
// it must not fall back through the goal on its way.
func TestAWorkerNobodyNamedIsCalledAfterItsSlug(t *testing.T) {
	agent, updates := familyAgent(t)
	family := agent.newOrchestrateFamily("competitive intelligence on the three vendors", "", "run-1")

	family.upsert([]orchestrate.NodeStatus{
		planned("pricing-sheet", "",
			"You are a competitive intelligence analyst. Collect every published price for the three vendors.",
			orchestrate.Running),
	})

	for _, notice := range familyNotices(t, updates) {
		if notice.Node == "" {
			continue
		}
		if notice.Title != "pricing sheet" {
			t.Fatalf("an unnamed worker is called %q, want its slug spelled out", notice.Title)
		}
	}
}

// THE SAME NAME REACHES THE WORK TREE, which is the other door a surface reads
// live work through — the rail's worker preview and the machine's pulse both
// come off it.
func TestTheWorkTreeDrawsAWorkersNameAndNotItsBrief(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	// A real run, held open: the frontier is only written by the planner, so the
	// two workers are put there by one and then kept alive by an executor that
	// does not return until the test has read the tree.
	held := make(chan struct{})
	t.Cleanup(func() { close(held) })
	planner := plannerOnce{add: []orchestrate.Node{
		{ID: "pricing-sheet", Title: "pricing sheet",
			Goal: "You are a competitive intelligence analyst. Collect every published price for the three vendors."},
		{ID: "feature-matrix",
			Goal: "You are assembling a feature matrix across the three vendors."},
	}}
	run := orchestrate.New("competitive intelligence on the three vendors",
		&planner, heldExec{held: held}, orchestrate.Options{Cap: 1, Lanes: 2})
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	go run.Run(ctx) //nolint:errcheck // the run is cancelled by the test, not read
	agent.mu.Lock()
	agent.orchestrations = map[string]*orchestration{"1": {run: run, cancel: stop, born: time.Now()}}
	agent.mu.Unlock()

	var tree []WorkNode
	for waited := time.Duration(0); waited < 5*time.Second; waited += 10 * time.Millisecond {
		if tree = agent.WorkingNow(); len(tree) == 1 && len(tree[0].Children) == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(tree) != 1 || len(tree[0].Children) != 2 {
		t.Fatalf("the tree is %+v", tree)
	}
	if tree[0].Children[0].Title != "pricing sheet" {
		t.Fatalf("the first worker is called %q", tree[0].Children[0].Title)
	}
	if tree[0].Children[1].Title != "feature matrix" {
		t.Fatalf("the unnamed worker is called %q, want its slug spelled out", tree[0].Children[1].Title)
	}
	for _, worker := range tree[0].Children {
		if strings.HasPrefix(worker.Title, "You are") {
			t.Fatalf("a worker in the tree was named out of its brief: %q", worker.Title)
		}
	}
}

// ── the minute before there is anything to look at ──────────────────────────

// THE RUN'S ROW SAYS THE WORK IS BEING FORMED, from the moment it is minted —
// which is before the opening planner call, the one call in a run with no work
// to overlap it.
func TestARunSaysItIsFormingBeforeItHasAnyWorkers(t *testing.T) {
	agent, updates := familyAgent(t)
	agent.newOrchestrateFamily("competitive intelligence on the three vendors", "cheap/model", "run-1")

	notices := familyNotices(t, updates)
	if len(notices) != 1 {
		t.Fatalf("%d rows published for a run nobody has planned yet: %+v", len(notices), notices)
	}
	if notices[0].Doing != orchestrateForming {
		t.Fatalf("the run's row says %q, want %q", notices[0].Doing, orchestrateForming)
	}
	if notices[0].State != TaskRunning {
		t.Fatalf("the run's row is %s", notices[0].State)
	}
}

// AND IT STOPS SAYING IT THE MOMENT THE WORKERS EXIST. Nothing lingers: from
// there the rows themselves are the picture, and the fold they already sit under
// is the collapsed view of them.
func TestTheFormingLineComesOffTheRowWhenTheWorkersLand(t *testing.T) {
	agent, updates := familyAgent(t)
	family := agent.newOrchestrateFamily("competitive intelligence on the three vendors", "cheap/model", "run-1")
	familyNotices(t, updates)

	family.upsert([]orchestrate.NodeStatus{
		planned("pricing-sheet", "pricing sheet", "You are a competitive intelligence analyst.", orchestrate.Running),
	})

	var last *TaskNotice
	for _, notice := range familyNotices(t, updates) {
		if notice.Node != "" {
			continue
		}
		row := notice
		last = &row
	}
	if last == nil {
		t.Fatal("the run's own row was never republished once its workers landed")
	}
	if last.Doing != "" {
		t.Fatalf("the run's row still says %q with its workers on the board", last.Doing)
	}

	// AND IT IS SAID ONCE. A second graph moving over the same workers is not the
	// work being formed again.
	family.upsert([]orchestrate.NodeStatus{
		planned("pricing-sheet", "pricing sheet", "You are a competitive intelligence analyst.", orchestrate.Done),
	})
	for _, notice := range familyNotices(t, updates) {
		if notice.Node == "" {
			t.Fatalf("the run's row was published again for a landing: %+v", notice)
		}
	}
}

// ── the workers a planner filed under a number ──────────────────────────────
//
// The second screen, and it is the one the slug fallback above could not answer.
// A live run drew `r1` through `r7` down the rail with `synth` under them: a
// planner numbered its own list, and an id with no words in it spells out as
// itself. So a node whose title is missing or is nothing but its own id goes to
// the same small namer every other piece of work here is named by, and until it
// answers the row is drawn as nothing rather than as the machine's filing.

// A WORKER WAITING FOR ITS NAME IS DRAWN AS NOTHING, NEVER AS ITS ID. This is
// the shape the frontier publishes while the namer is out (internal/orchestrate's
// apply), and `r1` on the row is exactly what the whole seam exists to prevent.
func TestAWorkerWaitingForItsNameIsNeverDrawnAsItsId(t *testing.T) {
	agent, updates := familyAgent(t)
	family := agent.newOrchestrateFamily("competitive intelligence on the three vendors", "cheap/model", "run-1")

	pending := orchestrate.NodeStatus{
		Node: orchestrate.Node{ID: "r1",
			Goal: "You are a competitive intelligence analyst. Collect every published price for the three vendors."},
		State: orchestrate.Running,
	}
	family.upsert([]orchestrate.NodeStatus{pending})

	var drawn []TaskNotice
	for _, notice := range familyNotices(t, updates) {
		if notice.Node != "" {
			drawn = append(drawn, notice)
		}
	}
	if len(drawn) != 1 {
		t.Fatalf("%d worker rows published: %+v", len(drawn), drawn)
	}
	if drawn[0].Title != "" {
		t.Fatalf("a worker whose name has not landed is drawn as %q, want nothing at all", drawn[0].Title)
	}

	// AND THE NAME LANDING REDRAWS THE ROW, with nothing about the work itself
	// moved. A node sitting on the frontier behind its needs would otherwise wait
	// for its next transition to learn what it is called, which for the last node
	// of a wide run is the whole run.
	named := pending
	named.Title = "vendor pricing"
	family.upsert([]orchestrate.NodeStatus{named})

	var renames []TaskNotice
	for _, notice := range familyNotices(t, updates) {
		if notice.Node != "" {
			renames = append(renames, notice)
		}
	}
	if len(renames) != 1 || renames[0].Title != "vendor pricing" {
		t.Fatalf("the name landing published %+v, want one row called \"vendor pricing\"", renames)
	}
	if renames[0].ID != drawn[0].ID {
		t.Fatalf("the name landed on row %d, and the row it belongs to is %d", renames[0].ID, drawn[0].ID)
	}

	// AND THE SAME GRAPH ARRIVING AGAIN IS NOT NEWS. A rename is published once,
	// exactly as a transition is.
	family.upsert([]orchestrate.NodeStatus{named})
	for _, notice := range familyNotices(t, updates) {
		if notice.Node != "" {
			t.Fatalf("a row was republished for a graph that did not move: %+v", notice)
		}
	}
}

// AND THE NAME COMES OFF THE SAME CALL EVERY OTHER PIECE OF WORK IS NAMED BY:
// the namer's own system line, the cheap tier, the answer cleaned by the hand
// that cleans an admitted task's (taskname.go). One naming door, so a run's
// workers and a `/task` stand in the same column reading alike.
func TestARunsWorkerIsNamedByTheOneNamerEverythingElseIsNamedBy(t *testing.T) {
	client := &scriptedCompleter{steps: []step{func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		if !isNameCall(messages) {
			t.Errorf("the run's worker was named by some other call: %q", messageContentText(messages[0]))
		}
		return textResponse("Title: vendor pricing"), nil
	}}}
	agent, _ := newTestAgent(t, client, func(c *Config) { c.RolesSource = nameSettings() })
	family := agent.newOrchestrateFamily("competitive intelligence on the three vendors", "cheap/model", "run-1")

	name := family.nameWorker(context.Background(), orchestrate.Node{ID: "r1",
		Goal: "You are a competitive intelligence analyst. Collect every published price for the three vendors."})
	if name != "vendor pricing" {
		t.Fatalf("the worker is named %q, want the cleaned answer", name)
	}
	if got := client.model(0); got != "cheap/model" {
		t.Fatalf("the worker's name was bought on %q, want the cheap tier", got)
	}
}

// AND THE RUN'S OWN ROW IS NAMED THE SAME WAY. It is the row every worker hangs
// under, it starts life as the first line of the goal, and a goal is a sentence.
func TestTheRunsOwnRowIsNamedByTheSameCall(t *testing.T) {
	client := &scriptedCompleter{steps: []step{func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		if !isNameCall(messages) {
			t.Errorf("the run was named by some other call: %q", messageContentText(messages[0]))
		}
		return textResponse("vendor intelligence"), nil
	}}}
	agent, _ := newTestAgent(t, client, func(c *Config) { c.RolesSource = nameSettings() })
	feed := agent.TaskUpdates()
	const goal = "look into the three vendors and tell me where each of them is strongest"
	family := agent.newOrchestrateFamily(goal, "cheap/model", "run-1")
	family.nameRun(goal)

	if !nameLanded(func() bool {
		family.mu.Lock()
		defer family.mu.Unlock()
		return family.title == "vendor intelligence"
	}) {
		t.Fatalf("the run is still called %q", family.title)
	}
	if !waitForNamedRow(feed, family.root, "vendor intelligence") {
		t.Fatal("no update carried the run's new name")
	}
}
