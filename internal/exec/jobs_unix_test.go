//go:build !windows

package exec

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// K6 preserves the POSIX process-group assertions where shell $$/$! are OS
// PIDs. Git Bash uses MSYS PIDs, which cannot probe native process identities.
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
	deadline := time.Now().Add(slack(3 * time.Second))
	for time.Now().Before(deadline) && testProcessSignal(childPID, 0) == nil {
		time.Sleep(20 * time.Millisecond)
	}
	if err := testProcessSignal(childPID, 0); err == nil {
		t.Fatalf("grandchild %d survived process-group kill", childPID)
	}
}

// K6 retains the POSIX $! process-group assertion where it identifies an OS process.
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
	if err := testProcessSignal(childPID, 0); err == nil {
		t.Fatalf("detached child %d survived its background shell", childPID)
	}
}

type backgroundThenFinalCompleter struct {
	space           *Workspace
	calls           atomic.Int32
	survivorStarted time.Time
}

func (c *backgroundThenFinalCompleter) CompleteWithMessages(ctx context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	if c.calls.Add(1) == 1 {
		return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{
			Role: "assistant", ToolCalls: []ai.ToolCall{call("bg", "sh", `{"cmd":"echo $$ > survivor.pid; sleep 30","bg":true}`)},
		}}}, Usage: &ai.Usage{}}, nil
	}
	path := filepath.Join(c.space.Root(), "survivor.pid")
	for {
		if pidBody, err := os.ReadFile(path); err == nil {
			pid, _ := strconv.Atoi(strings.TrimSpace(string(pidBody)))
			if started, identityErr := ProcessStartTime(pid); identityErr == nil {
				c.survivorStarted = started
				return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{
					Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "done"}},
				}}}, Usage: &ai.Usage{}}, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// K6 uses the POSIX shell PID to check the same process identity after leaf teardown.
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
		logBody, _ := os.ReadFile(filepath.Join(space.Root(), jobsDir, jobLogName("1", 1)))
		t.Fatalf("survivor did not start: %v; log=%q artifacts=%v", err, logBody, outcome.Artifacts)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(pidBody)))
	if started, err := ProcessStartTime(pid); err == nil && started.Equal(client.survivorStarted) {
		t.Fatalf("process %d survived leaf end", pid)
	}
	if _, ok := space.Locate(filepath.Join(jobsDir, jobLogName("1", 1))); !ok {
		t.Fatal("the job log was not retained past leaf end")
	}
	// The job log is machinery and stays out of the answer. survivor.pid is not:
	// a background command wrote it into the workspace, no write tool ever heard
	// about it, and the tree diff is the only thing that can see it — which is
	// the whole of rule 2. The assertion used to be "nothing at all", which was
	// only true while the file a shell wrote was invisible.
	for _, path := range outcome.Artifacts {
		if strings.HasPrefix(path, jobsDir) {
			t.Fatalf("leaf artifacts = %v, want the job log held out of the job's own output", outcome.Artifacts)
		}
	}
	if len(outcome.Artifacts) != 1 || outcome.Artifacts[0] != "survivor.pid" {
		t.Fatalf("leaf artifacts = %v, want the file the background command wrote", outcome.Artifacts)
	}
}
