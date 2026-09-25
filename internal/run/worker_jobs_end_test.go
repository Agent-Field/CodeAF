package run_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/run"
)

// TestBashWorkerStopsItsJobsWhenItsTaskEnds: a job a worker leaves running is
// the worker's, and it does not outlive the task. The fresh-install run left a
// `find /` walking the disk as job 1 for the rest of its task; whatever a worker
// did not stop itself, the end of its task must stop, process group and all.
func TestBashWorkerStopsItsJobsWhenItsTaskEnds(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	store := runOpenStore(t)
	workspace := t.TempDir()
	marker := filepath.Join(workspace, "group.txt")
	// The run's own wall is generous, because a loaded machine is not what
	// this test is about; the task is ended by cutting its run instead.
	ctx, cut := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cut()
	seat := &seat{script: []step{
		// The job writes its own process group (bash starts it as the group's
		// leader, and exec keeps the pid) and then sleeps far past the test.
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(`{"command":"echo $$ > group.txt; exec sleep 900","background":true}`), nil
		},
		// Once the job has said which group it is, the task ends with the job
		// still running: its run is cut, the way a person's stop cuts it. HOW
		// the task ends is not the point — every ending passes the same
		// deferred close of the worker's seat.
		func(context.Context, []ai.Message) (*ai.Response, error) {
			waitForFile(t, marker)
			cut()
			return textReply(""), nil
		},
	}}
	worker := run.NewBashWorker(store, workspace, "test/model", seat)
	_, _ = worker.Run(run.WithStepsPerTask(ctx, 9), *store.Task(store.RootID()))

	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("the job never wrote its process group: %v", err)
	}
	group, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || group <= 1 {
		t.Fatalf("the job wrote %q, not a process group", data)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := syscall.Kill(-group, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(-group, syscall.SIGKILL)
			t.Fatalf("process group %d outlived the task that started it", group)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitForFile waits for a file a background command is about to write.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}
