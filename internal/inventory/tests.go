package inventory

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// TestRun is the last test command a call ran to its end: whether it passed
// and, where its output said, how many tests failed. It is recorded only from a
// run that happened, so a chat that never ran tests has none.
type TestRun struct {
	Command string `json:"command"`
	Passed  bool   `json:"passed"`
	Failed  int    `json:"failed,omitempty"`
}

// testKind is one kind of test command and how its output counts failures. It
// is data: a runner is added by naming a pattern and a count, and nothing else
// changes.
type testKind struct {
	command *regexp.Regexp
	failed  *regexp.Regexp
}

// starts is where a test command may begin: the line's start, or after a step.
const starts = `(?:^|&&\s*|;\s*)`

func kind(command, failed string) testKind {
	return testKind{regexp.MustCompile(starts + command), regexp.MustCompile(failed)}
}

// testKinds is the runners the record knows. The failed pattern's first group
// is a count, or, for a pattern that matches once per failure, absent.
var testKinds = []testKind{
	kind(`go\s+test\b`, `(?m)^--- FAIL:`),
	kind(`(python3?\s+-m\s+)?pytest\b`, `(\d+) failed`),
	kind(`cargo\s+test\b`, `(\d+) failed`),
	kind(`(npm|pnpm|yarn)\s+(run\s+)?test\b`, `Tests:\s+(\d+) failed`),
	kind(`make\s+test\b`, `(\d+) failed`),
}

// shells are the programs that run a script handed to them with -c.
var shells = map[string]bool{"sh": true, "bash": true, "zsh": true}

// scriptOf is the line a call ran: the script of a shell -c call, else the
// words of the call.
func scriptOf(argv []string) string {
	if len(argv) >= 3 && shells[filepath.Base(argv[0])] && strings.HasSuffix(argv[1], "c") && strings.HasPrefix(argv[1], "-") {
		return argv[2]
	}
	return strings.Join(argv, " ")
}

// testRunOf reads one finished command: the run it was, when it was a test
// command that ran to its end. A command that was killed has no verdict.
func testRunOf(command string, exit int, output []byte) (TestRun, bool) {
	if exit < 0 {
		return TestRun{}, false
	}
	for _, k := range testKinds {
		if loc := k.command.FindStringIndex(command); loc != nil && !masksExit(command[loc[1]:]) {
			run := TestRun{Command: command, Passed: exit == 0}
			if !run.Passed {
				run.Failed = k.count(output)
			}
			return run, true
		}
	}
	return TestRun{}, false
}

// count is how many tests the output says failed, zero when it does not say.
func (k testKind) count(output []byte) int {
	all := k.failed.FindAllSubmatch(output, -1)
	if k.failed.NumSubexp() == 0 {
		return len(all)
	}
	for _, m := range all {
		if n, err := strconv.Atoi(string(m[1])); err == nil {
			return n
		}
	}
	return 0
}

// masksExit reports whether what follows a test command can change the exit
// status the call ended with: a pipe or a later step says nothing of the tests.
func masksExit(rest string) bool { return strings.ContainsAny(rest, "|;") }
