package verification

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/shell"
)

// Unix parity contract 2: snapshot failures retain exec.Cmd.Output's bounded
// diagnostic bytes on Unix, including its omitted-byte disclosure.
func TestUnixParitySnapshotDiagnostics(t *testing.T) {
	bash, err := shell.Bash()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), bash, "-c", "printf stdout; printf '%070000d' 0 >&2; exit 3")
	boundSnapshot(command)
	out, err := snapshotOutput(command)
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 3 || string(out) != "stdout" {
		t.Fatalf("snapshot = %q, %v", out, err)
	}
	if runtime.GOOS == "windows" {
		if len(exit.Stderr) != 70000 {
			t.Fatalf("Windows diagnostics = %d bytes", len(exit.Stderr))
		}
	} else {
		want := strings.Repeat("0", 32768) + "\n... omitting 4464 bytes ...\n" + strings.Repeat("0", 32768)
		if string(exit.Stderr) != want {
			t.Fatalf("Unix diagnostics differ from Output's original bytes (%d bytes)", len(exit.Stderr))
		}
	}
}
