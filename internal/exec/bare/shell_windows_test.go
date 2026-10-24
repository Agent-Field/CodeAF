//go:build windows

package bare

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/shell"
)

// N1 makes a shell with no implementation absent from both public inventories,
// and direct calls explain the setup instead of creating a process.
func TestWindowsMissingBashIsAbsent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEAF_HOME", dir)
	for _, name := range []string{"ProgramW6432", "ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA", "USERPROFILE", env.Legacy("CODEAF_BASH")} {
		t.Setenv(name, "")
	}
	t.Setenv("PATH", filepath.Join(os.Getenv("SystemRoot"), "System32"))
	for _, explicit := range []string{filepath.Join(dir, "missing.exe"), ""} {
		t.Setenv("CODEAF_BASH", explicit)
		for _, tools := range [][]Tool{AllTools(dir), Tools(dir)} {
			for _, tool := range tools {
				if tool.Name == "bash" {
					t.Fatal("unusable bash is advertised")
				}
			}
		}
		output, failed, err := newBashTool(dir, DefaultCaps()).Execute(context.Background(), []byte(`{"command":"echo WRONG > marker"}`))
		if err != nil || !failed || !strings.Contains(output, "CODEAF_BASH") {
			t.Fatalf("direct call: %q %v %v", output, failed, err)
		}
		if explicit == "" && output != shell.NoBash {
			t.Fatalf("missing discovery remedy: %q, want %q", output, shell.NoBash)
		}
		if _, err := os.Stat(filepath.Join(dir, "marker")); !os.IsNotExist(err) {
			t.Fatalf("command ran: %v", err)
		}
	}
}
