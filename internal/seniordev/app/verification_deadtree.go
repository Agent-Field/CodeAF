//go:build !windows

package app

import "strings"

// Dead-tree detection for the unsubmitted-tree finalizer.
//
// A tree that can no longer be built or imported at all fails every test with
// certainty: one collection error aborts a pytest run, one compile error runs
// zero cargo tests. Such a tree is worth strictly less than an earlier
// checkpoint, so the finalizer restores one when the run ends unsubmitted on
// it (solo_finalize.go).
//
// A dead tree is a much stronger claim than a failing one -- degraded trees
// usually still carry most of their value, so the burden of proof stays on
// the restore. The tokens below are therefore SUITE-ABORT markers, not
// failure markers: each one is printed only when the toolchain stopped before
// running the suite. A failing test that merely mentions an ImportError in
// its assertion output matches nothing here.
var deadTreeTokens = []string{
	// cargo: `error: could not compile <crate>` -- zero tests ran.
	"error: could not compile",
	// go test's standard marker when a package fails to build.
	"[build failed]",
	// pytest's abort summary: `!! Interrupted: N error(s) during collection !!`
	// (one import-time raise in a test file kills the whole run).
	" during collection",
	// pytest's per-file marker for the same condition.
	"ERROR collecting",
	// pytest aborts outright when a conftest fails to import.
	"ImportError while loading",
	// esbuild/mocha aborts before running any test when a TypeScript test file
	// cannot be transformed.
	"Exception during run: Error: Transform failed",
}

// suiteDeadOutput reports whether a FAILING verification command's full
// output shows the suite aborted before running (as opposed to running and
// failing).
func suiteDeadOutput(output string) bool {
	if output == "" {
		return false
	}
	for _, token := range deadTreeTokens {
		if strings.Contains(output, token) {
			return true
		}
	}
	return false
}

// safetyRegressionOutput is broader than suiteDeadOutput. Jest can continue
// running unrelated suites after one changed TypeScript file fails to parse,
// so that tree is not globally dead, but it is still not a coherent
// candidate. The finalizer records it and withholds the coherent checkpoint;
// the narrower dead-tree classifier remains the only one allowed to restore
// an unsubmitted workspace to an earlier checkpoint.
func safetyRegressionOutput(output string) bool {
	if suiteDeadOutput(output) {
		return true
	}
	lower := strings.ToLower(output)
	return strings.Contains(lower, "test suite failed to run") &&
		(strings.Contains(lower, "syntaxerror") ||
			strings.Contains(lower, "unexpected token") ||
			strings.Contains(lower, "transform failed"))
}

// verificationShowsDeadTree scans a completed verification's command
// evidence for a failure that (a) is this run's own doing -- not a timeout --
// and (b) carries a suite-abort signature. It returns the first such
// command.
func verificationShowsDeadTree(result projectVerificationResult) (string, bool) {
	for _, command := range result.Commands {
		evidence, ok := command.(map[string]any)
		if !ok {
			continue
		}
		exit, _ := evidence["exit"].(float64)
		timedOut, _ := evidence["timedOut"].(bool)
		dead, _ := evidence["suite_dead"].(bool)
		if exit != 0 && !timedOut && dead {
			cmd, _ := evidence["cmd"].(string)
			return cmd, true
		}
	}
	return "", false
}

func verificationShowsSafetyRegression(result projectVerificationResult) (string, bool) {
	for _, command := range result.Commands {
		evidence, ok := command.(map[string]any)
		if !ok {
			continue
		}
		exit, _ := evidence["exit"].(float64)
		timedOut, _ := evidence["timedOut"].(bool)
		unsafe, _ := evidence["safety_regression"].(bool)
		dead, _ := evidence["suite_dead"].(bool)
		if exit != 0 && !timedOut && (unsafe || dead) {
			cmd, _ := evidence["cmd"].(string)
			return cmd, true
		}
	}
	return "", false
}
