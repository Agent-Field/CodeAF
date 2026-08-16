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

	// Nobody is in the room yet: a stubbed runner has no child, and "there is no
	// worker to talk to" is a better answer than a line queued onto nothing.
	text, isError := runTool(t, agent, "tasks", fmt.Sprintf(`{"id":%d,"say":"use etc/"}`, id))
	if !isError || !strings.Contains(text, "no worker") {
		t.Fatalf("steering a node with no worker was not refused:\n%s", text)
	}

	// A worker arrives — the room's speaker is the agent the words reach.
	worker, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	node.openRoom().speaking(worker)

	text, isError = runTool(t, agent, "tasks",
		fmt.Sprintf(`{"id":%d,"say":"the config lives under etc/, not conf/"}`, id))
	if isError {
		t.Fatalf("steering a running node failed:\n%s", text)
	}
	if !strings.Contains(text, "brief and its acceptance are unchanged") {
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
	graph.admit(id, taskSpec{title: "Fix the nil-map crash", brief: "b", acceptance: "a"})
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
	graph.admit(id, taskSpec{title: title, brief: "b", acceptance: "a"})
	<-started
	node := graph.node(id)
	t.Cleanup(func() {
		close(release)
		waitDoneNode(t, node)
	})
	return agent, node, id
}
