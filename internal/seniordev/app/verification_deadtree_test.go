//go:build !windows

package app

import "testing"

func deadCommandEvidence(cmd string, extra map[string]any) map[string]any {
	evidence := map[string]any{"cmd": cmd, "exit": float64(1), "suite_dead": true}
	for key, value := range extra {
		evidence[key] = value
	}
	return evidence
}

// The fixtures follow real toolchain output: each dead sample is the shape a
// build tool or test runner prints when it stops before running the suite,
// and each alive sample is a suite that ran and failed.
func TestSuiteDeadOutputClassifier(t *testing.T) {
	dead := []string{
		// cargo build: a compile error, zero tests ran.
		"error[E0063]: missing fields `alpha`, `beta`, `gamma` and 5 other fields\nerror: could not compile `widget` (bin \"widget\") due to 1 previous error",
		// pytest: one import-time raise killed collection of the whole suite.
		"!!!!!!!! Interrupted: 1 error during collection !!!!!!!!\n= 1 error in 1.73s =",
		"ERROR collecting tests/unit/test_models.py",
		"ImportError while loading conftest '/repo/tests/conftest.py'.",
		"FAIL\tgithub.com/example/pkg [build failed]",
		// esbuild aborted before mocha ran a test.
		"Exception during run: Error: Transform failed with 1 error:\nref.test.js:12:9: ERROR: Expected \")\" but found \":\"",
	}
	for i, output := range dead {
		if !suiteDeadOutput(output) {
			t.Fatalf("dead fixture %d not classified:\n%s", i, output)
		}
	}
	alive := []string{
		"",
		// a suite that RAN and failed -- degraded, not dead
		"FAILED tests/test_docs.py::test_commands_are_documented\n= 3 failed, 240 passed in 41.02s =",
		// a failing test that merely mentions an import error in its output
		"E           ImportError: optional dependency 'foo' is not installed\n= 1 failed, 99 passed =",
		// an assertion failed after many tests ran.
		"TypeCheckError: Type 'Widget' does not satisfy constraint\n= 1 failed, 126 passed =",
		// network failures are not suite aborts
		"npm error 403 403 Forbidden - GET https://registry.npmjs.org/some-package",
		"go: downloading github.com/example/migrate v1.0.0",
	}
	for i, output := range alive {
		if suiteDeadOutput(output) {
			t.Fatalf("alive fixture %d wrongly classified dead:\n%s", i, output)
		}
	}
}

func TestVerificationShowsDeadTreeExcusesFailures(t *testing.T) {
	excused := projectVerificationResult{Commands: []any{
		deadCommandEvidence("make test", map[string]any{"timedOut": true}),
		map[string]any{"cmd": "cargo build", "exit": float64(0), "suite_dead": true},
	}}
	if cmd, dead := verificationShowsDeadTree(excused); dead {
		t.Fatalf("excused failures classified the tree dead via %q", cmd)
	}
	genuine := projectVerificationResult{Commands: []any{
		map[string]any{"cmd": "go vet", "exit": float64(0)},
		deadCommandEvidence("cargo build", nil),
	}}
	cmd, dead := verificationShowsDeadTree(genuine)
	if !dead || cmd != "cargo build" {
		t.Fatalf("genuine suite-abort not detected (cmd=%q dead=%v)", cmd, dead)
	}
}

func TestSafetyRegressionRecognizesJestParseAbortWithoutCallingWholeTreeDead(t *testing.T) {
	output := "FAIL tests/feature.test.js\nTest suite failed to run\n" +
		"SyntaxError: Jest encountered an unexpected token\n590 passed"
	if suiteDeadOutput(output) {
		t.Fatal("a suite-local Jest parse failure was classified as a globally dead tree")
	}
	if !safetyRegressionOutput(output) {
		t.Fatal("the Jest parse failure was not classified as a safety regression")
	}
	result := projectVerificationResult{Commands: []any{map[string]any{
		"cmd": "npm test", "exit": float64(1), "safety_regression": true,
	}}}
	if command, unsafe := verificationShowsSafetyRegression(result); !unsafe || command != "npm test" {
		t.Fatalf("safety regression = (%q, %v), want npm test, true", command, unsafe)
	}
}

func TestRememberVerifiedTreeRetainsTheVerdictForFinalization(t *testing.T) {
	runner := gitTestRepo(t)
	runner.rememberVerifiedTree(verificationWith(1))
	if runner.lastVerify == nil {
		t.Fatal("verification was not retained for finalization")
	}
}
