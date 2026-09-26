//go:build linux

package delegate_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/processgroup"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/tool"
)

// The engine alone becomes a subreaper, then ends every descendant of its
// model-written bash, including children that detached and discarded the
// marker. An unrelated sleep and the codeaf process remain alive.
func TestEngineEndsAllOfItsBackgroundProcesses(t *testing.T) {
	if os.Getenv("DESCENDANT_TEST_CHILD") == "1" {
		if err := processgroup.EnableSubreaper(); err != nil {
			t.Fatal(err)
		}
		defer processgroup.CleanupDescendants()
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
		defer stop()
		input, _ := json.Marshal(map[string]any{"command": os.Getenv("DESCENDANT_TEST_COMMAND")})
		if _, err := tool.New(os.Getenv("DESCENDANT_TEST_WORKSPACE")).Execute(ctx, steploop.ToolCall{ID: "background", Name: "bash", Input: input}); err != nil {
			t.Fatal(err)
		}
		if os.Getenv("DESCENDANT_TEST_WAIT") == "1" {
			<-ctx.Done()
		} else {
			time.Sleep(300 * time.Millisecond)
		}
		_, _ = os.Stdout.WriteString("{\"type\":\"hello\",\"protocol\":2,\"delegate\":\"fake\"}\n{\"type\":\"terminal\",\"status\":\"pass\"}\n")
		return
	}
	outsider := exec.Command("sleep", "301")
	if err := outsider.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = outsider.Process.Kill(); _, _ = outsider.Process.Wait() })
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		cmd  func(string) string
	}{
		{"background", func(file string) string { return "sleep 300 >/dev/null 2>&1 & echo $! > " + file }},
		{"setsid", func(file string) string { return "setsid sleep 300 >/dev/null 2>&1 & echo $! > " + file }},
		{"marker removed", func(file string) string {
			return "setsid env -u CODEAF_DELEGATE_RUN sleep 300 >/dev/null 2>&1 & echo $! > " + file
		}},
		{"empty environment", func(file string) string { return "env -i setsid sleep 300 >/dev/null 2>&1 & echo $! > " + file }},
		{"double fork", func(file string) string {
			return "sh -c 'sleep 300 >/dev/null 2>&1 & echo $! > " + file + "' >/dev/null 2>&1 &"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pidFile := filepath.Join(t.TempDir(), "pid")
			workspace := t.TempDir()
			t.Setenv("DESCENDANT_TEST_CHILD", "1")
			t.Setenv("DESCENDANT_TEST_COMMAND", tc.cmd(pidFile))
			t.Setenv("DESCENDANT_TEST_WORKSPACE", workspace)
			_, err := delegate.Run(context.Background(), delegate.Launch{Name: "fake", Bin: self,
				Args: []string{"-test.run=^TestEngineEndsAllOfItsBackgroundProcesses$"},
				Env:  delegate.ChildEnv(delegate.ModelAPI{}), Dir: workspace, Grace: time.Second}, nil)
			if err != nil {
				t.Logf("run result: %v", err)
			}
			data, err := os.ReadFile(pidFile)
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
			if processStillRunning(pid) {
				t.Fatalf("background descendant %d survived run cleanup", pid)
			}
			if err := outsider.Process.Signal(syscall.Signal(0)); err != nil {
				t.Fatalf("unrelated sleep was killed: %v", err)
			}
			if err := syscall.Kill(os.Getpid(), 0); err != nil {
				t.Fatalf("codeaf test process was killed: %v", err)
			}
		})
	}
}

// The same parent-link cleanup runs after SIGTERM from a person's stop or a
// run deadline, even when the detached child has an empty environment.
func TestSanitizedDescendantEndsOnStopAndDeadline(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"stop", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			pidFile := filepath.Join(t.TempDir(), "pid")
			workspace := t.TempDir()
			t.Setenv("DESCENDANT_TEST_CHILD", "1")
			t.Setenv("DESCENDANT_TEST_WAIT", "1")
			t.Setenv("DESCENDANT_TEST_COMMAND", "env -i setsid sleep 300 >/dev/null 2>&1 & echo $! > "+pidFile)
			t.Setenv("DESCENDANT_TEST_WORKSPACE", workspace)
			var ctx context.Context
			var cancel context.CancelFunc
			if mode == "deadline" {
				ctx, cancel = context.WithTimeout(context.Background(), time.Second)
			} else {
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			finished := make(chan error, 1)
			go func() {
				_, err := delegate.Run(ctx, delegate.Launch{Name: "fake", Bin: self,
					Args: []string{"-test.run=^TestEngineEndsAllOfItsBackgroundProcesses$"},
					Env:  delegate.ChildEnv(delegate.ModelAPI{}), Dir: workspace, Grace: time.Second}, nil)
				finished <- err
			}()
			var pid int
			for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
				data, readErr := os.ReadFile(pidFile)
				if readErr == nil {
					pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if pid == 0 {
				cancel()
				<-finished
				t.Fatal("the detached process never started")
			}
			t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
			if mode == "stop" {
				cancel()
			}
			select {
			case err := <-finished:
				if err != ctx.Err() {
					t.Fatalf("run ended with %v, want %v", err, ctx.Err())
				}
			case <-time.After(3 * time.Second):
				t.Fatal("the stopped run did not return")
			}
			if processStillRunning(pid) {
				t.Fatalf("detached process %d survived the %s ending", pid, mode)
			}
		})
	}
}
