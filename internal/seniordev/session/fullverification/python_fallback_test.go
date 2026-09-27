//go:build !windows

package fullverification

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPythonUnittestProjectWithoutPytestPassesDiscovery(t *testing.T) {
	pythonWithoutSitePackages(t)
	workspace := t.TempDir()
	writeDiscoveryFile(t, workspace, "calc.py", "def add(a, b):\n    return a + b\n")
	writeDiscoveryFile(t, workspace, "test_calc.py", "import unittest\nfrom calc import add\nclass TestCalc(unittest.TestCase):\n    def test_add(self):\n        self.assertEqual(add(1, 2), 3)\n")
	plan := Discover(workspace)
	if len(plan.Entrypoints) != 1 || plan.Entrypoints[0].Command != "python3 -m unittest discover" {
		t.Fatalf("entrypoints = %#v, want unittest discovery", plan.Entrypoints)
	}
	cmd := exec.Command("sh", "-c", plan.Entrypoints[0].Command)
	cmd.Dir = workspace
	if output, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(output), "OK") {
		t.Fatalf("discovered unittest command: %v\n%s", err, output)
	}
}

func pythonWithoutSitePackages(t *testing.T) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	bin := t.TempDir()
	// -S gives this fixture a real Python interpreter without site packages.
	writeDiscoveryFile(t, bin, "python3", "#!/bin/sh\nexec '"+python+"' -S \"$@\"\n")
	if err := os.Chmod(filepath.Join(bin, "python3"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestDeclaredPytestKeepsPytestWhenUnavailable(t *testing.T) {
	pythonWithoutSitePackages(t)
	for _, declared := range []struct{ path, body string }{
		{"pytest.ini", "[pytest]\n"},
		{"pyproject.toml", "[project]\ndependencies = ['pytest']\n"},
		{"setup.cfg", "[options.extras_require]\ntest = pytest\n"},
		{"tox.ini", "[testenv]\ndeps = pytest\n"},
		{"requirements.txt", "pytest\n"},
		{"requirements/dev.txt", "pytest\n"},
	} {
		t.Run(declared.path, func(t *testing.T) {
			workspace := t.TempDir()
			writeDiscoveryFile(t, workspace, declared.path, declared.body)
			writeDiscoveryFile(t, workspace, "test_calc.py", "def test_add():\n    assert 1 + 2 == 3\n")
			plan := Discover(workspace)
			if len(plan.Entrypoints) != 1 || plan.Entrypoints[0].Command != "python3 -m pytest" {
				t.Fatalf("declared pytest entrypoints = %#v", plan.Entrypoints)
			}
		})
	}
}

func TestImportablePytestKeepsPytest(t *testing.T) {
	pythonWithoutSitePackages(t)
	workspace := t.TempDir()
	writeDiscoveryFile(t, workspace, "pytest.py", "# importable fixture module\n")
	writeDiscoveryFile(t, workspace, "test_calc.py", "def test_add():\n    assert 1 + 2 == 3\n")
	plan := Discover(workspace)
	if len(plan.Entrypoints) != 1 || plan.Entrypoints[0].Command != "python3 -m pytest" {
		t.Fatalf("importable pytest entrypoints = %#v", plan.Entrypoints)
	}
}
