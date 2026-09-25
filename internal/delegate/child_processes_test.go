//go:build linux

package delegate_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/tool"
)

func TestRunEndsItsBashBackgroundProcesses(t *testing.T) {
	if mode := os.Getenv("FC_PROCESS_CHILD"); mode != "" {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
		defer stop()
		if err := os.WriteFile(os.Getenv("FC_PROCESS_PIDFILE")+".engine", []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			t.Fatal(err)
		}
		registry := tool.New(os.Getenv("FC_PROCESS_WORKSPACE"))
		command := os.Getenv("FC_PROCESS_COMMAND") + " sleep 300 >/dev/null 2>&1 & echo $! > " + os.Getenv("FC_PROCESS_PIDFILE")
		input, _ := json.Marshal(map[string]any{"command": command})
		if _, err := registry.Execute(ctx, steploop.ToolCall{ID: "background", Name: "bash", Input: input}); err != nil {
			t.Fatal(err)
		}
		if mode == "stop" {
			<-ctx.Done()
		} else if mode == "crash" {
			time.Sleep(5 * time.Minute)
		}
		os.Stdout.WriteString("{\"type\":\"hello\",\"protocol\":2,\"delegate\":\"fake\"}\n")
		os.Stdout.WriteString("{\"type\":\"terminal\",\"status\":\"pass\"}\n")
		return
	}
	for _, command := range []string{"", "setsid"} {
		for _, mode := range []string{"done", "stop", "crash"} {
			name := strings.TrimSpace(command + " " + mode)
			t.Run(name, func(t *testing.T) {
				workspace := t.TempDir()
				pidFile := filepath.Join(t.TempDir(), "pid")
				t.Setenv("FC_PROCESS_CHILD", mode)
				t.Setenv("FC_PROCESS_WORKSPACE", workspace)
				t.Setenv("FC_PROCESS_COMMAND", command)
				t.Setenv("FC_PROCESS_PIDFILE", pidFile)
				self, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				ended := make(chan error, 1)
				go func() {
					_, err := delegate.Run(ctx, delegate.Launch{
						Name: "fake", Bin: self, Args: []string{"-test.run=^TestRunEndsItsBashBackgroundProcesses$"},
						Env: delegate.ChildEnv(delegate.ModelAPI{}), Dir: workspace, Grace: time.Second,
					}, nil)
					ended <- err
				}()
				var pid int
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					data, readErr := os.ReadFile(pidFile)
					if readErr == nil {
						pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if pid <= 0 {
					cancel()
					<-ended
					t.Fatal("background process never started")
				}
				t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
				if mode == "stop" {
					cancel()
				} else if mode == "crash" {
					// The child records its own PID separately below in the helper.
					data, readErr := os.ReadFile(pidFile + ".engine")
					if readErr != nil {
						t.Fatal(readErr)
					}
					enginePID, _ := strconv.Atoi(strings.TrimSpace(string(data)))
					_ = syscall.Kill(enginePID, syscall.SIGKILL)
				}
				select {
				case err := <-ended:
					if mode == "done" && err != nil || mode == "stop" && !errors.Is(err, context.Canceled) {
						t.Fatalf("run ended: %v", err)
					}
				case <-time.After(4 * time.Second):
					t.Fatal("run did not end")
				}
				for until := time.Now().Add(time.Second); time.Now().Before(until) && processStillRunning(pid); {
					time.Sleep(10 * time.Millisecond)
				}
				if processStillRunning(pid) {
					t.Fatalf("background process %d outlived %s", pid, mode)
				}
			})
		}
	}
}

func processStillRunning(pid int) bool {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return false
	}
	fields := strings.Fields(string(data))
	return len(fields) > 2 && fields[2] != "Z"
}
