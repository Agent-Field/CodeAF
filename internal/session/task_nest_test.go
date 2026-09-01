package session

// NESTED WORK, AS TESTS: a task that finds independent parts inside its brief
// hands them out, and the family that makes is one the person can see, steer and
// stop.
//
// Every test here drives the real doors — the belt the node is actually handed,
// the tool call it actually makes, the graph the conversation actually owns —
// because the whole of nesting is that there is NO second machine: a sub-task is
// a node in the conversation's own graph with one field filled in (task.go).
// What is scripted is only the part a test may not have, which is a worktree and
// a provider.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── harness ─────────────────────────────────────────────────────────────────

// nest is one conversation, the node it admitted, and the agent that IS that
// node — the shape [Agent.newTaskAgent] builds, with the graph's runner scripted
// so no node ever cuts a worktree or reaches a provider.
type nest struct {
	session *Agent
	graph   *TaskGraph
	parent  *TaskNode
	node    *Agent
}

// newNest builds that shape. run is what happens to every node the graph starts
// — the parent included, which is why the default leaves a node RUNNING and
// waiting: a parent that landed the moment it started could never hand anything
// out.
func newNest(t *testing.T, child Completer, run func(*TaskNode)) *nest {
	t.Helper()
	session, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := session.graph()
	if run == nil {
		run = func(*TaskNode) {}
	}
	graph.run = run

	id := graph.reserve()
	graph.admit(id, taskSpec{title: "the whole job", brief: "b", acceptance: "a", depth: 1})
	parent := graph.node(id)

	if child == nil {
		child = &scriptedCompleter{}
	}
	node, err := newAgent(Config{
		Workspace: t.TempDir(),
		Model:     "test/model",
		System:    "SYSTEM",
		InTask:    true,
		tasker:    graph,
		taskID:    id,
		taskDepth: 1,
	}, child)
	if err != nil {
		t.Fatalf("newAgent for the node: %v", err)
	}
	t.Cleanup(func() { _ = node.Close() })
	parent.openRoom().speaking(node)
	return &nest{session: session, graph: graph, parent: parent, node: node}
}

// pieceArgs is one well-formed propose_task call.
func pieceArgs(title string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(
		`{"title":%q,"summary":"s","brief":"b","deliverable":"d","acceptance":"a"}`, title))
}

// handOut makes one proposal from inside the node and returns what the model was
// told. Nothing is asked: there is nobody in a worktree to show a card to, so the
// clock approves at once ([Agent.askTask]).
func (n *nest) handOut(t *testing.T, title string) string {
	t.Helper()
	answer, _, err := n.node.proposeTask(context.Background(), pieceArgs(title))
	if err != nil {
		t.Fatalf("propose_task: %v", err)
	}
	return answer
}

// ── the family ──────────────────────────────────────────────────────────────

func TestASubTaskRegistersUnderTheTaskThatHandedItOut(t *testing.T) {
	updates := make(chan Event, 32)
	nest := newNest(t, nil, nil)
	// Subscribed BEFORE the proposal: the piece's first update is sent the
	// moment it is admitted, and a lane opened afterwards would be a lane that
	// missed the news it exists for.
	lane := nest.session.TaskUpdates()
	go func() {
		for event := range lane {
			select {
			case updates <- event:
			default:
			}
		}
	}()

	answer := nest.handOut(t, "read the law")
	if !strings.Contains(answer, "started") {
		t.Fatalf("the node was told %q, want the piece started", answer)
	}

	kids := nest.graph.children(nest.parent.id)
	if len(kids) != 1 {
		t.Fatalf("the parent has %d pieces, want the one it handed out", len(kids))
	}
	kid := kids[0]
	if kid.parent != nest.parent.id {
		t.Fatalf("piece %d hangs off %d, want the task that asked for it (%d)", kid.id, kid.parent, nest.parent.id)
	}
	if kid.depth != 2 {
		t.Fatalf("piece %d sits at depth %d, want one below its parent", kid.id, kid.depth)
	}
	if kid.id == nest.parent.id {
		t.Fatal("the piece took its parent's id: the family is not sharing one sequence")
	}

	// THE ROSTER'S HALF. A tree is drawn from TaskNotice.Parent and from nothing
	// else (internal/tui3's taskstrip.go), so a family the surface cannot see is
	// a family that does not exist.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-updates:
			if event.Task == nil || event.Task.ID != kid.id {
				continue
			}
			if event.Task.Parent != nest.parent.id {
				t.Fatalf("the piece's update says parent %d, want %d", event.Task.Parent, nest.parent.id)
			}
			return
		case <-deadline:
			t.Fatal("no update for the piece ever reached the conversation")
		}
	}
}

func TestAPieceOfWorkIsRunByTheTaskThatAskedForIt(t *testing.T) {
	var (
		mu     sync.Mutex
		owners = map[uint64]*Agent{}
	)
	nest := newNest(t, nil, func(node *TaskNode) {
		mu.Lock()
		owners[node.id] = node.graph.runner(node)
		mu.Unlock()
	})
	nest.handOut(t, "read the law")

	kid := nest.graph.children(nest.parent.id)[0]
	waitFor(t, "the piece to start", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return owners[kid.id] != nil
	})
	mu.Lock()
	defer mu.Unlock()
	if owners[kid.id] != nest.node {
		t.Fatal("the piece is run by the conversation, so it would branch off the person's tree instead of its parent's")
	}
	if owners[nest.parent.id] != nest.session {
		t.Fatal("the conversation's own task is run by somebody else")
	}
}

// ── the two bounds ──────────────────────────────────────────────────────────

func TestTheFanCapRefusesInWordsTheModelCanRead(t *testing.T) {
	nest := newNest(t, nil, nil)
	for i := 0; i < taskFanLimit; i++ {
		if answer := nest.handOut(t, fmt.Sprintf("piece %d", i)); strings.HasPrefix(answer, "no:") {
			t.Fatalf("piece %d was refused inside the cap: %s", i, answer)
		}
	}
	answer, isError, err := nest.node.proposeTask(context.Background(), pieceArgs("one too many"))
	if err != nil {
		t.Fatalf("propose_task: %v", err)
	}
	if !isError {
		t.Fatal("the refusal was handed back as an ordinary result")
	}
	for _, want := range []string{
		fmt.Sprintf("already handed out %d pieces", taskFanLimit),
		"Do the rest in your own hands",
	} {
		if !strings.Contains(answer, want) {
			t.Fatalf("the refusal reads %q, want it to say %q", answer, want)
		}
	}
	if kids := nest.graph.children(nest.parent.id); len(kids) != taskFanLimit {
		t.Fatalf("%d pieces were admitted, want the cap to hold at %d", len(kids), taskFanLimit)
	}
}

// A BATCH RUNS CONCURRENTLY (loop.go), and a model fanning out sends its calls
// in one batch — which is the exact moment a cap counted off admitted nodes
// alone would let everything through.
func TestTheFanCapHoldsAgainstOneBatchOfProposals(t *testing.T) {
	nest := newNest(t, nil, nil)
	var wait sync.WaitGroup
	for i := 0; i < taskFanLimit*3; i++ {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			_, _, _ = nest.node.proposeTask(context.Background(), pieceArgs(fmt.Sprintf("piece %d", i)))
		}(i)
	}
	wait.Wait()
	if kids := nest.graph.children(nest.parent.id); len(kids) != taskFanLimit {
		t.Fatalf("%d pieces were admitted at once, want no more than %d", len(kids), taskFanLimit)
	}
}

// A DECLINED PROPOSAL GIVES ITS SLOT BACK. The cap counts work that exists, not
// questions that were asked.
func TestARefusedProposalDoesNotSpendAFanSlot(t *testing.T) {
	nest := newNest(t, nil, nil)
	answer, _, _ := nest.node.proposeTask(context.Background(), json.RawMessage(`{"title":"t","summary":"s","brief":"b","deliverable":"d"}`))
	if !strings.Contains(answer, "acceptance is required") {
		t.Fatalf("a proposal with no done-condition answered %q", answer)
	}
	for i := 0; i < taskFanLimit; i++ {
		if answer := nest.handOut(t, fmt.Sprintf("piece %d", i)); strings.HasPrefix(answer, "no:") {
			t.Fatalf("piece %d was refused: %s", i, answer)
		}
	}
}

func TestATaskAtTheFloorOfTheTreeIsNotGivenTheVerb(t *testing.T) {
	nest := newNest(t, nil, nil)
	if !nest.node.hasTool("propose_task") {
		t.Fatal("a task that may split its work has no propose_task")
	}
	if !nest.node.hasTool("tasks") {
		t.Fatal("a task that may split its work cannot look at the pieces it handed out")
	}

	floor, err := newAgent(Config{
		Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM",
		InTask: true, tasker: nest.graph, taskID: nest.parent.id, taskDepth: taskDepthLimit,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	t.Cleanup(func() { _ = floor.Close() })
	for _, gone := range []string{"propose_task", "tasks"} {
		if floor.hasTool(gone) {
			t.Fatalf("%s is on the belt at the floor of the tree, where it can do nothing", gone)
		}
	}

	// AND THE PROMPT AGREES WITH THE BELT. A worker told how to split its work
	// and handed no tool to split it with is a worker that will try.
	deep := Config{Workspace: t.TempDir(), InTask: true, tasker: nest.graph, taskDepth: taskDepthLimit}
	if strings.Contains(renderSystem(deep), "Breaking the work up") {
		t.Fatal("the floor of the tree is told how to hand work out")
	}
	shallow := Config{Workspace: t.TempDir(), InTask: true, tasker: nest.graph, taskDepth: 1}
	rendered := renderSystem(shallow)
	if !strings.Contains(rendered, "Breaking the work up") {
		t.Fatal("a task that may split its work is never told so")
	}
	if !strings.Contains(rendered, fmt.Sprintf("at most %d pieces", taskFanLimit)) {
		t.Fatal("the prompt does not carry the fan cap the code enforces")
	}
	if strings.Contains(renderSystem(Config{Workspace: t.TempDir()}), "Breaking the work up") {
		t.Fatal("the conversation is given the task's own page")
	}
}

// ── the parent, re-entered ──────────────────────────────────────────────────

// A PARENT'S TURN ENDING IS NOT THE PARENT ENDING. It hands a piece out, says
// what it started, and stops talking; the runner holds it open, and the piece's
// report re-enters it exactly as a landing re-enters a conversation.
func TestAParentIsReEnteredWithItsPiecesReport(t *testing.T) {
	release := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "propose_task", string(pieceArgs("read the law"))), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("handed the reading out; carrying on with the rest"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the reading landed and I have folded it in"), nil
		},
	}}
	nest := newNest(t, completer, func(node *TaskNode) {
		if node.parent == 0 {
			return
		}
		go func() {
			<-release
			node.finish("the law is in section four", nil, "", "")
			node.graph.complete(node, TaskDone)
		}()
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = runTaskChild(context.Background(), nest.node, nest.parent,
			"do the whole job", nest.node.config.Workspace,
			taskLimits{maxSteps: taskMaxSteps, noProgress: taskNoProgress}, nil, io.Discard)
	}()

	// The parent's first turn ends with the piece still running. Nothing may end
	// the node here: that is the defect this whole lane closes.
	waitRequests(t, completer, 2)
	waitQuiet(t, nest.node)
	select {
	case <-done:
		t.Fatal("the task ended while a piece of it was still running")
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the task never ended after its piece reported")
	}

	if got := completer.requests(); got < 3 {
		t.Fatalf("the model was asked %d times, want a turn for the piece's report", got)
	}
	last := userTextIn(completer.request(completer.requests() - 1))
	for _, want := range []string{"the law is in section four", "finished"} {
		if !strings.Contains(last, want) {
			t.Fatalf("the report the parent was re-entered with reads %q, want %q in it", last, want)
		}
	}
}

// AND A PARENT THAT WAS STOPPED TAKES ITS PIECES WITH IT. Their work merges into
// the parent's copy of the repository, so a piece outliving it is work with
// nowhere to come home to.
func TestStoppingAParentStopsThePiecesItHandedOut(t *testing.T) {
	nest := newNest(t, nil, nil)
	nest.handOut(t, "read the law")
	kid := nest.graph.children(nest.parent.id)[0]

	nest.graph.stopChildren(nest.parent.id)
	waitDoneNode(t, kid)
	if state := kid.stateNow(); state != TaskFailed {
		t.Fatalf("the piece settled as %s, want the stop to have ended it", state)
	}
	if !kid.reported() {
		t.Fatal("the piece was stopped without its news reaching anybody")
	}
}

// THE PERSON'S OWN CAP MUST NOT DEADLOCK A FAMILY. A parent that is only waiting
// for its pieces is not using a lane, and holding one would mean the piece it is
// waiting for can never start (task_run.go's [TaskGraph.park]).
func TestAParentWaitingOnAPieceHandsBackItsLane(t *testing.T) {
	release := make(chan struct{})
	started := make(chan uint64, 4)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "propose_task", string(pieceArgs("read the law"))), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("handed the reading out"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("folded it in"), nil
		},
	}}
	nest := newNest(t, completer, func(node *TaskNode) {
		if node.parent == 0 {
			return
		}
		started <- node.id
		go func() {
			<-release
			node.finish("the law is in section four", nil, "", "")
			node.graph.complete(node, TaskDone)
		}()
	})
	// One lane, and the parent is already standing in it.
	nest.graph.mu.Lock()
	nest.graph.limit = 1
	nest.graph.mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = runTaskChild(context.Background(), nest.node, nest.parent,
			"do the whole job", nest.node.config.Workspace,
			taskLimits{maxSteps: taskMaxSteps, noProgress: taskNoProgress}, nil, io.Discard)
	}()

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the piece never started: the parent held the only lane while waiting for it")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the task never ended after its piece reported")
	}
}

// THE FAMILY IS ON DISK. A resume that lost the parent would redraw the tree
// flat and brief nobody wrongly — but the roster would be lying about work the
// person can still see (task_store.go).
func TestTheFamilySurvivesACheckpoint(t *testing.T) {
	nest := newNest(t, nil, nil)
	nest.handOut(t, "read the law")
	kid := nest.graph.children(nest.parent.id)[0]

	encoded, err := json.Marshal(nest.graph.document())
	if err != nil {
		t.Fatalf("encoding the graph: %v", err)
	}
	document, err := decodeTasks(encoded)
	if err != nil {
		t.Fatalf("the checkpoint a family wrote does not load: %v", err)
	}
	var found bool
	for _, record := range document.Nodes {
		if record.ID != kid.id {
			continue
		}
		found = true
		if record.Parent != nest.parent.id || record.Depth != 2 {
			t.Fatalf("the piece was written down as parent %d depth %d, want %d and 2", record.Parent, record.Depth, nest.parent.id)
		}
		restored := restoreNode(newTaskGraph(), record)
		if restored.parent != nest.parent.id || restored.depth != 2 {
			t.Fatalf("the piece came back as parent %d depth %d", restored.parent, restored.depth)
		}
	}
	if !found {
		t.Fatal("the piece is not in the checkpoint at all")
	}
}

// ── talking to a piece ──────────────────────────────────────────────────────

func TestAPieceCanBeSteeredFromTheConversationAndFromItsParent(t *testing.T) {
	worker, err := newAgent(Config{Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM", InTask: true},
		&scriptedCompleter{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	t.Cleanup(func() { _ = worker.Close() })

	nest := newNest(t, nil, func(node *TaskNode) {
		if node.parent != 0 {
			node.openRoom().speaking(worker)
		}
	})
	nest.handOut(t, "read the law")
	kid := nest.graph.children(nest.parent.id)[0]
	waitFor(t, "somebody to be in the piece's room", func() bool {
		return kid.openRoom().speaker() != nil
	})

	// The conversation's own door: one graph, so the person reaches a piece
	// exactly as they reach the task that handed it out.
	if _, err := nest.session.SteerTask(kid.id, "mind the lock order"); err != nil {
		t.Fatalf("the person cannot say anything to a piece: %v", err)
	}
	// And the parent's, through the tool it was given for it.
	answer, isError, err := nest.node.tasksTool().Execute(context.Background(),
		json.RawMessage(fmt.Sprintf(`{"id":"%d","say":"start with the frontier"}`, kid.id)))
	if err != nil || isError {
		t.Fatalf("the parent cannot say anything to its own piece: %q (%v)", answer, err)
	}
	said := strings.Join(steeringQueue(worker), "\n")
	for _, want := range []string{"mind the lock order", "start with the frontier"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the piece's queue reads %q, want %q in it", said, want)
		}
	}
}

// A NODE'S WINDOW IS ITS OWN FAMILY AND NOTHING ELSE. Its brief is still its
// whole world; what it has a right to is the work it handed out itself.
func TestTheTasksToolInsideANodeSeesOnlyItsOwnPieces(t *testing.T) {
	nest := newNest(t, nil, nil)
	stranger := nest.graph.reserve()
	nest.graph.admit(stranger, taskSpec{title: "somebody else's job", brief: "b", acceptance: "a", depth: 1})
	nest.handOut(t, "read the law")

	listed, _, err := nest.node.tasksTool().Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("tasks: %v", err)
	}
	if !strings.Contains(listed, "read the law") {
		t.Fatalf("the node's own piece is missing from %q", listed)
	}
	if strings.Contains(listed, "somebody else") {
		t.Fatalf("the node can read work it never asked for: %q", listed)
	}
	refused, isError, err := nest.node.tasksTool().Execute(context.Background(),
		json.RawMessage(fmt.Sprintf(`{"id":"%d"}`, stranger)))
	if err != nil {
		t.Fatalf("tasks: %v", err)
	}
	if !isError || !strings.Contains(refused, "among the pieces you handed out") {
		t.Fatalf("reading somebody else's task answered %q", refused)
	}
}

// ── waits ───────────────────────────────────────────────────────────────────

func waitRequests(t *testing.T, completer *scriptedCompleter, want int) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if completer.requests() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the model was asked %d times, want %d", completer.requests(), want)
}

// waitQuiet waits for a turn to be over rather than for a clock. It is what
// "the parent has stopped talking" means from outside.
func waitQuiet(t *testing.T, a *Agent) {
	t.Helper()
	for i := 0; i < 500; i++ {
		a.mu.Lock()
		running := a.running
		a.mu.Unlock()
		if !running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the turn never ended")
}

// ── steering a parent that is waiting on its pieces ──────────────────────────

// A PARKED PARENT IS STILL SOMEBODY YOU CAN TALK TO. It has said everything it
// had to say and handed its lane back, and [Agent.wakeLocked] declines inside a
// task — so the person's line has no turn of its own to land in and nothing on
// the queue can start one. It used to sit there for as long as the slowest piece
// ran, and be dropped outright when the last report closed the loop, while the
// tool that took it said "it arrives in its loop as the person's own words".
//
// Now the line IS the news: it releases the wait, the runner re-enters the model
// with it, and the door says the node was waiting so a surface can tell the
// person what they are about to see.
func TestSteeringAParkedParentWakesItAndArrivesInItsNextTurn(t *testing.T) {
	release := make(chan struct{})
	started := make(chan uint64, 4)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "propose_task", string(pieceArgs("read the law"))), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("handed the reading out"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("noted, I will hold it to that"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("folded it in"), nil
		},
	}}
	nest := newNest(t, completer, func(node *TaskNode) {
		if node.parent == 0 {
			return
		}
		started <- node.id
		go func() {
			<-release
			node.finish("the law is in section four", nil, "", "")
			node.graph.complete(node, TaskDone)
		}()
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = runTaskChild(context.Background(), nest.node, nest.parent,
			"do the whole job", nest.node.config.Workspace,
			taskLimits{maxSteps: taskMaxSteps, noProgress: taskNoProgress}, nil, io.Discard)
	}()

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the piece never started")
	}
	waitFor(t, "the parent to park on its piece", nest.parent.waitingOnItsPieces)

	waiting, err := nest.session.SteerTask(nest.parent.id, "the config lives under etc/")
	if err != nil {
		t.Fatalf("the person cannot say anything to a parked parent: %v", err)
	}
	if !waiting {
		t.Fatal("the door answered that the parent was working, so every surface would promise the line lands at a step it is not going to take")
	}

	// The whole claim: a third request, with the person's words in it, and no
	// piece has reported anything.
	waitRequests(t, completer, 3)
	said := userTextIn(completer.request(2))
	if !strings.Contains(said, "the config lives under etc/") {
		t.Fatalf("the turn the line woke reads %q, want the person's own words in it", said)
	}
	if nest.graph.children(nest.parent.id)[0].stateNow().settled() {
		t.Fatal("the piece landed first, so this proves nothing about the line waking anything")
	}

	// AND THE PARENT IS BACK TO WAITING, not finished: answering the person is
	// not the same as being done with the work it handed out.
	waitFor(t, "the parent to park again", nest.parent.waitingOnItsPieces)
	close(release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the task never ended after its piece reported")
	}
}

// AND A LINE NOBODY CAN READ ANY MORE IS REFUSED OUT LOUD. The worker closes the
// instant its last piece is folded in; a sentence handed to it after that goes
// onto a queue nothing will ever drain, and the person has to hear that rather
// than watch a room that says it arrived.
func TestALineSteeredAtAClosedWorkerIsRefusedRatherThanSwallowed(t *testing.T) {
	nest := newNest(t, nil, nil)
	if err := nest.node.Close(); err != nil {
		t.Fatalf("closing the worker: %v", err)
	}
	waiting, err := nest.session.SteerTask(nest.parent.id, "one more thing")
	if err == nil {
		t.Fatal("the person's line was taken by an agent that will never read it")
	}
	if waiting {
		t.Fatal("a refused line was reported as one that woke something")
	}
	if !strings.Contains(err.Error(), "nobody left to say it to") {
		t.Fatalf("the refusal reads %q, want it to say there is nobody in there", err)
	}
	if steeringContains(nest.node, "one more thing") {
		t.Fatal("the line was queued on the closed worker anyway")
	}
}

// ── THE FAMILY'S LEDGER ─────────────────────────────────────────────────────
//
// A family's product is its LEDGER — the paths it wrote — because the ledger is
// what a landing carries: [stageTaskWork] stages it on a repository ground and
// [taskTree.landMirror] lays it back over a folder one, and nothing anywhere
// walks the directory (task_ledger.go, and task_landing_test.go's header for
// what walking it cost).
//
// So a parent that hands work out has a ledger with a hole in it. The parts
// wrote into ITS tree and their paths are on THEIR lists; on a repository ground
// git closes the hole underneath, because a part's work arrives as commits on
// the very tree the parent merges. On a folder ground nothing closes it: the
// mirror held every file the family made, the parent's ledger named its own, and
// the person's folder got that one. The task said done and the check passed.
//
// The two tests below are that, run rather than described — a real graph, real
// workers, a real mirror, and a real folder to land into.

// familyCompleter is ONE provider standing behind a whole family, which is what
// a family really has: every worker in a tree is built with the conversation's
// own client ([Agent.newTaskAgentOn]). Each node is told apart by a mark its own
// brief carries, read off the FIRST user message and nowhere else — a parent's
// later turns quote its parts' briefs back at it, in the division it made and in
// the reports it is handed, so a router reading the whole transcript would
// answer as a part on the parent's own turn.
type familyCompleter struct {
	mu sync.Mutex
	// marks are tried in order, so a part is matched before the parent whose
	// words its own brief may carry.
	marks []string
	steps map[string][]step
	seen  map[string]int
	// checks is every packet a checker was shown, in the order they were asked.
	// It is the only place a test can see what a divider's check actually stood
	// on (task_audit.go's [auditQuestion]).
	checks []string
}

// checked answers what every checker in this family was shown.
func (c *familyCompleter) checked() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.checks...)
}

func (c *familyCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	// THE DIVISION'S REVIEWER IS NOT A WORKER, and it is asked with the whole
	// plan under its own brief — every mark at once. An answer it cannot parse is
	// the fail-open path, which admits the parts exactly as the worker wrote them
	// (task_divide.go's [Agent.reviewDivision]).
	if len(messages) > 0 && messageText(messages[0]) == divideReviewBrief {
		return textResponse("(no reviewer here)"), nil
	}
	// AND A CHECKER IS NOT A WORKER EITHER. Every node in the family is checked,
	// each one answers on its own packet, and the packets are kept so a test can
	// assert what the divider's check was standing on.
	if len(messages) > 1 && messages[0].Role == "system" &&
		strings.Contains(messageText(messages[0]), "You are an AUDITOR") {
		c.mu.Lock()
		c.checks = append(c.checks, messageText(messages[1]))
		c.mu.Unlock()
		return textResponse("VERIFIED — the notes are written"), nil
	}
	var brief string
	for _, message := range messages {
		if message.Role == "user" {
			brief = messageText(message)
			break
		}
	}
	c.mu.Lock()
	var next step
	for _, mark := range c.marks {
		if !strings.Contains(brief, mark) {
			continue
		}
		if index := c.seen[mark]; index < len(c.steps[mark]) {
			next = c.steps[mark][index]
		}
		c.seen[mark]++
		break
	}
	c.mu.Unlock()
	if next == nil {
		// A worker with nothing left to say ends its turn. A parent woken twice
		// by two parts landing at two instants takes this on the second pass.
		return textResponse("nothing further"), nil
	}
	return next(ctx, messages)
}

// dividesInto is one well-formed divide_work call whose parts carry the briefs
// named, which is how each part's worker is told apart from its siblings.
func dividesInto(evidence string, briefs ...string) step {
	parts := make([]string, 0, len(briefs))
	for i, brief := range briefs {
		parts = append(parts, fmt.Sprintf(
			`{"title":"part %d","summary":"s","brief":%q,"acceptance":"a"}`, i+1, brief))
	}
	arguments := fmt.Sprintf(`{"evidence":%q,"parts":[%s]}`, evidence, strings.Join(parts, ","))
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse("call-divide", "divide_work", arguments), nil
	}
}

// aFolderFamily runs one scripted family end to end and answers what it landed
// into. The parent stands on a plain folder, so it works in a mirror of it and
// lands by laying its ledger back over the original by name: it writes one note,
// hands two parts out, and each part writes one of its own into the same tree.
// All three are the deliverable.
func aFolderFamily(t *testing.T, audited bool) (ground string, family *TaskNode, said *familyCompleter) {
	t.Helper()
	ground = t.TempDir()
	writeFile(t, filepath.Join(ground, "notes.md"), "the original line\n")
	t.Setenv("HOME", t.TempDir())

	const (
		wholeMark  = "MARK-THE-WHOLE-JOB"
		firstMark  = "MARK-THE-FIRST-PART"
		secondMark = "MARK-THE-SECOND-PART"
	)
	said = &familyCompleter{
		marks: []string{firstMark, secondMark, wholeMark},
		seen:  map[string]int{},
		steps: map[string][]step{
			wholeMark: {
				writeCall("call-notes", "notes.md", "the line the parent wrote\n"),
				dividesInto(wideEvidence, firstMark, secondMark),
				finalText("the parts are out"),
				finalText("both parts are in, and the three notes are written"),
			},
			firstMark: {
				writeCall("call-first", "first.md", "the first part\n"),
				finalText("wrote first.md"),
			},
			secondMark: {
				writeCall("call-second", "second.md", "the second part\n"),
				finalText("wrote second.md"),
			},
		},
	}
	agent, _ := newTestAgent(t, said, func(config *Config) {
		config.Workspace = t.TempDir()
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskAudit = audited
		config.Divide = true
	})
	graph := agent.graph()
	id := graph.reserve()
	graph.admit(id, taskSpec{
		title: "write the three notes", named: true, wide: true,
		brief: wholeMark, deliverable: "three notes in the folder",
		acceptance: "the three notes are written",
		ground:     ground, mode: TaskModeMirror, depth: 1,
	})
	family = graph.node(id)
	waitDoneNode(t, family)

	notice := family.notice()
	if notice.State != TaskDone {
		t.Fatalf("state = %q, report = %q", notice.State, notice.Report)
	}
	if notice.Mode != TaskModeMirror {
		t.Fatalf("mode = %q, want the family on a folder to be mirrored", notice.Mode)
	}
	kids := graph.children(family.id)
	if len(kids) != 2 {
		t.Fatalf("the parent handed out %d parts, want the two it divided into", len(kids))
	}
	for _, kid := range kids {
		if kid.stateNow() != TaskDone {
			t.Fatalf("part %d landed %q: %s", kid.id, kid.stateNow(), kid.notice().Report)
		}
	}
	return ground, family, said
}

// theThreeNotes is what the person's folder must hold when the family lands.
var theThreeNotes = map[string]string{
	"notes.md":  "the line the parent wrote\n",
	"first.md":  "the first part\n",
	"second.md": "the second part\n",
}

// holdsTheThreeNotes fails unless every one of them is there, with its contents.
func holdsTheThreeNotes(t *testing.T, folder string) {
	t.Helper()
	for path, want := range theThreeNotes {
		if got := readFile(t, filepath.Join(folder, path)); got != want {
			t.Fatalf("%s in %s is %q, want %q", path, folder, got, want)
		}
	}
}

// THE DEFECT, PINNED: a folder family lands what the FAMILY made. The person's
// own folder holds the parent's note and both parts' notes, with no `files:`
// re-declaration anywhere.
func TestAFolderFamilyLandsWhatEveryPartWrote(t *testing.T) {
	ground, family, _ := aFolderFamily(t, false)
	holdsTheThreeNotes(t, ground)
	// AND THE LEDGER IT SETTLED WITH SAYS SO, which is the half every later road
	// reads: a parent absorbs what its parts settled holding, and a person's
	// accept hours later has nothing else to land (task_ledger.go).
	_, changed, _, _ := family.leavings()
	for want := range theThreeNotes {
		if !containsString(changed, want) {
			t.Fatalf("the family settled with %v, want %s on it", changed, want)
		}
	}
}

// AND THE SAME FAMILY WITH THE CHECK ON. The divider is checked on the whole
// tree its parts came home into — its own note named as its own, its parts'
// named as theirs — and only then does the family's product reach the folder.
func TestAnAuditedFolderFamilyIsCheckedOnItsWholeProductAndLandsIt(t *testing.T) {
	ground, family, said := aFolderFamily(t, true)
	holdsTheThreeNotes(t, ground)
	if merge := family.notice().Merge; merge != mergeInPlace {
		t.Fatalf("merge = %q, want a folder family to land in place", merge)
	}

	// THE DIVIDER'S OWN PACKET. Three checks were asked — the family's and its
	// two parts' — and the one that names the parts is the family's.
	var divider string
	for _, packet := range said.checked() {
		if strings.Contains(packet, "And the parts it handed out wrote, into the same tree:") {
			divider = packet
		}
	}
	if divider == "" {
		t.Fatalf("no check was told it was standing on a divided landing; %d were asked", len(said.checked()))
	}
	for _, want := range []string{"first.md", "second.md"} {
		if !strings.Contains(divider, want) {
			t.Fatalf("the divider's check was not shown %s:\n%s", want, divider)
		}
	}
	if !strings.Contains(divider, "Files it wrote: notes.md") {
		t.Fatalf("the divider's check was not told which note it wrote itself:\n%s", divider)
	}
}

// AND THE FOLD IS ON THE LEDGER, SO IT COMPOSES DOWNWARD AND OUTLIVES THE RUN.
//
// Three generations and two landings, both through the one road every landing
// takes (task_ledger.go's [landHome] and [keepHome]). The part lands first and
// absorbs its own part; the family then settles NEEDING A LOOK, which merges
// nothing; and a person accepts it afterwards, which is the road that has
// nothing to read but what the first landing wrote onto the node. Until the fold
// was persisted there, an accepted folder family laid the parent's slice over
// the person's folder and dropped everything under it.
//
// The third generation is put into the graph rather than run, because the runner
// bounds a family at two levels ([taskDepthLimit]) and a test may not pretend
// otherwise. What is exercised is the landing: the part's ledger is what the
// landing left on it, never a list the test wrote there.
func TestAnAcceptedFamilyLandsEveryGenerationsWork(t *testing.T) {
	ground := t.TempDir()
	writeFile(t, filepath.Join(ground, "notes.md"), "the original line\n")
	t.Setenv("HOME", t.TempDir())

	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = t.TempDir()
		config.AskConsent = false
	})
	graph := stubbedGraph(agent, func(*TaskNode) {})

	tree, err := prepareTaskTreeOn(context.Background(), Place{}, agent.config.Workspace,
		"cccc3333cccc3333", 7, "write the three notes", taskStand{dir: ground, mode: TaskModeMirror})
	if err != nil {
		t.Fatalf("prepareTaskTreeOn: %v", err)
	}
	// The family's whole product, in the one tree it was assembled in — a part of
	// a folder family works in its parent's own directory.
	writeFile(t, filepath.Join(tree.dir, "notes.md"), "the line the parent wrote\n")
	writeFile(t, filepath.Join(tree.dir, "part.md"), "the part's own\n")
	writeFile(t, filepath.Join(tree.dir, "under", "deeper.md"), "the part's part\n")

	familyID := graph.reserve()
	graph.admit(familyID, taskSpec{title: "write the three notes", named: true,
		brief: "b", acceptance: "a", ground: ground, mode: TaskModeMirror, depth: 1})
	family := graph.node(familyID)
	family.setTree(tree)

	partID := graph.reserve()
	graph.admit(partID, taskSpec{title: "one of the notes", named: true,
		brief: "b", acceptance: "a", parent: familyID, depth: 2})
	part := graph.node(partID)

	deeperID := graph.reserve()
	graph.admit(deeperID, taskSpec{title: "the note under it", named: true,
		brief: "b", acceptance: "a", parent: partID, depth: 3})
	deeper := graph.node(deeperID)
	deeper.finish("wrote the deeper note", []string{"under/deeper.md"}, "", mergeInPlace)
	graph.complete(deeper, TaskDone)

	// THE PART'S OWN LANDING. It works in its parent's directory, so there is
	// nothing to lay anywhere — and the ledger it settles with is still the
	// whole of the subtree under it.
	partTree := taskTree{dir: tree.dir, ground: tree.dir, merge: mergeInPlace, mode: TaskModeFolder}
	partLedger, merge, _ := landHome(part, partTree, []string{"part.md"})
	part.finish("wrote the note", partLedger, "", merge)
	graph.complete(part, TaskDone)

	if _, settled, _, _ := part.leavings(); !containsString(settled, "under/deeper.md") {
		t.Fatalf("the part settled with %v, want its own part's work folded in", settled)
	}

	// AND THE FAMILY LANDS NEEDING A LOOK, which merges nothing at all.
	kept, keptLedger := keepHome(family, tree, []string{"notes.md"})
	family.finish(needsLookLead+"nobody could judge this", keptLedger, "", kept)
	graph.complete(family, TaskUnverified)
	if _, err := os.Stat(filepath.Join(ground, "part.md")); !os.IsNotExist(err) {
		t.Fatal("work that was never accepted was laid over the person's folder")
	}
	// AND THE LIST IT SETTLED WITH IS ALREADY THE WHOLE FAMILY'S. Everything that
	// happens to this node from here — the accept below, a re-audit, the row a
	// dependent is handed, the checkpoint a resumed process reads — has nothing
	// else to read (task_ledger.go's [keepHome]).
	if _, settled, _, _ := family.leavings(); !containsString(settled, "under/deeper.md") {
		t.Fatalf("the family settled needing a look with %v, want its whole subtree on it", settled)
	}

	// AND THEN SOMEBODY READS IT AND SAYS IT HOLDS.
	if err := agent.ResolveUnverified(familyID, TaskAccept, "I read it myself"); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if state := family.stateNow(); state != TaskDone {
		t.Fatalf("the accepted family is %q: %s", state, family.notice().Report)
	}
	for path, want := range map[string]string{
		"notes.md":        "the line the parent wrote\n",
		"part.md":         "the part's own\n",
		"under/deeper.md": "the part's part\n",
	} {
		if got := readFile(t, filepath.Join(ground, filepath.FromSlash(path))); got != want {
			t.Fatalf("%s in the person's folder is %q, want %q", path, got, want)
		}
	}
}

// AND THE CHECK STANDS ON THE FAMILY'S PRODUCT, IN A CLEAN RESTORE OF IT.
// A mirror is restored from the folder it stands on with the ledger laid over it
// ([restoreFromFolder]), so a divider whose ledger has absorbed its parts is
// judged on the tree that would ship rather than on its own slice of it.
func TestTheRestoreOfADividersMirrorHoldsThePartsWork(t *testing.T) {
	ground := t.TempDir()
	writeFile(t, filepath.Join(ground, "notes.md"), "the original line\n")

	tree, err := prepareTaskTreeOn(context.Background(), Place{}, t.TempDir(), "dddd4444dddd4444", 8,
		"write the three notes", taskStand{dir: ground, mode: TaskModeMirror})
	if err != nil {
		t.Fatalf("prepareTaskTreeOn: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "notes.md"), "the line the parent wrote\n")
	writeFile(t, filepath.Join(tree.dir, "first.md"), "the first part\n")
	writeFile(t, filepath.Join(tree.dir, "build.log"), "noise nobody wrote\n")

	parent := loneTestNode(t, "write the three notes")
	parent.graph.mu.Lock()
	part := &TaskNode{graph: parent.graph, id: 2, parent: parent.id, state: TaskDone,
		spec: taskSpec{title: "the first note"}, changed: []string{"first.md"}}
	parent.graph.nodes[part.id] = part
	parent.graph.order = append(parent.graph.order, part.id)
	parent.graph.mu.Unlock()

	restored, why := restoreTaskWork(parent, tree, absorbedLedger(parent, []string{"notes.md"}))
	if !restored.restored {
		t.Fatalf("no restore was made: %s", why)
	}
	defer restored.drop()
	for path, want := range map[string]string{
		"notes.md": "the line the parent wrote\n",
		"first.md": "the first part\n",
	} {
		if got := readFile(t, filepath.Join(restored.dir, path)); got != want {
			t.Fatalf("%s in the restore is %q, want %q", path, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(restored.dir, "build.log")); !os.IsNotExist(err) {
		t.Fatal("the restore carried something nobody wrote")
	}
}

// AND A PART THAT WROTE NOTHING ABSORBS NOTHING. The emptiness law, at the one
// seam where a stray empty entry would become a path a landing tried to stage.
func TestAPartThatWroteNothingAddsNothingToTheLedger(t *testing.T) {
	parent := loneTestNode(t, "write the notes")
	parent.graph.mu.Lock()
	part := &TaskNode{graph: parent.graph, id: 2, parent: parent.id, state: TaskDone,
		spec: taskSpec{title: "the note nobody had to write"}}
	parent.graph.nodes[part.id] = part
	parent.graph.order = append(parent.graph.order, part.id)
	parent.graph.mu.Unlock()

	if got := absorbedLedger(parent, []string{"notes.md"}); len(got) != 1 || got[0] != "notes.md" {
		t.Fatalf("the ledger absorbed %v from a part that wrote nothing", got)
	}
	if got := absorbedLedger(parent, nil); len(got) != 0 {
		t.Fatalf("a family that wrote nothing settled with %v", got)
	}
}
