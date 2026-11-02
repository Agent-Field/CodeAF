package verification

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A MACHINE WHOSE ONLY SHELL IS DASH still runs a check whose program name has
// a hyphen, with that program's own exit status. Dash parses the bash-only
// observer without running it, and records nothing.
func TestADashShellRunsAHyphenatedCheckWithItsOwnExit(t *testing.T) {
	if _, err := os.Stat("/bin/dash"); err != nil {
		t.Skip("no /bin/dash on this machine")
	}
	for _, want := range []int{0, 7} {
		t.Run(strconv.Itoa(want), func(t *testing.T) {
			root, prefix := fakeChecks(t)
			transcript := filepath.Join(root, "session.jsonl")
			wrapped, err := Prepare(transcript, prefix+"CHECK_EXIT="+strconv.Itoa(want)+" golangci-lint run")
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("/bin/dash", "-c", wrapped)
			cmd.Dir = root
			output, err := cmd.CombinedOutput()
			got := 0
			if exit, ok := err.(*exec.ExitError); ok {
				got = exit.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}
			if got != want || !strings.Contains(string(output), "PASS") {
				t.Fatalf("dash: exit %d want %d, output %s", got, want, output)
			}
			if checks, err := Read(transcript); err != nil || len(checks) != 0 {
				t.Fatalf("dash recorded checks it cannot observe: %+v, %v", checks, err)
			}
		})
	}
}

// BASH STILL OBSERVES THE HYPHENATED CHECKS, run directly and from a child bash
// that only has the exported functions.
func TestBashObservesHyphenatedChecksDirectlyAndInAChildShell(t *testing.T) {
	for command, program := range map[string]string{
		"CHECK_EXIT=3 golangci-lint run":           "golangci-lint",
		"CHECK_EXIT=3 pip-audit":                   "pip-audit",
		"bash -c 'CHECK_EXIT=3 golangci-lint run'": "golangci-lint",
		"bash -c 'CHECK_EXIT=3 pip-audit'":         "pip-audit",
	} {
		t.Run(command, func(t *testing.T) {
			root, prefix := fakeChecks(t)
			transcript := filepath.Join(root, "session.jsonl")
			if _, exit := runObserved(t, transcript, root, prefix+command); exit != 3 {
				t.Fatalf("exit %d, want the check's own 3", exit)
			}
			checks, err := Read(transcript)
			if err != nil || len(checks) != 1 || checks[0].Exit == nil || *checks[0].Exit != 3 {
				t.Fatalf("bash did not observe %q: %+v, %v", command, checks, err)
			}
			if len(checks[0].Args) == 0 || checks[0].Args[0] != program {
				t.Fatalf("observed args %q, want them to start with %s", checks[0].Args, program)
			}
		})
	}
}

// Every wrapper is written inside single quotes for eval, so no observed
// program name and no wrapper may hold one.
func TestNoObservedProgramCanBreakItsQuotedWrapper(t *testing.T) {
	for _, rule := range checkInvocations {
		if strings.Contains(rule[0], "'") {
			t.Fatalf("program %q holds a single quote", rule[0])
		}
	}
	for _, line := range strings.Split(observerFunctions(), "\n") {
		body, ok := strings.CutPrefix(line, "eval '")
		if !ok {
			continue
		}
		if strings.Count(strings.TrimSuffix(body, "'"), "'") != 0 {
			t.Fatalf("a wrapper holds a single quote: %s", line)
		}
	}
}
