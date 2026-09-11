package verify

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

// textualRealShape is textual's own tree as the task image holds it: the module
// naming convention, the package entry point that re-exports each widget's
// public name, and enough of the 251-file tests/ tree to reproduce s8. The
// decoys are real ones — every file here whose body says "log" inside "dialog",
// "catalog", "logic" or "logging" is a file the flattened reader selected.
func textualRealShape(t *testing.T) string {
	t.Helper()
	files := map[string]string{
		"Makefile": "run := poetry run\n\n.PHONY: test\ntest:\n\t$(run) pytest tests/ -n 16 --dist=loadgroup\n",
		"pyproject.toml": "[tool.poetry]\nname = \"textual\"\n\n" +
			"[tool.pytest.ini_options]\ntestpaths = [\"tests\"]\n",

		// The touched modules, and the package entry point that says what each
		// of them is called from outside.
		"src/textual/widget.py":            "class Widget:\n    pass\n",
		"src/textual/messages.py":          "class Message:\n    pass\n",
		"src/textual/widgets/_log.py":      "class Log:\n    pass\n",
		"src/textual/widgets/_rich_log.py": "class RichLog:\n    pass\n",
		"src/textual/widgets/__init__.py": "if TYPE_CHECKING:\n" +
			"    from textual.widgets._log import Log\n" +
			"    from textual.widgets._loading_indicator import LoadingIndicator\n" +
			"    from textual.widgets._rich_log import RichLog\n" +
			"__all__ = [\"Log\", \"LoadingIndicator\", \"RichLog\"]\n",

		// Named after a touched module, by pytest's own convention.
		"tests/test_log.py":    "from textual.widgets import Log\ndef test_log():\n    assert True\n",
		"tests/test_widget.py": "from textual.widget import Widget\ndef test_widget():\n    assert True\n",

		// Imports the touched module through the package's front door, which is
		// how every user of the library imports it — and the only way to know
		// it is the entry point's own re-export line.
		"tests/test_textlog.py":     "from rich.text import Text\nfrom textual.widgets import RichLog\ndef test_a():\n    assert True\n",
		"tests/test_concurrency.py": "from textual.widgets import RichLog\ndef test_b():\n    assert True\n",

		// Named after a DIFFERENT module whose name merely begins the same way.
		// This is rule 3: `Log` is not `Logger`, and `test_logger.py` is the
		// check for `logger.py`.
		"src/textual/logger.py": "class Logger:\n    pass\n",
		"tests/test_logger.py":  "from textual.logger import Logger\ndef test_c():\n    assert True\n",
	}
	// The decoys s8 actually selected: their bodies contain the letters of a
	// touched stem and their imports name nothing that was touched.
	for path, body := range map[string]string{
		"tests/animations/test_disabling_animations.py": "def test_d():\n    # dialog animation logic\n    assert True\n",
		"tests/animations/test_scrolling_animation.py":  "import logging\ndef test_e():\n    assert True\n",
		"tests/command_palette/test_discover.py":        "def test_f():\n    # catalog of commands\n    assert True\n",
		"tests/css/test_stylesheet.py":                  "def test_g():\n    # logical order\n    assert True\n",
		"tests/directory_tree/test_change_path.py":      "def test_h():\n    # dialog\n    assert True\n",
		"tests/document/test_document_delete.py":        "def test_i():\n    # logging\n    assert True\n",
		"tests/footer/test_footer.py":                   "def test_j():\n    # catalogue\n    assert True\n",
		"tests/input/test_input_validation.py":          "def test_k():\n    # logic\n    assert True\n",
	} {
		files[path] = body
	}
	return project(t, files)
}

// ADJACENCY IS A RELATIONSHIP, NOT A SUBSTRING. textual s8 is the whole reason
// this test exists: the job touched `_log.py`, `_rich_log.py`, `widget.py` and
// `messages.py`, the reader flattened every name to its letters and asked
// whether a test file's TEXT contained one, and the stem `log` matched `dialog`,
// `catalog`, `logic` and `logging` wherever they appeared. The selection came
// back as forty files across tests/animations, command_palette, css,
// directory_tree, document, footer and input — a third of the suite — and the
// reading was killed at its ceiling of 1m53s naming nothing at all.
func TestAdjacencyIsStructuralAndNeverASubstring(t *testing.T) {
	root := textualRealShape(t)
	touched := Focus{
		"src/textual/widgets/_log.py",
		"src/textual/widgets/_rich_log.py",
		"src/textual/widget.py",
		"src/textual/messages.py",
	}
	paths, core, ok := Adjacent(root, touched)
	if !ok {
		t.Fatal("nothing adjacent was found in a tree that holds several")
	}
	selected := map[string]bool{}
	for _, path := range paths {
		selected[path] = true
	}

	// Rank 1: named after a touched module by the runner's own convention.
	for _, want := range []string{"tests/test_log.py", "tests/test_widget.py"} {
		if !selected[want] {
			t.Errorf("%s is named after a touched module and was not selected: %#v", want, paths)
		}
	}
	// Rank 2: imports it, through the package's own re-export. textual has no
	// tests/test_rich_log.py at all — the checks for RichLog are these, and
	// finding them is the whole of what rule 2 is for.
	for _, want := range []string{"tests/test_textlog.py", "tests/test_concurrency.py"} {
		if !selected[want] {
			t.Errorf("%s imports a touched module and was not selected: %#v", want, paths)
		}
	}
	// Rule 3, both halves: a longer identifier is not a match, and a body that
	// merely contains the letters is not a relationship.
	if selected["tests/test_logger.py"] {
		t.Error("`Log` matched inside `Logger`: an identifier is not a prefix")
	}
	for path := range selected {
		for _, elsewhere := range []string{"animations/", "command_palette/", "css/",
			"directory_tree/", "document/", "footer/", "input/"} {
			if strings.Contains(path, elsewhere) {
				t.Errorf("a check about something else was selected on a substring: %q", path)
			}
		}
	}
	// And the size. s8's answer was forty files, a third of the suite; the
	// structural answer is the handful above.
	if len(paths) > 6 {
		t.Errorf("the selection is %d files, which is a suite rather than a scope: %#v",
			len(paths), paths)
	}
	// Rank 1 leads, so a selection trimmed to fit keeps the checks the change
	// is actually in.
	if paths[0] != "tests/test_log.py" {
		t.Errorf("the ranking does not put the named-after checks first: %#v", paths)
	}
	if core != 2 {
		t.Errorf("the rank-1 core is %d, want the two checks named after a touched "+
			"module: %#v", core, paths)
	}

	// The reading built on it is the one textual should have taken.
	ladder, ok := ReadingStrategies(root, Discover(root), touched)
	if !ok {
		t.Fatal("the project produced no strategy")
	}
	if !strings.Contains(ladder[0].Command, "tests/test_log.py") ||
		strings.Contains(ladder[0].Command, "tests/css/") {
		t.Errorf("the scoped reading is not the structural one: %q", ladder[0].Command)
	}
}

// A CHANGE TAKES THE CHECKS BESIDE IT TOO, and a relative import in JavaScript
// is a path that resolves — the same rule as a Python re-export, read in the
// other grammar.
func TestAdjacencyReadsBesideAndResolvesRelativeImports(t *testing.T) {
	beside := project(t, map[string]string{
		"pytest.ini":            "[pytest]\n",
		"src/pkg/other.py":      "x = 1\n",
		"src/pkg/test_other.py": "def test_other():\n    assert True\n",
		"tests/test_far.py":     "def test_far():\n    assert True\n",
	})
	paths, _, ok := Adjacent(beside, Focus{"src/pkg/other.py"})
	if !ok || len(paths) != 1 || paths[0] != "src/pkg/test_other.py" {
		t.Errorf("the check sitting beside the change was not the selection: %#v", paths)
	}

	javascript := project(t, map[string]string{
		"package.json":                 `{"name": "p", "scripts": {"test": "vitest run"}, "devDependencies": {"vitest": "^4"}}`,
		"vitest.config.ts":             "export default {}\n",
		"src/nodes/Element.ts":         "export class Element {}\n",
		"test/nodes/Element.test.ts":   "import { Element } from '../../src/nodes/Element';\nit('a', () => {})\n",
		"test/console/Console.test.ts": "import { Console } from '../../src/console/Console';\nit('b', () => {})\n",
	})
	paths, _, ok = Adjacent(javascript, Focus{"src/nodes/Element.ts"})
	if !ok {
		t.Fatal("a relative import that resolves to the touched file found nothing")
	}
	joined := strings.Join(paths, " ")
	if !strings.Contains(joined, "test/nodes/Element.test.ts") {
		t.Errorf("the check named after the touched file was not selected: %#v", paths)
	}
	if strings.Contains(joined, "Console.test.ts") {
		t.Errorf("a check that imports something else was selected: %#v", paths)
	}

	// And a job that named nothing scopes nothing.
	if _, _, ok := Adjacent(beside, nil); ok {
		t.Error("a focus that names nothing produced a selection")
	}
}

// NEVER THE SAME BLIND CEILING TWICE. A scoped reading killed at its ceiling
// having named nothing has measured one thing after all — this project's pace —
// and that is exactly what was missing when the size was chosen. Every other
// refusal is a fact about the tree, the project or the wall and IS inherited.
func TestACutScopedReadingIsRetakenAtTheSizeItsPaceAffords(t *testing.T) {
	cut := Reading{
		CutAfter: 113 * time.Second,
		// The budget it was killed at, which is also the most a retake can be
		// handed — see Reading.Retakeable, where a retake that could not be
		// smaller than the reading that was cut is refused.
		Budget: 113 * time.Second,
		Strategy: Strategy{
			Base:     "python3 -m pytest -rA",
			Selected: make([]string, 40),
			Scope:    "touched packages (40 files)",
		},
	}
	if !cut.Retakeable() {
		t.Fatal("a scoped reading cut at its ceiling was inherited as a settled refusal")
	}
	// A cut proves only that forty files cost MORE than 113s, so the average it
	// yields is a ceiling and never a target: taken as a target it would say the
	// same budget affords thirty-six, which is the same reading again.
	if got := cut.Pace().Affords(113 * time.Second); got != 20 {
		t.Errorf("the pace ceiling is %d files, want the halving at 20", got)
	}
	// And the core is the floor. The two checks named after what the job
	// touched are a reading of this change; twenty is a reading of its
	// neighbourhood, and it is the size that was just killed.
	cut.Strategy.Core = 2
	if got := cut.Strategy.retakeSize(cut.Pace().Affords(113 * time.Second)); got != 2 {
		t.Errorf("the retake is %d files, want the rank-1 core of 2", got)
	}
	// Every other refusal stays inherited: paying to learn the same fact twice
	// is what the baseline memory exists to stop.
	for _, settled := range []Reading{
		{Unread: "this project declares no way of checking itself"},
		{Unread: "a wall of 1m0s cannot afford a reading worth taking"},
		{Taken: true, Before: Result{Reported: []string{"a"}}},
	} {
		if settled.Retakeable() {
			t.Errorf("a settled answer was made retakeable: %#v", settled)
		}
	}

	// And the narrowing keeps the front of the ranked selection.
	ranked := Strategy{
		Base:     "python3 -m pytest -rA",
		Selected: []string{"tests/test_log.py", "tests/test_widget.py", "tests/test_textlog.py"},
	}
	narrowed, ok := ranked.narrowedTo(2)
	if !ok {
		t.Fatal("a scoped strategy could not be narrowed")
	}
	if narrowed.Command != "python3 -m pytest -rA tests/test_log.py tests/test_widget.py" {
		t.Errorf("the trim did not keep the most specific checks: %q", narrowed.Command)
	}
	if narrowed.Scope != "touched packages (2 files)" {
		t.Errorf("the narrowed reading does not say its new size: %q", narrowed.Scope)
	}
	if _, ok := ranked.narrowedTo(3); ok {
		t.Error("a strategy was narrowed to the size it already was")
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
			"touch .checks-emitted\n"+
			"sleep 30\n")
	// Cut only after the fixture has emitted its checks. A deadline measured
	// from process launch can expire before the shell starts on a busy machine;
	// that correctly yields no roster and never exercises partial preservation.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan Reading, 1)
	go func() {
		finished <- photograph(ctx, root, Plan{},
			[]Strategy{{Command: streaming, Read: FormatPlain, Scope: ScopeWhole}},
			30*time.Second)
	}()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	waiting := true
	for waiting {
		select {
		case <-tick.C:
			if _, err := os.Stat(filepath.Join(root, ".checks-emitted")); err == nil {
				waiting = false
			}
		case <-deadline.C:
			t.Fatal("the fixture never emitted its checks")
		case early := <-finished:
			t.Fatalf("the fixture ended before the requested cut: %#v", early)
		}
	}
	cancel()
	var reading Reading
	select {
	case reading = <-finished:
	case <-deadline.C:
		t.Fatal("the reading did not settle after cancellation")
	}
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

// igelShape is the tree the s8 run stood in: one library package, one test file
// beside it under tests/, and a Makefile whose test target runs the whole of
// tests/ through pytest. The new file is what the run itself wrote.
func igelShape(t *testing.T, withTheRunsOwnTest bool) string {
	t.Helper()
	tree := map[string]string{
		"Makefile":                       "test:\n\tpython3 -m pytest -rA tests/\n",
		"setup.py":                       "from setuptools import setup\nsetup(name=\"igel\")\n",
		"igel/__init__.py":               "from igel.igel import Igel\n",
		"igel/igel.py":                   "class Igel:\n    results_path = None\n",
		"tests/test_igel/test_igel.py":   "from igel import Igel\ndef test_fit():\n    assert True\n",
		"tests/test_utils/test_utils.py": "def test_elsewhere():\n    assert True\n",
		"tests/test_cli/test_cli.py":     "def test_cli():\n    assert True\n",
	}
	if withTheRunsOwnTest {
		tree["tests/test_igel/test_feature_schema.py"] =
			"from igel import Igel\ndef test_schema_is_persisted():\n    assert True\n"
	}
	return project(t, tree)
}

// THE RUN'S OWN CHECKS ARE ALWAYS IN SCOPE. igel's s8 run scoped every reading
// of every round to `touched (1 file)` — the one source file the request named —
// and each reading named the two checks that file's neighbour already held. The
// run had meanwhile written about forty checks into a NEW file under tests/,
// and no reading ever saw one of them: the scope was decided before the work
// existed, so it could not contain anything the work went on to write.
//
// That is not a small loss. The coverage mapping is handed the ROSTER of a
// reading, so a checklist point whose only exercise is a check this round wrote
// stays unexercised however good the check is, the before/after cannot move, and
// the round buys a repair for work it has already done.
//
// So the after reading's file set is the structural adjacency UNION every check
// file in the record of what the run left behind — from the world's record of
// the tree, not from the worker's account of what it tested, which is the one
// claim this whole gate exists not to weigh.
func TestTheRunsOwnChecksJoinTheReading(t *testing.T) {
	before := igelShape(t, false)
	focus := Focus{"igel/igel.py"}
	ladder, ok := ReadingStrategies(before, Discover(before), focus)
	if !ok {
		t.Fatal("a project with a make test target produced no strategy at all")
	}
	scoped := ladder[0]
	if scoped.Scope == ScopeWhole {
		t.Fatalf("the reading before the work is not scoped at all: %#v", scoped)
	}
	if !strings.Contains(scoped.Command, "tests/test_igel/test_igel.py") {
		t.Fatalf("the scoped reading missed the checks beside the change: %q", scoped.Command)
	}

	// And now the tree the run left behind, with its own new test file in it.
	after := igelShape(t, true)
	record := []string{
		"igel/igel.py",
		"model_results/feature_schema.joblib",
		"tests/test_igel/test_feature_schema.py",
	}
	widened, added := scoped.WithChangedWork(after, record)
	if !added {
		t.Fatal("the check file the run wrote did not join the reading")
	}
	if !strings.Contains(widened.Command, "tests/test_igel/test_feature_schema.py") {
		t.Errorf("the after reading does not name the run's own checks: %q", widened.Command)
	}
	if !strings.Contains(widened.Command, "tests/test_igel/test_igel.py") {
		t.Errorf("the after reading dropped the checks it had before: %q", widened.Command)
	}
	// Only checks. A joblib the run also produced is an artifact, not something
	// to hand a test runner.
	if strings.Contains(widened.Command, "feature_schema.joblib") {
		t.Errorf("a produced artifact was handed to the runner as a check: %q", widened.Command)
	}
	if strings.Contains(widened.Command, "test_cli.py") {
		t.Errorf("the widening reached work this job never touched: %q", widened.Command)
	}
	if !strings.Contains(widened.Scope, "2 files") {
		t.Errorf("the reading does not say what it is now a reading of: %q", widened.Scope)
	}

	// THE SUBTRACTION STILL HOLDS. A wider after reading covers the narrower
	// before one, so the two are still comparable...
	if !widened.covers(scoped) {
		t.Error("the widened reading no longer covers the reading it is subtracted from")
	}
	if scoped.covers(widened) {
		t.Error("the narrower reading claims to cover the wider one")
	}
	// ...and a check that did not exist before cannot be a regression, however
	// red it is now.
	const (
		old = "tests/test_igel/test_igel.py::test_fit"
		new = "tests/test_igel/test_feature_schema.py::test_schema_is_persisted"
	)
	reading := Reading{
		Taken: true, AfterTaken: true,
		Before: Result{Strategy: scoped, Reported: []string{old}},
		After: Result{
			Strategy: widened,
			Reported: []string{old, new},
			Failing:  []string{new},
		},
	}
	if broke := reading.Regressed(); len(broke) > 0 {
		t.Errorf("a check the run wrote itself was reported as a regression: %v", broke)
	}
	// A check the project had, green before and red after, still is one.
	reading.After.Failing = []string{old}
	if broke := reading.Regressed(); len(broke) != 1 || broke[0] != old {
		t.Errorf("a real regression stopped being one: %v", broke)
	}
}

// And the run's own checks are what every cut is floored at. A scope is capped
// twice — at an eighth of the suite, and at what a measured pace affords — and
// both cuts take the tail of a selection. igel s8's new file sorted after the
// tests the repository already had, so a selection ordered by name alone would
// hand exactly the run's own checks to the cut. They lead the selection instead:
// they are not a sample of the suite, they ARE the work.
func TestTheRunsOwnChecksSurviveEveryCut(t *testing.T) {
	tree := map[string]string{
		"Makefile":         "test:\n\tpython3 -m pytest -rA tests/\n",
		"setup.py":         "from setuptools import setup\nsetup(name=\"igel\")\n",
		"igel/__init__.py": "from igel.igel import Igel\n",
		"igel/igel.py":     "class Igel:\n    pass\n",
		// The check the run wrote, named so that it sorts AFTER every check the
		// repository already had.
		"tests/test_zeta_feature_schema.py": "def test_schema():\n    assert True\n",
	}
	for i := range 200 {
		tree[fmt.Sprintf("tests/test_a%03d.py", i)] = "def test_x():\n    assert True\n"
	}
	root := project(t, tree)

	paths, core, ok := Adjacent(root, Focus{"igel/igel.py", "tests/test_zeta_feature_schema.py"})
	if !ok {
		t.Fatal("a focus naming a source file and a new test selected nothing")
	}
	if core == 0 {
		t.Fatal("nothing was ranked as beside the change")
	}
	if paths[0] != "tests/test_zeta_feature_schema.py" {
		t.Errorf("the run's own check does not lead the selection: %v", paths[:min(4, len(paths))])
	}
	if len(paths) >= 200 {
		t.Errorf("the selection was never cut at all, so this proves nothing: %d files", len(paths))
	}
	held := false
	for _, path := range paths {
		if path == "tests/test_zeta_feature_schema.py" {
			held = true
		}
	}
	if !held {
		t.Errorf("the cut dropped the run's own check: %v", paths)
	}
}

// textualS10Shape is the tree textual s10 stood in, as its task image holds it:
// two log widgets side by side, a package front door that re-exports both, and
// three test files — one named after `_log.py` by pytest's own convention, and
// two that reach `_rich_log.py` only through the front door.
func textualS10Shape(t *testing.T) string {
	t.Helper()
	return project(t, map[string]string{
		"Makefile": "run := poetry run\n\n.PHONY: test\ntest:\n\t$(run) pytest tests/ $(ARGS)\n",
		"pyproject.toml": "[tool.poetry]\nname = \"textual\"\n\n" +
			"[tool.pytest.ini_options]\ntestpaths = [\"tests\"]\n",
		"src/textual/widgets/__init__.py": "from textual.widgets._log import Log\n" +
			"from textual.widgets._rich_log import RichLog\n",
		"src/textual/widgets/_log.py":      "class Log:\n    pass\n",
		"src/textual/widgets/_rich_log.py": "class RichLog:\n    pass\n",
		"tests/test_log.py": "from textual.app import App\nfrom textual.widgets import Log\n" +
			"async def test_process_line():\n    assert True\n",
		"tests/test_textlog.py": "from rich.text import Text\nfrom textual.widgets import RichLog\n" +
			"async def test_make_renderable_expand_tabs():\n    assert True\n",
		"tests/test_concurrency.py": "from threading import Thread\nfrom textual.widgets import RichLog\n" +
			"async def test_call_from_thread():\n    assert True\n",
		"tests/test_button.py":         "def test_pressed():\n    assert True\n",
		"src/textual/widgets/_tree.py": "class Tree:\n    pass\n",
		"tests/test_tree.py":           "from textual.widgets import Tree\ndef test_node():\n    assert True\n",
	})
}

// textualS10Request is the request that job was given, verbatim in the parts
// that matter: it names `Log` and `RichLog` and spells no source path at all.
const textualS10Request = "RichLog still snaps back to the newest entry after users scroll up, " +
	"unlike Log, and RichLog.write(expand=True) no longer preserves full-width justified " +
	"rendering with current Rich. Make Log and RichLog expose is_following_end: bool, " +
	"follow_end(animate: bool = False), and a FollowChanged message carrying widget, " +
	"is_following_end, scroll_y, and max_scroll_y."

// THE SECOND READING IS AIMED AT THE CHANGE, NOT ONLY AT THE REQUEST.
//
// A scope has to be a reading of the REQUEST, because at the moment the first
// reading is taken there is no diff to read — and a request is not a diff.
// textual s10 asked for "Log and RichLog": `RichLog` resolves to `_rich_log.py`,
// `Log` is a single word that resolves to nothing, and both readings therefore
// ran `tests/test_concurrency.py tests/test_textlog.py` — the two files that
// import RichLog through the package front door. The change touched `_log.py`
// AND `_rich_log.py`, and `tests/test_log.py` — a file the repository already
// had, named after the file the work changed by pytest's own convention — was
// read on neither side of the photograph.
//
// By the time the second reading is taken the diff exists, and it is the only
// account of where the work actually went.
func TestTheSecondReadingIsAimedAtTheChangeAndNotOnlyTheRequest(t *testing.T) {
	root := textualS10Shape(t)
	focus := Focus(NamedSubjects(textualS10Request))
	ladder, ok := ReadingStrategies(root, Discover(root), focus)
	if !ok {
		t.Fatal("a project with a make test target produced no strategy at all")
	}
	before := ladder[0]
	if before.Scope == ScopeWhole {
		t.Fatalf("the first reading is not scoped at all: %#v", before)
	}
	// The request says "Log and RichLog", so the reading it asks for reaches
	// both widgets: the two files that import RichLog through the package front
	// door, and the check named after `_log.py` by pytest's own convention.
	for _, wanted := range []string{
		"tests/test_textlog.py", "tests/test_concurrency.py", "tests/test_log.py",
	} {
		if !strings.Contains(before.Command, wanted) {
			t.Errorf("the request's own reading missed %s: %q", wanted, before.Command)
		}
	}

	// And now the record of what the run left behind, which names a widget the
	// request never mentioned at all.
	record := []string{
		"src/textual/widgets/_tree.py",
		"src/textual/widgets/_rich_log.py",
		"examples/rich_log_follow_state.py",
	}
	after, widened := before.WithChangedWork(root, record)
	if !widened {
		t.Fatal("the checks beside the files the run changed did not join the reading")
	}
	if !strings.Contains(after.Command, "tests/test_tree.py") {
		t.Errorf("the second reading still cannot see the checks beside `_tree.py`: %q", after.Command)
	}
	for _, held := range []string{"tests/test_textlog.py", "tests/test_concurrency.py"} {
		if !strings.Contains(after.Command, held) {
			t.Errorf("the second reading dropped %s: %q", held, after.Command)
		}
	}
	// A file the change never touched and nothing imports is still out.
	if strings.Contains(after.Command, "tests/test_button.py") {
		t.Errorf("the widening reached work this job never touched: %q", after.Command)
	}
	// The subtraction still holds: wider covers narrower, never the reverse.
	if !after.covers(before) {
		t.Error("the widened reading no longer covers the reading it is subtracted from")
	}
	if before.covers(after) {
		t.Error("the narrower reading claims to cover the wider one")
	}
}

// AND A WHOLE READING THAT DID NOT FIT DOES NOT FIT TWICE. Where the request
// resolved to nothing — ofetch s10 took six readings and every one of them was
// `whole` — the first rung is the whole suite, and a whole suite killed at its
// ceiling has proved it is bigger than the wall. The change always resolves: it
// is a list of files that exist.
func TestAWholeReadingThatDidNotFitIsRetakenOnTheChange(t *testing.T) {
	root := textualS10Shape(t)
	// A job that named nothing the workspace holds: every rung is whole.
	ladder, ok := ReadingStrategies(root, Discover(root), nil)
	if !ok || ladder[0].Scope != ScopeWhole {
		t.Fatalf("a job that named nothing did not get a whole reading: %#v", ladder)
	}
	record := []string{"src/textual/widgets/_log.py", "src/textual/widgets/_rich_log.py"}
	// A whole rung has nothing to widen — it already runs everything.
	if _, widened := ladder[0].WithChangedWork(root, record); widened {
		t.Error("a reading of the whole suite was widened, which means nothing")
	}
	narrowed, ok := ChangedWorkStrategy(root, Discover(root), record)
	if !ok {
		t.Fatal("the change resolved to no reading at all")
	}
	if narrowed.Scope == ScopeWhole {
		t.Fatalf("the reading aimed at the change is still whole: %#v", narrowed)
	}
	if !strings.Contains(narrowed.Command, "tests/test_log.py") {
		t.Errorf("the reading aimed at the change misses the checks beside it: %q", narrowed.Command)
	}
	// It is a SUBSET of the whole reading, so the pair is deliberately not
	// comparable and nothing is subtracted from it.
	if narrowed.covers(ladder[0]) {
		t.Error("a reading of a handful of files claims to cover a reading of everything")
	}
	reading := Reading{
		Taken: true, AfterTaken: true,
		Before: Result{Strategy: ladder[0], Failing: []string{"tests/test_button.py::test_pressed"}},
		After:  Result{Strategy: narrowed, Failing: []string{"tests/test_log.py::test_process_line"}},
	}
	if broke := reading.Regressed(); len(broke) > 0 {
		t.Errorf("two readings of different things were subtracted: %v", broke)
	}
}

// textualN1Shape is textual as the nemotron n1 run stood in it, including the
// thing that actually broke the reading: the repository DOCUMENTS ITSELF, so
// `rich_log.py` and `log.py` each exist twice — once under `docs/` where the
// examples live, and once under `src/` where the widget lives.
func textualN1Shape(t *testing.T) string {
	t.Helper()
	return project(t, map[string]string{
		"Makefile": "run := poetry run\n\n.PHONY: test\ntest:\n\t$(run) pytest tests/ $(ARGS)\n",
		"pyproject.toml": "[tool.poetry]\nname = \"textual\"\n\n" +
			"[tool.pytest.ini_options]\ntestpaths = [\"tests\"]\n",
		// The documentation copies, which sort first and used to win.
		"docs/examples/widgets/log.py":      "from textual.widgets import Log\n",
		"docs/examples/widgets/rich_log.py": "from textual.widgets import RichLog\n",
		// The widgets themselves, spelled the way python spells a private
		// module behind a package front door.
		"src/textual/widgets/__init__.py": "from textual.widgets._log import Log\n" +
			"from textual.widgets._rich_log import RichLog\n",
		"src/textual/widgets/_log.py":      "class Log:\n    pass\n",
		"src/textual/widgets/_rich_log.py": "class RichLog:\n    pass\n",
		"tests/test_log.py": "from textual.widgets import Log\n" +
			"async def test_process_line():\n    assert True\n",
		"tests/test_textlog.py": "from textual.widgets import RichLog\n" +
			"async def test_make_renderable():\n    assert True\n",
		"tests/test_button.py": "def test_pressed():\n    assert True\n",
		"tests/test_tabs.py":   "def test_tabs():\n    assert True\n",
	})
}

// A REQUEST THAT SAYS BOTH `RichLog` AND `Log` HAS NAMED TWO THINGS.
//
// textual's nemotron n1 run took fourteen readings and the request's own scope
// was `tests/test_concurrency.py tests/test_textlog.py` — 3 checks — on every
// one of them, while the change touched `widgets/_log.py` AND
// `widgets/_rich_log.py`. `tests/test_log.py` reached the selection only after
// the work, off the diff, and the baseline it was compared against never had it.
//
// Two structural faults, and neither is about vocabulary. A single capitalised
// word is not a subject on sight — it is as likely to be the first word of a
// sentence — so `Log` was dropped and `_log.py` was never located. And a name
// that resolved kept the FIRST file the walk tripped over, so a repository that
// documents itself shadows its own source: `docs/examples/widgets/rich_log.py`
// sorts before `src/textual/widgets/_rich_log.py` and every reading was aimed at
// the documentation copy of the widget being changed.
func TestARequestThatSpellsANameBothWaysReachesBothWidgets(t *testing.T) {
	root := textualN1Shape(t)
	subjects := NamedSubjects(textualS10Request)
	if !contains(subjects, "Log") {
		t.Fatalf("a request that says `Log and RichLog` named only the compound: %v", subjects)
	}
	if !contains(subjects, "RichLog") {
		t.Fatalf("the compound itself stopped being a subject: %v", subjects)
	}

	located := Locate(root, Focus(subjects))
	// EVERY file a name carries, so the implementation is not shadowed by the
	// documentation of it.
	for _, wanted := range []string{
		"src/textual/widgets/_rich_log.py", "src/textual/widgets/_log.py",
	} {
		if !contains(located, wanted) {
			t.Errorf("%s was shadowed by a file of the same name: %v", wanted, located)
		}
	}

	paths, core, ok := Adjacent(root, located)
	if !ok || core == 0 {
		t.Fatalf("nothing was ranked as beside the change: ok=%v core=%d %v", ok, core, paths)
	}
	held := strings.Join(paths, " ")
	for _, wanted := range []string{"tests/test_log.py", "tests/test_textlog.py"} {
		if !strings.Contains(held, wanted) {
			t.Errorf("the reading the request asks for misses %s: %v", wanted, paths)
		}
	}
	// tests/test_log.py is named after `_log.py` by pytest's own convention —
	// the underscore is the module being private, not part of its name — so it
	// is in the FIRST rank, which is what survives every cut.
	if !contains(paths[:core], "tests/test_log.py") {
		t.Errorf("the check named after the changed module is not in the first rank: %v",
			paths[:core])
	}
	// And nothing this job never touched.
	for _, elsewhere := range []string{"test_button.py", "test_tabs.py"} {
		if strings.Contains(held, elsewhere) {
			t.Errorf("the selection reached work this job never touched: %v", paths)
		}
	}

	// End to end: the rung the run would actually have taken.
	ladder, lok := ReadingStrategies(root, Discover(root), Focus(subjects))
	if !lok {
		t.Fatal("a project with a make test target produced no strategy at all")
	}
	if !strings.Contains(ladder[0].Command, "tests/test_log.py") {
		t.Errorf("the first rung still cannot see the Log side of the change: %q",
			ladder[0].Command)
	}
}

// A short name is admitted because the REQUEST spelled it both ways, and never
// because it is short. The compound alone does not admit it, and a bare word
// standing on its own is not a subject.
func TestAShortNameIsOnlyASubjectWhenTheRequestSpellsItBothWays(t *testing.T) {
	for _, probe := range []struct {
		name string
		text string
		want bool
	}{
		{name: "both ways", text: "Make Log and RichLog expose is_following_end.", want: true},
		{name: "only inside the compound", text: "RichLog must follow the end.", want: false},
		{name: "only on its own", text: "The Log must follow the end.", want: false},
		{name: "an initial is not a name", text: "The Ab and AbCd widgets.", want: false},
	} {
		t.Run(probe.name, func(t *testing.T) {
			subjects := NamedSubjects(probe.text)
			got := contains(subjects, "Log") || contains(subjects, "Ab")
			if got != probe.want {
				t.Errorf("NamedSubjects(%q) = %v, want the short name admitted=%v",
					probe.text, subjects, probe.want)
			}
		})
	}
}

// A REQUEST THAT NAMES A PACKAGE IS READ OVER THAT PACKAGE.
//
// The errand measured in #429 said `go test ./internal/subharness/ -count=1`,
// which spells its subject as plainly as a request ever does, and the focus came
// out EMPTY: the path reader wanted a dot and an extension, the name reader
// wanted CamelCase or snake_case, and a directory is neither. So the ladder had
// one whole rung, and a job about seventeen files photographed 4,587 tests nine
// times, every reading killed at its ceiling.
func TestARequestThatNamesAPackageIsReadOverThatPackage(t *testing.T) {
	const errand = "Run the command 'go test ./internal/subharness/ -count=1' in this " +
		"workspace and report the final line it prints. Change no files."
	subjects := NamedSubjects(errand)
	if !slices.Contains(subjects, "internal/subharness") {
		t.Fatalf("the package the request names is not a subject of it: %#v", subjects)
	}

	root := project(t, map[string]string{
		"go.mod":                           "module example.com/thing\n\ngo 1.22\n",
		"internal/subharness/card.go":      "package subharness\n",
		"internal/subharness/card_test.go": "package subharness\n\nfunc TestCard(t *testing.T) {}\n",
		"internal/session/turn.go":         "package session\n",
		"internal/session/turn_test.go":    "package session\n\nfunc TestTurn(t *testing.T) {}\n",
	})
	located := Locate(root, Focus(subjects))
	if !slices.Contains([]string(located), "internal/subharness") {
		t.Fatalf("a directory the workspace holds did not survive resolution: %#v", located)
	}
	// And prose is not a path because it has a slash in it. A place the
	// workspace does not hold is dropped, so nothing aims a reading at a
	// directory called `and`.
	if kept := Locate(root, Focus{"and/or", "vendor/nothing"}); len(kept) != 0 {
		t.Errorf("a place the workspace does not hold was kept as a subject: %#v", kept)
	}

	ladder, ok := ReadingStrategies(root, Discover(root), located)
	if !ok || len(ladder) == 0 {
		t.Fatal("a project with a go.mod produced no strategy at all")
	}
	first := ladder[0]
	if first.Scope == ScopeWhole {
		t.Fatalf("the request named a package and the reading is of everything: %#v", first)
	}
	if !strings.Contains(first.Command, "./internal/subharness/") {
		t.Errorf("the reading is not taken over the package the request named: %q", first.Command)
	}
	if strings.Contains(first.Command, "internal/session") {
		t.Errorf("the reading reached a package the request never named: %q", first.Command)
	}
	// AND THE RUNNER'S OWN "EVERYTHING" IS NOT STILL ON THE COMMAND LINE. `go
	// test -json ./... ./internal/subharness/...` is a reading of the whole
	// repository wearing a scope's clothes: measured at 4,588 checks against a
	// selection that had correctly chosen seventeen files.
	if strings.Contains(first.Command, "./...") {
		t.Errorf("the scoped reading still names the whole tree: %q", first.Command)
	}
}
