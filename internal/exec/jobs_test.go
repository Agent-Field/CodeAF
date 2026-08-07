package exec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func backgroundToolbox(t *testing.T) (*Toolbox, *Workspace) {
	t.Helper()
	space := workspace(t)
	tools := NewToolbox(space, 1, nil)
	t.Cleanup(func() { tools.Close() })
	return tools, space
}

func waitForFileText(t *testing.T, path, contains string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(path)
		if err == nil && ((contains == "" && strings.TrimSpace(string(body)) != "") ||
			(contains != "" && strings.Contains(string(body), contains))) {
			return string(body)
		}
		time.Sleep(20 * time.Millisecond)
	}
	body, err := os.ReadFile(path)
	t.Fatalf("%s did not contain %q: body=%q err=%v", path, contains, body, err)
	return ""
}

func waitForJobDone(t *testing.T, tools *Toolbox, id int, timeout time.Duration) {
	t.Helper()
	tools.jobs.mutex.Lock()
	job := tools.jobs.jobs[id]
	tools.jobs.mutex.Unlock()
	if job == nil {
		t.Fatalf("job %d not registered", id)
	}
	select {
	case <-job.done:
	case <-time.After(timeout):
		t.Fatalf("job %d did not finish within %s", id, timeout)
	}
}

func primaryJobResult(content string) string {
	if before, _, found := strings.Cut(content, "\n\n["); found {
		return before
	}
	return content
}

func TestBackgroundStartReturnsImmediatelyAndCreatesDurableLog(t *testing.T) {
	tools, space := backgroundToolbox(t)
	started := time.Now()
	result := tools.Execute(context.Background(), "sh", `{"cmd":"printf 'ready\\n'; sleep 30","bg":true}`)
	if result.IsError {
		t.Fatalf("background start failed: %s", result.Content)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("background start blocked for %s", elapsed)
	}
	if !strings.Contains(result.Content, "job 1 started · log .aforge/jobs/1.log") {
		t.Fatalf("start result = %q", result.Content)
	}
	logPath := filepath.Join(space.Root(), ".aforge", "jobs", "1.log")
	waitForFileText(t, logPath, "ready")
	if artifacts := space.Artifacts(1); len(artifacts) != 1 || artifacts[0] != ".aforge/jobs/1.log" {
		t.Fatalf("artifacts = %v, want durable job log", artifacts)
	}
}

func TestKeepTransfersOwnershipAndLeafTeardownCountsOnlyKilledJobs(t *testing.T) {
	tools, _ := backgroundToolbox(t)
	for index := 0; index < 2; index++ {
		result := tools.Execute(context.Background(), "sh", `{"cmd":"sleep 30","bg":true}`)
		if result.IsError {
			t.Fatalf("start job %d: %s", index+1, result.Content)
		}
	}
	kept := tools.Execute(context.Background(), "job", `{"id":1,"keep":{"name":"dev-server","health":"port:5173"}}`)
	if kept.IsError || !strings.Contains(kept.Content, "promotion requested") {
		t.Fatalf("keep result = %+v", kept)
	}
	requests := tools.ServiceRequests("leaf-1")
	if len(requests) != 1 || requests[0].Name != "dev-server" || requests[0].LeafNodeID != "leaf-1" {
		t.Fatalf("service requests = %+v", requests)
	}
	if killed := tools.Close(); killed != 1 {
		t.Fatalf("leaf teardown killed %d jobs, want only the unpromoted job", killed)
	}
	if err := syscall.Kill(requests[0].PID, 0); err != nil {
		t.Fatalf("requested service did not survive leaf teardown: %v", err)
	}
	requests[0].Stop()
	if err := syscall.Kill(requests[0].PID, 0); err == nil {
		t.Fatal("declined service process still alive")
	}
}

func TestJobGuidanceNamesKeepAndForbidsNohup(t *testing.T) {
	tools, _ := backgroundToolbox(t)
	definitions := tools.Definitions()
	var description string
	for _, definition := range definitions {
		if definition.Function.Name == "job" {
			description = definition.Function.Description
		}
	}
	if !strings.Contains(description, "use keep — never nohup") {
		t.Fatalf("job guidance = %q", description)
	}
}

func TestBackgroundStartFailureIsImmediateError(t *testing.T) {
	tools, _ := backgroundToolbox(t)
	if result := tools.Execute(context.Background(), "sh", `{"cmd":"","bg":true}`); !result.IsError {
		t.Fatalf("empty background command = %+v, want error", result)
	}
	t.Setenv("PATH", t.TempDir())
	result := tools.Execute(context.Background(), "sh", `{"cmd":"printf unreachable","bg":true}`)
	if !result.IsError || !strings.Contains(result.Content, "could not start background job") {
		t.Fatalf("spawn failure = %+v, want immediate error", result)
	}
}

func TestBackgroundTimeoutIsCappedByLeafDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	duration, err := backgroundDuration(ctx, map[string]any{"t": float64(3600)})
	if err != nil {
		t.Fatal(err)
	}
	if duration <= 0 || duration > 2*time.Second {
		t.Fatalf("deadline-capped duration = %s", duration)
	}
	withoutDeadline, err := backgroundDuration(context.Background(), map[string]any{"t": float64(7200)})
	if err != nil || withoutDeadline != maxUndeadlinedJobSeconds*time.Second {
		t.Fatalf("undeadlined duration = %s, %v; want %s", withoutDeadline, err, maxUndeadlinedJobSeconds*time.Second)
	}
}

func TestForegroundShBytesMatchWhenBGIsAbsentOrFalse(t *testing.T) {
	tools, _ := backgroundToolbox(t)
	absent := tools.Execute(context.Background(), "sh", `{"cmd":"printf exact"}`)
	explicitFalse := tools.Execute(context.Background(), "sh", `{"cmd":"printf exact","bg":false}`)
	if absent.IsError || explicitFalse.IsError || absent.Content != "exact" || explicitFalse.Content != absent.Content {
		t.Fatalf("foreground results changed: absent=%+v false=%+v", absent, explicitFalse)
	}
}

func TestJobPeekReturnsOnlyNewOutput(t *testing.T) {
	tools, space := backgroundToolbox(t)
	started := tools.Execute(context.Background(), "sh", `{"cmd":"printf 'first\\n'; sleep 1; printf 'second\\n'; sleep 30","bg":true}`)
	if started.IsError {
		t.Fatal(started.Content)
	}
	logPath := filepath.Join(space.Root(), ".aforge", "jobs", "1.log")
	waitForFileText(t, logPath, "first")
	first := primaryJobResult(tools.Execute(context.Background(), "job", `{"id":1}`).Content)
	if !strings.Contains(first, "first") {
		t.Fatalf("first peek = %q", first)
	}
	waitForFileText(t, logPath, "second")
	second := primaryJobResult(tools.Execute(context.Background(), "job", `{"id":1}`).Content)
	if !strings.Contains(second, "second") || strings.Contains(second, "first") {
		t.Fatalf("second peek did not contain only new output: %q", second)
	}
	third := primaryJobResult(tools.Execute(context.Background(), "job", `{"id":1}`).Content)
	if strings.Contains(third, "first") || strings.Contains(third, "second") {
		t.Fatalf("third peek replayed output: %q", third)
	}
}

func TestJobWaitReturnsOnExitAndCapsAStillRunningWait(t *testing.T) {
	t.Run("exit before cap", func(t *testing.T) {
		tools, _ := backgroundToolbox(t)
		if result := tools.Execute(context.Background(), "sh", `{"cmd":"sleep 1; printf done","bg":true}`); result.IsError {
			t.Fatal(result.Content)
		}
		started := time.Now()
		result := tools.Execute(context.Background(), "job", `{"id":1,"wait":5}`)
		if result.IsError || !strings.Contains(result.Content, "exited 0") || !strings.Contains(result.Content, "done") {
			t.Fatalf("wait result = %+v", result)
		}
		if elapsed := time.Since(started); elapsed > 3*time.Second {
			t.Fatalf("wait ignored early exit and blocked %s", elapsed)
		}
	})

	t.Run("wait expiry", func(t *testing.T) {
		tools, _ := backgroundToolbox(t)
		if result := tools.Execute(context.Background(), "sh", `{"cmd":"sleep 30","bg":true}`); result.IsError {
			t.Fatal(result.Content)
		}
		started := time.Now()
		result := tools.Execute(context.Background(), "job", `{"id":1,"wait":1}`)
		elapsed := time.Since(started)
		if result.IsError || !strings.Contains(result.Content, "job 1 · running") {
			t.Fatalf("wait result = %+v", result)
		}
		if elapsed < 800*time.Millisecond || elapsed > 3*time.Second {
			t.Fatalf("one-second wait lasted %s", elapsed)
		}
	})
}

func TestJobKillTerminatesTheWholeProcessGroup(t *testing.T) {
	tools, space := backgroundToolbox(t)
	command := `{"cmd":"bash -c 'sleep 30 & echo $! > child.pid; sleep 30'","bg":true}`
	if result := tools.Execute(context.Background(), "sh", command); result.IsError {
		t.Fatal(result.Content)
	}
	pidText := strings.TrimSpace(waitForFileText(t, filepath.Join(space.Root(), "child.pid"), ""))
	childPID, err := strconv.Atoi(pidText)
	if err != nil {
		t.Fatalf("child pid %q: %v", pidText, err)
	}
	result := tools.Execute(context.Background(), "job", `{"id":1,"kill":true}`)
	if result.IsError || !strings.Contains(result.Content, "killed") {
		t.Fatalf("kill result = %+v", result)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && syscall.Kill(childPID, 0) == nil {
		time.Sleep(20 * time.Millisecond)
	}
	if err := syscall.Kill(childPID, 0); err == nil {
		t.Fatalf("grandchild %d survived process-group kill", childPID)
	}
}

func TestExitedBackgroundShellDoesNotLeaveDetachedChildren(t *testing.T) {
	tools, space := backgroundToolbox(t)
	command := `{"cmd":"sleep 30 & echo $! > detached.pid","bg":true}`
	if result := tools.Execute(context.Background(), "sh", command); result.IsError {
		t.Fatal(result.Content)
	}
	pidText := strings.TrimSpace(waitForFileText(t, filepath.Join(space.Root(), "detached.pid"), ""))
	childPID, err := strconv.Atoi(pidText)
	if err != nil {
		t.Fatalf("child pid %q: %v", pidText, err)
	}
	waitForJobDone(t, tools, 1, 5*time.Second)
	if err := syscall.Kill(childPID, 0); err == nil {
		t.Fatalf("detached child %d survived its background shell", childPID)
	}
}

func TestBackgroundHardCapMarksTimedOut(t *testing.T) {
	tools, _ := backgroundToolbox(t)
	if result := tools.Execute(context.Background(), "sh", `{"cmd":"sleep 30","bg":true,"t":1}`); result.IsError {
		t.Fatal(result.Content)
	}
	result := tools.Execute(context.Background(), "job", `{"id":1,"wait":4}`)
	if result.IsError || !strings.Contains(result.Content, "timed out") {
		t.Fatalf("hard-cap result = %+v", result)
	}
}

func TestTurnBoundaryReportsRunningAndOneTerminalTransition(t *testing.T) {
	noJobs, _ := backgroundToolbox(t)
	if result := noJobs.Execute(context.Background(), "sh", `{"cmd":"printf exact"}`); result.Content != "exact" {
		t.Fatalf("no-job path changed bytes: %q", result.Content)
	}

	tools, _ := backgroundToolbox(t)
	if result := tools.Execute(context.Background(), "sh", `{"cmd":"sleep 2","bg":true}`); result.IsError {
		t.Fatal(result.Content)
	}
	running := tools.Execute(context.Background(), "sh", `{"cmd":"printf unrelated"}`)
	if !strings.Contains(running.Content, "unrelated\n\n[job 1 · running") {
		t.Fatalf("running report missing from unrelated tool: %q", running.Content)
	}
	waitForJobDone(t, tools, 1, 4*time.Second)
	terminal := tools.Execute(context.Background(), "sh", `{"cmd":"printf after"}`)
	if !strings.Contains(terminal.Content, "[job 1 · exited 0 after") {
		t.Fatalf("terminal report missing: %q", terminal.Content)
	}
	again := tools.Execute(context.Background(), "sh", `{"cmd":"printf again"}`)
	if strings.Contains(again.Content, "job 1") {
		t.Fatalf("terminal transition was reported twice: %q", again.Content)
	}
}

func TestNonzeroExitReportIncludesCodeAndLastLine(t *testing.T) {
	tools, _ := backgroundToolbox(t)
	if result := tools.Execute(context.Background(), "sh", `{"cmd":"sleep 1; printf '\\033[31mboom\\033[0m\\n'; exit 7","bg":true}`); result.IsError {
		t.Fatal(result.Content)
	}
	waitForJobDone(t, tools, 1, 4*time.Second)
	result := tools.Execute(context.Background(), "sh", `{"cmd":"printf boundary"}`)
	if !strings.Contains(result.Content, "exited 7") || !strings.Contains(result.Content, "last: boom") {
		t.Fatalf("nonzero report lacked failure detail: %q", result.Content)
	}
	if strings.Contains(result.Content, "\x1b[") {
		t.Fatalf("nonzero report retained ANSI escapes: %q", result.Content)
	}
}

func TestJobListShowsEveryStateAndLastLogLine(t *testing.T) {
	tools, space := backgroundToolbox(t)
	if result := tools.Execute(context.Background(), "sh", `{"cmd":"printf '\\033[32mlatest line\\033[0m\\n'; sleep 30","bg":true}`); result.IsError {
		t.Fatal(result.Content)
	}
	waitForFileText(t, filepath.Join(space.Root(), ".aforge", "jobs", "1.log"), "latest line")
	result := tools.Execute(context.Background(), "job", `{}`)
	if result.IsError || !strings.Contains(result.Content, "job 1 · running") ||
		!strings.Contains(result.Content, "last: latest line") {
		t.Fatalf("job list = %+v", result)
	}
	if strings.Contains(result.Content, "\x1b[") {
		t.Fatalf("job list retained ANSI escapes: %q", result.Content)
	}
}

type backgroundThenFinalCompleter struct {
	space *Workspace
	calls atomic.Int32
}

func (c *backgroundThenFinalCompleter) CompleteWithMessages(ctx context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	if c.calls.Add(1) == 1 {
		return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{
			Role: "assistant", ToolCalls: []ai.ToolCall{call("bg", "sh", `{"cmd":"echo $$ > survivor.pid; sleep 30","bg":true}`)},
		}}}, Usage: &ai.Usage{}}, nil
	}
	path := filepath.Join(c.space.Root(), "survivor.pid")
	for {
		if _, err := os.Stat(path); err == nil {
			return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{
				Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "done"}},
			}}}, Usage: &ai.Usage{}}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestLeafEndTerminatesSurvivorsAndNotesCount(t *testing.T) {
	space := workspace(t)
	client := &backgroundThenFinalCompleter{space: space}
	linear := NewLinear(client, space, nil, 5, 1_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "start a server"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(outcome.Text, "1 background jobs terminated at leaf end") {
		t.Fatalf("leaf result omitted teardown: %q", outcome.Text)
	}
	pidBody, err := os.ReadFile(filepath.Join(space.Root(), "survivor.pid"))
	if err != nil {
		logBody, _ := os.ReadFile(filepath.Join(space.Root(), ".aforge", "jobs", "1.log"))
		t.Fatalf("survivor did not start: %v; log=%q artifacts=%v", err, logBody, outcome.Artifacts)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(pidBody)))
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("process %d survived leaf end", pid)
	}
	if artifacts := outcome.Artifacts; len(artifacts) != 1 || artifacts[0] != ".aforge/jobs/1.log" {
		t.Fatalf("leaf artifacts = %v, want retained job log", artifacts)
	}
}

type backgroundThenWedgeCompleter struct {
	space   *Workspace
	release chan struct{}
	calls   atomic.Int32
}

func (c *backgroundThenWedgeCompleter) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	if c.calls.Add(1) == 1 {
		return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{
			Role: "assistant", ToolCalls: []ai.ToolCall{call("bg", "sh", `{"cmd":"echo $$ > abandoned.pid; sleep 30","bg":true}`)},
		}}}, Usage: &ai.Usage{}}, nil
	}
	waitForPath := filepath.Join(c.space.Root(), "abandoned.pid")
	for {
		if _, err := os.Stat(waitForPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	<-c.release // deliberately ignores context until the test releases it
	return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{
		Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "late"}},
	}}}, Usage: &ai.Usage{}}, nil
}

func TestSchedulerAbandonmentTearsDownLeafJobs(t *testing.T) {
	space := workspace(t)
	release := make(chan struct{})
	client := &backgroundThenWedgeCompleter{space: space, release: release}
	linear := NewLinear(client, space, nil, 5, 1_000_000, time.Minute)
	graph := &plan.Graph{Goal: "g", Stages: []plan.Stage{{Title: "One"}}, NextID: 1}
	id := graph.Add(plan.Node{Stage: 1, Title: "Wedged"})
	scheduler := NewScheduler(NewRegistry(linear), space, 1)
	scheduler.NodeTimeout = 2 * time.Second
	if err := scheduler.Run(context.Background(), graph); err != nil {
		close(release)
		t.Fatalf("Run: %v", err)
	}
	close(release)
	node := graph.Node(id)
	if node.State != plan.StateFailed || !strings.Contains(node.Failure, "1 background jobs terminated at leaf end") {
		t.Fatalf("abandoned node = %s, failure %q", node.State, node.Failure)
	}
	pidBody, err := os.ReadFile(filepath.Join(space.Root(), "abandoned.pid"))
	if err != nil {
		logBody, _ := os.ReadFile(filepath.Join(space.Root(), ".aforge", "jobs", "1.log"))
		t.Fatalf("abandoned process did not start: %v; log=%q", err, logBody)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(pidBody)))
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("process %d survived scheduler abandonment", pid)
	}
}

func TestBackgroundJobIDsAreUniqueAcrossLeafToolboxes(t *testing.T) {
	space := workspace(t)
	first := NewToolbox(space, 1, nil)
	second := NewToolbox(space, 2, nil)
	t.Cleanup(func() { first.Close(); second.Close() })
	for index, tools := range []*Toolbox{first, second} {
		result := tools.Execute(context.Background(), "sh", `{"cmd":"sleep 30","bg":true}`)
		want := fmt.Sprintf("job %d started", index+1)
		if result.IsError || !strings.Contains(result.Content, want) {
			t.Fatalf("start %d = %+v, want %q", index, result, want)
		}
	}
}
