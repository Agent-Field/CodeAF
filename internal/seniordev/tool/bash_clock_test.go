//go:build !windows

package tool

import (
	"strings"
	"testing"
	"time"
)

func TestBashTimeoutWithFakeClockInTempDir(t *testing.T) {
	previous := bashAfter
	t.Cleanup(func() { bashAfter = previous })
	bashAfter = func(time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		ch <- time.Unix(0, 0)
		return ch
	}

	registry := New(t.TempDir())
	result, err := execute(t, registry, "bash", map[string]any{
		"command":    "sleep 30",
		"timeout_ms": 10_000,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "command timed out after 10000ms") ||
		!strings.Contains(result.Output, "[environment-signal] this command timed out") {
		t.Fatalf("Output = %q", result.Output)
	}
}
