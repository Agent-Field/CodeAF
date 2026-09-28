//go:build !windows

package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A tests/ folder without __init__.py was invisible to plain unittest
// discovery. The fallback must run its test and record the selected command.
func TestUnittestFallbackRunsTestsFolderWithoutPackageMarker(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "python3"), []byte("#!/bin/sh\nexec '"+python+"' -S \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	runner := verificationWorkspace(t, map[string]string{
		"store.py":            "def value():\n    return 8\n",
		"tests/test_store.py": "import unittest\nfrom store import value\nclass StoreTest(unittest.TestCase):\n    def test_value(self):\n        self.assertEqual(value(), 8)\n",
	})
	result := runner.runProjectVerification(context.Background())
	if result.Failed != nil || !strings.Contains(result.Prompt, "Ran 1 test") || !strings.Contains(result.Prompt, "discover -s tests") {
		t.Fatalf("unittest folder was not verified: %+v", result)
	}
}

// A declared command that exits zero after discovering no tests cannot turn
// the submitted candidate into a verified pass.
func TestZeroTestCommandEndsSubmittedRunUnchecked(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	runner, state, _, _ := soloPipeline(t)
	if err := writeFile(filepath.Join(runner.workspace, "tests", "test_store.py"), "import unittest\nclass StoreTest(unittest.TestCase):\n    def test_value(self): self.assertTrue(True)\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(runner.workspace, "Makefile"), "test:\n\t@python3 -m unittest discover\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(runner.workspace, "feature.txt"), "candidate\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.soloFreezeWithContext(context.Background(), state, soloSubmission("done")); err != nil {
		t.Fatal(err)
	}
	outcome := &soloOutcome{}
	runner.soloShip(context.Background(), state, outcome, nil)
	if outcome.Status != "pass-unverified" || !strings.Contains(outcome.TerminalReason, "no tests were found by `make test`") {
		t.Fatalf("zero-test command ended as %q: %q", outcome.Status, outcome.TerminalReason)
	}
}
