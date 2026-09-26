//go:build linux

package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
)

func TestPlainBackgroundBashReturnsBeforeItsChildAndIsClosedWithRun(t *testing.T) {
	registry := New(t.TempDir())
	defer registry.CloseShellProcesses()
	pidFile := filepath.Join(t.TempDir(), "pid")
	input, _ := json.Marshal(map[string]any{
		"command":    "sleep 300 & echo $! > " + pidFile,
		"timeout_ms": 500,
	})
	start := time.Now()
	result, err := registry.Execute(context.Background(), steploop.ToolCall{
		ID: "background", Name: "bash", Input: input,
	})
	if err != nil || strings.Contains(result.Output, "timed out") || time.Since(start) > time.Second {
		t.Fatalf("background bash waited for its child: output=%q error=%v", result.Output, err)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("background process ended before the run: %v", err)
	}
	registry.CloseShellProcesses()
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline) && shellProcessRunning(pid); {
		time.Sleep(10 * time.Millisecond)
	}
	if shellProcessRunning(pid) {
		t.Fatalf("background process %d survived the run", pid)
	}
}

func shellProcessRunning(pid int) bool {
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return false
	}
	fields := strings.Fields(string(raw))
	return len(fields) > 2 && fields[2] != "Z"
}
