//go:build !windows

package fullverification

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeVenv makes a virtual environment at dir whose python and python3 answer
// with their own name, so a test can tell which interpreter a command reached.
func fakeVenv(t *testing.T, dir string) {
	t.Helper()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pyvenv.cfg"), []byte("home = /usr/bin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"python", "python3"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\necho \"the project's $0\"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// THE PROJECT'S CHECK RUNS IN ITS OWN ENVIRONMENT. A `.venv` in the folder a
// command runs in, else at the top of the project, comes first on PATH, so the
// check's `python3 -m pytest` is the environment's python3 and what was
// installed there is what it uses; a folder that is not a virtual environment
// is not taken for one.
func TestTheChecksCommandsRunInTheProjectsOwnEnvironment(t *testing.T) {
	workspace := t.TempDir()
	if ProjectVenv(workspace, "") != "" || ActivationPreamble(workspace, "") != "" {
		t.Fatal("a project with no environment was given one")
	}
	if err := os.MkdirAll(filepath.Join(workspace, "venv"), 0o755); err != nil {
		t.Fatal(err)
	}
	if ProjectVenv(workspace, "") != "" {
		t.Fatal("a plain folder named venv was taken for an environment")
	}
	fakeVenv(t, filepath.Join(workspace, ".venv"))
	fakeVenv(t, filepath.Join(workspace, "service", ".venv"))
	if got := ProjectVenv(workspace, "service"); got != filepath.Join(workspace, "service", ".venv") {
		t.Fatalf("a command in service/ runs in %q, want its own environment", got)
	}
	if got := ProjectVenv(workspace, "docs"); got != filepath.Join(workspace, ".venv") {
		t.Fatalf("a command in docs/ runs in %q, want the project's", got)
	}
	out, err := exec.Command("bash", "-c", "set -euo pipefail\n"+ActivationPreamble(workspace, "")+"python3").CombinedOutput()
	if err != nil || !strings.Contains(string(out), filepath.Join(workspace, ".venv", "bin", "python3")) {
		t.Fatalf("python3 under the preamble = %q (%v), want the project's", out, err)
	}
	if got := projectPython(workspace); got != filepath.Join(workspace, ".venv", "bin", "python") {
		t.Fatalf("the pytest probe asks %q, want the project's python", got)
	}
}
