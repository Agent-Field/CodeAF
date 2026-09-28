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

func TestCorrectUnittestFixPassesWithoutPytest(t *testing.T) {
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
		"calc.py":      "def add(a, b):\n    return a + b\n",
		"test_calc.py": "import unittest\nfrom calc import add\nclass TestCalc(unittest.TestCase):\n    def test_add(self):\n        self.assertEqual(add(1, 2), 3)\n",
	})
	result := runner.runProjectVerification(context.Background())
	if result.Failed != nil || result.NewFailures != 0 || !strings.Contains(result.Prompt, "python3 -m unittest discover") {
		t.Fatalf("correct unittest fix failed verification: %#v", result)
	}
}
