package verify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeScript stages an executable script and returns the path to it.
func writeScript(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// textualTreeShape is textual with enough of its own tests/ tree to be worth
// scoping: the Makefile recipe s6 could not read, the pytest config, the module
// the request is about, and three test files of which exactly one is about it.
func textualTreeShape(t *testing.T) string {
	t.Helper()
	return project(t, map[string]string{
		"Makefile": "run := poetry run\n\n.PHONY: test\ntest:\n\t$(run) pytest tests/ -n 16 --dist=loadgroup $(ARGS)\n",
		"pyproject.toml": "[tool.poetry]\nname = \"textual\"\n\n" +
			"[tool.pytest.ini_options]\ntestpaths = [\"tests\"]\n",
		"src/textual/widgets/_rich_log.py":       "class RichLog:\n    pass\n",
		"tests/test_rich_log.py":                 "from textual.widgets import RichLog\ndef test_follows():\n    assert True\n",
		"tests/test_button.py":                   "def test_pressed():\n    assert True\n",
		"tests/snapshot_tests/test_snapshots.py": "def test_snap():\n    assert True\n",
	})
}

// A READING IS SCOPED BEFORE IT IS BOUNDED. textual's whole-repository reading
// collects 3,422 tests and takes 793 seconds, measured in its own task image,
// against the 5m30s its wall afforded — so the only rung the ladder had was one
// that could not finish, and the run's whole record of its own verification was
// five and a half minutes of silence.
//
// The budget was doing exactly what it was written to do. What was missing is
// that nothing had decided WHAT to measure before deciding HOW LONG to measure
// it. The checks next to the change come first; the whole suite is what is below
// them.
func TestAReadingIsScopedBeforeItIsBounded(t *testing.T) {
	root := textualTreeShape(t)
	focus := Focus{"src/textual/widgets/_rich_log.py"}
	ladder, ok := ReadingStrategies(root, Discover(root), focus)
	if !ok {
		t.Fatal("a project with a make test target produced no strategy at all")
	}
	first := ladder[0]
	if first.Scope == ScopeWhole {
		t.Fatalf("the first rung is still a reading of the whole repository: %#v", first)
	}
	if !strings.Contains(first.Scope, "1 file") {
		t.Errorf("the reading does not say what it is a reading of: %q", first.Scope)
	}
	if !strings.Contains(first.Command, "tests/test_rich_log.py") {
		t.Errorf("the scoped reading does not name the checks next to the change: %q", first.Command)
	}
	for _, elsewhere := range []string{"test_button.py", "test_snapshots.py"} {
		if strings.Contains(first.Command, elsewhere) {
			t.Errorf("the scoped reading reaches work this job never touched: %q", first.Command)
		}
	}
	// THE PROJECT'S OWN SCOPE IS REPLACED, NOT ADDED TO. pytest handed both
	// `tests/` and one file inside it runs the whole of `tests/`, which is the
	// reading this rung exists to avoid.
	if strings.Contains(first.Command, "tests/ ") {
		t.Errorf("the project's own whole-suite scope survived beside the selection: %q", first.Command)
	}
	// And the whole suite is still down there, in the order it was in before.
	whole := 0
	for _, rung := range ladder[1:] {
		if rung.Scope == ScopeWhole {
			whole++
		}
	}
	if whole == 0 {
		t.Error("the ladder lost its whole-suite rungs")
	}
	// A job that named nothing is a job with nothing to scope by, and its
	// reading is of the whole project — which is what every reading here was.
	unscoped, _ := ReadingStrategies(root, Discover(root), nil)
	if unscoped[0].Scope != ScopeWhole {
		t.Errorf("a job that named nothing got a scoped reading anyway: %#v", unscoped[0])
	}
}

// The checks adjacent to a change are found three ways, and each of them is a
// way a repository spells the link between a module and the check for it.
func TestTheChecksNextToAChangeAreFoundByShape(t *testing.T) {
	root := project(t, map[string]string{
		"pytest.ini":            "[pytest]\n",
		"src/pkg/_rich_log.py":  "class RichLog:\n    pass\n",
		"src/pkg/other.py":      "x = 1\n",
		"src/pkg/test_other.py": "def test_other():\n    assert True\n",
		// Named after the module, in the project's one tests/ tree.
		"tests/test_rich_log.py": "def test_a():\n    assert True\n",
		// Not named after it, but it imports it — in the spelling the project
		// writes imports in, which is not the spelling on disk.
		"tests/test_widgets.py": "from pkg import RichLog\ndef test_b():\n    assert True\n",
		// About something else entirely.
		"tests/test_colours.py": "def test_c():\n    assert True\n",
	})
	paths, ok := Adjacent(root, Focus{"src/pkg/_rich_log.py"})
	if !ok {
		t.Fatal("nothing adjacent to a change was found in a tree that holds two")
	}
	joined := strings.Join(paths, " ")
	for _, want := range []string{"tests/test_rich_log.py", "tests/test_widgets.py"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%s is a check next to this change and was not selected: %#v", want, paths)
		}
	}
	if strings.Contains(joined, "test_colours.py") {
		t.Errorf("a check about something else was selected: %#v", paths)
	}
	// A change in a directory takes the checks BESIDE it too.
	beside, ok := Adjacent(root, Focus{"src/pkg/other.py"})
	if !ok || !strings.Contains(strings.Join(beside, " "), "src/pkg/test_other.py") {
		t.Errorf("the check sitting beside the change was not selected: %#v", beside)
	}
	// And a job that named nothing scopes nothing.
	if _, ok := Adjacent(root, nil); ok {
		t.Error("a focus that names nothing produced a selection")
	}
}

// TWO READINGS ONLY SUBTRACT WHEN THEY ARE READINGS OF THE SAME THING. A before
// reading of a whole suite minus an after reading of three files is every check
// that was not selected reported as one that stopped existing — a finding per
// untouched test, out of a fact about a command line.
func TestReadingsOfDifferentScopesDoNotSubtract(t *testing.T) {
	wholeStrategy := Strategy{Command: "pytest -rA", Scope: ScopeWhole}
	scoped := Strategy{Command: "pytest -rA tests/test_a.py", Scope: "touched packages (1 file)"}
	mixed := Reading{
		Taken: true, AfterTaken: true,
		Before: Result{Strategy: wholeStrategy,
			Reported: []string{"test_a", "test_b", "test_c"}, Failing: nil},
		After: Result{Strategy: scoped,
			Reported: []string{"test_a"}, Failing: []string{"test_a"}},
	}
	if got := mixed.Vanished(); len(got) != 0 {
		t.Errorf("checks the second reading never ran were reported as gone: %#v", got)
	}
	if got := mixed.Regressed(); len(got) != 0 {
		t.Errorf("two readings of different suites were subtracted: %#v", got)
	}
	// The same pair taken the same way subtracts exactly as it always did.
	same := mixed
	same.Before.Strategy, same.After.Strategy = scoped, scoped
	same.Before.Reported = []string{"test_a"}
	if got := same.Regressed(); len(got) != 1 || got[0] != "test_a" {
		t.Errorf("a real regression stopped being one: %#v", got)
	}
}

// A CUT READING IS AN INCOMPLETE OBSERVATION, NOT AN ABSENT ONE. ink s7's
// `npx ava --tap` was killed at its ceiling of 1m53s having already streamed
// part of its 922 checks; the whole reading was thrown away, the next round's
// gate had no roster at all, and a deliverable at 13 of 25 hidden checks passed
// with nothing to weigh against it.
//
// What it named is kept, marked partial. A partial roster answers "does a check
// for this exist" for everything it reached; it answers "did this work break
// something" not at all, and those are two questions.
func TestACutReadingKeepsTheChecksItNamed(t *testing.T) {
	root := t.TempDir()
	// A runner that names two checks and then hangs, which is what a streaming
	// reporter killed at its ceiling looks like from outside.
	streaming := writeScript(t, "streaming.sh",
		"#!/bin/sh\n"+
			"echo 'ok 1 - opens after five failures'\n"+
			"echo 'ok 2 - closes on a good probe'\n"+
			"sleep 30\n")
	reading := photograph(context.Background(), root, Plan{},
		[]Strategy{{Command: streaming, Read: FormatPlain, Scope: ScopeWhole}},
		400*time.Millisecond)
	if !reading.Taken {
		t.Fatalf("a reading that named two checks before its ceiling was thrown away: %q",
			reading.Unread)
	}
	if !reading.Partial {
		t.Error("a cut reading is not marked as one, so it would be subtracted")
	}
	if len(reading.Before.Reported) != 2 {
		t.Errorf("the names it did reach were lost: %#v", reading.Before.Reported)
	}
	if reading.CutAfter <= 0 {
		t.Error("the run learned nothing about how long the reading took")
	}
	// And it is not comparable: subtracting a partial roster from a whole one
	// reports the entire tail of the suite as checks that disappeared.
	pair := reading
	pair.After, pair.AfterTaken = Result{Strategy: reading.Before.Strategy,
		Reported: []string{"opens after five failures"}}, true
	if got := pair.Vanished(); len(got) != 0 {
		t.Errorf("a partial roster was subtracted: %#v", got)
	}
}
