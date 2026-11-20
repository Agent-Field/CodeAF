package session

// The backstop and the app's quiet clock (bash_backstop.go). Every clock here is
// the test's own small figure through the Config seams, so nothing waits out the
// production bounds, and every wait is bounded by a select with a deadline.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// wedgeBash replaces the belt's bash with one that never answers and ignores
// its context — the shape of the run44 call, whose script finished and whose
// tool call never came back. Cleanup releases it.
func wedgeBash(t *testing.T, agent *Agent) {
	t.Helper()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	agent.armMu.Lock()
	defer agent.armMu.Unlock()
	for i := range agent.tools {
		if agent.tools[i].Name == "bash" {
			agent.tools[i].Execute = func(context.Context, json.RawMessage) (string, bool, error) {
				<-release
				return "late", false, nil
			}
			return
		}
	}
	t.Fatal("the belt has no bash tool")
}

// turnEndsWithin collects one turn, failing when it does not end in time.
func turnEndsWithin(t *testing.T, turn <-chan Event, within time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		for range turn {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(within):
		t.Fatalf("the turn was still running after %v", within)
	}
}

func scriptedBashThenReply(command string) *scriptedCompleter {
	args, _ := json.Marshal(map[string]any{"command": command})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("stuck-call", "bash", string(args)), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("That command hung, so I stopped it."), nil
		},
	}}
	answerTheReadingsOffTheQueue(completer)
	return completer
}

// THE RUN44 HANG: a bash call that never answers is answered for at the
// dispatcher's own clock, the model is told in plain words, the hang is
// written down, and the turn goes on to a reply.
func TestABashCallThatNeverAnswersIsAnsweredForAndTheTurnReplies(t *testing.T) {
	completer := scriptedBashThenReply("echo DASH=x")
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.bashBackstop = 200 * time.Millisecond
	})
	wedgeBash(t, agent)

	turnEndsWithin(t, mustSubmit(t, agent, "can u make a dashboard for the pipeline sheet"), 10*time.Second)

	if completer.requests() < 2 {
		t.Fatalf("the model was never asked again after the stuck call (%d requests)", completer.requests())
	}
	result := roleText(completer.request(1), "tool")
	if !strings.Contains(result, "Command stopped: it did not finish or answer within") {
		t.Fatalf("the model was not told the command was stopped: %q", result)
	}
	dumps, _ := filepath.Glob(filepath.Join(droppingsDir(agent.config.Place, agent.config.Workspace, droppingStuck), "*.txt"))
	if len(dumps) != 1 {
		t.Fatalf("want one stack dump naming the hang, got %v", dumps)
	}
	stacks, err := os.ReadFile(dumps[0])
	if err != nil || !strings.Contains(string(stacks), "goroutine") {
		t.Fatalf("the dump does not hold the stacks: %v", err)
	}
}

// A STOP THE TOOL CANNOT HEAR IS STILL A STOP: a person's interrupt ends a turn
// whose bash call is wedged, after the abort grace.
func TestAStopEndsATurnWhoseBashCallIsWedged(t *testing.T) {
	completer := scriptedBashThenReply("echo hi")
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		// The backstop is far away, so it is the stop that ends this turn.
		config.bashBackstop = time.Hour
		config.bashAbort = 150 * time.Millisecond
	})
	wedgeBash(t, agent)

	turn := mustSubmit(t, agent, "go")
	waitFor(t, "the bash call to start", func() bool {
		return agent.toolsInFlight.Load() > 0
	})
	agent.Interrupt()
	turnEndsWithin(t, turn, 10*time.Second)
}

// THE APP'S QUIET CLOCK: a foreground command that prints nothing is stopped
// with the plain sentence, before any handoff, and the turn replies.
func TestAQuietForegroundCommandIsStoppedAndTheTurnReplies(t *testing.T) {
	completer := scriptedBashThenReply("echo started; sleep 100000")
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.shellQuiet = 300 * time.Millisecond
		config.BashBackgroundAfterSeconds = 0
		config.LiveToolOutput = true
	})

	started := time.Now()
	turnEndsWithin(t, mustSubmit(t, agent, "go"), 10*time.Second)
	if took := time.Since(started); took > 8*time.Second {
		t.Fatalf("the quiet command held the turn for %v", took)
	}
	result := roleText(completer.request(1), "tool")
	if !strings.Contains(result, "Command stopped: it printed nothing for") || !strings.Contains(result, "started") {
		t.Fatalf("the model was not told the quiet command was stopped, with its output: %q", result)
	}
	if jobs := agent.jobs.all(); len(jobs) != 0 {
		t.Fatalf("a stopped command left %d jobs", len(jobs))
	}
}

// The clock is told to the model in the app, and only there.
func TestTheBashDescriptionNamesTheQuietClockOnlyWhenItIsOn(t *testing.T) {
	off, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if got := off.appShellQuietDescription(); got != "" {
		t.Fatalf("outside the app the description names a quiet clock: %q", got)
	}
	on, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.shellQuiet = AppShellQuietSeconds * time.Second
	})
	if got := on.appShellQuietDescription(); !strings.Contains(got, "120s") {
		t.Fatalf("the description does not name the app's figure: %q", got)
	}
}

// The backstop waits for the first bound the call will REALLY meet: a task's
// search is never handed to a job, so the handoff clock is not its bound.
func TestTheBackstopWaitsForTheBoundTheCallWillMeet(t *testing.T) {
	chat, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.BashBackgroundAfterSeconds = 30
	})
	if got := chat.bashBackstopAfter(json.RawMessage(`{"command":"sleep 100"}`)); got != 30*time.Second+bashBackstopGrace {
		t.Fatalf("a chat command's backstop is %v, want the handoff clock plus the grace", got)
	}
	task, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.BashBackgroundAfterSeconds = 30
		config.InTask = true
	})
	if got := task.bashBackstopAfter(json.RawMessage(`{"command":"grep -rn needle ."}`)); got != BashCeilingSeconds*time.Second+bashBackstopGrace {
		t.Fatalf("a task search's backstop is %v, want its own timeout plus the grace", got)
	}
}

// The manual quotes the figures this file enforces (the manual law).
func TestTheManualStatesTheShellBackstopFigures(t *testing.T) {
	page, err := os.ReadFile(filepath.Join("..", "manual", "chat", "what-i-can-do.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{
		appShellQuietSentence(AppShellQuietSeconds * time.Second),
		strconv.Itoa(int(bashBackstopGrace/time.Second)) + " seconds after",
		bashAbortedSentence,
		"Command stopped: it did not finish or answer within N\n  seconds, so it was ended and its result is lost.",
	} {
		if !strings.Contains(strings.Join(strings.Fields(string(page)), " "), strings.Join(strings.Fields(wanted), " ")) {
			t.Errorf("the manual does not say %q", wanted)
		}
	}
}
