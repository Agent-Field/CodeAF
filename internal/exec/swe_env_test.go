package exec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The engine verifies with the commands it discovers, spelled bare. A cell
// that carries its own venv must have that venv answer for `pytest`, or the
// verification judges the machine's Python and not the project — the smoke
// run failed four audit cycles in a row on a jiwer import the venv had and
// the system interpreter did not.
func TestChildPathLeadsWithTheProjectVenv(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, ".venv", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	worker := NewSWE(nil, "vendor/model", "key", "", 0)
	path := childPath(t, worker.environ(dir))
	prefix := bin + string(os.PathListSeparator)
	if !strings.HasPrefix(path, prefix) {
		t.Fatalf("child PATH does not lead with the project venv:\n%s", path)
	}
}

// A workspace without a venv changes nothing: the child inherits PATH as-is.
func TestChildPathUntouchedWithoutAVenv(t *testing.T) {
	worker := NewSWE(nil, "vendor/model", "key", "", 0)
	if got, want := childPath(t, worker.environ(t.TempDir())), os.Getenv("PATH"); got != want {
		t.Fatalf("child PATH rewritten with no venv present:\ngot  %s\nwant %s", got, want)
	}
}

func childPath(t *testing.T, environ []string) string {
	t.Helper()
	for _, entry := range environ {
		if value, ok := strings.CutPrefix(entry, "PATH="); ok {
			return value
		}
	}
	t.Fatal("child environment carries no PATH")
	return ""
}

// The flight recorder never appears in the deliverable's diff. The auditor
// proved the need: it refused a change set whose only addition was a
// 47,245-line trace of the run that produced it.
func TestObsIsExcludedFromTheWorkspaceRepository(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	excludeFromGit(dir, obsDir+"/")
	excludeFromGit(dir, obsDir+"/") // twice writes once
	raw, err := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Count(string(raw), obsDir+"/"), 1; got != want {
		t.Fatalf("exclude carries the pattern %d times, want %d:\n%s", got, want, raw)
	}
}
