//go:build !windows

package session

import (
	"fmt"
	"runtime"
	"testing"
	"time"
)

// C8 pins the complete Unix project footer to its pre-Windows-shell bytes.
func TestUnixWorkerFooterBytes(t *testing.T) {
	config := Config{Workspace: t.TempDir()}
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	want := fmt.Sprintf("\n\n# Project\n- Workstation: %s/%s\n- Working directory: %s\n", runtime.GOOS, runtime.GOARCH, config.Workspace) + nowLine(now)
	if got := workerFooter(config, now); got != want {
		t.Fatalf("Unix worker footer changed:\ngot %q\nwant %q", got, want)
	}
}
