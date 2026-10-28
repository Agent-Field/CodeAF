//go:build !windows

package executor_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
)

func TestWorkspaceEnvironmentRoutesRealTemporaryFilesThroughBothRunnerDoors(t *testing.T) {
	scratch, cache := t.TempDir(), t.TempDir()
	originalHOME := os.Getenv("HOME")
	seat := executor.WithEnvironment(executor.Host, []string{"TMPDIR=" + scratch, "XDG_CACHE_HOME=" + cache, "CODEAF_WORKSPACE_URI=codeaf-workspace://example/run"})
	command := `printf '%s\n' "$HOME" "$XDG_CACHE_HOME" "$CODEAF_WORKSPACE_URI"; mktemp`
	for _, door := range []string{"exec", "command", "setup"} {
		runner := seat.In(t.TempDir())
		if door == "setup" {
			runner = executor.ForSetup(seat).In(t.TempDir())
		}
		request := executor.ExecRequest{Argv: []string{"sh", "-c", command}}
		var output []byte
		if door == "command" {
			cmd, err := runner.Command(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			output, err = cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
		} else {
			result, err := runner.Exec(context.Background(), request, nil)
			if err != nil || result.Exit != 0 {
				t.Fatalf("%s: %#v %v", door, result, err)
			}
			output = result.Stdout
		}
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		if len(lines) != 4 || lines[0] != originalHOME || lines[1] != cache || lines[2] != "codeaf-workspace://example/run" || filepath.Dir(lines[3]) != scratch {
			t.Fatalf("%s environment: %q", door, output)
		}
		if _, err := os.Stat(lines[3]); err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("HOME") != originalHOME {
		t.Fatal("host environment changed")
	}
}
