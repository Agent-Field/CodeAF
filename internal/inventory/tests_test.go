package inventory

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
)

func TestATestCommandIsReadFromItsExit(t *testing.T) {
	cases := []struct {
		name, cmd string
		exit      int
		out       string
		want      TestRun
		ok        bool
	}{
		{"go pass", "go test ./...", 0, "ok x", TestRun{Command: "go test ./...", Passed: true}, true},
		{"go fail", "go test ./...", 1, "--- FAIL: A\n--- FAIL: B\nFAIL", TestRun{Command: "go test ./...", Failed: 2}, true},
		{"pytest fail", "cd app && pytest -q", 1, "3 failed, 4 passed", TestRun{Command: "cd app && pytest -q", Failed: 3}, true},
		{"fail without count", "make test", 2, "boom", TestRun{Command: "make test"}, true},
		{"killed", "go test ./...", -1, "", TestRun{}, false},
		{"piped", "go test ./... | tail", 0, "", TestRun{}, false},
		{"followed by a step", "go test ./...; echo done", 0, "", TestRun{}, false},
		{"not a test", "go build ./...", 0, "", TestRun{}, false},
	}
	for _, c := range cases {
		got, ok := testRunOf(c.cmd, c.exit, []byte(c.out))
		if ok != c.ok || got != c.want {
			t.Errorf("%s: got %+v %v, want %+v %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestAShellCallIsReadByItsScript(t *testing.T) {
	if got := scriptOf([]string{"/bin/bash", "-lc", "go test ./..."}); got != "go test ./..." {
		t.Fatalf("scriptOf = %q", got)
	}
	if got := scriptOf([]string{"go", "test", "./..."}); got != "go test ./..." {
		t.Fatalf("scriptOf = %q", got)
	}
}

func TestTheObserverKeepsTheLastTestRunOnly(t *testing.T) {
	r := newRig(t)
	if r.store.Snapshot().Tests != nil {
		t.Fatal("a run was recorded before any call")
	}
	r.obs.Observe(executor.ExecRequest{Argv: []string{"bash", "-c", "go test ./..."}}, executor.ExecResult{Exit: 1, Stdout: []byte("--- FAIL: A\n")})
	r.obs.Observe(executor.ExecRequest{Argv: []string{"ls"}}, executor.ExecResult{})
	if got := r.store.Snapshot().Tests; got == nil || got.Passed || got.Failed != 1 {
		t.Fatalf("tests = %+v", got)
	}
	r.obs.Observe(executor.ExecRequest{Argv: []string{"go", "test"}}, executor.ExecResult{})
	if got := r.store.Snapshot().Tests; got == nil || !got.Passed {
		t.Fatalf("tests = %+v", got)
	}
}
