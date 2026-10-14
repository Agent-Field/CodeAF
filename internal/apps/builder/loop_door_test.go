package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/apps/draft"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

// doorEnv marks a re-run of this test binary as the `codeaf` a worker's shell
// reaches. The worker's shim execs the running codeaf, and in a test the
// running binary is this one, so TestMain answers `app …` and `plandb …` the
// way cmd/codeaf does and never starts the suite.
const doorEnv = "BUILDER_LOOP_TEST_DOOR"

func TestMain(m *testing.M) {
	if os.Getenv(doorEnv) == "1" {
		os.Exit(door(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func door(args []string) int {
	if len(args) > 0 && args[0] == "plandb" {
		return plandb.Main(args[1:])
	}
	if len(args) > 0 && args[0] == "app" {
		return draft.Main(args[1:], os.Stdin, os.Stdout, os.Stderr)
	}
	fmt.Fprintln(os.Stderr, "door: unknown command")
	return 2
}

// armDoor makes `codeaf` in a worker's shell this test binary.
func armDoor(t *testing.T) {
	t.Helper()
	t.Setenv("CODEAF_TASK_BELT", "bash")
	path := filepath.Join(t.TempDir(), "codeaf")
	script := "#!/bin/sh\n" + doorEnv + "=1 exec " + os.Args[0] + " \"$@\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	session.SetRunningCLI(path)
	t.Cleanup(func() { session.SetRunningCLI("") })
}

// workerScript is the provider the loop's worker talks to: one answer per call,
// in order. When the script is spent every call answers with plain words, which
// is a worker that has stopped acting.
type workerScript struct {
	mu    sync.Mutex
	steps []func(context.Context) *ai.Response
	calls int
}

func (w *workerScript) CompleteWithMessages(ctx context.Context, msgs []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	if isSideCall(msgs) {
		return wordsReply("A short summary of the work so far."), nil
	}
	w.mu.Lock()
	n := w.calls
	w.calls++
	w.mu.Unlock()
	if n >= len(w.steps) {
		return wordsReply("I have nothing more to do."), nil
	}
	return w.steps[n](ctx), nil
}

// isSideCall tells the session's own housekeeping request (a summary of the
// recent transcript, sent when it chooses to, on a timer of its own) from a turn
// of the worker. It must not take a step: under load it arrived between two
// turns and the worker lost the step it was owed.
func isSideCall(msgs []ai.Message) bool {
	if len(msgs) == 0 {
		return false
	}
	for _, part := range msgs[len(msgs)-1].Content {
		if strings.HasPrefix(part.Text, "Recent transcript:") {
			return true
		}
	}
	return false
}

func (w *workerScript) used() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls
}

func wordsReply(text string) *ai.Response {
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}}}}},
		Usage:   &ai.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
}

func commandReply(command string) *ai.Response {
	args, _ := json.Marshal(struct {
		Command string `json:"command"`
	}{command})
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{
			ID: "call", Type: "function", Function: ai.ToolCallFunction{Name: "bash", Arguments: string(args)},
		}}}}},
		Usage: &ai.Usage{PromptTokens: 20, CompletionTokens: 7, TotalTokens: 27},
	}
}

// types is a step in which the worker runs one shell command.
func types(command string) func(context.Context) *ai.Response {
	return func(context.Context) *ai.Response { return commandReply(command) }
}

// script joins commands into a worker script.
func script(steps ...func(context.Context) *ai.Response) *workerScript {
	return &workerScript{steps: steps}
}

// plannedByWorker is the worker's first task: a name, the plan lines and
// `plan ready`, then the end of its task.
func planScript() []func(context.Context) *ai.Response {
	return []func(context.Context) *ai.Response{
		types(`codeaf app name "CRM" --about "Companies, deals and contacts"`),
		types(`codeaf app plan data deal "Deal · name, stage, amount"`),
		types(`codeaf app plan screen pipeline "Pipeline · deals by stage"`),
		types(`codeaf app plan agent "Drafts follow-ups for stale deals"`),
		types(`codeaf app plan ready`),
		types(`plandb done plan --agent plan --result "plan ready"`),
	}
}

func finishTask(id string) func(context.Context) *ai.Response {
	return types(`plandb done ` + id + ` --agent ` + id + ` --result "finished"`)
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if strings.Contains(h, needle) {
			return true
		}
	}
	return false
}
