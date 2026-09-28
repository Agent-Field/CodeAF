package session

// THE PLAN PULSE, AS TESTS (docs/design/plandb-cli/DESIGN.md, the wiring
// section). Behind CODEAF_TASK_BELT=bash
// the worker's `plandb` calls and the runtime share one store, and the pulse
// is the two moments the two roads meet: a node that lands writes its ending
// back to the store task it was dispatched as, and a store task that became
// ready becomes a node the frontier starts. Every test here drives the pulse
// by hand — it is public in the package for exactly that — against a real
// store at a real path, grown only through the store's own API: a
// hand-written JSON file would be a second store format, which is the named
// wrong answer of the whole wave.
//
// DETERMINISM IS THE CHANNEL, NOT THE CLOCK. Every worker is held on a lane
// by its own completer — one channel per plan task — so a node is running
// exactly when the test says so, lands exactly when the test releases it, and
// the passes between are the test's own calls. Nothing here sleeps and hopes.
//
// TWO FACTS OF THE WIRING THESE TESTS PIN RATHER THAN PRESUME, both noted at
// the assertions that meet them and reported with this wave: the runtime's
// claim — the task's own id as agent, the supervisor's own trick — is
// taken at WRITEBACK (plandb_plan.go's planSettleStoreTask), not at dispatch
// as DESIGN.md's wiring section says; and a landing's own pulse reads the
// landing node as still running, because the node's final state is written by
// [TaskGraph.complete] after [Agent.workTaskNode] returns — so the pass that
// writes a node back is the one AFTER its landing. In the live loop that pass
// is the parent's next bash call; here it is the test's.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// ── the lane completer ───────────────────────────────────────────────────────

// planLineMark opens the plan line every plan-born node's brief carries, the
// line the seeded work order composes. It is the one mark that tells a node
// worker's context from the conversation's — the store's id in the CLI's own
// spelling — and it is what the lanes below are keyed by.
const planLineMark = "YOUR TASK IN THE PLAN IS t-"

// planLaneCompleter is one provider serving every lane the run has: the
// conversation that proposes the run's task, and each node's worker, in the
// shape [routedCompleter] built for this experiment. The errands beside the
// work — the task namer, the narrator, the session namer — are answered
// before any lane is touched, because a namer that landed in a work lane
// would take the step the test scripted for the worker. A work lane is then
// HELD on its own channel until the test releases it, which is what makes a
// node's landing a thing the test does rather than a thing the test watches
// for.
type planLaneCompleter struct {
	mu     sync.Mutex
	parent []step
	seen   int
	// requests keeps every call a work lane took, in arrival order, keyed by
	// the plan id the call's context named. The worker's opening document is
	// the only place the composed brief can be observed from the outside.
	requests map[string][][]ai.Message
	// arrived is closed on a lane's first call, so a test can wait for the
	// worker to actually be parked in its turn before it releases the lane.
	// It is created on either side — by the call, or by the waiter — under the
	// one lock, so neither can miss the other.
	arrived map[string]chan struct{}
	// announced guards that close: a lane whose worker is asked twice (a
	// turn that ran on, a retried call) must not close its channel twice.
	announced map[string]bool
	// lanes is the hold itself: one unbuffered channel per plan id, carrying
	// the prose the released worker answers — the worker's whole account, and
	// therefore the report its node lands with.
	lanes map[string]chan string
	// released marks a lane the test has already ended, with the prose it
	// answered. A landing's notes WAKE a worker whose turn is over — a fresh
	// call whose history still carries the plan line — and a completer that
	// parked it again would hold the node open on a channel nothing more will
	// send to. Every call after the release answers the same prose, so the
	// woken turn ends and the node lands.
	released map[string]bool
	prose    map[string]string
}

func newPlanLaneCompleter(parent []step) *planLaneCompleter {
	return &planLaneCompleter{
		parent:    parent,
		requests:  map[string][][]ai.Message{},
		arrived:   map[string]chan struct{}{},
		announced: map[string]bool{},
		lanes:     map[string]chan string{},
		released:  map[string]bool{},
		prose:     map[string]string{},
	}
}

func (c *planLaneCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	snapshot := make([]ai.Message, len(messages))
	copy(snapshot, messages)
	if isNameCall(snapshot) || isCaptionCall(snapshot) || isTitleCall(snapshot) {
		return textResponse(""), nil
	}
	// THE END-OF-TURN READING IS ANSWERED WITH THE CONTRACT'S OWN TOKEN. A
	// turn that runs one step past its script — a worker released, a
	// conversation handed its proposal's result — is read for what remains of
	// the ask, and prose to that question is a test running to the meter's
	// ceiling rather than terminating. This is the one answer the remains
	// contract spells for "nothing more to do here".
	if len(snapshot) > 0 && strings.Contains(messageText(snapshot[len(snapshot)-1]), "[still asked]") {
		return textResponse(checkpointNothingLeft), nil
	}
	if id := planLaneOf(snapshot); id != "" {
		return c.workCall(ctx, id, snapshot)
	}
	// The conversation, scripted positionally: one lane, one turn, no errands
	// in it.
	c.mu.Lock()
	var next step
	if c.seen < len(c.parent) {
		next = c.parent[c.seen]
	}
	c.seen++
	c.mu.Unlock()
	if next == nil {
		return textResponse("(unscripted)"), nil
	}
	return next(ctx, snapshot)
}

// workCall is one call by a plan-born node's worker: recorded, announced to
// the waiter, and held on the lane's channel until the test releases it.
// Calls after the release answer the release's prose, for the reason the
// field above states.
func (c *planLaneCompleter) workCall(ctx context.Context, id string, messages []ai.Message) (*ai.Response, error) {
	c.mu.Lock()
	c.requests[id] = append(c.requests[id], messages)
	arrived := c.arrived[id]
	if arrived == nil {
		arrived = make(chan struct{})
		c.arrived[id] = arrived
	}
	if !c.announced[id] {
		c.announced[id] = true
		close(arrived)
	}
	if c.released[id] {
		prose := c.prose[id]
		c.mu.Unlock()
		return textResponse(prose), nil
	}
	lane := c.lanes[id]
	if lane == nil {
		lane = make(chan string, 1)
		c.lanes[id] = lane
	}
	c.mu.Unlock()
	select {
	case prose := <-lane:
		c.mu.Lock()
		c.released[id] = true
		c.prose[id] = prose
		c.mu.Unlock()
		return textResponse(prose), nil
	case <-ctx.Done():
		// The run is closing under the worker. Answering lets the goroutine's
		// turn end and the node settle at cleanup, rather than sitting on a
		// channel nothing will ever send to.
		return textResponse("(cut)"), nil
	}
}

// awaitRequest waits for the lane's worker to be parked in its first call and
// hands the call's context back. A test releases only a lane it has waited
// for, so a release is always received and never a value parked on a channel
// no worker is reading.
func (c *planLaneCompleter) awaitRequest(t *testing.T, id string) []ai.Message {
	t.Helper()
	c.mu.Lock()
	arrived := c.arrived[id]
	if arrived == nil {
		arrived = make(chan struct{})
		c.arrived[id] = arrived
	}
	c.mu.Unlock()
	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatalf("the worker for plan task %s never started", id)
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.requests[id]) == 0 {
		t.Fatalf("plan task %s announced a call without recording it", id)
	}
	return c.requests[id][0]
}

// release ends a lane's hold with the prose its worker answers — once, for
// the parked call, and again for every call the run wakes in it afterwards.
// The lane is BUFFERED, so the send completes whether or not a call is parked
// on it: a release that ran ahead of its worker's opening — a node whose
// worktree was still being carved — must not hold the test on a receiver
// that the released flag will answer directly.
func (c *planLaneCompleter) release(id, prose string) {
	c.mu.Lock()
	lane := c.lanes[id]
	if lane == nil {
		lane = make(chan string, 1)
		c.lanes[id] = lane
	}
	c.released[id] = true
	c.prose[id] = prose
	c.mu.Unlock()
	lane <- prose
}

// planLaneOf reads the plan id one request's context carries, or "" when the
// call is not a plan-born node's. The id follows the plan line's t- spelling
// and ends at the first character the CLI's own sentence puts after it.
func planLaneOf(messages []ai.Message) string {
	for _, message := range messages {
		text := messageText(message)
		at := strings.Index(text, planLineMark)
		if at < 0 {
			continue
		}
		rest := text[at+len(planLineMark):]
		end := strings.IndexAny(rest, ", \t\n")
		if end < 0 {
			return rest
		}
		return rest[:end]
	}
	return ""
}

// ── the store, from the outside ──────────────────────────────────────────────

// planOpenStore is a FRESH handle on the run's store, the same road
// [planState.open] takes. A handle's memory is only as fresh as its last
// transaction, and the graph's own pulse has been writing since any earlier
// read — every assertion reads the file, never a copy.
func planOpenStore(t *testing.T, dir string) *plandb.Store {
	t.Helper()
	store, err := plandb.Open(filepath.Join(dir, planStoreFilename), "", planRootID, "", "")
	if err != nil {
		t.Fatalf("open the plan store at %s: %v", dir, err)
	}
	return store
}

// planTaskAt reads one store task through a fresh handle. The handle is closed
// again because opening the store holds its database open, and the helpers here
// read the same store over and over.
func planTaskAt(t *testing.T, dir, id string) *plandb.Task {
	t.Helper()
	store := planOpenStore(t, dir)
	defer store.Close()
	task := store.Task(id)
	if task == nil {
		t.Fatalf("plan task %q is not in the store", id)
	}
	return task
}

// planWaitStoreRootTerminal waits, bounded, for the store's root task to
// reach a terminal status — the word the landing pulse's completion writes a
// breath after the last node settles. The wait is on the store's own file,
// and the failure names the status it saw.
func planWaitStoreRootTerminal(t *testing.T, dir string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		store := planOpenStore(t, dir)
		root := store.Task(planRootID)
		_ = store.Close()
		if root != nil && (root.Status == plandb.StatusDone || root.Status == plandb.StatusFailed) {
			return
		}
		if time.Now().After(deadline) {
			status := "absent"
			if root != nil {
				status = string(root.Status)
			}
			t.Fatalf("the store's root task never completed; it reads %s", status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// planGrow adds children to the run's store the way the worker's CLI does:
// through the store's own door, on a fresh handle. A test that grew the file
// by hand would be testing a second store format.
func planGrow(t *testing.T, dir string, specs ...plandb.TaskSpec) {
	t.Helper()
	store := planOpenStore(t, dir)
	defer store.Close()
	if _, err := store.AddMany(specs); err != nil {
		t.Fatalf("add to the plan: %v", err)
	}
}

// planNodeByPlanID finds the graph node a store task was dispatched as, or
// nil when the task has no node — the pulse's own mapping, read the way it
// keeps it: on the node's spec.
func planNodeByPlanID(g *TaskGraph, id string) *TaskNode {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, node := range g.nodes {
		if node != nil && node.spec.planID == id {
			return node
		}
	}
	return nil
}

// ── the seeded run ──────────────────────────────────────────────────────────

// planProposeCall is the conversation proposing the run's task with the
// arguments the door reads, carrying the limits a real proposal carries so
// the node's runner has a bar to count against.
func planProposeCall(title, brief string) step {
	arguments, _ := json.Marshal(taskArguments{
		Title:       title,
		Summary:     "s",
		Brief:       brief,
		Deliverable: "d",
		Acceptance:  "a",
		MaxSteps:    200,
		NoProgress:  6,
	})
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse("call-task", "propose_task", string(arguments)), nil
	}
}

// planRunSubmit seeds the run the way the live session does: a person's turn
// proposes the run's task through the conversation's own door, so the request
// the worker's brief quotes is the person's words the door captured. The
// session folder is the test's own, the ground a throwaway repository, and
// the root's worker is HELD on its lane — the test owns when the run's own
// work lands.
func planRunSubmit(t *testing.T, completer *planLaneCompleter, dir, title, brief, ask string) (*Agent, *TaskNode) {
	t.Helper()
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	collect(t, mustSubmit(t, agent, ask))
	root := planNodeByPlanID(agent.graph(), planRootID)
	if root == nil {
		t.Fatal("the submitted task seeded no plan root")
	}
	return agent, root
}

// planBriefOf is the worker's opening document: the user message carrying the
// plan line, whole. It is the only place the composed work order can be read.
func planBriefOf(t *testing.T, messages []ai.Message) string {
	t.Helper()
	for _, message := range messages {
		if message.Role != "user" {
			continue
		}
		if text := messageText(message); strings.Contains(text, planLineMark) {
			return text
		}
	}
	t.Fatal("the worker's opening carries no plan line")
	return ""
}

// THE SEED. The first ordinary task under the switch seeds the store beside
// the session folder, takes the root task as its own plan id, and the brief
// its worker reads is composed back FROM the store read — the contract went in
// as the root task's description and came out carrying the plan's own lines,
// which is what makes the store the source and not a copy. And the resumed
// shape: a fresh graph over the same session folder adopts the store rather
// than seeding a second one, and the next task admitted under it becomes a
// plan child of the root, claimed under its own id the way every dispatched
// task is.
func TestPlandbCliSeedComposesTheWorkOrderFromTheStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TASK_BELT", "bash")
	dir := t.TempDir()
	completer := newPlanLaneCompleter([]step{
		planProposeCall("The whole run", "write the pulse wiring"),
		finalText("handed off"), finalText("handed off"), finalText("handed off"),
	})
	agent, root := planRunSubmit(t, completer, dir, "The whole run", "write the pulse wiring", "port the plandb cli properly")

	if _, err := os.Stat(filepath.Join(dir, planStoreFilename)); err != nil {
		t.Fatalf("the store was not seeded beside the session folder: %v", err)
	}
	agent.graph().mu.Lock()
	planID := root.spec.planID
	agent.graph().mu.Unlock()
	if planID != planRootID {
		t.Fatalf("the seeding node's plan id = %q, want the store's root %q", planID, planRootID)
	}
	brief := planBriefOf(t, completer.awaitRequest(t, planRootID))
	for _, want := range []string{
		// THE WORK ORDER CAME THROUGH THE STORE: the proposal's brief is the
		// root task's description, and the plan's own lines follow it — the id
		// in the CLI's spelling, and the agent the finish command names.
		"write the pulse wiring",
		"YOUR TASK IN THE PLAN IS t-root, claimed by agent root.",
		// AND THE PERSON'S OWN WORDS RIDE AHEAD OF IT — the request the door
		// captured, quoted the way every brief quotes it, unedited.
		"port the plandb cli properly",
	} {
		if !strings.Contains(brief, want) {
			t.Fatalf("the worker's opening is missing %q:\n%s", want, brief)
		}
	}
	// THE RESUMED SHAPE, BOTH ARMS. A second graph, no plan of its own, over
	// the same session folder: while the first run is still open, a new task
	// joins its plan as a CHILD of the root with its own id — a graph that
	// re-seeded would make a second root, and two sessions would then be
	// dispatching each other's children. After the first run has completed,
	// the same door seeds a FRESH plan whose root is the new task, and the
	// finished one is archived beside the session — one store per run, and the
	// one live name is the one both roads find.
	//
	// The mid-run arm first, against the still-open plan:
	resume, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
	})
	id := resume.graph().reserve()
	resume.graph().admit(id, taskSpec{title: "The whole run", brief: "the resumed follow-up", acceptance: "a"})
	second := resume.graph().node(id)
	resume.graph().mu.Lock()
	minted := second.spec.planID
	resume.graph().mu.Unlock()
	if minted == "" || minted == planRootID {
		t.Fatalf("the second task under the same session folder took plan id %q, want its own", minted)
	}
	task := planTaskAt(t, dir, minted)
	if task.ParentID != planRootID {
		t.Fatalf("the second task's store parent = %q, want the root", task.ParentID)
	}
	if task.ClaimedBy != minted || task.Status != plandb.StatusRunning {
		t.Fatalf("the second task stands %s@%q, want running under its own id", task.Status, task.ClaimedBy)
	}
	// AND THE STORE GREW BY A CHILD, NOT BY A SECOND RUN: the root is still
	// the one root, and the whole plan is it and the child.
	tasks := planOpenStore(t, dir).Tasks()
	roots := 0
	for _, one := range tasks {
		if one.ID == planRootID {
			roots++
		}
	}
	if roots != 1 || len(tasks) != 2 {
		t.Fatalf("the store holds %d tasks with %d roots, want the one root and one child", len(tasks), roots)
	}
	resumed := planBriefOf(t, completer.awaitRequest(t, minted))
	if want := "YOUR TASK IN THE PLAN IS t-" + minted; !strings.Contains(resumed, want) {
		t.Fatalf("the resumed worker's opening does not name its own task: want %q in:\n%s", want, resumed)
	}
	// THE RUN ENDS ONLY WHEN ITS PLAN-BORN WORK HAS: the root and the resumed
	// task both land, and the last landing's pulse completes the store.
	completer.release(planRootID, "the root report")
	completer.release(minted, "the follow-up report")
	waitDoneNode(t, root)
	waitDoneNode(t, second)

	// AND THE AFTER-COMPLETION ARM: a new task once the run is over seeds a
	// fresh plan — its own root in its own store — and the finished plan is
	// archived beside the session, never deleted.
	planWaitStoreRootTerminal(t, dir)
	fresh, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
	})
	id2 := fresh.graph().reserve()
	fresh.graph().admit(id2, taskSpec{title: "The next run", brief: "a new run's own plan", acceptance: "a"})
	third := fresh.graph().node(id2)
	fresh.graph().mu.Lock()
	reminted := third.spec.planID
	fresh.graph().mu.Unlock()
	if reminted != planRootID {
		t.Fatalf("a task after a completed run took plan id %q, want a fresh root of its own", reminted)
	}
	if _, err := os.Stat(dir + "/plandb.db.1"); err != nil {
		t.Fatalf("the finished plan was not archived beside the session: %v", err)
	}
	if live := planTaskAt(t, dir, planRootID); live.Status != plandb.StatusRunning {
		t.Fatalf("the fresh plan's root stands %s, want the new run's own running root", live.Status)
	}
}

// THE WHOLE LOOP, DRIVEN BY HAND. The root's worker is held; the store grows
// the two children the way the worker's CLI would grow it; and the pulse —
// the same pass the live loop runs after every bash call and every landing —
// is called by the test. One pass dispatches the ready child and not the
// blocked one; the ready child's landing, and the pass after it, writes the
// child done, promotes its dependent and dispatches it; and when the root
// itself lands, the next pass completes the store's root — the one write the
// design refuses to make early.
func TestPlandbCliPulseDrivesTheWholeLoop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TASK_BELT", "bash")
	dir := t.TempDir()
	completer := newPlanLaneCompleter([]step{
		planProposeCall("The whole run", "drive the loop end to end"),
		finalText("handed off"), finalText("handed off"), finalText("handed off"),
	})
	agent, root := planRunSubmit(t, completer, dir, "The whole run", "drive the loop end to end", "run the whole plan through the store")
	completer.awaitRequest(t, planRootID)

	planGrow(t, dir,
		plandb.TaskSpec{ID: "a", Title: "part a", Description: "the a work order", ParentID: planRootID},
		plandb.TaskSpec{ID: "b", Title: "part b", Description: "the b work order", ParentID: planRootID,
			Dependencies: []plandb.Dependency{{TaskID: "a"}}},
	)
	agent.graph().planPulse()

	// THE READY CHILD IS DISPATCHED, ONE LEVEL UNDER THE ROOT in the graph's
	// own tree; THE BLOCKED ONE IS NOT, its dependency still open.
	a := planNodeByPlanID(agent.graph(), "a")
	if a == nil {
		t.Fatal("the ready store task was not dispatched as a node")
	}
	agent.graph().mu.Lock()
	aDepth, aParent, rootDepth := a.depth, a.parent, root.depth
	agent.graph().mu.Unlock()
	if aDepth != rootDepth+1 || aParent != root.id {
		t.Fatalf("part a stands at depth %d under node %d, want one under the root (depth %d, node %d)", aDepth, aParent, rootDepth+1, root.id)
	}
	if planNodeByPlanID(agent.graph(), "b") != nil {
		t.Fatal("part b was dispatched before its dependency landed")
	}
	// AND THE STORE TASK IS CLAIMED AT DISPATCH, under its own id — the
	// reference supervisor's trick. That claim is what makes the worker's
	// `plandb done --agent <id>` pass the ownership check from the moment the
	// node exists, and what the writeback completes as.
	if task := planTaskAt(t, dir, "a"); task.Status != plandb.StatusRunning || task.ClaimedBy != "a" {
		t.Fatalf("part a stands %s@%q at dispatch, want running@a — the dispatch's own claim", task.Status, task.ClaimedBy)
	}

	// THE CHILD LANDS, AND THE PASS AFTER IT WRITES THE STORE. The landing's
	// own pulse reads the landing node as still running — its state is
	// written by complete, after workTaskNode returns — so the writeback is
	// the next pass: in the live loop the parent's next bash call, here the
	// test's own.
	completer.release("a", "the a report")
	waitDoneNode(t, a)
	agent.graph().planPulse()

	task := planTaskAt(t, dir, "a")
	if task.Status != plandb.StatusDone || task.ClaimedBy != "a" {
		t.Fatalf("part a stands %s@%q after its landing, want done under its own id", task.Status, task.ClaimedBy)
	}
	if !strings.Contains(task.Result, "the a report") {
		t.Fatalf("part a's result = %q, want the worker's report in it", task.Result)
	}
	// AND ITS DEPENDENT WAS PROMOTED AND DISPATCHED BY THE SAME PASS.
	b := planNodeByPlanID(agent.graph(), "b")
	if b == nil {
		t.Fatal("part b was not dispatched once its dependency landed")
	}
	completer.release("b", "the b report")
	waitDoneNode(t, b)
	agent.graph().planPulse()
	task = planTaskAt(t, dir, "b")
	if task.Status != plandb.StatusDone || task.ClaimedBy != "b" || !strings.Contains(task.Result, "the b report") {
		t.Fatalf("part b stands %s@%q with %q, want done under its own id with the report", task.Status, task.ClaimedBy, task.Result)
	}
	// AND THE RUN'S OWN TASK IS STILL RUNNING: the writeback skips the root by
	// design — a root marked done while its children still worked would be a
	// plan that says the run is over while it is not. Only completion writes
	// it, and only when the whole tree has settled.
	if task := planTaskAt(t, dir, planRootID); task.Status != plandb.StatusRunning {
		t.Fatalf("the root stands %s with its children landed, want running until the run completes", task.Status)
	}

	// THE RUN'S OWN ENDING. The root lands, and the pass after it completes
	// the store's root with the run's report.
	completer.release(planRootID, "the root report")
	waitDoneNode(t, root)
	agent.graph().planPulse()
	task = planTaskAt(t, dir, planRootID)
	if task.Status != plandb.StatusDone {
		t.Fatalf("the root stands %s after the run, want completed", task.Status)
	}
	if !strings.Contains(task.Result, "the root report") {
		t.Fatalf("the root's result = %q, want the run's report in it", task.Result)
	}
}

// THE WORKER'S OWN DONE IS NEVER WRITTEN OVER. A task claimed under its own
// id — the road the door takes for a later ordinary task — is finished by its
// worker through the store, the taught finish, BEFORE the node lands; the
// pulse's writeback must leave the worker's own words standing rather than
// settle the store task again from the node's report.
func TestPlandbCliWritebackSkipsTheWorkerSOwnDone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TASK_BELT", "bash")
	dir := t.TempDir()
	completer := newPlanLaneCompleter([]step{
		planProposeCall("The whole run", "hold the run open"),
		finalText("handed off"), finalText("handed off"), finalText("handed off"),
	})
	agent, _ := planRunSubmit(t, completer, dir, "The whole run", "hold the run open", "keep the run open while the follow-up finishes")
	// The root's worker stays held: the run stays open, and the guard on root
	// completion reads it that way.
	completer.awaitRequest(t, planRootID)

	// A SECOND ORDINARY TASK, through the graph's own door: the store takes it
	// as a child of the root and the door claims it under its own id — the
	// claim the taught finish command answers.
	id := agent.graph().reserve()
	agent.graph().admit(id, taskSpec{title: "the follow-up", brief: "b", acceptance: "a"})
	node := agent.graph().node(id)
	agent.graph().mu.Lock()
	minted := node.spec.planID
	agent.graph().mu.Unlock()
	if minted == "" || minted == planRootID {
		t.Fatalf("the door gave the follow-up plan id %q, want its own child id", minted)
	}
	completer.awaitRequest(t, minted)

	store := planOpenStore(t, dir)
	task := store.Task(minted)
	if task == nil || task.Status != plandb.StatusRunning || task.ClaimedBy != minted {
		t.Fatalf("the follow-up stands %v, want running under its own id", task)
	}
	// THE WORKER FINISHES ITS OWN TASK through the store, the taught road,
	// before its node lands.
	if _, err := store.Done(minted, minted, "the worker's own words", nil, nil); err != nil {
		t.Fatalf("the taught finish was refused: %v", err)
	}
	completer.release(minted, "the node's later report")
	waitDoneNode(t, node)
	agent.graph().planPulse()

	kept := planTaskAt(t, dir, minted)
	if kept.Status != plandb.StatusDone || kept.Result != "the worker's own words" {
		t.Fatalf("the writeback settled over the worker's own done: %s with %q", kept.Status, kept.Result)
	}
	if kept.ClaimedBy != minted {
		t.Fatalf("the finished task's agent = %q, want its own id", kept.ClaimedBy)
	}
}

// THE DEPTH FLOOR. A node already at the tree's limit has store children it
// can never hand out: the pulse leaves them in the store while the node runs,
// and the pass after the node lands cancels them with a reason that names the
// tree's depth — a plan that lies about work it will never deliver is worse
// than one that says the work is not coming.
func TestPlandbCliDepthFloorCancelsItsUndeliverableChildren(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TASK_BELT", "bash")
	dir := t.TempDir()
	completer := newPlanLaneCompleter([]step{
		planProposeCall("The whole run", "hold the run open at the floor"),
		finalText("handed off"), finalText("handed off"), finalText("handed off"),
	})
	agent, _ := planRunSubmit(t, completer, dir, "The whole run", "hold the run open at the floor", "keep the run open at the floor")
	completer.awaitRequest(t, planRootID)

	// The store's own shape: a task under the root whose node will stand at
	// the floor, and a ready leaf under THAT task — deliverable only through
	// a node one past the limit.
	planGrow(t, dir,
		plandb.TaskSpec{ID: "floor", Title: "the floor task", Description: "the floor work order", ParentID: planRootID},
		plandb.TaskSpec{ID: "under", Title: "the undeliverable leaf", Description: "never runs", ParentID: "floor"},
	)
	// THE NODE AT THE FLOOR, admitted with the plan task the store already
	// names — the resumed-node road, the brief composed from the store read.
	id := agent.graph().reserve()
	agent.graph().admit(id, taskSpec{title: "the floor task", brief: "b", acceptance: "a", depth: taskDepthLimit, planID: "floor"})
	node := agent.graph().node(id)
	completer.awaitRequest(t, "floor")

	// WHILE IT RUNS: the child is left in the store, undelivered — no node,
	// still ready, not cancelled into a lie.
	agent.graph().planPulse()
	if planNodeByPlanID(agent.graph(), "under") != nil {
		t.Fatal("a child of a floor node was dispatched past the tree's limit")
	}
	if task := planTaskAt(t, dir, "under"); task.Status != plandb.StatusReady {
		t.Fatalf("the undeliverable leaf stands %s while its parent runs, want ready in the store", task.Status)
	}

	completer.release("floor", "the floor report")
	waitDoneNode(t, node)
	agent.graph().planPulse()

	leaf := planTaskAt(t, dir, "under")
	if leaf.Status != plandb.StatusCancelled {
		t.Fatalf("the undeliverable leaf stands %s after its parent landed, want cancelled", leaf.Status)
	}
	if !strings.Contains(leaf.Error, "depth limit") {
		t.Fatalf("the cancellation does not name the tree's depth: %q", leaf.Error)
	}
	// AND THE FLOOR'S OWN TASK CANNOT BE THE WRITEBACK'S: it is a parent in
	// the store — it owns the child the tree could not deliver — and the store
	// never claims a composite, so its ending is written by the composite law
	// from its children: a cancelled child makes a failed parent, and none
	// of the node's report reaches it.
	floor := planTaskAt(t, dir, "floor")
	if floor.Status != plandb.StatusFailed {
		t.Fatalf("the floor task stands %s under a cancelled child, want the composite law's failed", floor.Status)
	}
	if floor.Result != "" || floor.ClaimedBy != "" {
		t.Fatalf("the floor task carries %q@%q, want the store's own verdict with no claim and no result", floor.Result, floor.ClaimedBy)
	}
}

// ROOT COMPLETION CANCELS THE UNDELIVERED. When the run's root has settled
// and no plan-born node is open, whatever the store still holds is cancelled
// with the plain reason — nothing will deliver it — and the root is
// completed. The undeliverable shape here is the composite parent: a
// childless ready child of a settled root is deliverable (the same pass
// dispatches it, the promotion road), so the task nothing can deliver is the
// one the dispatch loop itself refuses — the composite parent holding a
// ready leaf whose parent has no node.
func TestPlandbCliRootCompletionCancelsTheUndelivered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TASK_BELT", "bash")
	dir := t.TempDir()
	completer := newPlanLaneCompleter([]step{
		planProposeCall("The whole run", "leave work undelivered"),
		finalText("handed off"), finalText("handed off"), finalText("handed off"),
	})
	agent, root := planRunSubmit(t, completer, dir, "The whole run", "leave work undelivered", "end the run with work held back")
	// THE HELD-BACK WORK GOES IN WHILE THE RUN IS STILL RUNNING: the store's
	// root is the runtime's until completion, and once the root's own landing
	// pulse has run, the run is over and the store refuses a child of a
	// terminal root. Undelivered work exists in a running plan, not after it.
	planGrow(t, dir,
		plandb.TaskSpec{ID: "held", Title: "the held-back middle", Description: "d", ParentID: planRootID},
		plandb.TaskSpec{ID: "leaf", Title: "the ready leaf", Description: "d", ParentID: "held"},
	)
	// The run's own work lands, and its landing pulse ends the run: the
	// undelivered tasks are cancelled with a plain reason and the root is
	// written once, by completion.
	completer.release(planRootID, "the root report")
	waitDoneNode(t, root)
	agent.graph().planPulse()

	held := planTaskAt(t, dir, "held")
	if held.Status != plandb.StatusCancelled {
		t.Fatalf("the undelivered task stands %s, want cancelled", held.Status)
	}
	if !strings.Contains(held.Error, "nothing will deliver") {
		t.Fatalf("the cancellation does not say why in the plan's own words: %q", held.Error)
	}
	if leaf := planTaskAt(t, dir, "leaf"); leaf.Status != plandb.StatusCancelled {
		t.Fatalf("the leaf under the cancelled task stands %s, want the cascade's cancelled", leaf.Status)
	}
	// AND THE ROOT IS COMPLETED — as failed, which is the store's own verdict
	// on a run that left work undelivered: CompleteRoot marks the root done
	// only when every task under it is.
	rootTask := planTaskAt(t, dir, planRootID)
	if rootTask.Status != plandb.StatusFailed {
		t.Fatalf("the root stands %s with work left undelivered, want the store's failed verdict", rootTask.Status)
	}
	if !strings.Contains(rootTask.Result, "the root report") {
		t.Fatalf("the completed root's result = %q, want the run's report in it", rootTask.Result)
	}
}

// ── the pulse points a turn and a freed slot are ─────────────────────────────

// A TURN THAT ENDS IS A PULSE POINT. The store can grow while a worker is
// mid-turn — a sibling's CLI call whose own pass ran before the task was
// ready, a command the worker backgrounded, the person's own shell — and the
// pass that ran after its last bash call is behind it by then. With no pass at
// the turn's end the worker lands on work it owns and never waited for: the
// ready task is handed out by the LANDING's pass instead, as a child of a node
// that has already settled and already cut its subtree, so nobody folds its
// report and the run's ending is written over a worker still running. What
// must come back: the turn's end hands the task out, the worker stays open on
// the part it now has, and it lands only after the part has.
func TestPlandbCliATurnEndingWithPlanWorkOpenStillDispatches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TASK_BELT", "bash")
	dir := t.TempDir()
	completer := newPlanLaneCompleter([]step{
		planProposeCall("The whole run", "coordinate the plan"),
		finalText("handed off"), finalText("handed off"), finalText("handed off"),
	})
	agent, root := planRunSubmit(t, completer, dir, "The whole run", "coordinate the plan", "run the whole plan through the store")
	completer.awaitRequest(t, planRootID)

	// THE STORE GROWS INSIDE THE WORKER'S TURN and no pass takes it: the task
	// is ready, has no node, and the worker's next act is the prose that ends
	// the turn. This is the shape a sibling's CLI call leaves behind when the
	// sibling's own pass ran before the task was ready.
	planGrow(t, dir,
		plandb.TaskSpec{ID: "x", Title: "part x", Description: "the x work order", ParentID: planRootID})
	if planNodeByPlanID(agent.graph(), "x") != nil {
		t.Fatal("a ready task was dispatched with no pass to dispatch it")
	}

	// THE TURN ENDS, AND THE WORKER STAYS OPEN ON THE PART IT NOW HAS. Parked
	// is not the assertion; RUNNING is — a worker that landed here would leave
	// the part an orphan of a settled node.
	completer.release(planRootID, "the root report")
	waitFor(t, "the turn's end to dispatch the ready task", func() bool {
		return planNodeByPlanID(agent.graph(), "x") != nil && root.stateNow() == TaskRunning
	})
	part := planNodeByPlanID(agent.graph(), "x")
	completer.awaitRequest(t, "x")
	if state := root.stateNow(); state != TaskRunning {
		t.Fatalf("the worker stands %q while the part it owns is still open, want it waiting on the part", state)
	}

	// AND IT LANDS ONLY AFTER THE PART HAS.
	completer.release("x", "the x report")
	waitDoneNode(t, part)
	waitDoneNode(t, root)
	agent.graph().planPulse()
	task := planTaskAt(t, dir, "x")
	if task.Status != plandb.StatusDone || !strings.Contains(task.Result, "the x report") {
		t.Fatalf("part x stands %s with %q, want done with its own report", task.Status, task.Result)
	}
	planWaitStoreRootTerminal(t, dir)
}

// A FAN SLOT GOING BACK IS A DISPATCH BECOMING POSSIBLE. A pass refuses a
// ready task whose parent has handed out as many pieces as one task may and
// leaves it in the store for the next pass — which, on a belt whose worker
// makes no further bash call, is a pass that never comes. The slot is given
// back by a proposal that came to nothing ([TaskGraph.releaseChild]), and that
// moment is a pulse point: what the cap refused an instant ago is
// dispatchable now, and the worker that owns it is still running.
func TestPlandbCliAFreedFanSlotDispatchesWhatTheCapRefused(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TASK_BELT", "bash")
	dir := t.TempDir()
	completer := newPlanLaneCompleter([]step{
		planProposeCall("The whole run", "hold the fan open"),
		finalText("handed off"), finalText("handed off"), finalText("handed off"),
	})
	agent, root := planRunSubmit(t, completer, dir, "The whole run", "hold the fan open", "run the whole plan through the store")
	completer.awaitRequest(t, planRootID)

	// THE FAN IS FULL: as many slots held against this worker as one task may,
	// which is what a batch of proposals leaves behind when every call in it
	// read the same count and passed ([TaskGraph.claimChild]).
	for i := 0; i < taskFanLimit; i++ {
		if refused := agent.graph().claimChild(root.id); refused != "" {
			t.Fatalf("slot %d was refused before the cap: %s", i, refused)
		}
	}
	planGrow(t, dir,
		plandb.TaskSpec{ID: "x", Title: "part x", Description: "the x work order", ParentID: planRootID})
	agent.graph().planPulse()
	if planNodeByPlanID(agent.graph(), "x") != nil {
		t.Fatal("a task past the fan cap was dispatched")
	}
	if task := planTaskAt(t, dir, "x"); task.Status != plandb.StatusReady {
		t.Fatalf("the refused task stands %s, want it left ready in the store for the pass that can take it", task.Status)
	}

	// ONE PROPOSAL COMES TO NOTHING AND ITS SLOT GOES BACK. The pass this takes
	// is the one that hands the refused task out, with no bash call anywhere
	// near it.
	agent.graph().releaseChild(root.id)
	waitFor(t, "the freed slot to dispatch the task the cap refused", func() bool {
		return planNodeByPlanID(agent.graph(), "x") != nil
	})
	if task := planTaskAt(t, dir, "x"); task.Status != plandb.StatusRunning || task.ClaimedBy != "x" {
		t.Fatalf("the dispatched task stands %s@%q, want running under its own id", task.Status, task.ClaimedBy)
	}

	// AND THE RUN ENDS CLEANLY: the part lands, the worker that held the fan
	// open lands, and the last pass writes the root.
	part := planNodeByPlanID(agent.graph(), "x")
	completer.awaitRequest(t, "x")
	completer.release("x", "the x report")
	waitDoneNode(t, part)
	completer.release(planRootID, "the root report")
	waitDoneNode(t, root)
	planWaitStoreRootTerminal(t, dir)
}

// A PASS THAT HANDED WORK OUT IS NOT THE RUN'S END. The dispatch reads the
// store and admits nodes; the completion below it asks whether anything is
// still open of the pass's own SNAPSHOT, taken before the dispatch ran — so a
// node this very pass admitted is invisible to the question, and the pass goes
// on to cancel what is still ready and to write the run's ending. What it
// cancels is the work that node was about to become the parent of: a ready
// leaf whose parent has no node is refused by the dispatch, and a parent has
// just been made for it. Driven by hand against a settled seed rather than
// through a run, because the shape is one pass's own ordering — the turn's end
// and the freed slot both take their pass earlier, on a seed that is still
// running, and so never reach the completion at all.
func TestPlandbCliAPassThatHandedWorkOutDoesNotEndTheRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TASK_BELT", "bash")
	// A graph whose runner is a no-op, so a node this test admits is a node and
	// never a worker: the pass under test is the store's and the graph's, and
	// nothing here needs a lane.
	nest := newNest(t, nil, nil)
	g := nest.graph
	dir := filepath.Dir(g.planPath())

	// THE SEED HAS LANDED, and the store still holds one childless ready task
	// the dispatch CAN hand out beside the pair it cannot — a composite parent
	// and the ready leaf under it, the undeliverable shape the cancellation
	// road exists for.
	nest.parent.finish("the root report", nil, "", "")
	g.complete(nest.parent, TaskDone)
	planGrow(t, dir,
		plandb.TaskSpec{ID: "x", Title: "part x", Description: "the x work order", ParentID: planRootID},
		plandb.TaskSpec{ID: "held", Title: "the held-back middle", Description: "d", ParentID: planRootID},
		plandb.TaskSpec{ID: "leaf", Title: "the ready leaf", Description: "d", ParentID: "held"},
	)

	g.planPulse()

	// THE PASS TOOK THE TASK IT COULD DELIVER ...
	x := planNodeByPlanID(g, "x")
	if x == nil {
		t.Fatal("the ready childless task was not dispatched")
	}
	// ... AND THEREFORE STOPPED SHORT OF THE RUN'S ENDING. The leaf has a
	// deliverer now — the node this pass admitted is the parent it was refused
	// for want of — and the pass that ends the run is the one after that node
	// lands.
	for _, id := range []string{"held", "leaf"} {
		if task := planTaskAt(t, dir, id); task.Status == plandb.StatusCancelled {
			t.Fatalf("%s was cancelled by the pass that dispatched its own deliverer: %s", id, task.Error)
		}
	}
	if task := planTaskAt(t, dir, planRootID); task.Status != plandb.StatusRunning {
		t.Fatalf("the root stands %s on a pass that just handed work out, want it still running while that work does", task.Status)
	}

	// AND THE RUN STILL ENDS. The dispatched node settles, its own pass finds
	// nothing left to hand out, and the undeliverable pair is cancelled then —
	// one pass later, by the road that was always meant to cancel it.
	x.finish("the x report", nil, "", "")
	g.complete(x, TaskDone)
	g.planPulse()
	for _, id := range []string{"held", "leaf"} {
		if task := planTaskAt(t, dir, id); task.Status != plandb.StatusCancelled {
			t.Fatalf("%s stands %s after the run ended, want the undeliverable cancelled", id, task.Status)
		}
	}
	if task := planTaskAt(t, dir, planRootID); !terminalStoreStatus(task.Status) {
		t.Fatalf("the root stands %s after the last node landed, want the run's ending written", task.Status)
	}
}

// planPageSection is one `## `-headed section of a rendered page, whole. It
// bounds the assertions that read a section's own words, the same bound the
// composed page splices by.
func planPageSection(page, heading string) string {
	start := strings.Index(page, heading)
	if start < 0 {
		return ""
	}
	rest := page[start+len(heading):]
	if next := strings.Index(rest, "\n## "); next >= 0 {
		rest = rest[:next]
	}
	return rest
}

// THE BELT AND THE PAGE, BOTH WAYS OF THE SWITCH. The belt is the config's —
// the door builds the bash belt for a task worker when the switch is on — and
// the store wiring is the switch's. With it on, a bash-belt worker carries no
// graph verb at all — the plan CLI is their replacement, and a belt that
// carried both would teach the model two ways to say one thing — while the
// person's redirection lane stays; without it, the plain worker carries the
// verbs it always carried, its page has no plan section, and admitting a
// task stores no plandb.db beside the session.
func TestPlandbCliTheBeltAndPageFollowTheSwitch(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	now := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)

	belt, _ := newTestAgent(t, &scriptedCompleter{}, bashBeltWorkerConfig(t))
	carried := beltNameSet(belt.beltTools())
	for _, gone := range []string{"propose_task", "quick_task", "divide_work", "tasks"} {
		if carried[gone] {
			t.Errorf("the bash belt still carries %s", gone)
		}
	}
	// AND THE PERSON'S REDIRECTION LANE IS NOT COORDINATION, so it stays.
	if !carried["revise_assignment"] {
		t.Error("the bash belt lost revise_assignment")
	}

	page := renderSystemAt(belt.config, now)
	// THE DOCTRINE THE WORKER COORDINATES BY: the automatic dispatch is the
	// page's own promise, and the plan section carries the CLI's grammar.
	if !strings.Contains(page, "DISPATCH is automatic") {
		t.Error("the bash worker's page does not promise automatic dispatch")
	}
	section := planPageSection(page, "## The plan")
	if section == "" {
		t.Fatal("the bash worker's page carries no plan section")
	}
	if !strings.Contains(section, "plandb add") {
		t.Error("the plan section does not teach the CLI's own add")
	}
	// AND THE SECTION ITSELF NAMES NO GRAPH VERB. The assertion is scoped to
	// the section because the page as a whole still carries fanout.md's own
	// verb — the forward-law test for the bash page (TestBashWorkerPrompt-
	// NamesOnlyWhatTheBeltCarries) is already red on exactly that, and this
	// wave does not widen to fix it.
	if strings.Contains(section, "propose_task") {
		t.Error("the plan section still names propose_task")
	}

	// WITHOUT THE SWITCH: the plain worker the door builds when the
	// experiment is off — same shape, no bash belt — carries the graph verbs,
	// and its page is the one it always read.
	t.Setenv("CODEAF_TASK_BELT", "node")
	plain, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.InTask = true
		config.tasker = graphForShape(t)
		config.taskID = 1
		config.taskDepth = 1
	})
	plainCarried := beltNameSet(plain.beltTools())
	if !plainCarried["propose_task"] {
		t.Error("the plain worker lost propose_task")
	}
	if plainPage := renderSystemAt(plain.config, now); strings.Contains(plainPage, "## The plan") {
		t.Error("a worker outside the experiment was handed the plan section")
	}

	// AND ADMITTING A TASK STORES NOTHING: the switch gates the store wiring
	// whole, so a session outside the experiment leaves no plandb.db beside
	// itself. A workspace that is not a repository keeps the node off git
	// entirely — the point here is the one file, not the landing.
	dir := t.TempDir()
	ground, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = t.TempDir()
		config.Place = Place{Dir: dir}
	})
	id := ground.graph().reserve()
	ground.graph().admit(id, taskSpec{title: "the untracked work", brief: "b", acceptance: "a"})
	waitDoneNode(t, ground.graph().node(id))
	if _, err := os.Stat(filepath.Join(dir, planStoreFilename)); !os.IsNotExist(err) {
		t.Fatalf("a session outside the experiment seeded a store: %v", err)
	}
}

// THE SHIM RIDES THE COMMAND, NOT THE PROCESS. Arming writes the shim and
// leaves the process environment exactly where it was — the process PATH is
// shared by every session in this process, and one that grew by a directory
// per plan would never shrink and would leak into conversations that never
// asked for a plan. What carries the directory instead is a PATH assignment
// prefixed to each bash command the belt runs ([Agent.planCommandArgs]), and
// the belt's own hand resolves the shim through it. The override wins unprobed
// (resolvePlanCLI), so a stub is all the CLI the arming needs here.
func TestPlandbShimRidesTheCommandNotTheProcessPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TASK_BELT", "bash")
	stub := filepath.Join(t.TempDir(), "stub-codeaf")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(planCLIBinEnv, stub)

	agent, _ := newTestAgent(t, &scriptedCompleter{}, bashBeltWorkerConfig(t))
	g := agent.graph()
	dir := t.TempDir()
	plan := &planState{path: filepath.Join(dir, planStoreFilename)}
	before := os.Getenv("PATH")
	g.planMu.Lock()
	err := plan.armShim()
	g.planMu.Unlock()
	if err != nil {
		t.Fatalf("the shim did not arm: %v", err)
	}
	g.planMu.Lock()
	g.plan = plan
	g.planMu.Unlock()
	if after := os.Getenv("PATH"); after != before {
		t.Fatalf("arming moved the process PATH: %q became %q", before, after)
	}

	// THE BELT'S OWN BASH RESOLVES THE SHIM. The command the model named is
	// prefixed with the assignment at the wrapper, and `command -v` performs
	// its lookup under the PATH the command itself was given.
	var bash bare.Tool
	for _, tool := range agent.beltTools() {
		if tool.Name == "bash" {
			bash = tool
			break
		}
	}
	if bash.Name != "bash" {
		t.Fatal("the belt carries no bash hand")
	}
	text, failed, err := bash.Execute(context.Background(), json.RawMessage(`{"command":"command -v plandb"}`))
	if err != nil || failed {
		t.Fatalf("the belt's bash failed: %q failed=%v err=%v", text, failed, err)
	}
	want := filepath.Join(dir, "bin", "plandb")
	if !strings.Contains(text, want) {
		t.Fatalf("command -v plandb answered %q, want the armed shim %q", text, want)
	}

	// AND THE CONVERSATION'S BASH CARRIES NO PREFIX. The graph is the
	// conversation's, shared with every node it admits, so an armed plan is
	// visible from a belt the experiment never composed — and the belt gate,
	// not the armed plan, is what keeps the person's own shell free of it.
	if got := agent.planCommand("plandb status"); !strings.HasPrefix(got, "export PATH=") || !strings.Contains(got, "PLANDB_DB=") {
		t.Fatalf("the belt worker's command carries no shim and store prefix: %q", got)
	}
	plain, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.tasker = g
	})
	if got := plain.planCommand("plandb status"); got != "plandb status" {
		t.Fatalf("the conversation's command was rewritten to %q, want it unchanged", got)
	}
}
