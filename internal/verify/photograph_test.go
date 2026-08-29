package verify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// textualShape is the project textual actually is, in the three files that
// decide how it is read: a Makefile whose test recipe reaches its runner through
// a variable and a launcher, and a pyproject that configures pytest. Copied from
// the task image the s6 run worked in.
func textualShape(t *testing.T) string {
	t.Helper()
	return project(t, map[string]string{
		"Makefile": "run := poetry run\n\n" +
			".PHONY: test\ntest:\n\t$(run) pytest tests/ -n 16 --dist=loadgroup $(ARGS)\n\n" +
			".PHONY: typecheck\ntypecheck:\n\t$(run) mypy src/textual\n",
		"pyproject.toml": "[tool.poetry]\nname = \"textual\"\n\n" +
			"[tool.pytest.ini_options]\nasyncio_mode = \"auto\"\ntestpaths = [\"tests\"]\n",
		"tests/test_log.py": "def test_process_line():\n    assert True\n",
	})
}

// THE RUNNER IS BEHIND A VARIABLE AND A LAUNCHER, AND BOTH ARE STRUCTURE. This
// is textual s6 exactly: the recipe is `$(run) pytest tests/ -n 16
// --dist=loadgroup $(ARGS)` with `run := poetry run` at the top of the file, so
// a reader that took `$(run)` for the command name found no runner in the
// recipe at all. It fell through to the whole repository — 3,422 tests, none of
// the project's own scoping — and was killed at its ceiling five and a half
// minutes later, silently.
func TestARecipeReachesItsRunnerThroughVariablesAndLaunchers(t *testing.T) {
	root := textualShape(t)
	ladder, ok := ReadingStrategies(root, Discover(root), nil)
	if !ok {
		t.Fatal("a project with a make test target produced no strategy at all")
	}
	first := ladder[0]
	if first.Runner != "pytest" {
		t.Fatalf("the runner behind the variable was not found: %#v", first)
	}
	for _, want := range []string{"poetry run", "pytest", "tests/", "-rA"} {
		if !strings.Contains(first.Command, want) {
			t.Errorf("the reading command is missing %q: %q", want, first.Command)
		}
	}
	if strings.Contains(first.Command, "$(") {
		t.Errorf("a make variable was handed to a shell as a word: %q", first.Command)
	}
	if strings.Contains(first.Command, "ARGS") {
		t.Errorf("a variable with nothing behind it was left standing: %q", first.Command)
	}
	if first.Declared != "make test" {
		t.Errorf("the project's own spelling of its verification was lost: %#v", first)
	}
	// And the rung s6 went straight to is still there, below it.
	if len(ladder) < 2 || !strings.Contains(ladder[1].Command, "python3 -m pytest") {
		t.Fatalf("the ladder has no rung below the project's own invocation: %#v", ladder)
	}
}

// A rung that names nothing has said nothing about the suite. textual's own
// `make test` passes `-n 16 --dist=loadgroup` and the task image has no
// pytest-xdist, so it exits in eight seconds on `unrecognized arguments: -n`.
// A reader with one rung takes that for the whole answer.
func TestARungThatNamesNothingFallsToTheNextOne(t *testing.T) {
	root := t.TempDir()
	fussy := stageRunner(t, root)
	ladder := []Strategy{
		{Command: fussy + " --dist=loadgroup", Read: FormatPlain, Runner: "fussy"},
		{Command: fussy, Read: FormatPlain, Runner: "fussy"},
	}
	reading := photograph(context.Background(), root, Plan{}, ladder, 30*time.Second)
	if !reading.Taken {
		t.Fatalf("the ladder gave up where a rung below it could read the suite: %q",
			reading.Unread)
	}
	if len(reading.Before.Reported) != 2 {
		t.Fatalf("the second rung's roster is %#v, want two checks", reading.Before.Reported)
	}
	if reading.Strategy.Command != fussy {
		t.Errorf("the reading does not say which rung it was taken on: %#v", reading.Strategy)
	}
	if strings.TrimSpace(reading.Unread) != "" {
		t.Errorf("a reading that was taken carries a reason for not being: %q", reading.Unread)
	}
}

// AND THE LAST RUNG'S ANSWER IS TAKEN WHATEVER IT NAMED. A runner that reports
// no identities is a real reading with an empty roster — plenty of verification
// commands say only `ok` — and treating that as no reading at all is the exact
// short-circuit that let fifty-two stated behaviours go unasked at the gate.
func TestTheLastRungsEmptyRosterIsStillAReading(t *testing.T) {
	root := t.TempDir()
	fussy := stageRunner(t, root)
	ladder := []Strategy{{Command: fussy + " --dist=loadgroup", Read: FormatPlain}}
	reading := photograph(context.Background(), root, Plan{}, ladder, 30*time.Second)
	if !reading.Taken {
		t.Fatalf("a runner that answered and named nothing was read as nobody having "+
			"looked: %q", reading.Unread)
	}
	if len(reading.Before.Reported) != 0 || reading.Before.Exit == 0 {
		t.Errorf("the reading is not the empty one this is a test about: %#v", reading.Before)
	}
}

// EVERY WAY OF HAVING NO READING SAYS WHY. The four of them cost a run nothing,
// nothing, nothing and five and a half minutes, and before this they were one
// zero value.
func TestEveryWayOfHavingNoReadingCarriesItsReason(t *testing.T) {
	// A project that declares nothing.
	bare := Photograph(context.Background(), t.TempDir(), 90*time.Minute, nil)
	if bare.Taken || !strings.Contains(bare.Unread, "no way of checking itself") {
		t.Errorf("a project that declares no verification said %q", bare.Unread)
	}

	// A wall too short to afford one. The command must be named — the sentence
	// is about what was NOT run — and it must not have run.
	root := textualShape(t)
	short := Photograph(context.Background(), root, time.Minute, nil)
	if short.Taken {
		t.Fatal("a sixty-second leaf took a reading")
	}
	if !strings.Contains(short.Unread, "cannot afford") || !strings.Contains(short.Unread, "pytest") {
		t.Errorf("the refusal does not say what was not run and why: %q", short.Unread)
	}

	// A command killed at its ceiling. This is s6: the budget is spent, nothing
	// is known, and the run must be able to say so.
	slow := t.TempDir()
	script := filepath.Join(slow, "slow.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	hung := photograph(context.Background(), slow, Plan{},
		[]Strategy{{Command: script, Read: FormatPlain}}, 300*time.Millisecond)
	if hung.Taken {
		t.Fatal("a reading that never finished was taken as one that did")
	}
	if !strings.Contains(hung.Unread, "killed at its ceiling") {
		t.Errorf("a hung reading does not say it was killed: %q", hung.Unread)
	}
	if !strings.Contains(hung.Unread, script) {
		t.Errorf("a hung reading does not name the command that hung: %q", hung.Unread)
	}
}

// stageRunner writes a runner that refuses the flag it is given and names two
// checks when it is not, and returns the path to it. It stands for the real
// shape this ladder exists for: an invocation the project wrote against a plugin
// its environment does not have.
func stageRunner(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fussy.sh")
	body := "#!/bin/sh\n" +
		"for arg in \"$@\"; do\n" +
		"  case \"$arg\" in --dist*) echo 'error: unrecognized arguments: --dist' >&2; exit 4;; esac\n" +
		"done\n" +
		"echo 'PASSED tests/test_a.py::test_one'\n" +
		"echo 'PASSED tests/test_b.py::test_two'\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
