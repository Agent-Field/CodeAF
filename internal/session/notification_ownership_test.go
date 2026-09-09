package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A job's receipt does not hand the same request to a second owner. The two
// turns use the real admission and notification doors; the worker is held in
// the existing stubbed executor so its completion cannot race the assertion.
func TestNotificationWritesDoNotHandOffAnOwnedRequestAgain(t *testing.T) {
	testNotificationOwnership(t, "writes")
}

// A short report must also stop without buying a reading of the whole goal.
// Otherwise fixing only the write seam leaves the same work repeating inline.
func TestNotificationReportDoesNotReopenAnOwnedRequest(t *testing.T) {
	testNotificationOwnership(t, "report")
}

func TestNotificationCannotProposeTheOwnedRequestAgain(t *testing.T) {
	testNotificationOwnership(t, "proposal")
}

// Repeating the same words is a new request, not a background wake. Exercise
// the real recorder and admission stamp so text equality cannot hide a new ask.
func TestNotificationOwnershipDoesNotFollowIdenticalNewUserWords(t *testing.T) {
	var graph *TaskGraph
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			graph.admit(graph.reserve(), taskSpec{title: "prepare the account", brief: "Write account.txt", acceptance: "account.txt contains the requested account"})
			return textResponse("The account is being prepared."), nil
		},
		finalText("This new request remains with this conversation."),
		finalText("The earlier command exited successfully; its account is still being prepared."),
	}}
	a := checkpointAgent(t, completer, func(c *Config) { c.Interactive = true })
	graph = stubbedGraph(a, func(*TaskNode) {})
	const words = "Prepare the requested account in account.txt."
	first, err := a.Submit(context.Background(), words)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, first)
	node := theRunningNode(t, graph)
	a.mu.Lock()
	firstRequest := a.personSeq
	a.mu.Unlock()
	second, err := a.Submit(context.Background(), words)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, second)
	a.mu.Lock()
	secondRequest := a.personSeq
	a.mu.Unlock()
	if secondRequest == firstRequest || node.admitRequest != firstRequest {
		t.Fatal("the new human message did not acquire its own request identity")
	}
	// The older job keeps its producer's identity even after the same words
	// were typed again. Its receipt still belongs beside the older worker.
	notice := userText("while you worked: the earlier job exited 0")
	notice.wake, notice.authored = true, true
	notice.backgroundResults = []backgroundResult{{text: "the earlier job exited 0", request: firstRequest}}
	events, err := a.submitUser(context.Background(), notice)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if got := a.backgroundWorkOwner(); got != node.id {
		t.Fatalf("earlier job lost its actual owner: got %d, want %d", got, node.id)
	}
	// A command belonging to the new request must not inherit that old owner.
	a.mu.Lock()
	a.owedAsks = []owedAsk{{text: backgroundReplyObligation, from: owedByBackground, request: a.personSeq}}
	a.mu.Unlock()
	if got := a.backgroundWorkOwner(); got != 0 {
		t.Fatalf("older task %d claimed the newer request's command receipt", got)
	}
}

func testNotificationOwnership(t *testing.T, mode string) {
	t.Helper()
	var waking atomic.Bool
	var rounds, readers, handoffs atomic.Int64
	steps := make([]step, 50)
	for i := range steps {
		steps[i] = func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForRemains(messages) {
				if waking.Load() {
					readers.Add(1)
				}
				return textResponse(checkpointNothingLeft), nil
			}
			if askedForSketch(messages) {
				return textResponse(checkpointChainSketch), nil
			}
			if askedForHandoff(messages) || askedToWriteHandoff(messages) {
				if waking.Load() {
					handoffs.Add(1)
				}
				// The paraphrase deliberately contains neither the task's number
				// nor its title. Request ownership must not depend on that wording.
				return textResponse("Produce the requested account, check its contents, and leave the completed document in the workspace."), nil
			}
			if !waking.Load() {
				if !alreadyProposed(messages) {
					return proposeAs("owner", "prepare the account", "Write account.txt with the requested account and check its contents.")(ctx, messages)
				}
				return textResponse("The account is being prepared."), nil
			}
			n := rounds.Add(1)
			if mode == "proposal" && n == 1 {
				return proposeAs("duplicate", "complete the document", "Produce the requested account, check its contents, and leave account.txt in the workspace.")(ctx, messages)
			}
			if mode == "writes" && n <= writeAllowanceCalls+1 {
				args, _ := json.Marshal(map[string]string{"path": fmt.Sprintf("receipt-%d.txt", n), "content": "The background command exited successfully.\n"})
				return toolResponseWithText(fmt.Sprintf("receipt-%d", n), "write", string(args), "Recording the background result."), nil
			}
			return textResponse("The command exited successfully; the account is still being prepared."), nil
		}
	}
	completer := &scriptedCompleter{steps: steps}
	agent, _ := writeSeamAgent(t, completer, func(c *Config) {
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	graph := stubbedGraph(agent, func(*TaskNode) {})
	first, err := agent.Submit(context.Background(), "Delegate preparation of account.txt, then report the finished account.")
	if err != nil {
		t.Fatal(err)
	}
	approveTasks(t, agent, first)
	node := theRunningNode(t, graph)
	agent.mu.Lock()
	request := agent.personSeq
	agent.mu.Unlock()
	if node.admitRequest != request || request == 0 {
		t.Fatalf("admission request=%d, human request=%d", node.admitRequest, request)
	}
	waking.Store(true)
	notice := userText("while you worked: job 7 exited 0: receipt ready")
	notice.wake, notice.authored = true, true
	notice.backgroundResults = []backgroundResult{{text: "job 7 exited 0: receipt ready", request: request}}
	events, err := agent.submitUser(context.Background(), notice)
	if err != nil {
		t.Fatal(err)
	}
	collected := approveTasks(t, agent, events)
	if got := admitted(graph); got != 1 {
		t.Fatalf("a background receipt admitted %d tasks; want only the original owner", got)
	}
	if readers.Load() != 0 || handoffs.Load() != 0 {
		t.Fatalf("report bought %d remains readings and %d handoff calls", readers.Load(), handoffs.Load())
	}
	if node.stateNow() != TaskRunning {
		t.Fatalf("report changed its worker's state to %s", node.stateNow())
	}
	if saidSomething(noticeTexts(collected), checkpointDoneNote) ||
		strings.Contains(transcriptText(agent), checkpointCarryOnLead) {
		t.Fatal("report either completed the unfinished goal or reopened its work")
	}
	if mode == "writes" && !saidSomething(noticeTexts(collected), backgroundOwnerNote(node.id)) {
		t.Fatalf("the write handoff boundary was not reached: %q", noticeTexts(collected))
	}
	if mode == "writes" && !saidSomething(noticeTexts(collected), "job 7 exited 0: receipt ready") {
		t.Fatal("parking at the write boundary dropped the actual background result")
	}
	if mode == "proposal" {
		if kind, found := toolResultKind(collected, "propose_task"); !found || kind != EventToolFailed {
			t.Fatalf("duplicate proposal result=%v, found=%v; want an explicit refusal", kind, found)
		}
	}
}

// Identity, provenance and liveness are independent of what an assignment is
// called. These controls keep a notification rule from serializing other work.
func TestNotificationOwnerRequiresTheSameLiveRequest(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Agent, *TaskNode)
		want   bool
	}{
		{"same request", func(*Agent, *TaskNode) {}, true},
		{"queued owner", func(_ *Agent, n *TaskNode) { n.state = TaskQueued }, true},
		{"older job after a new human request", func(a *Agent, _ *TaskNode) { a.personSeq++ }, true},
		{"new request's command", func(a *Agent, _ *TaskNode) { a.personSeq++; a.owedAsks[0].request = a.personSeq }, false},
		{"unknown producer", func(a *Agent, _ *TaskNode) { a.owedAsks[0].request = 0 }, false},
		{"mixed owned and main-owned receipts", func(a *Agent, _ *TaskNode) {
			a.owedAsks = append(a.owedAsks, owedAsk{text: backgroundReplyObligation, from: owedByBackground, request: 5})
		}, false},
		{"new human obligation", func(a *Agent, _ *TaskNode) {
			a.owedAsks = append(a.owedAsks, owedAsk{text: "same words", from: owedByPerson})
		}, false},
		{"owned task result", func(a *Agent, _ *TaskNode) {
			a.owedAsks = append(a.owedAsks, owedAsk{text: "same words", from: owedByResult, task: 1})
		}, false},
		{"worker context", func(a *Agent, _ *TaskNode) { a.config.InTask = true }, false},
		{"settled owner", func(_ *Agent, n *TaskNode) { n.state = TaskDone }, false},
		{"another agent's owner", func(_ *Agent, n *TaskNode) { n.admitBy = &Agent{} }, false},
		{"unproven restored identity", func(_ *Agent, n *TaskNode) { n.admitRequest = 0 }, false},
		{"unread user correction", func(a *Agent, _ *TaskNode) { queueDirection(a, "Add a second independent account.") }, false},
		{"child is not this conversation's owner", func(_ *Agent, n *TaskNode) { n.parent = 99 }, false},
		{"no notification obligation", func(a *Agent, _ *TaskNode) { a.owedAsks = nil }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := newTestAgent(t, &scriptedCompleter{}, nil)
			g := stubbedGraph(a, func(*TaskNode) {})
			a.personSeq = 4
			a.owedAsks = []owedAsk{{text: backgroundReplyObligation, from: owedByBackground, request: 4}}
			n := &TaskNode{graph: g, id: 1, admitBy: a, admitRequest: 4, state: TaskRunning}
			g.nodes[1], g.order = n, []uint64{1}
			tc.mutate(a, n)
			if got := a.backgroundWorkOwner(); (got == 1) != tc.want {
				t.Fatalf("background owner=%d, want owned=%v", got, tc.want)
			}
		})
	}
}
