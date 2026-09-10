package session

// The tasks tool, as tests: the search it always was, the LIVE read it grew,
// and the steering door beside it.
//
// Everything here runs against a STUBBED GRAPH and a hand-fed room. The seam
// under test is not the child agent — task_room_test.go exercises a real one
// end to end — it is what the model is told when it asks about work that is
// still going, and that answer is composed from a node's state, its recorder
// and its row. None of those needs a provider or a repository, and a test that
// took one would be testing the runner again.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// A MODEL CAN LOOK AT WORK THAT IS STILL GOING. It names the task, and what
// comes back is the present: the state, the call in flight, the steps behind it
// and the last lines the node said — never the index file, which by
// construction knows only work that is over.
func TestTasksToolReadsARunningNodeLive(t *testing.T) {
	agent, node, id := runningStubbedNode(t, "Fix the nil-map crash")
	room := node.openRoom()
	if room == nil {
		t.Fatal("a running node has no room")
	}
	room.publish(Event{Kind: EventTextDelta, Text: "The guard is missing;\nI will add it and a test.\n"})
	room.publish(Event{Kind: EventToolBegin, Tool: "bash", Hint: "bash go test ./internal/reconciler/…"})

	text, isError := runTool(t, agent, "tasks", fmt.Sprintf(`{"id":%d}`, id))
	if isError {
		t.Fatalf("tasks id %d answered as an error:\n%s", id, text)
	}
	for _, want := range []string{
		"Fix the nil-map crash",
		string(TaskRunning),
		// The one line a row has for what is happening in it.
		"live: bash go test ./internal/reconciler/…",
		// The node's own words, and the call under them.
		"The guard is missing;",
		"I will add it and a test.",
		"· bash go test ./internal/reconciler/…",
		// And the one thing the reader can do about it.
		fmt.Sprintf(`tasks id %d say "…"`, id),
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the live read is missing %q:\n%s", want, text)
		}
	}

	// A NAME IS THE SAME HANDLE AS AN ID, because the model has both in front of
	// it and should not have to know which one this tool prefers.
	byName, isError := runTool(t, agent, "tasks", `{"id":"fix-the-nil-map-crash"}`)
	if isError || !strings.Contains(byName, "live: bash go test") {
		t.Fatalf("the read by name did not answer the same task:\n%s", byName)
	}

	// The tail is the model's to bound, and the newest lines are the ones kept:
	// "what is it doing" is a question about the end of the story.
	short, _ := runTool(t, agent, "tasks", fmt.Sprintf(`{"id":%d,"lines":1}`, id))
	if !strings.Contains(short, "its last 1 line:") {
		t.Fatalf("lines:1 did not bound the tail:\n%s", short)
	}
	if strings.Contains(short, "The guard is missing;") {
		t.Fatalf("lines:1 kept the oldest line instead of the newest:\n%s", short)
	}

	// A FAILED CALL IS NEWS, and it is written from the tool's name and the
	// failure rather than from a gloss the failure never carried.
	room.publish(Event{Kind: EventToolFailed, Tool: "bash", Hint: "exit status 1"})
	failed, _ := runTool(t, agent, "tasks", fmt.Sprintf(`{"id":%d}`, id))
	if !strings.Contains(failed, "✗ bash: exit status 1") {
		t.Fatalf("the failed call is not in the tail:\n%s", failed)
	}
	// With nothing in flight the row says so, with the count that tells a node
	// thinking from a node stuck.
	if !strings.Contains(failed, "live: thinking after bash go test") {
		t.Fatalf("the row did not say the node is between calls:\n%s", failed)
	}
}

// A node nobody has watched has nothing to quote, and that is an answer rather
// than an empty section: the row is still the whole state.
func TestTasksToolReadsANodeThatHasNotSpoken(t *testing.T) {
	agent, node, id := runningStubbedNode(t, "Sweep the imports")
	if room := node.openRoom(); room == nil {
		t.Fatal("a running node has no room")
	}
	text, isError := runTool(t, agent, "tasks", fmt.Sprintf(`{"id":"%d"}`, id))
	if isError {
		t.Fatalf("tasks id answered as an error:\n%s", text)
	}
	if !strings.Contains(text, "It has not said anything yet.") {
		t.Fatalf("a silent node did not say so:\n%s", text)
	}
	if !strings.Contains(text, "live: starting") {
		t.Fatalf("a node with no call in flight did not say what it is:\n%s", text)
	}
}

// THE SEARCH ROWS CARRY IT TOO. A model that called tasks with no arguments and
// saw one running should not need a second call to learn whether it is working
// or circling.
func TestTasksToolRowsSayWhatARunningNodeIsDoing(t *testing.T) {
	agent, node, _ := runningStubbedNode(t, "Rewrite the parser")
	node.openRoom().publish(Event{Kind: EventToolBegin, Tool: "read", Hint: "read internal/parse/lex.go"})

	text, isError := runTool(t, agent, "tasks", `{}`)
	if isError {
		t.Fatalf("the search answered as an error:\n%s", text)
	}
	if !strings.Contains(text, "live: read internal/parse/lex.go") {
		t.Fatalf("the running row does not say what it is doing:\n%s", text)
	}
	// And it is never written down: the index is what work came to, and a line
	// on disk claiming a call in flight would be a present that is over.
	if line, err := json.Marshal(TaskIndexEntry{Title: "x", Activity: "read something"}); err != nil {
		t.Fatalf("marshal: %v", err)
	} else if strings.Contains(string(line), "read something") {
		t.Fatalf("the live activity reached the index file: %s", line)
	}
}

// THE STEERING DOOR, from the model's side: pull the state, decide, say one
// line. It lands in the worker's own lane, unframed, exactly as the person's
// line does through the room.
func TestTasksToolSteersARunningNode(t *testing.T) {
	agent, node, id := runningStubbedNode(t, "Move the config")

	// Nobody is in the room yet: a stubbed runner has no child. The line is kept
	// on the task's own record for its next round rather than queued onto nothing
	// or sent back (assignment.go), and the answer says the one thing a relayed
	// line must always say — that it is not the person's authority.
	text, isError := runTool(t, agent, "tasks", fmt.Sprintf(`{"id":%d,"say":"use etc/"}`, id))
	if isError {
		t.Fatalf("a line to a running node was refused:\n%s", text)
	}
	if !strings.Contains(text, "on the task's record") || !strings.Contains(text, "only the person's own direction") {
		t.Fatalf("the answer does not say what became of the line:\n%s", text)
	}

	// A worker arrives — the room's speaker is the agent the words reach.
	worker, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	node.openRoom().speaking(worker)

	text, isError = runTool(t, agent, "tasks",
		fmt.Sprintf(`{"id":%d,"say":"the config lives under etc/, not conf/"}`, id))
	if isError {
		t.Fatalf("steering a running node failed:\n%s", text)
	}
	if !strings.Contains(text, "only the person's own direction can move that") {
		t.Fatalf("the answer does not state the contract:\n%s", text)
	}
	if !steeringContains(worker, "the config lives under etc/, not conf/") {
		t.Fatalf("the line never reached the worker: %v", steeringQueue(worker))
	}
	// UNFRAMED: from the worker's side it is somebody talking, not a system
	// event about a task.
	for _, line := range steeringQueue(worker) {
		if strings.Contains(line, "task ") && strings.Contains(line, "says") {
			t.Fatalf("the line was decorated before it was delivered: %q", line)
		}
	}
}

// What the door says when there is nothing behind it. Every one of these is an
// ordinary tool result — the model asked a reasonable question — and every one
// of them names which thing was missing.
func TestTasksToolRefusesWhatItCannotDo(t *testing.T) {
	agent, _, id := runningStubbedNode(t, "Fix the nil-map crash")

	if text, isError := runTool(t, agent, "tasks", `{"say":"hello"}`); !isError ||
		!strings.Contains(text, "say needs an id") {
		t.Fatalf("say without an id was not refused:\n%s", text)
	}
	if text, isError := runTool(t, agent, "tasks", `{"continue":true}`); !isError ||
		!strings.Contains(text, "continue needs an id") {
		t.Fatalf("continue without an id was not refused:\n%s", text)
	}
	if text, isError := runTool(t, agent, "tasks", fmt.Sprintf(`{"id":%d,"continue":true}`, id)); !isError ||
		!strings.Contains(text, "still running") {
		t.Fatalf("continue on a running node was not refused:\n%s", text)
	}
	if text, isError := runTool(t, agent, "tasks", fmt.Sprintf(`{"id":%d}`, id+7)); !isError ||
		!strings.Contains(text, "No task") {
		t.Fatalf("an unknown id was not refused:\n%s", text)
	}
	if text, isError := runTool(t, agent, "tasks", `{"id":"not-a-task"}`); !isError ||
		!strings.Contains(text, "No task") {
		t.Fatalf("an unknown name was not refused:\n%s", text)
	}
	// A malformed call is the one thing that is not a question.
	if text, isError := runTool(t, agent, "tasks", `{"id":`); !isError ||
		!strings.Contains(text, "Invalid arguments") {
		t.Fatalf("malformed arguments were accepted:\n%s", text)
	}
}

// A LANDED NODE READS AS HISTORY. The live read is not a second way of asking
// what the work came to, so a node that has finished answers with its row and
// stops — no tail, no steering offer nobody can take.
func TestTasksToolReadsALandedNodeAsARow(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		node.openRoom().publish(Event{Kind: EventToolBegin, Tool: "read", Hint: "read one.go"})
		node.finish("added the guard", nil, "", "")
		node.graph.complete(node, TaskDone)
	})
	id := graph.reserve()
	// named, because this stands in for a proposal the model groomed and named
	// itself — the one kind of work the namer leaves alone (taskname.go).
	graph.admit(id, taskSpec{title: "Fix the nil-map crash", named: true, brief: "b", acceptance: "a"})
	waitDoneNode(t, graph.node(id))

	text, isError := runTool(t, agent, "tasks", fmt.Sprintf(`{"id":%d}`, id))
	if isError {
		t.Fatalf("reading a landed node failed:\n%s", text)
	}
	if !strings.Contains(text, "added the guard") {
		t.Fatalf("the landed row lost its outcome:\n%s", text)
	}
	for _, unwanted := range []string{"live:", "its last", "Steer it with"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("a landed node was drawn as a live one (%q):\n%s", unwanted, text)
		}
	}
}

// THE RESOLVE VERB, from the model's side. A node nobody could verify is the
// one kind of work the model can still settle: accept it on evidence it has
// read, or refute it. Both go through the same door a person's surface uses
// ([Agent.ResolveUnverified]), and both say what happened to the work waiting
// on it.
func TestTasksToolResolvesAnUnverifiedNode(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		node.finish("UNVERIFIED — the auditor answered neither VERIFIED nor REFUTED", nil, "", "")
		node.graph.complete(node, TaskUnverified)
	})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "Research the reconciler", brief: "b", acceptance: "a"})
	waitDoneNode(t, graph.node(id))

	// A resolution with no id is a resolution aimed at nothing.
	if text, isError := runTool(t, agent, "tasks", `{"resolve":"accept"}`); !isError ||
		!strings.Contains(text, "resolve needs an id") {
		t.Fatalf("resolve without an id was not refused:\n%s", text)
	}
	// A word nobody defined is refused rather than guessed at.
	if text, isError := runTool(t, agent, "tasks", fmt.Sprintf(`{"id":%d,"resolve":"probably"}`, id)); !isError ||
		!strings.Contains(text, "not a resolution") {
		t.Fatalf("an undefined resolution was accepted:\n%s", text)
	}

	text, isError := runTool(t, agent, "tasks",
		fmt.Sprintf(`{"id":%d,"resolve":"accept","say":"I read the diff: the tests are there and they pass"}`, id))
	if isError {
		t.Fatalf("accepting an unverified node failed:\n%s", text)
	}
	if !strings.Contains(text, "nobody else's check") {
		t.Fatalf("the answer hides that nobody verified it:\n%s", text)
	}
	if state := graph.node(id).stateNow(); state != TaskDone {
		t.Fatalf("the accepted node is %q, want done", state)
	}
	// THE REASON IS THE RECORD. `say` is the person's words when it rides a
	// resolution, and it is what the row will say this work came to.
	if report := graph.node(id).notice().Report; !strings.Contains(report, "the tests are there and they pass") {
		t.Fatalf("the reason was dropped from the report: %q", report)
	}
	// And a node that is not unverified cannot be resolved.
	if text, isError := runTool(t, agent, "tasks", fmt.Sprintf(`{"id":%d,"resolve":"refute"}`, id)); !isError ||
		!strings.Contains(text, "only a task that needs a look") {
		t.Fatalf("a done node was resolved a second time:\n%s", text)
	}
}

// ── harness ─────────────────────────────────────────────────────────────────

// runningStubbedNode admits one node whose runner never returns, so the graph
// holds it in TaskRunning for the whole test: the state every question in this
// file is asked in. The runner is released by the cleanup, so nothing is left
// blocked when the test ends.
func runningStubbedNode(t *testing.T, title string) (*Agent, *TaskNode, uint64) {
	t.Helper()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)

	started := make(chan struct{})
	release := make(chan struct{})
	graph := stubbedGraph(agent, func(node *TaskNode) {
		close(started)
		<-release
		node.finish("released", nil, "", "")
		node.graph.complete(node, TaskDone)
	})
	id := graph.reserve()
	// named, because the caller passes the name it wants the row drawn under —
	// which is what a groomed proposal does, and the one kind of work the namer
	// leaves alone (taskname.go).
	graph.admit(id, taskSpec{title: title, named: true, brief: "b", acceptance: "a"})
	<-started
	node := graph.node(id)
	t.Cleanup(func() {
		close(release)
		waitDoneNode(t, node)
	})
	return agent, node, id
}

// ── the stop verb ───────────────────────────────────────────────────────────

// THE MODEL CAN END RUNNING WORK, AND THROUGH THE PERSON'S OWN DOOR.
//
// Told to stop task 2, a model with no stop verb did the only thing its belt
// allowed: it said "stopped in favour of task 3, do not continue" INTO the task.
// The worker wrote down that it had been told to stop and delivered nothing, the
// check read that as an ordinary unfinished run, a round opened to close the
// gaps, and the task went on spending. This test is the verb that replaces that
// move: one call, the same [Agent.Cancel] road the stop card takes, the reason on
// the node's own record, and no check.
func TestTasksToolStopsARunningNodeThroughTheStopDoor(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	started, release := make(chan struct{}), make(chan struct{})
	// The runner takes no claim — no [TaskNode.setCancel] — so the stop settles
	// the node where it stands rather than promising a landing nothing would
	// bring, which is cancel.go's road for a node no goroutine has taken up.
	graph := stubbedGraph(agent, func(node *TaskNode) {
		close(started)
		<-release
	})
	t.Cleanup(func() { close(release) })

	id := graph.reserve()
	graph.admit(id, taskSpec{title: "Port the parser", named: true, brief: "b", acceptance: "a"})
	waitSignal(t, started, "the node to start")

	text, isError := runTool(t, agent, "tasks",
		fmt.Sprintf(`{"id":%d,"stop":true,"say":"I changed my mind, task 3 covers this"}`, id))
	if isError {
		t.Fatalf("stopping a running node was refused:\n%s", text)
	}
	for _, want := range []string{
		// The engine's own line, with the reason where a person's would be.
		"stopped " + taskStopName(id, "Port the parser") + ": I changed my mind, task 3 covers this",
		// And the fact the old workaround got wrong: a stop is not an unfinished
		// run, so nothing sends it back to close its gaps.
		"it is not checked and nothing re-runs it",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the stop answer is missing %q:\n%s", want, text)
		}
	}

	node := graph.node(id)
	if state := node.stateNow(); state != TaskFailed {
		t.Fatalf("the stopped node is %q, want settled the moment it was stopped", state)
	}
	notice := node.notice()
	if !notice.Stopped {
		t.Fatalf("the landing does not say somebody stopped it: %+v", notice)
	}
	// THE REASON IS THE RECORD, exactly as it is for an accept and a refute: the
	// row afterwards says why this work ended, and not merely that it did.
	if want := taskStoppedWord + ": I changed my mind, task 3 covers this"; notice.Report != want {
		t.Fatalf("the stopped node's report is %q, want %q", notice.Report, want)
	}

	// AND A SECOND STOP IS NOT AN ERROR. The task is settled, so the answer is
	// what it IS, in the word the person's own screen is showing them.
	again, isError := runTool(t, agent, "tasks", fmt.Sprintf(`{"id":%d,"stop":true}`, id))
	if isError {
		t.Fatalf("stopping a settled node answered as an error:\n%s", again)
	}
	if !strings.Contains(again, "is stopped") || !strings.Contains(again, "nothing to stop") {
		t.Fatalf("the answer does not say what the task is now:\n%s", again)
	}
}

// A TASK THAT FINISHED SAYS SO, IN THE PERSON'S WORD FOR IT. "Stop task 2" over
// work that landed a minute ago is a reasonable thing to have said, and a refusal
// would leave the model guessing whether it had ended anything.
func TestTasksToolStopOnASettledNodeAnswersWithItsState(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		node.finish("added the guard", nil, "", "")
		node.graph.complete(node, TaskDone)
	})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "Fix the nil-map crash", named: true, brief: "b", acceptance: "a"})
	waitDoneNode(t, graph.node(id))

	text, isError := runTool(t, agent, "tasks", fmt.Sprintf(`{"id":%d,"stop":true}`, id))
	if isError {
		t.Fatalf("stopping a landed node answered as an error:\n%s", text)
	}
	if !strings.Contains(text, "is "+taskWordDone) || !strings.Contains(text, "nothing to stop") {
		t.Fatalf("a landed node's stop does not say what it is:\n%s", text)
	}
}

// STOP IS THE ONE VERB THAT CANNOT SHARE A CALL. Continue puts the work back on,
// resolve settles what it produced, forward sends the person's words into it —
// and every one of those is a decision about a task this call is ending.
func TestTasksToolRefusesAStopSentWithAnotherVerb(t *testing.T) {
	agent, _, id := runningStubbedNode(t, "Sweep the call sites")

	for _, call := range []string{
		fmt.Sprintf(`{"id":%d,"stop":true,"continue":true}`, id),
		fmt.Sprintf(`{"id":%d,"stop":true,"resolve":"accept"}`, id),
		fmt.Sprintf(`{"id":%d,"stop":true,"forward":true}`, id),
	} {
		text, isError := runTool(t, agent, "tasks", call)
		if !isError || !strings.Contains(text, "stop ends the task") {
			t.Fatalf("%s was not refused:\n%s", call, text)
		}
	}
	// And a stop aimed at nothing names what is missing, the way every other
	// verb on this tool does.
	if text, isError := runTool(t, agent, "tasks", `{"stop":true}`); !isError ||
		!strings.Contains(text, "stop needs an id") {
		t.Fatalf("stop without an id was not refused:\n%s", text)
	}
}

// WHAT THE MODEL IS TOLD ABOUT THE TWO VERBS. The defect was not that the door
// was missing from the engine — a person's stop has always worked — it was that
// the model reading this schema had no word for ending work and one that looked
// close enough. So the description carries both halves: stop ends it, and a `say`
// telling a task to stop does not.
func TestTheTasksSchemaSaysStopEndsWorkAndSayDoesNot(t *testing.T) {
	if !strings.Contains(tasksDescription, "To END running work use stop") {
		t.Fatalf("the tool's description does not name the stop verb:\n%s", tasksDescription)
	}
	var schema struct {
		Properties map[string]struct {
			Type        string `json:"type"`
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(tasksSchemaJSON), &schema); err != nil {
		t.Fatalf("the tasks schema does not parse: %v", err)
	}
	stop, present := schema.Properties["stop"]
	if !present {
		t.Fatal("the tasks schema has no stop field")
	}
	if stop.Type != "boolean" {
		t.Fatalf("stop is a %q, want a boolean beside continue and forward", stop.Type)
	}
	for _, want := range []string{"same door the person's own stop pulls", "asks no confirmation"} {
		if !strings.Contains(stop.Description, want) {
			t.Fatalf("the stop field does not say %q:\n%s", want, stop.Description)
		}
	}
	// AND THE FIELD THE MODEL USED TO REACH FOR SAYS WHAT IT IS NOT.
	if say := schema.Properties["say"].Description; !strings.Contains(say, "It ends nothing") ||
		!strings.Contains(say, "stop is the door that ends work") {
		t.Fatalf("say does not say that it ends nothing:\n%s", say)
	}
}
