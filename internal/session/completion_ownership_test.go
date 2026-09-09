package session

// An ordinary turn can finish its answer while its command continues. The job
// retains its original owner and wakes that conversation with the final result.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const theAsk = "First run ./slow-build.sh here and let it finish. While it runs, write the requested CSV reports."

func awaitingAgent(t *testing.T, completer Completer) *Agent {
	t.Helper()
	answerTheNamerOffTheQueue(completer)
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.Divide = true
		config.Interactive = true
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
	})
	return agent
}

func TestAWaitOnItsOwnCommandAdmitsNoTask(t *testing.T) {
	var steps []step
	for i := 1; i <= 7; i++ {
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponseWithText(fmt.Sprintf("write-%d", i), "write", fmt.Sprintf(`{"path":"report%d.csv","content":"service,port"}`, i), "Writing the requested report."), nil
		})
	}
	steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("The reports are written; the build is still running."), nil
	})
	completer := &scriptedCompleter{steps: steps}
	agent := awaitingAgent(t, completer)
	graph := stubbedGraph(agent, func(*TaskNode) {})
	liveJob(agent, 1, jobKindBash, "./slow-build.sh")
	collect(t, mustSubmit(t, agent, theAsk))
	if admitted(graph) != 0 {
		t.Fatal("waiting on the caller's command admitted a task")
	}
	if !jobStillRunning(t, agent, 1) || !agent.turnIsWaitingOnItsOwnWork() {
		t.Fatal("finishing the reply lost or stopped the pending command")
	}
	if got := messageContentText(lastMessage(agent)); got != "The reports are written; the build is still running." {
		t.Fatalf("the caller's final answer was replaced: %q", got)
	}
	if completer.requests() != 8 {
		t.Fatalf("%d requests, want seven writes and the final answer", completer.requests())
	}
}

func jobStillRunning(t *testing.T, agent *Agent, id int) bool {
	t.Helper()
	one := agent.jobs.find(id)
	if one == nil {
		return false
	}
	return one.info().state == jobRunning
}

func endJob(t *testing.T, agent *Agent, id int) {
	if t != nil {
		t.Helper()
	}
	one := agent.jobs.find(id)
	one.mu.Lock()
	one.state = jobExited
	one.mu.Unlock()
}

func queueDirection(agent *Agent, words string) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.steering = append(agent.steering, steerMessage(&turnSteer{
		note: SteerNote{ID: agent.steerSeq.Add(1), Words: words},
	}))
}

func answeredAfterTheEnding(agent *Agent, note, answer string) bool {
	messages := agent.snapshot()
	for index, message := range messages {
		if message.Role != "user" || !strings.Contains(messageText(message), note) {
			continue
		}
		for _, later := range messages[index+1:] {
			if later.Role == "assistant" && strings.Contains(messageText(later), answer) {
				return true
			}
		}
	}
	return false
}

func TestTheAwaitedCommandsEndingWakesTheConversation(t *testing.T) {
	flag := filepath.Join(t.TempDir(), "release")
	command := "while [ ! -f " + flag + " ]; do sleep 0.02; done; echo built"

	var started, writes atomic.Int64
	steps := make([]step, 24)
	for index := range steps {
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			for _, message := range messages {
				if message.Role == "user" && strings.Contains(messageText(message), "job 1 exited 0") {
					return textResponse("The build finished successfully and the report is ready."), nil
				}
			}
			if started.Add(1) == 1 {
				arguments, _ := json.Marshal(struct {
					Command    string `json:"command"`
					Background bool   `json:"background"`
				}{Command: command, Background: true})
				return toolResponseWithText("start-build", "bash", string(arguments),
					"Starting the build in the background."), nil
			}
			if round := writes.Add(1); round <= 7 {
				arguments, _ := json.Marshal(struct {
					Path string `json:"path"`
					Text string `json:"content"`
				}{Path: fmt.Sprintf("report%d.csv", round), Text: "service,port\n"})
				return toolResponseWithText(fmt.Sprintf("write-%d", round), "write", string(arguments),
					"Writing the report."), nil
			}
			return textResponse("report.csv is written; the build is still running."), nil
		}
	}

	completer := &scriptedCompleter{steps: steps}
	agent := awaitingAgent(t, completer)
	graph := stubbedGraph(agent, func(*TaskNode) {})
	// THE WAKE LANE IS SUBSCRIBED BEFORE THE COMMAND CAN POSSIBLY END, and the
	// command is released whatever this case does next, so a failure leaves no
	// process behind.
	wakes := agent.Wakes()
	t.Cleanup(func() { _ = os.WriteFile(flag, []byte("go\n"), 0o600) })

	events, err := agent.Submit(context.Background(), theAsk)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	if count := admitted(graph); count != 0 {
		t.Fatalf("%d tasks were admitted over a command this conversation was awaiting", count)
	}
	if !jobStillRunning(t, agent, 1) {
		t.Fatal("the command was not left running by the decline")
	}

	// AND NOW THE COMMAND IS LET GO. Nothing else is touched: what happens next
	// is the registry's own ending and the session's own wake.
	if err := os.WriteFile(flag, []byte("go\n"), 0o600); err != nil {
		t.Fatalf("releasing the command: %v", err)
	}

	select {
	case stream := <-wakes:
		if stream == nil {
			t.Fatal("the wake lane carried a nil stream")
		}
		var ended bool
		for event := range stream {
			if event.Kind == EventTurnDone {
				ended = true
			}
		}
		if !ended {
			t.Fatal("the woken turn never ended")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the awaited command ended and no turn was started for it")
	}

	// THE ENDING IS IN THE CONVERSATION AS THE JOB'S OWN NEWS, and the model
	// answered AFTER it — which is the whole return path this decline rests on:
	// `job 1 exited 0` arrives as a note and the turn it starts speaks.
	if !answeredAfterTheEnding(agent, "job 1 exited", "The build finished successfully and the report is ready.") {
		t.Fatalf("nothing was said after the command's ending:\n%s", transcriptText(agent))
	}
	// AND NOTHING WAS DONE TWICE. The command ran once and no task was started
	// for it after the wake either.
	if got := started.Load(); got < 1 {
		t.Fatal("the fixture never started the command")
	}
	if ran := commandsRun(completer, command); ran != 1 {
		t.Fatalf("the command was issued %d times, want exactly one", ran)
	}
	if count := admitted(graph); count != 0 {
		t.Fatalf("%d tasks were admitted after the wake", count)
	}
}

// The latest request contains the complete short fixture transcript. Count
// actual assistant calls there rather than counting each replay of the history.
func commandsRun(completer *scriptedCompleter, command string) int {
	if completer.requests() == 0 {
		return 0
	}
	count := 0
	for _, message := range completer.request(completer.requests() - 1) {
		for _, call := range message.ToolCalls {
			var args struct {
				Command string `json:"command"`
			}
			if call.Function.Name == "bash" && json.Unmarshal([]byte(call.Function.Arguments), &args) == nil && args.Command == command {
				count++
			}
		}
	}
	return count
}
