package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/session/contract"
)

// Validation contract for the base-contract check, derived from werkzeug-3146.
//
// The check exists to answer one question: does the acceptance contract fail at
// the parent commit, i.e. is the reported bug real? If it answers "no", the
// harness declares the issue stale and re-scopes the run to "report + regression
// test, no behavioral change" — it stops trying to fix the bug. So a false
// "passes at base" silently discards the entire point of the run.
//
// An editable install (`pip install -e .`, the standard dev setup) puts the MAIN
// checkout's absolute source path on sys.path. Running the contract with cwd in
// a detached base worktree does not undo that, so the import resolves to the
// already-patched source and the contract passes at "base".
//
//   - the base worktree's sources must win over an editable install;
//   - the pin must survive a compound shell command;
//   - a pre-existing PYTHONPATH must still be honoured, just at lower priority;
//   - non-Python projects must be left alone.
func TestPinContractToWorktreeSources(t *testing.T) {
	pythonWorktree := func(t *testing.T, withSrc bool) string {
		t.Helper()
		worktree := t.TempDir()
		if err := writeFile(filepath.Join(worktree, "pyproject.toml"),
			"[project]\nname = \"demo\"\n"); err != nil {
			t.Fatal(err)
		}
		if withSrc {
			if err := os.MkdirAll(filepath.Join(worktree, "src"), 0o777); err != nil {
				t.Fatal(err)
			}
		}
		return worktree
	}

	t.Run("src layout is pinned ahead of site-packages", func(t *testing.T) {
		worktree := pythonWorktree(t, true)
		got := pinContractToWorktreeSources(worktree, "python -m pytest tests/test_x.py")
		if !strings.Contains(got, filepath.Join(worktree, "src")) {
			t.Errorf("src root not pinned: %q", got)
		}
		if !strings.HasSuffix(got, "python -m pytest tests/test_x.py") {
			t.Errorf("original command not preserved: %q", got)
		}
		if !strings.Contains(got, "${PYTHONPATH:+:$PYTHONPATH}") {
			t.Errorf("pre-existing PYTHONPATH dropped rather than demoted: %q", got)
		}
	})

	t.Run("flat layout pins the worktree root", func(t *testing.T) {
		worktree := pythonWorktree(t, false)
		got := pinContractToWorktreeSources(worktree, "pytest -q")
		if !strings.Contains(got, worktree) {
			t.Errorf("worktree root not pinned: %q", got)
		}
	})

	t.Run("non-Python projects are untouched", func(t *testing.T) {
		worktree := t.TempDir()
		if err := writeFile(filepath.Join(worktree, "go.mod"), "module demo\n"); err != nil {
			t.Fatal(err)
		}
		if got := pinContractToWorktreeSources(worktree, "go test ./..."); got != "go test ./..." {
			t.Errorf("command modified for a Go project: %q", got)
		}
	})

	t.Run("the pin actually changes which module python imports", func(t *testing.T) {
		// End-to-end: stand up the exact trap. A "site-packages" copy holding a
		// PATCHED module is put on sys.path via PYTHONPATH (standing in for the
		// .pth an editable install writes), and the base worktree holds the
		// ORIGINAL. Without the pin the contract reads the patched copy and
		// wrongly passes; with it, it reads the original and correctly fails.
		python, err := exec.LookPath("python3")
		if err != nil {
			t.Skip("python3 unavailable")
		}
		installed := t.TempDir()
		if err := writeFile(filepath.Join(installed, "demo.py"), "VALUE = 'patched'\n"); err != nil {
			t.Fatal(err)
		}
		// src layout, as werkzeug uses: the module is NOT in the worktree root,
		// so cwd does not shadow the installed copy and the trap is faithful.
		worktree := pythonWorktree(t, true)
		if err := writeFile(filepath.Join(worktree, "src", "demo.py"), "VALUE = 'base'\n"); err != nil {
			t.Fatal(err)
		}

		// The contract: "assert the bug is fixed". It must FAIL at base.
		command := python + ` -c "import demo; assert demo.VALUE == 'patched', demo.VALUE"`

		run := func(cmd string) error {
			execution := exec.Command("bash", "-lc", cmd)
			execution.Dir = worktree
			execution.Env = append(os.Environ(), "PYTHONPATH="+installed)
			return execution.Run()
		}

		if err := run(command); err != nil {
			t.Fatalf("precondition: unpinned run should have read the patched copy and passed: %v", err)
		}
		if err := run(pinContractToWorktreeSources(worktree, command)); err == nil {
			t.Error("pinned run still read the patched copy — a real bug would be declared stale " +
				"and the run re-scoped to report-only")
		}
	})
}

// The issue-#22 regression, end to end through runBaseContractCheck.
//
// Goal: "create hello.txt containing 'hello engine'". The coder writes
// test-hello.sh (which reads hello.txt) and hello.txt itself. The base commit
// holds only README.md, so the contract MUST fail at base — the file genuinely
// is not there yet.
//
// Before the fix, the harness copied every registered path into the base
// worktree, hello.txt included, so the contract passed at base, the run was
// declared stale and re-scoped to "report + regression test", and it deadlocked
// on done-criteria against a deliverable it had cancelled.
//
// Declaring the deliverable in asserted_paths withholds it, so the base check
// answers honestly. The overlap case covers a coder that lists it in both.
func TestBaseContractCheckFailsWhenDeliverableIsAsserted(t *testing.T) {
	for _, test := range []struct {
		name          string
		paths         []string
		assertedPaths []string
		wantPass      bool
		wantNote      string
	}{
		{
			name:          "deliverable asserted",
			paths:         []string{"test-hello.sh"},
			assertedPaths: []string{"hello.txt"},
			wantPass:      false,
		},
		{
			name:          "deliverable listed in both arrays fails safe",
			paths:         []string{"test-hello.sh", "hello.txt"},
			assertedPaths: []string{"hello.txt"},
			wantPass:      false,
			wantNote:      "NOT copying hello.txt into the base worktree",
		},
		{
			// The pre-fix behavior, kept visible: an undeclared deliverable is
			// still copied and the base check still passes for the wrong
			// reason. This is what layer 3's copy provenance exists to catch.
			name:          "undeclared deliverable still contaminates",
			paths:         []string{"test-hello.sh", "hello.txt"},
			assertedPaths: nil,
			wantPass:      true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := baseContractRepo(t)
			var notes strings.Builder
			runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
				Events: newEventWriter(io.Discard), Notes: &notes,
			})
			defer runner.runtime.Close()

			baseSHA := gitOutput(context.Background(), workspace, "rev-parse", "HEAD")
			if baseSHA == "" {
				t.Fatal("no base commit")
			}
			// The coder's work, uncommitted in the live tree.
			if err := writeFile(filepath.Join(workspace, "hello.txt"), "hello engine"); err != nil {
				t.Fatal(err)
			}
			if err := writeFile(filepath.Join(workspace, "test-hello.sh"),
				"#!/usr/bin/env bash\ntest \"$(cat hello.txt)\" = \"hello engine\"\n"); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Join(workspace, "test-hello.sh"), 0o755); err != nil {
				t.Fatal(err)
			}

			result, copies := runner.runBaseContractCheck(
				context.Background(), baseSHA,
				contract.Contract{
					Command:       "./test-hello.sh",
					Paths:         test.paths,
					AssertedPaths: test.assertedPaths,
				},
			)
			if result == nil {
				t.Fatal("base contract check folded to nil")
			}
			if result.Pass != test.wantPass {
				t.Fatalf("base contract pass = %v, want %v (tail: %s)",
					result.Pass, test.wantPass, result.TailOutput)
			}
			if test.wantNote != "" && !strings.Contains(notes.String(), test.wantNote) {
				t.Errorf("notes missing %q:\n%s", test.wantNote, notes.String())
			}
			// Copy provenance: test-hello.sh never existed at base, and it is
			// reported so the staleness judge can weigh it.
			for _, copied := range copies {
				if copied.ExistedAtBase {
					t.Errorf("copy %q reported as existing at base; base holds only README.md", copied.Path)
				}
			}
			if len(copies) == 0 {
				t.Error("expected the scaffolding copy to be reported")
			}
		})
	}
}

func baseContractRepo(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.name", "codeaf-base"},
		{"config", "user.email", "codeaf@example.test"},
	} {
		if err := gitRun(workspace, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeFile(filepath.Join(workspace, "README.md"), "# demo\n"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "add", "README.md"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "commit", "-m", "seed"); err != nil {
		t.Fatal(err)
	}
	return workspace
}
