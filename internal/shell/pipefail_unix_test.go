//go:build !windows

package shell

import (
	"errors"
	"os/exec"
	"slices"
	"testing"
)

func TestPipefailForgivesOnlyAReaderThatStoppedEarly(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	for _, tc := range []struct {
		command, output string
		want            int
	}{
		{"yes x | head -1", "x\n", 0},
		{"false | tail -20", "", 1},
		{"false | yes | head -1", "y\n", 1},
		{"yes x | head -1 | false", "", 1},
		{"yes x | head -1; exit 3", "x\n", 3},
		{"set -e; yes x | head -1; echo after", "x\n", 141},
		{"(yes x | head -1)", "x\n", 141},
		{"set -x; yes x | head -1 2>/dev/null", "x\n", 0},
		{"echo hi; exit 141", "hi\n", 141},
		{"echo a || echo b", "a\n", 0},
		{"yes x | head -1 && false", "x\n", 141},
		{"yes x | head -1 && echo next", "x\n", 141},
		{"trap 'echo bye' EXIT; yes x | head -1", "x\nbye\n", 141},
		{"trap 'echo bye; exit 4' EXIT; echo hi", "hi\nbye\n", 4},
		{"{ yes x | head -1; }", "x\n", 0},
		{"set -u; echo a | cat; exit 7", "a\n", 7},
		{"set -u; yes x | head -1", "x\n", 0},
		{"trap 'echo err' ERR; yes x | head -1", "x\nerr\n", 141},
	} {
		cmd := exec.Command(bash, PipefailArgs(bash, []string{"-c"}, tc.command)...)
		var exit *exec.ExitError
		out, err := cmd.Output()
		got := 0
		if errors.As(err, &exit) {
			got = exit.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		if got != tc.want || string(out) != tc.output {
			t.Errorf("%q: exit %d output %q, want exit %d output %q", tc.command, got, out, tc.want, tc.output)
		}
	}
}

func TestPipefailLeavesShAndPipeFreeCommandsAsWritten(t *testing.T) {
	if got := PipefailArgs("sh", []string{"-c"}, "a | b"); !slices.Equal(got, []string{"-c", "a | b"}) {
		t.Errorf("sh args = %q", got)
	}
	if got := PipefailArgs("/bin/bash", []string{"-lc"}, "make test"); !slices.Equal(got, []string{"-o", "pipefail", "-lc", "make test"}) {
		t.Errorf("pipe-free args = %q", got)
	}
}
