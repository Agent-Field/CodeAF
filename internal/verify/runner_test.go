package verify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture is one runner's real output, captured from a graded run.
func fixture(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "readings", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(body)
}

// project writes a workspace out of the files a real repository declares itself
// with, and returns its root.
func project(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// THE READING IS OF THE RUNNER, NOT OF THE LIFECYCLE SCRIPT. This is ofetch s5
// exactly: the project declares `pnpm test`, whose body lints and typechecks
// before it reaches vitest, so a formatting complaint exits 1 in front of a
// suite that never ran and the gate reported "`pnpm test` exited 1 and named 0
// checks" of a green suite. The strategy has to reach past the lifecycle to the
// runner, keep the project's own flags on it, and ask it for a reading a
// machine can take.
func TestTheReadingReachesPastTheLifecycleScriptToTheRunner(t *testing.T) {
	root := project(t, map[string]string{
		"pnpm-lock.yaml":   "lockfileVersion: '9.0'\n",
		"vitest.config.ts": "export default {}\n",
		"package.json": `{
  "scripts": {
    "lint": "eslint . && prettier -c src test",
    "test:types": "tsc --noEmit",
    "test": "pnpm lint && pnpm test:types && vitest run --coverage"
  },
  "devDependencies": {"vitest": "^4.0.5", "eslint": "^9"}
}`,
	})
	strategy, ok := ReadingStrategy(root, Discover(root), nil)
	if !ok {
		t.Fatal("a project that declares a test script produced no strategy")
	}
	if strategy.Runner != "vitest" {
		t.Fatalf("the runner under the lifecycle script was not found: %#v", strategy)
	}
	for _, want := range []string{"vitest", "run", "--coverage", "--reporter=json"} {
		if !strings.Contains(strategy.Command, want) {
			t.Errorf("the reading command is missing %q: %q", want, strategy.Command)
		}
	}
	for _, unwanted := range []string{"eslint", "prettier", "tsc"} {
		if strings.Contains(strategy.Command, unwanted) {
			t.Errorf("the reading still runs %q, so a formatting complaint can "+
				"still exit in front of the suite: %q", unwanted, strategy.Command)
		}
	}
	if !strings.HasPrefix(strategy.Command, "pnpm exec ") {
		t.Errorf("a runner installed into node_modules was invoked without the "+
			"package manager that installed it: %q", strategy.Command)
	}
	if strategy.Read != FormatNodeJSON {
		t.Errorf("the runner's own machine-readable output is not being read: %q", strategy.Read)
	}
	if strategy.Declared != "pnpm test" {
		t.Errorf("the project's own spelling of its verification was lost: %#v", strategy)
	}
	if !strings.Contains(strategy.Source, "package.json#scripts.test") {
		t.Errorf("the strategy does not say where the runner was found: %#v", strategy)
	}
}

// A bare `vitest` watches the filesystem and never exits. A reading taken with
// it hits its ceiling every time, names nothing, and costs an eighth of the
// wall — so the run subcommand is supplied where the project's own invocation
// does not carry one, and left alone where it does.
func TestAWatchingRunnerIsAskedForASingleRun(t *testing.T) {
	root := project(t, map[string]string{
		"package.json": `{"scripts": {"test": "vitest"}, "devDependencies": {"vitest": "^3"}}`,
	})
	strategy, _ := ReadingStrategy(root, Discover(root), nil)
	if !strings.Contains(strategy.Command, "vitest run") {
		t.Errorf("a watching runner was asked to watch: %q", strategy.Command)
	}
	root = project(t, map[string]string{
		"package.json": `{"scripts": {"test": "vitest bench"}, "devDependencies": {"vitest": "^3"}}`,
	})
	strategy, _ = ReadingStrategy(root, Discover(root), nil)
	if strings.Contains(strategy.Command, "run") {
		t.Errorf("a subcommand the project chose was overridden: %q", strategy.Command)
	}
}

// PYTEST'S DEFAULT NAMES NOTHING THAT PASSED. Its quiet output is one dot per
// check and its normal output names only the red ones, so a roster read off it
// is empty however green the suite is — which is the empty roster the whole
// acceptance mapping had nothing to map onto. `-rA` is the flag that asks for
// the short summary over every check, in the `PASSED path::name` lines the
// shared vocabulary already reads.
func TestPytestIsAskedForItsWholeRoster(t *testing.T) {
	root := project(t, map[string]string{
		"pyproject.toml":      "[tool.pytest.ini_options]\nasyncio_mode = \"auto\"\n",
		"Makefile":            "test:\n\tpytest --cov=src tests/\n",
		"tests/test_thing.py": "def test_thing():\n    assert True\n",
	})
	strategy, ok := ReadingStrategy(root, Discover(root), nil)
	if !ok {
		t.Fatal("a python project with a test target produced no strategy")
	}
	if strategy.Runner != "pytest" {
		t.Fatalf("pytest under a make target was not found: %#v", strategy)
	}
	if !strings.Contains(strategy.Command, "-rA") {
		t.Errorf("pytest was not asked for the checks that passed: %q", strategy.Command)
	}
	if !strings.Contains(strategy.Command, "--cov=src") {
		t.Errorf("the project's own flags were dropped: %q", strategy.Command)
	}
	// The measured hole, and the flag that closes it. Both readings are of the
	// same forty-seven green checks in textual's own repository, run in the
	// task's own image: pytest's default prints one dot per check and names
	// nothing, and `-rA` names every one of them.
	blind, _, _ := FormatPlain.Read(fixture(t, "pytest-default.txt"))
	if len(blind) != 0 {
		t.Fatalf("pytest's default reading is not the empty one this is a test "+
			"about: %#v", blind)
	}
	reported, failing, ok := FormatPlain.Read(fixture(t, "pytest-rA.txt"))
	if !ok || len(reported) != 47 {
		t.Fatalf("a real pytest roster named %d checks, want 47: %#v", len(reported), reported)
	}
	if !contains(reported, "tests/test_log.py::test_process_line") {
		t.Errorf("a named check is missing from the roster: %#v", reported)
	}
	if len(failing) != 0 {
		t.Errorf("a green reading named red checks: %#v", failing)
	}
}

// The measured hole, stated as the two readings of one real output. vitest's
// default reporter names FILES for the checks that passed and names the red
// ones only in a banner — so the plain reading of a whole green suite is one
// entry that is not a check at all, and the acceptance mapping had 52 stated
// behaviours and that to map them onto.
func TestTheDefaultReporterNamesFilesAndTheJSONReporterNamesChecks(t *testing.T) {
	plain, _, _ := FormatPlain.Read(fixture(t, "vitest-default-reporter.txt"))
	if !contains(plain, "test/index.test.ts (28 tests) 2344ms") {
		t.Fatalf("the default reporter's reading is not the file-level one this "+
			"is a test about: %#v", plain)
	}
	// Twenty-eight checks ran and the default reporter named the FILE they are
	// in. Whatever else it printed — the two it thought slow enough to mention
	// — the roster is three entries where the mapping needs twenty-eight.
	if len(plain) > 3 {
		t.Fatalf("the default reporter named %d things; it names files, not "+
			"checks: %#v", len(plain), plain)
	}
	structured, red, ok := FormatNodeJSON.Read(fixture(t, "vitest-json.json"))
	if !ok {
		t.Fatal("the runner's own machine-readable document was not read at all")
	}
	if len(structured) != 28 {
		t.Fatalf("the machine-readable reading named %d checks and the suite ran "+
			"28: %#v", len(structured), structured)
	}
	if len(red) == 0 {
		t.Error("the machine-readable reading named no failing check, so a red " +
			"suite reads as green")
	}
	for _, name := range structured {
		if strings.HasSuffix(name, ".test.ts") {
			t.Errorf("a file is in the roster where a check should be: %q", name)
		}
	}
}

// The failing half of the same runner, read off the real tail. Five checks were
// red and the vocabulary named none of them, because vitest prints its failures
// in a banner that names the file and the chain rather than in the tick-and-cross
// lines the vocabulary knew.
func TestARunnersFailureBannerNamesItsRedChecks(t *testing.T) {
	// The fixture is the tail the worker itself read — `npx vitest run | tail
	// -60` — and it holds three of the run's five banners. Every banner in it
	// is a named check, and before this pattern existed every one of them was
	// invisible.
	failing := FailingTests(fixture(t, "vitest-default-reporter-failing.txt"))
	if len(failing) != 3 {
		t.Fatalf("a reading of three red banners named %d: %#v", len(failing), failing)
	}
	if !contains(failing, "test/circuit-breaker.test.ts > circuit breaker > counts onRequestError hook exceptions as circuit failures") {
		t.Errorf("the red checks are not named by their own identities: %#v", failing)
	}
}

// go test's own document, and the fail-safe under every strategy: a reader that
// recognises nothing hands the same bytes to the shared vocabulary, so a runner
// nobody here has met is read exactly as well as it was before strategies
// existed. Nothing about the fallback can turn a red suite green.
func TestAStrategyThatRecognisesNothingFallsBackToPlainText(t *testing.T) {
	document := strings.Join([]string{
		`{"Time":"2026-08-29T00:00:00Z","Action":"run","Package":"example/pkg","Test":"TestOne"}`,
		`{"Time":"2026-08-29T00:00:01Z","Action":"pass","Package":"example/pkg","Test":"TestOne"}`,
		`{"Time":"2026-08-29T00:00:02Z","Action":"fail","Package":"example/pkg","Test":"TestTwo"}`,
	}, "\n")
	reported, failing, ok := FormatGoJSON.Read(document)
	if !ok || len(reported) != 2 || len(failing) != 1 {
		t.Fatalf("go test's own document was not read: %#v %#v ok=%v", reported, failing, ok)
	}
	if _, _, ok := FormatGoJSON.Read("--- FAIL: TestOne\nFAIL\texample/pkg\t0.1s\n"); ok {
		t.Fatal("a plain-text reading was decoded as a machine-readable one")
	}
	// And the fall itself, through the whole reader: the strategy says JSON,
	// the runner printed prose, and the roster is still read.
	if names := FailingTests("--- FAIL: TestOne\n"); len(names) != 1 {
		t.Errorf("the shared vocabulary did not read the same bytes: %#v", names)
	}
}

// A project that declares no runner this program has met is read exactly as it
// was: its own command, its own output, the shared vocabulary. A CAPABILITY
// THAT CANNOT WORK IS ABSENT, NOT BROKEN — and here the absence is the old
// behaviour rather than no behaviour.
func TestAProjectWithNoKnownRunnerKeepsItsOwnCommand(t *testing.T) {
	root := project(t, map[string]string{
		"Makefile":          "test:\n\t./run-the-checks.sh\n",
		"run-the-checks.sh": "#!/bin/sh\nexit 0\n",
	})
	strategy, ok := ReadingStrategy(root, Discover(root), nil)
	if !ok {
		t.Fatal("a project with a test target produced no strategy")
	}
	if strategy.Read != FormatPlain {
		t.Errorf("an unrecognised runner was read as something structured: %#v", strategy)
	}
	if strategy.Empty() {
		t.Error("a project that says how it is checked was left with no command")
	}
}

// A tree with nothing to run produces no strategy, which is the same silence
// RunTests answers a project with no test entrypoint with.
func TestATreeThatSaysNothingProducesNoStrategy(t *testing.T) {
	if _, ok := ReadingStrategy(t.TempDir(), Plan{}, nil); ok {
		t.Error("a plan with no test entrypoint produced a strategy to run")
	}
}

// contains says a roster holds a name. It is spelled here rather than reached
// for from the standard library so a failure message can print the roster it
// looked in.
func contains(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

// A THIRD LIFECYCLE SCRIPT, A THIRD RUNNER, THE SAME SHAPE. ink's `npm test` is
// `npm run typecheck && npm run lint && FORCE_COLOR=true ava`, so at the base
// commit it dies in xo on four pre-existing lint errors and names nothing at all
// — 0 of 923 checks — while the runner underneath speaks TAP and names every one
// of them. Measured in ink's own image.
//
// It is also the case that proves the table is a vocabulary of runners rather
// than of ecosystems: ava needed one row and no parser, because TAP was already
// in the shared vocabulary.
func TestARunnerBehindTwoLintGatesIsStillFound(t *testing.T) {
	root := project(t, map[string]string{
		"package.json": `{
  "name": "ink",
  "scripts": {
    "test": "npm run typecheck && npm run lint && FORCE_COLOR=true ava",
    "lint": "xo",
    "typecheck": "tsc --noEmit"
  },
  "devDependencies": {"ava": "^5.1.1", "xo": "^1.2.3"},
  "ava": {"files": ["test/*.tsx"]}
}`,
	})
	strategy, ok := ReadingStrategy(root, Discover(root), nil)
	if !ok {
		t.Fatal("a project that declares a test script produced no strategy")
	}
	if strategy.Runner != "ava" {
		t.Fatalf("the runner behind the lint gates was not found: %#v", strategy)
	}
	if !strings.Contains(strategy.Command, "--tap") {
		t.Errorf("ava was not asked for output that names its checks: %q", strategy.Command)
	}
	if !strings.HasPrefix(strategy.Command, "FORCE_COLOR=true ") {
		t.Errorf("the project's own environment was dropped from the command: %q", strategy.Command)
	}
	for _, gate := range []string{"xo", "tsc", "typecheck"} {
		if strings.Contains(strategy.Command, gate) {
			t.Errorf("the reading still runs %q ahead of the suite: %q", gate, strategy.Command)
		}
	}
	// ink declares no lockfile at all, so the launcher is the one every node
	// install has.
	if !strings.Contains(strategy.Command, "npx ava") {
		t.Errorf("a runner in node_modules was invoked without a launcher: %q", strategy.Command)
	}

	// And the real output that invocation produces, read by the shared
	// vocabulary: every one of the 922 checks it ran, and the 57 that were red.
	reported, failing, _ := FormatPlain.Read(fixture(t, "ava-tap.txt"))
	// ava's own trailer counts 922. The roster is asserted as a FLOOR rather
	// than an equality because a roster is only ever compared with another
	// roster taken the same way: what a name has to be is STABLE between two
	// readings, not identical to the runner's own spelling of it. Four of
	// these names carry a bracket or a ` - ` that normalisation reads
	// differently from ava, which splits one of them in two — visible here,
	// and invisible to every subtraction this roster feeds.
	if len(reported) < 922 {
		t.Fatalf("a real ava roster named %d checks, and ava ran 922", len(reported))
	}
	if len(failing) != 57 {
		t.Errorf("a real ava reading named %d red checks, want 57", len(failing))
	}
	if !contains(reported, "ansi-tokenizer › tokenize plain text") {
		t.Errorf("the checks are not named by their own identities: %#v", reported[:4])
	}
}
