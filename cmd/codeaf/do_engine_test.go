package main

// `codeaf do` ON THE RUN ENGINE — the second road do.go takes when the bash
// belt is asked for. These drive the whole command the way the resident road's
// own tests drive theirs (do_test.go): one doErrand, its real envelope on
// stdout, and the exit code read off the one ladder. The provider is scripted
// through the run road's own completer seam, so the belt worker's session is
// real and only the model's words are fake — the same bargain internal/run's
// bashworker tests make.
//
// THREE FACTS ARE UNDER TEST. A brief the scripted model completes leaves with
// exit 0 and an envelope naming the root's result, its landed file and the
// branch the landing answered. A ceiling of nothing leaves with exit 3 and
// `blocked_on` naming the price it was held to. And the usage ledger is the
// session's own — the worker the run hosts writes it, so the door adds no
// second accounting.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/session"
)

// beltAnswer is what the scripted worker reports as its own account of the
// work, and the text the envelope must carry as the root's result.
const beltAnswer = "the run engine wrote out.txt and reported it"

// beltSeat is the run road's scripted provider: one answer per call, in the
// order they arrive, and an `ever` answer for every call after the script runs
// out. It is [session.Completer], the type [run.CrewFactory]'s completer seam
// hands back, so the worker it hosts is the real belt worker and only the model
// is fake.
type beltSeat struct {
	mu     sync.Mutex
	script []func(context.Context, []ai.Message) (*ai.Response, error)
	ever   func(context.Context, []ai.Message) (*ai.Response, error)
	seen   int
}

func (s *beltSeat) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	s.mu.Lock()
	var next func(context.Context, []ai.Message) (*ai.Response, error)
	switch {
	case s.seen < len(s.script):
		next = s.script[s.seen]
	case s.ever != nil:
		next = s.ever
	}
	s.seen++
	s.mu.Unlock()
	if next == nil {
		return beltTextReply(beltAnswer), nil
	}
	return next(ctx, messages)
}

// beltTextReply is one plain assistant answer — the shape a belt turn ends on.
func beltTextReply(text string) *ai.Response {
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{
			Role:    "assistant",
			Content: []ai.ContentPart{{Type: "text", Text: text}},
		}}},
		Usage: &ai.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
}

// beltToolReply is one bash call, the shape the belt's one-action envelope
// accepts: exactly one call, named bash, its argument one command.
func beltToolReply(command string) *ai.Response {
	arguments := `{"command":` + quoteJSON(command) + `}`
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{
			Role: "assistant",
			ToolCalls: []ai.ToolCall{{
				ID:       "call-1",
				Type:     "function",
				Function: ai.ToolCallFunction{Name: "bash", Arguments: arguments},
			}},
		}}},
		Usage: &ai.Usage{PromptTokens: 20, CompletionTokens: 7, TotalTokens: 27},
	}
}

// quoteJSON spells one string as a JSON string literal. The command is the
// test's own and holds no character JSON needs escaped beyond the quotes it is
// wrapped in, so the encoding/json of it here is the whole of it.
func quoteJSON(text string) string {
	encoded, _ := json.Marshal(text)
	return string(encoded)
}

// beltStubCLI is the plandb shim's override: a program that answers nothing and
// exits, which is all the shim's arming probes through its override road
// (internal/session's resolvePlanCLI). Without it the belt worker cannot arm
// its `plandb` and never runs.
func beltStubCLI(t *testing.T) string {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "stub-codeaf")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return stub
}

// beltRunEnv puts the door on the run road: the belt's switch, the shim's
// override, a home of its own to read the ledger under, and a profile the crew
// resolves its seats from.
func beltRunEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CODEAF_HOME", home)
	t.Setenv("CODEAF_PROFILE_DIR", filepath.Join(home, "profile"))
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", beltStubCLI(t))
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	return home
}

// beltRepoWorkspace is the working copy the run lands on: a real repository on
// one committed file, so the landing has a branch to commit the run's work to
// and the envelope has a branch to name.
func beltRepoWorkspace(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	dir := t.TempDir()
	beltGit(t, dir, "init")
	beltGit(t, dir, "checkout", "-b", "work")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("the project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	beltGit(t, dir, "add", "-A")
	beltGit(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "first")
	return dir
}

func beltGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// THE RUN ROAD COMPLETES A BRIEF AND NAMES THE ROOT'S RESULT.
//
// The scripted worker writes one file through bash and then reports what it
// did; the run lands that file on the copy's branch, and the caller reads on
// stdout the root's own result, the landed path, and the branch the landing
// answered — the whole of what the run road owes an envelope.
func TestDoOnTheRunEngineCompletesABriefAndNamesTheRootResult(t *testing.T) {
	beltRunEnv(t)
	workspace := beltRepoWorkspace(t)
	seat := &beltSeat{
		script: []func(context.Context, []ai.Message) (*ai.Response, error){
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return beltToolReply("printf 'written by the run' > out.txt"), nil
			},
		},
		ever: func(context.Context, []ai.Message) (*ai.Response, error) { return beltTextReply(beltAnswer), nil },
	}

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "write out.txt and say what you did", workspace: workspace, asJSON: true,
		timeout: 60 * time.Second, slots: 1, stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return seat },
	})
	if err != nil {
		t.Fatalf("a brief the model completed left with %v, want 0\nstdout:\n%s\nstderr:\n%s",
			err, stdout.String(), stderr.String())
	}
	outcome := decodeErrand(t, stdout.String())
	if !strings.Contains(outcome.Deliverable, beltAnswer) {
		t.Fatalf("the envelope does not carry the root's result: %q", outcome.Deliverable)
	}
	if outcome.Nodes < 1 {
		t.Fatalf("the run reported %d nodes, want at least the root worker", outcome.Nodes)
	}
	if outcome.Seconds <= 0 {
		t.Fatal("the run reported no elapsed time")
	}
	// The landing committed the worker's file, and both the file and the
	// branch it went to are on the object.
	want := filepath.Join(workspace, "out.txt")
	if len(outcome.Artifacts) != 1 || outcome.Artifacts[0] != want {
		t.Fatalf("artifacts = %v, want the one landed path %s", outcome.Artifacts, want)
	}
	if !strings.Contains(outcome.Deliverable, "landed on work") {
		t.Fatalf("the answer never named the branch the landing answered:\n%s", outcome.Deliverable)
	}
}

// A CEILING OF NOTHING IS A LIMIT THAT STOPPED THE RUN, before any worker.
//
// The run road has no cost flag, and this is why the ceiling lives on the
// request: a caller holding a run to a price says so, and a price of zero
// admits no work at all. Exit 3 is the ladder's rung for a limit, and
// `blocked_on` names the price so a caller knows what to raise.
func TestDoOnTheRunEngineStopsAtACostCapOfZero(t *testing.T) {
	beltRunEnv(t)
	workspace := t.TempDir()
	zero := 0.0

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "write out.txt and say what you did", workspace: workspace, asJSON: true,
		timeout: 60 * time.Second, costCap: &zero, stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return &beltSeat{} },
	})
	var status exitStatus
	if !asExitStatus(err, &status) || status != exitLimit {
		t.Fatalf("a run held to nothing left with %v, want exit status 3", err)
	}
	outcome := decodeErrand(t, stdout.String())
	if !strings.Contains(outcome.BlockedOn, "cost cap") {
		t.Fatalf("blocked_on does not name the ceiling that stopped it: %q", outcome.BlockedOn)
	}
	if strings.TrimSpace(outcome.Deliverable) != "" {
		t.Fatalf("a run that did nothing carried a deliverable: %q", outcome.Deliverable)
	}
}

// THE USAGE LEDGER IS THE SESSION'S OWN, AND THE DOOR ADDS NO SECOND ONE.
//
// The worker the run hosts is a session agent, and that session is what writes
// the v3 usage ledger — the same file a conversation and every other task
// worker append to. This proves the run road leaves that accounting to the
// session rather than duplicating it into a ledger of its own: after a
// completed brief, the ledger this home holds has the worker's own call in it.
func TestDoOnTheRunEngineLeavesTheUsageLedgerToTheSession(t *testing.T) {
	home := beltRunEnv(t)
	workspace := beltRepoWorkspace(t)
	seat := &beltSeat{
		script: []func(context.Context, []ai.Message) (*ai.Response, error){
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return beltToolReply("printf 'written by the run' > out.txt"), nil
			},
		},
		ever: func(context.Context, []ai.Message) (*ai.Response, error) { return beltTextReply(beltAnswer), nil },
	}

	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task: "write out.txt and say what you did", workspace: workspace, asJSON: true,
		timeout: 60 * time.Second, slots: 1, stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return seat },
	}); err != nil {
		t.Fatalf("errand: %v\n%s", err, stderr.String())
	}
	// The ledger the session writes lives under this home; its path is the
	// session's own ([session.UsageLedgerPath]) and the door never spells it.
	lines, err := session.ReadUsage(filepath.Join(home, "v3", "usage.jsonl"), time.Time{})
	if err != nil {
		t.Fatalf("read the session's usage ledger: %v", err)
	}
	if len(lines) == 0 {
		t.Fatalf("the session the worker hosted wrote no usage row, so the run road "+
			"is accounting somewhere of its own:\n%s", stderr.String())
	}
}
