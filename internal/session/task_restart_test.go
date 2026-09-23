package session

import (
	"context"
	"encoding/json"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/exec"
	"github.com/Agent-Field/codeaf/internal/provider"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRetryTaskRestoresEachRunnerWithoutChangingIdentity(t *testing.T) {
	for _, kind := range []TaskKind{"", TaskKindQuick, TaskKindHarness, TaskKindSubharness} {
		for _, ending := range []TaskEnding{TaskEndingStopped, TaskEndingInterrupted, TaskEndingWire, TaskEndingRefused} {
			t.Run(string(kind)+"/"+string(ending), func(t *testing.T) {
				nest := newNest(t, nil, nil)
				node := pieceUnder(t, nest.graph, nest.parent.id, "retry the original work")
				nest.graph.mu.Lock()
				node.kind = kind
				node.state = TaskFailed
				node.stopped = true
				node.ending = ending
				node.report = "the previous attempt stopped"
				switch kind {
				case TaskKindQuick:
					node.spec.quick = &quickTaskSpec{line: "finish this", items: []string{"one", "two"}, done: []bool{true, false}}
				case TaskKindHarness:
					node.spec.design = &harnessDesignSpec{goal: "original goal", model: "test/model", effort: provider.EffortLow}
				case TaskKindSubharness:
					node.spec.run = &subharnessRunSpec{name: "original-program", input: json.RawMessage(`{"place":"here"}`), model: "test/model"}
				}
				record := node.recordLocked()
				bytes, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				var saved taskRecord
				if err = json.Unmarshal(bytes, &saved); err != nil {
					t.Fatal(err)
				}
				restored := restoreNode(nest.graph, saved)
				nest.graph.nodes[node.id] = restored
				nest.graph.mu.Unlock()
				if err := nest.session.RetryTask(node.id); err != nil {
					t.Fatal(err)
				}
				notice := restored.notice()
				if notice.State != TaskRunning || notice.Stopped || notice.Ending != "" {
					t.Fatalf("retry retained ending: %+v", notice)
				}
				nest.graph.mu.Lock()
				if restored.kind != kind || restored.id != node.id || restored.spec.brief != node.spec.brief {
					t.Error("retry changed assignment or identity")
				}
				switch kind {
				case TaskKindQuick:
					if restored.spec.quick.line != "finish this" || !restored.spec.quick.done[0] {
						t.Error("quick progress lost")
					}
				case TaskKindHarness:
					if restored.spec.design.goal != "original goal" || restored.spec.design.effort != provider.EffortLow {
						t.Error("design inputs lost")
					}
				case TaskKindSubharness:
					if restored.spec.run.name != "original-program" || string(restored.spec.run.input) != `{"place":"here"}` {
						t.Error("workflow inputs lost")
					}
				}
				nest.graph.mu.Unlock()
			})
		}
	}
}

func TestRetryTaskRefusesLegacyRowsWithoutOriginalRunner(t *testing.T) {
	for _, kind := range []TaskKind{TaskKindQuick, TaskKindHarness, TaskKindSubharness, TaskKindJob, TaskKindAdaptive} {
		nest := newNest(t, nil, nil)
		node := pieceUnder(t, nest.graph, nest.parent.id, "old row")
		nest.graph.mu.Lock()
		node.kind, node.state = kind, TaskFailed
		nest.graph.mu.Unlock()
		if err := nest.session.RetryTask(node.id); err == nil || !strings.Contains(err.Error(), "no saved restart instructions") {
			t.Fatalf("%s: %v", kind, err)
		}
		if node.stateNow() != TaskFailed {
			t.Fatal("missing instructions restarted work")
		}
	}
}

func TestRetryTaskRunsQuickWorkWithItsPreviousFinding(t *testing.T) {
	completer := newQuickLanes([]step{
		quickCall("q1", "read the ledger", "read RETRY-LEDGER and summarize", nil, nil),
		finalText("asked for it"),
	}).lane("RETRY-LEDGER", finalText("first result"), finalText("finished remaining work"))
	agent, graph, _ := quickAgent(t, completer)
	collect(t, mustSubmit(t, agent, "read the ledger"))
	node := quickNodeSaying(t, graph, "RETRY-LEDGER")
	waitDoneNode(t, node)
	graph.mu.Lock()
	node.state = TaskFailed
	node.report = "the missing ledger needs another attempt"
	graph.mu.Unlock()
	if err := agent.RetryTask(node.id); err != nil {
		t.Fatal(err)
	}
	waitDoneNode(t, node)
	if node.stateNow() != TaskDone {
		t.Fatalf("quick retry ended %s", node.stateNow())
	}
	if !completer.sawIn("RETRY-LEDGER", "the missing ledger needs another attempt") {
		t.Fatal("quick retry lost previous finding")
	}
}

func TestRetryTaskRestartsAStoppedDesignThroughItsOwnRunner(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) { <-ctx.Done(); return nil, ctx.Err() },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(designReply), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(reviewReply), nil },
	}}
	agent, _ := buildAgent(t, completer, t.TempDir())
	lane := agent.HarnessDesigns()
	submitBuild(t, agent)
	node := designNode(t, agent)
	waitForPhase(t, node, harnessPhaseDesigning)
	if _, err := agent.Cancel("task:" + itoa64(node.id)); err != nil {
		t.Fatal(err)
	}
	designOutcome(t, agent)
	if err := agent.RetryTask(node.id); err != nil {
		t.Fatal(err)
	}
	done := designDone(t, lane)
	if done.ID != node.id || done.Harness == nil {
		t.Fatal("retry did not produce a design under the same task")
	}
	agent.ResolveHarness(done.ID, true, "")
	if report := designOutcome(t, agent); !strings.Contains(report, "saved") {
		t.Fatal(report)
	}
}

func TestRetryTaskRunsSavedWorkflowWithOriginalInput(t *testing.T) {
	var calls atomic.Int32
	inputs := make(chan string, 2)
	program := &fakeRunner{manifest: theProgram(), run: func(_ context.Context, input json.RawMessage, _ exec.Env) (exec.RunResult, error) {
		inputs <- string(input)
		if calls.Add(1) == 1 {
			return exec.RunResult{Incomplete: "first attempt stopped"}, nil
		}
		return exec.RunResult{Output: json.RawMessage(`{"finding":"fixed"}`)}, nil
	}}
	agent := agentWithPrograms(t, registryWith(t, &fakeGeneralist{}, program), nil)
	input := json.RawMessage(`{"test":"TestFoo"}`)
	id, _, err := agent.SubharnessRun(context.Background(), "flake-triage", input)
	if err != nil {
		t.Fatal(err)
	}
	node := agent.taskNode(id)
	waitDoneNode(t, node)
	if node.stateNow() != TaskFailed {
		t.Fatal("first attempt did not fail")
	}
	if err := agent.RetryTask(id); err != nil {
		t.Fatal(err)
	}
	waitDoneNode(t, node)
	if node.stateNow() != TaskDone || calls.Load() != 2 {
		t.Fatal("workflow was not retried in place")
	}
	first, second := <-inputs, <-inputs
	if first != second || !strings.Contains(second, "TestFoo") {
		t.Fatalf("input changed: %s / %s", first, second)
	}
}
