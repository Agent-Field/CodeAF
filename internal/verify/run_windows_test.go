//go:build windows

package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/shell"
)

// N1 and N2 take a real native reading with shell variables, the selected cwd,
// and the command's own failing exit status, without requiring make on Windows.
func TestWindowsNativeReading(t *testing.T) {
	if _, err := shell.Bash(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker"), []byte("right"), 0600); err != nil {
		t.Fatal(err)
	}
	strategy := Strategy{Command: `export PATH='/c/Program Files (x86)/fixture':$PATH; x=inner; test "$x" = inner; test "$(cat marker)" = right; printf '%s\n' '--- FAIL: TestWindowsNative (0s)'; exit 23`}
	result, ok := RunReading(context.Background(), dir, strategy, 20*time.Second)
	if !ok || result.Exit != 23 || result.TimedOut || len(result.Failing) != 1 || result.Failing[0] != "TestWindowsNative" {
		t.Fatalf("native reading: %+v, present=%v", result, ok)
	}
	t.Setenv("CODEAF_BASH", filepath.Join(dir, "missing.exe"))
	if _, ok := RunReading(context.Background(), dir, strategy, 20*time.Second); ok {
		t.Fatal("a reading with no usable shell was present")
	}
}
