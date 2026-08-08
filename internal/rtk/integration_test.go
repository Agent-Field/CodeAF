package rtk

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestRealBinaryPreservesExitCodes is the one test that touches a real rtk. It
// is skipped unless AFORGE_TEST_RTK=1, because the suite must run offline and
// without the binary; run it when bumping Version, since the property it checks
// — that putting rtk in the middle never changes whether a command succeeded —
// is the whole basis for wrapping anything.
//
//	AFORGE_TEST_RTK=1 go test -run RealBinary ./internal/rtk/
func TestRealBinaryPreservesExitCodes(t *testing.T) {
	if os.Getenv("AFORGE_TEST_RTK") != "1" {
		t.Skip("set AFORGE_TEST_RTK=1 to exercise a real rtk")
	}
	tool, ok := Available()
	if !ok {
		t.Skip("no rtk resolved")
	}

	for _, command := range []string{
		"ls", "ls /definitely-not-here", "cat go.mod", "cat /definitely-not-here",
		"git status", "git log --oneline -3", "grep -rn zzzznomatch .",
	} {
		wrapped, class := tool.Wrap(context.Background(), command)
		if class == ClassNone {
			continue
		}
		plain := run(t, command, "")
		compressed := run(t, wrapped, tool.Path)
		if plain != compressed {
			t.Errorf("%q: plain exited %d, %q exited %d", command, plain, wrapped, compressed)
		}
	}
}

// run executes one command from the repository root and reports only its exit
// status, which is the single thing this test is about.
func run(t *testing.T, command, rtkPath string) int {
	t.Helper()
	if rtkPath != "" {
		command = "export PATH=\"" + filepath.Dir(rtkPath) + ":$PATH\"\n" + command
	}
	cmd := exec.Command("bash", "-lc", command)
	cmd.Dir = filepath.Join("..", "..")
	_, _ = cmd.CombinedOutput()
	if cmd.ProcessState == nil {
		t.Fatalf("%q never ran", command)
	}
	return cmd.ProcessState.ExitCode()
}
