package session

import (
	"context"
	"errors"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// TestBashBeltWorkerStopsItsOwnJobThroughJobs is the defect the fresh-install
// run showed: a bash-belt worker whose `find /` had become job 1 called the
// `jobs` tool its belt carries, and the envelope answered that `jobs` was not
// on this belt, so the walk ran on for the rest of the task. A worker must be
// able to stop a job it started through the door the belt hands it, and the
// kill must reach the job's whole process group.
func TestBashBeltWorkerStopsItsOwnJobThroughJobs(t *testing.T) {
	completer := &routedCompleter{parent: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("start", "bash", `{"command":"sleep 900","background":true}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("stop", "jobs", `{"action":"kill","id":1}`), nil
		},
		finalText("stopped it"),
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.bashBelt = true
		config.InTask = true
		config.taskID = 1
	})
	collect(t, mustSubmit(t, agent, "go"))

	jobs := agent.jobs.all()
	if len(jobs) != 1 || jobs[0].cmd == nil || jobs[0].cmd.Process == nil {
		t.Fatalf("the background start left %d jobs, want the one sleep", len(jobs))
	}
	group := jobs[0].cmd.Process.Pid

	var answer string
	agent.mu.Lock()
	for _, message := range agent.messages {
		if message.Role == "tool" && message.ToolCallID == "stop" {
			answer = messageContentText(message)
		}
	}
	agent.mu.Unlock()
	if strings.HasPrefix(answer, bashEnvelopeMark) {
		t.Fatalf("the jobs call was refused by the envelope: %s", answer)
	}
	if answer != "job 1 killed" {
		t.Fatalf("the jobs kill answered %q, want the registry's own `job 1 killed`", answer)
	}
	if !processGroupGone(group) {
		t.Fatalf("process group %d is still alive after the worker stopped its job", group)
	}
}

// TestBashWorkerPageNamesOnlyDoorsTheEnvelopeAdmits is CLAUDE.md's law about
// system prompts, read against the envelope rather than against the composed
// list: a tool the page sends the worker to must be one a call to it RUNS, not
// merely one the wire lists. The page named `jobs` and `read_document` while
// the envelope refused every name but bash, and the prompt law's other test
// could not see it, because both names were composed onto the belt.
func TestBashWorkerPageNamesOnlyDoorsTheEnvelopeAdmits(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, bashBeltWorkerConfig(t))
	page := renderSystemAt(agent.config, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC))
	carried := beltNameSet(agent.beltTools())
	named := namesIn(page)
	for _, door := range []string{"jobs", "read_document"} {
		if !named[door] {
			t.Errorf("the bash worker's page no longer names `%s`; this law reads it", door)
		}
	}
	for name := range named {
		if !carried[name] || name == "bash" {
			continue
		}
		call := ai.ToolCall{ID: "call-" + name, Type: "function", Function: ai.ToolCallFunction{Name: name, Arguments: `{}`}}
		if fault := agent.bashEnvelopeFault([]ai.ToolCall{call}); fault != "" {
			t.Errorf("the page names `%s`, the belt carries it, and the envelope refuses a call to it: %s", name, fault)
		}
	}
}

// TestBashBeltEnvelopeStillRefusesANameTheBeltDoesNotCarry keeps the other
// half of the envelope: a name nothing on this belt answers to is refused
// before anything runs, in the envelope's own voice.
func TestBashBeltEnvelopeStillRefusesANameTheBeltDoesNotCarry(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, bashBeltWorkerConfig(t))
	for _, name := range []string{"read", "edit", "grep", "no_such_tool"} {
		call := ai.ToolCall{ID: "call-" + name, Type: "function", Function: ai.ToolCallFunction{Name: name, Arguments: `{}`}}
		fault := agent.bashEnvelopeFault([]ai.ToolCall{call})
		if !strings.HasPrefix(fault, bashEnvelopeMark) || !strings.Contains(fault, "`"+name+"` is not on this belt") {
			t.Errorf("a call to %s was not refused as a name the belt does not carry: %q", name, fault)
		}
	}
}

// processGroupGone polls briefly for a process group to have no members left,
// because a kill that has been answered may still be reaping the last child.
func processGroupGone(group int) bool {
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := syscall.Kill(-group, 0); errors.Is(err, syscall.ESRCH) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}
