package session

// The room, as tests: the three doors on a node, and what each of them says
// when there is nobody home.
//
// The live door is exercised end to end, against a real child agent in a real
// worktree, because "the events a person sees are the child's own, as they
// happen" is not a claim a stubbed graph can make: the whole seam under test is
// the fan-out inside runTaskChild. The refusals are exercised against a stubbed
// runner, because "no such task" and "that one is over" are the room's own law
// and want neither a provider nor git.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// steerLine is what the person types into the node's page. It is a correction
// with no frame around it: the child should read it as somebody talking.
const steerLine = "the greeting file is hello.txt, not greeting.txt"

// A NODE IS A PLACE. Somebody walks into a running node, watches its child work
// in real time, says one line to it, and the line lands in the child's own
// transcript as the person's words — while the node's journal, its history, is
// a file on disk the whole time.
func TestTaskRoomWatchesAndSteersARunningNode(t *testing.T) {
	repo := newTestRepo(t)
	t.Setenv("HOME", t.TempDir())

	var (
		// working says the child is inside its first provider call: the node is
		// running, its room is open, and nothing it does has happened yet.
		working = make(chan struct{})
		// release is the test letting the child take its first step, after the
		// watcher has subscribed and the person has spoken.
		release = make(chan struct{})
		// heard is what the child's SECOND request actually contained.
		heard = make(chan string, 1)
	)
	completer := &routedCompleter{
		parent: []step{
			proposeCall("Add the greeting", "write hello.txt containing hi"),
			finalText("handed off"),
		},
		child: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				close(working)
				<-release
				return toolResponse("call-write", "write",
					`{"path":"hello.txt","content":"hi\n"}`), nil
			},
			func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
				var said string
				for _, message := range messages {
					if message.Role != "user" {
						continue
					}
					if text := messageText(message); strings.Contains(text, steerLine) {
						said = text
					}
				}
				heard <- said
				return textResponse("Wrote hello.txt with the greeting."), nil
			},
		},
	}
	// The audit is off: this test is about the room, and the verdict lane is
	// task_test.go's subject.
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskAudit = false
	})
	graph := agent.graph()

	events, err := agent.Submit(context.Background(), "add a greeting")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	select {
	case <-working:
	case <-time.After(10 * time.Second):
		t.Fatal("the node's child never reached the provider")
	}
	node := graph.node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}

	// THE DOOR. Subscribed while the node is running and before it has done
	// anything, so everything below is live rather than replayed.
	stream, err := watchErr(agent.WatchTask(1))
	if err != nil {
		t.Fatalf("WatchTask: %v", err)
	}
	if err := agent.SteerTask(1, steerLine); err != nil {
		t.Fatalf("SteerTask: %v", err)
	}
	// The journal is the OTHER lane, and it is answerable while the work runs.
	journal := agent.TaskJournal(1)
	if !strings.Contains(filepath.ToSlash(journal), "/.aforge/v3/tasks/") ||
		!strings.HasSuffix(journal, "_1.jsonl") {
		t.Fatalf("TaskJournal = %q, want this node's own session file", journal)
	}
	close(release)

	watched := drainRoom(t, stream)
	if !watchedTool(watched, EventToolBegin, "write") {
		t.Fatalf("the watcher never saw the child begin its write: %v", kinds(watched))
	}
	if !watchedTool(watched, EventToolEnd, "write") {
		t.Fatalf("the watcher never saw the child's write land: %v", kinds(watched))
	}

	// The channel closed on its own, which is the node reaching its final state.
	waitDoneNode(t, node)
	if state := node.stateNow(); state != TaskDone {
		t.Fatalf("state = %q, report = %q", state, node.notice().Report)
	}

	select {
	case said := <-heard:
		if said == "" {
			t.Fatal("the person's line never reached the child's transcript")
		}
		if said != steerLine {
			t.Fatalf("the child was told %q, want the person's words undecorated", said)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the child never made a second request")
	}

	// The history door points at a file that exists.
	if _, err := os.Stat(journal); err != nil {
		t.Fatalf("the node's journal is not on disk: %v", err)
	}

	// AND THE ROOM IS SHUT. The work is over: a second watcher gets the close,
	// not a replay, and there is nobody left to talk to.
	closed, err := watchErr(agent.WatchTask(1))
	if err != nil {
		t.Fatalf("WatchTask on a finished node: %v", err)
	}
	if replayed := drainRoom(t, closed); len(replayed) != 0 {
		t.Fatalf("a finished node replayed %d events; the journal is the history", len(replayed))
	}
	if err := agent.SteerTask(1, "one more thing"); err == nil {
		t.Fatal("a finished node accepted steering")
	}
}

// The doors are honest about what is not there: an id this session never
// admitted, and a line with nothing in it.
func TestTaskRoomDoorsRefuseWhatIsNotThere(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)

	// A session that never groomed a task answers every door, and building a
	// graph is not the price of asking.
	if _, err := agent.WatchTask(1); err == nil {
		t.Fatal("WatchTask invented a task")
	}
	if err := agent.SteerTask(1, "hello"); err == nil {
		t.Fatal("SteerTask invented a task")
	}
	if path := agent.TaskJournal(1); path != "" {
		t.Fatalf("TaskJournal = %q for a task that does not exist", path)
	}

	graph := stubbedGraph(agent, func(node *TaskNode) {
		node.finish("stubbed", nil, "", "")
		node.graph.complete(node, TaskDone)
	})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "a node", brief: "b", acceptance: "a"})
	waitDoneNode(t, graph.node(id))

	if _, err := agent.WatchTask(id + 7); err == nil {
		t.Fatal("WatchTask answered for an unknown id")
	}
	if err := agent.SteerTask(id+7, "hello"); err == nil {
		t.Fatal("SteerTask answered for an unknown id")
	}
	if path := agent.TaskJournal(id + 7); path != "" {
		t.Fatalf("TaskJournal = %q for an unknown id", path)
	}
	// Nothing to say is not a message: it must not reach the child's lane as an
	// empty user turn.
	if err := agent.SteerTask(id, "   "); err == nil {
		t.Fatal("SteerTask accepted an empty line")
	}
}

// The room closes when the node lands, from whichever direction the landing
// comes — here the runner's own — and a watcher holding the channel is told so
// rather than left waiting on work that is over.
func TestTaskRoomClosesWhenTheNodeLands(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)

	started := make(chan struct{})
	release := make(chan struct{})
	graph := stubbedGraph(agent, func(node *TaskNode) {
		close(started)
		<-release
		node.finish("it landed", nil, "", "")
		node.graph.complete(node, TaskDone)
	})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "a node", brief: "b", acceptance: "a"})
	<-started

	stream, err := watchErr(agent.WatchTask(id))
	if err != nil {
		t.Fatalf("WatchTask on a running node: %v", err)
	}
	// Running, but nobody is in there: a stubbed runner has no child agent, and
	// "there is no worker to talk to" is a better answer than a line queued onto
	// nothing.
	if err := agent.SteerTask(id, "hello"); err == nil {
		t.Fatal("SteerTask found a worker where there is none")
	}

	close(release)
	waitDoneNode(t, graph.node(id))
	drainRoom(t, stream)

	if err := agent.SteerTask(id, "hello"); err == nil {
		t.Fatal("a landed node accepted steering")
	}
	if state := graph.node(id).stateNow(); state != TaskDone {
		t.Fatalf("state = %q", state)
	}
}

// ── harness ─────────────────────────────────────────────────────────────────

// watchErr is WatchTask's two returns, so a test can name the error line
// separately from the channel it is asserting on.
func watchErr(stream <-chan Event, err error) (<-chan Event, error) {
	return stream, err
}

// drainRoom reads one room's stream to close, failing rather than hanging: a
// channel that never closes is exactly the fault the room's close exists to
// prevent.
func drainRoom(t *testing.T, stream <-chan Event) []Event {
	t.Helper()
	var collected []Event
	deadline := time.After(10 * time.Second)
	for {
		select {
		case event, open := <-stream:
			if !open {
				return collected
			}
			collected = append(collected, event)
		case <-deadline:
			t.Fatalf("the room never closed; events so far: %v", kinds(collected))
			return nil
		}
	}
}

func watchedTool(events []Event, kind EventKind, tool string) bool {
	for _, event := range events {
		if event.Kind == kind && event.Tool == tool {
			return true
		}
	}
	return false
}
