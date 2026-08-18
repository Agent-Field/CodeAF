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
		`{"title":%q,"summary":"s","brief":"b","acceptance":"a"}`, title))
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
	answer, _, _ := nest.node.proposeTask(context.Background(), json.RawMessage(`{"title":"t","summary":"s","brief":"b"}`))
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
	if err := nest.session.SteerTask(kid.id, "mind the lock order"); err != nil {
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
