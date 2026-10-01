package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/manual"
)

// These scripts try to do the refused work anyway. They exercise the real
// Agent and the real file and shell tools, so an obedient model cannot hide a
// missing runtime guard.
func TestADeclinedProposalPreventsSameRequestWritesAndReplacementWork(t *testing.T) {
	for _, via := range []string{"", "senior-dev"} {
		name := via
		if name == "" {
			name = "ordinary task"
		}
		t.Run(name, func(t *testing.T) {
			registerBeltRunEngine(t, newBeltRunDouble("unused"))
			completer := &scriptedCompleter{steps: []step{
				declineProposalCall(via),
				declineActionCall("edit", `{"path":"strutil.py","edits":[{"oldText":"return s.lower()","newText":"return '-'.join(s.lower().split())"}]}`),
				writeCall("call-write", "written.txt", "unasked\n"),
				bashCall("call-bash", "printf unasked > shell.txt"),
				declineProposalCall(via),
				declineActionCall("quick_task", `{"title":"fix slugify","brief":"fix slugify in strutil.py","acceptance":"tests pass"}`),
				declineActionCall("read", `{"path":"strutil.py"}`),
				finalText("The work was not started. Tell me if you want a direct fix."),
			}}
			agent, workspace := declineTestAgent(t, completer)
			const original = "def slugify(s):\n    return s.lower()\n"
			writeFile(t, filepath.Join(workspace, "strutil.py"), original)
			graph := stubbedGraph(agent, func(*TaskNode) {
				t.Error("work started after the person declined")
			})
			asked := "fix slugify and its tests"
			if via != "" {
				asked += " with " + via
			}
			collected := drainAnsweringTasks(t, mustSubmit(t, agent, asked), func(event Event) {
				if event.Kind == EventTaskProposal && event.Task.Decided == nil {
					agent.ResolveTask(event.Task.ID, TaskAnswer{Redirect: "leave it alone"})
				}
			})
			if got, err := os.ReadFile(filepath.Join(workspace, "strutil.py")); err != nil || string(got) != original {
				t.Fatalf("declined work changed strutil.py: %q, %v", got, err)
			}
			for _, path := range []string{"written.txt", "shell.txt"} {
				if _, err := os.Stat(filepath.Join(workspace, path)); !os.IsNotExist(err) {
					t.Fatalf("declined work created %s: %v", path, err)
				}
			}
			if got := admitted(graph); got != 0 {
				t.Fatalf("declined work admitted %d tasks", got)
			}
			for _, tool := range []string{"edit", "write", "bash", "quick_task"} {
				assertDeclineRefusal(t, collected, tool)
			}
			outputs := proposeOutputs(collected)
			if len(outputs) != 2 || !strings.Contains(outputs[0], "the person declined this task: leave it alone") ||
				!strings.Contains(outputs[0], "no work was started") || !strings.Contains(outputs[1], "until a new message from the person") {
				t.Fatalf("proposal results lost the decline or allowed replacement work: %q", outputs)
			}
			if kind, ok := toolResultKind(collected, "propose_task"); !ok || kind != EventToolEnd {
				t.Fatal("the original decline became an error")
			}
			if kind, ok := toolResultKind(collected, "read"); !ok || kind != EventToolEnd {
				t.Fatal("reading the unchanged file was refused")
			}
		})
	}
}

func TestANewHumanMessageAllowsWorkAfterADeclinedProposal(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		declineProposalCall(""),
		writeCall("call-unasked", "strutil.py", "unasked\n"),
		finalText("The work was not started. Would you like a direct fix?"),
		writeCall("call-asked", "strutil.py", "def slugify(s):\n    return '-'.join(s.lower().split())\n"),
		finalText("Applied the direct fix you asked for."),
	}}
	agent, workspace := declineTestAgent(t, completer)
	writeFile(t, filepath.Join(workspace, "strutil.py"), "original\n")
	first := drainAnsweringTasks(t, mustSubmit(t, agent, "fix slugify and its tests"), func(event Event) {
		if event.Kind == EventTaskProposal && event.Task.Decided == nil {
			agent.ResolveTask(event.Task.ID, TaskAnswer{})
		}
	})
	assertDeclineRefusal(t, first, "write")
	second := collect(t, mustSubmit(t, agent, "Fix strutil.py directly now."))
	if kind, ok := toolResultKind(second, "write"); !ok || kind != EventToolEnd {
		t.Fatal("a new direct-fix request did not lift the refusal")
	}
	if got, err := os.ReadFile(filepath.Join(workspace, "strutil.py")); err != nil || !strings.Contains(string(got), "'-'.join") {
		t.Fatalf("the newly requested fix did not reach the file: %q, %v", got, err)
	}
}

func TestAnAutomaticWakeDoesNotLiftADeclinedProposal(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		declineProposalCall(""),
		finalText("The work was not started."),
		writeCall("call-wake", "strutil.py", "unasked\n"),
		finalText("The work still needs your word."),
	}}
	agent, workspace := declineTestAgent(t, completer)
	writeFile(t, filepath.Join(workspace, "strutil.py"), "original\n")
	drainAnsweringTasks(t, mustSubmit(t, agent, "fix slugify and its tests"), func(event Event) {
		if event.Kind == EventTaskProposal && event.Task.Decided == nil {
			agent.ResolveTask(event.Task.ID, TaskAnswer{})
		}
	})
	events, err := agent.submitUser(context.Background(), wakeNote("a background job ended"))
	if err != nil {
		t.Fatalf("automatic wake: %v", err)
	}
	assertDeclineRefusal(t, collect(t, events), "write")
	if got, err := os.ReadFile(filepath.Join(workspace, "strutil.py")); err != nil || string(got) != "original\n" {
		t.Fatalf("an automatic wake changed the declined file: %q, %v", got, err)
	}
}

func TestADeclineStopsAnAlreadyStagedSiblingFromStarting(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		proposalsInOneMessage("Fix slugify", "Fix its tests"),
		finalText("The work was not started."),
	}}
	agent, _ := declineTestAgent(t, completer)
	graph := stubbedGraph(agent, func(*TaskNode) { t.Error("a sibling started after a no") })
	var first uint64
	var questions int
	drainAnsweringTasks(t, mustSubmit(t, agent, "fix slugify and its tests"), func(event Event) {
		if event.Kind == EventTaskProposal && event.Task.Decided == nil {
			questions++
			if first == 0 {
				first = event.Task.ID
				return
			}
			// Both cards are open before either is answered. The second call
			// already passed pre-action, so admission must still respect the no.
			agent.ResolveTask(first, TaskAnswer{})
			agent.ResolveTask(event.Task.ID, TaskAnswer{Approved: true})
		}
	})
	if questions != 2 || admitted(graph) != 0 {
		t.Fatalf("a decline admitted sibling work: questions %d, nodes %d", questions, admitted(graph))
	}
}

func TestADeclineHoldsActionsAndKeepsObservation(t *testing.T) {
	agent, _ := declineTestAgent(t, &scriptedCompleter{})
	agent.mu.Lock()
	agent.proposalDeclined = true
	agent.mu.Unlock()
	for _, c := range []struct {
		tool, args string
		observe    bool
	}{
		{"read", `{"path":"strutil.py"}`, true},
		{"manual", `{"query":"decline a task"}`, true},
		{"ask", `{}`, true},
		{"tasks", `{}`, true},
		{"tasks", `{"id":"1","continue":true}`, false},
		{"tasks", `{"id":"1","say":"fix slugify"}`, false},
		{"tasks", `{"id":"1","forward":true}`, false},
		{"tasks", `{"id":"1","note":"fix slugify"}`, false},
		{"jobs", `{"action":"list"}`, true},
		{"jobs", `{"action":"output","id":1}`, true},
		{"jobs", `{"action":"kill","id":1}`, false},
		{"bash", `{"command":"python3 -m unittest"}`, false},
		{"generate_image", `{"prompt":"a picture"}`, false},
		{"workspace_restore", `{"confirm":true}`, false},
		{"workspace_merge", `{}`, false},
		{"propose_subharness", `{}`, false},
		{"a_future_writing_tool", `{}`, false},
	} {
		t.Run(c.tool+"/"+c.args, func(t *testing.T) {
			call := ai.ToolCall{Function: ai.ToolCallFunction{Name: c.tool, Arguments: c.args}}
			result, held := agent.declinedProposalCall(call)
			if held == c.observe || (held && !strings.Contains(result.text, "until a new message from the person")) {
				t.Fatalf("%s observation %v, held %v, result %q", c.tool, c.observe, held, result.text)
			}
		})
	}
}

func TestADeclinedProposalCannotBecomeAnAutomaticHandoff(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		declineProposalCall(""),
		finalText("The work was not started."),
	}}
	agent, _ := declineTestAgent(t, completer)
	graph := stubbedGraph(agent, func(*TaskNode) { t.Error("automatic replacement work started") })
	drainAnsweringTasks(t, mustSubmit(t, agent, "fix slugify and its tests"), func(event Event) {
		if event.Kind == EventTaskProposal && event.Task.Decided == nil {
			agent.ResolveTask(event.Task.ID, TaskAnswer{})
		}
	})
	if agent.checkpoints(context.Background(), userText("fix slugify and its tests")) {
		t.Fatal("declined work was eligible to be moved automatically")
	}
	said, id := agent.launchRouteTask(nil, routeVerdict{Goal: "fix slugify and its tests"}, "Fix slugify", drawnDivision{}, nil)
	if id != 0 || !strings.Contains(said, "until a new message from the person") {
		t.Fatalf("automatic route answered id %d, %q", id, said)
	}
	ending := agent.handOverRunningTurn(context.Background(), nil, &Usage{}, time.Now(), "test/model", "move the work", "test", 0, nil, routeVerdict{}, checkpointRead{}, nil)
	if ending.moved || ending.taskID != 0 || !strings.Contains(ending.reason, "until a new message from the person") {
		t.Fatalf("pending automatic handoff escaped the decline: %+v", ending)
	}
	if admitted(graph) != 0 {
		t.Fatal("automatic replacement work was admitted")
	}
}

func TestReadingAfterADeclineDoesNotStartAQuickWorker(t *testing.T) {
	registerBeltRunEngine(t, newBeltRunDouble("unused"))
	completer := &scriptedCompleter{steps: []step{
		declineProposalCall(""),
		declineActionCall("read", `{"path":"strutil.py"}`),
		declineActionCall("read", `{"path":"test_strutil.py"}`),
		declineActionCall("read", `{"path":"README.md"}`),
		finalText("The work was not started. I can discuss what I read."),
	}}
	agent, workspace := declineTestAgent(t, completer)
	for _, path := range []string{"strutil.py", "test_strutil.py", "README.md"} {
		writeFile(t, filepath.Join(workspace, path), "unchanged "+path+"\n")
	}
	graph := stubbedGraph(agent, func(node *TaskNode) {
		// Settle immediately so a missing guard fails on admission rather than
		// spending the read handoff's real timeout waiting for a fake worker.
		node.graph.complete(node, TaskDone)
	})
	collected := drainAnsweringTasks(t, mustSubmit(t, agent, "fix slugify and its tests"), func(event Event) {
		if event.Kind == EventTaskProposal && event.Task.Decided == nil {
			agent.ResolveTask(event.Task.ID, TaskAnswer{})
		}
	})
	if admitted(graph) != 0 {
		t.Fatal("reading after a decline started replacement work through a quick worker")
	}
	for _, path := range []string{"strutil.py", "test_strutil.py", "README.md"} {
		found := false
		for _, event := range collected {
			found = found || (event.Tool == "read" && event.Kind == EventToolEnd && strings.Contains(event.Output, "unchanged "+path))
		}
		if !found {
			t.Fatalf("%s was not read inline after the decline", path)
		}
	}
}

func declineTestAgent(t *testing.T, completer Completer) (*Agent, string) {
	t.Helper()
	return newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.TaskAutoApproveSeconds = 0
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		config.Delegates = testPrograms("senior-dev")
	})
}

func declineProposalCall(via string) step {
	arguments, _ := json.Marshal(taskArguments{
		Title: "Fix slugify and its tests", Summary: "Fix the slug utility and tests.",
		Brief: "Fix slugify in strutil.py and its tests.", Deliverable: "the fixed utility and tests",
		Acceptance: "python3 -m unittest passes", Via: via,
	})
	return declineActionCall("propose_task", string(arguments))
}

func declineActionCall(tool, arguments string) step {
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse("call-"+tool, tool, arguments), nil
	}
}

func assertDeclineRefusal(t *testing.T, events []Event, tool string) {
	t.Helper()
	for _, event := range events {
		if event.Tool == tool && event.Kind == EventToolFailed && event.Refused &&
			strings.Contains(event.Output, "until a new message from the person") {
			return
		}
	}
	t.Fatalf("%s did not return the engine's decline refusal; events: %v", tool, kinds(events))
}

func TestTheTaskManualQuotesTheDeclineGuard(t *testing.T) {
	page, ok := manual.Chat().Page("tasks")
	const sentence = "the person declined a task proposal, so no work was started; do not change files or start replacement work until a new message from the person; explain that and ask or offer in words"
	if !ok || !strings.Contains(page, sentence) {
		t.Fatalf("the task manual does not quote the decline refusal %q", sentence)
	}
}
