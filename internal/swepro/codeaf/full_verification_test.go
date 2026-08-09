package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/engine/steploop"
	"github.com/Agent-Field/swe-pro-go/internal/session/auditorgate"
	"github.com/Agent-Field/swe-pro-go/internal/session/ledgers"
)

func TestGreenHarnessClearsExhaustedNotVerifiedTemplate(t *testing.T) {
	// Round 4 contracts C1-C3: after the bounded retries from
	// auditor-gate.ts:2071-2076 are exhausted, the pipeline must reconcile the
	// exact runH absence blocker against this cycle's process-derived evidence.
	t.Setenv("CODEAF_AUDITOR", "1")
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")
	t.Setenv("CODEAF_ADMISSIBILITY", "0")
	t.Setenv("CODEAF_AUDITOR_MAX_ATTEMPTS", "2")
	t.Setenv("CODEAF_TAMPER", "0")
	t.Setenv("CODEAF_SPEC_IDS", "0")
	workspace, base := guardWorkspace(t)
	if err := writeFile(filepath.Join(workspace, "go.mod"), "module example.test/runh\n\ngo 1.23\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "green.go"), "package runh\n"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "add", "go.mod", "green.go"); err != nil {
		t.Fatal(err)
	}

	auditorCalls := 0
	runner := newPipeline(cliArgs{High: "provider/high"}, workspace, pipelineDeps{
		Backend: backendFunc(func(_ context.Context, request turn) (turnResult, error) {
			if request.Agent != "auditor" && request.Agent != "auditor-light" {
				return turnResult{}, nil
			}
			auditorCalls++
			body := `{"verdict":"fail","step2_signal":{"reproduced":false,"commands":[{"cmd":"go build ./...","exit":0,"kind":"build","source":"go.mod"},{"cmd":"go test ./...","exit":0,"kind":"test","source":"go.mod"}],"spec_examples_matched":"n/a","notes":"Not yet run"},"step3_scope":{},"step4_structural":{"shape_matches_spec":false},"blockers":[{"file":"unknown","line":0,"step":2,"detail":"Build and tests not yet verified independently"}],"repair_hints":[]}`
			path := filepath.Join(request.Workspace, ".codeaf", "auditor-verdict.json")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return turnResult{}, err
			}
			return turnResult{Text: "unfinished audit"}, os.WriteFile(path, []byte(body), 0o600)
		}),
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	defer runner.runtime.Close()
	audit, _, err := runner.auditFixLoop(context.Background(), "Verify the green Go project.", base, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if auditorCalls != 2 {
		t.Fatalf("auditor calls = %d, want 2", auditorCalls)
	}
	if audit.Status != auditorgate.StatusPass || audit.Verdict == nil ||
		audit.Verdict.Verdict != auditorgate.VerdictPass || len(audit.Verdict.Blockers) != 0 {
		t.Fatalf("reconciled audit = %#v", audit)
	}
	persisted, readErr := auditorgate.ReadVerdictFile(workspace)
	if readErr != nil || persisted == nil || persisted.Verdict != auditorgate.VerdictPass {
		t.Fatalf("persisted verdict = %#v, err=%v", persisted, readErr)
	}
	provenance := auditorgate.ReadAuditProvenance(workspace)
	if provenance == nil || provenance.Verdict.Verdict != auditorgate.VerdictPass {
		t.Fatalf("persisted provenance = %#v", provenance)
	}
	if open := ledgers.LoadOpenBlockers(workspace); len(open) != 0 {
		t.Fatalf("open blockers = %#v", open)
	}
}

func TestRunFShapedFailingPackageSuiteCannotPass(t *testing.T) {
	// Validation contracts 1 and 4: reproduce runF's exact defect shape. The
	// package test runs from cmd/exprcalc and incorrectly asks Go to build
	// ./cmd/exprcalc, so the full suite must block pass with executed evidence.
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
	t.Setenv("CODEAF_AUTORESUME", "0")
	t.Setenv("CODEAF_FRONTIER", "0")
	t.Setenv("CODEAF_VALIDITY", "0")
	t.Setenv("CODEAF_TAMPER", "0")
	t.Setenv("CODEAF_SPEC_IDS", "0")

	workspace, _ := guardWorkspace(t)
	if err := writeFile(filepath.Join(workspace, "go.mod"), "module example.test/runf\n\ngo 1.23\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "cmd", "exprcalc", "main.go"), "package main\n\nfunc main() {}\n"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "add", "go.mod", "cmd/exprcalc/main.go"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "commit", "-m", "expression calculator"); err != nil {
		t.Fatal(err)
	}
	base := strings.TrimSpace(gitOutput(context.Background(), workspace, "rev-parse", "HEAD"))
	testSource := `package main

import (
	"os/exec"
	"testing"
)

func TestBuildExprcalc(t *testing.T) {
	command := exec.Command("go", "build", "./cmd/exprcalc")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("failed to build cmd/exprcalc: %v\n%s", err, output)
	}
}
`
	if err := writeFile(filepath.Join(workspace, "cmd", "exprcalc", "exprcalc_test.go"), testSource); err != nil {
		t.Fatal(err)
	}

	backendCalls := 0
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Backend: backendFunc(func(context.Context, turn) (turnResult, error) {
			backendCalls++
			return turnResult{Text: "unexpected auditor call"}, nil
		}),
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	defer runner.runtime.Close()
	audit, _, err := runner.auditFixLoop(
		context.Background(), "Add and verify the exprcalc package regression test.",
		base, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if audit.Status != auditorgate.StatusFail || audit.Verdict == nil {
		t.Fatalf("red suite result = %#v, want fail", audit)
	}
	if backendCalls != 0 {
		t.Fatalf("backend calls = %d, failing machine verification should skip the audit LLM", backendCalls)
	}
	if len(audit.Verdict.Blockers) != 1 ||
		!strings.Contains(audit.Verdict.Blockers[0].Detail, "project test verification failed") ||
		!strings.Contains(audit.Verdict.Blockers[0].Detail, "go test ./...") ||
		!strings.Contains(audit.Verdict.Blockers[0].Detail, "failed to build cmd/exprcalc") {
		t.Fatalf("verification blocker = %#v", audit.Verdict.Blockers)
	}
	if audit.Verdict.Step2Signal == nil || len(audit.Verdict.Step2Signal.Commands) != 2 {
		t.Fatalf("verification commands = %#v", audit.Verdict.Step2Signal)
	}
	commands := audit.Verdict.Step2Signal.Commands
	if auditorgate.CommandName(commands[0]) != "go build ./..." ||
		auditorgate.CommandExit(commands[0]) != "0" ||
		auditorgate.CommandName(commands[1]) != "go test ./..." ||
		auditorgate.CommandExit(commands[1]) == "0" {
		t.Fatalf("verification evidence = %#v", commands)
	}
	persisted, readErr := auditorgate.ReadVerdictFile(workspace)
	if readErr != nil || persisted == nil || persisted.Verdict != auditorgate.VerdictFail ||
		persisted.Step2Signal == nil || len(persisted.Step2Signal.Commands) != 2 {
		t.Fatalf("persisted verification evidence = %#v, %v", persisted, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, "cmd", "exprcalc", "exprcalc_test.go")); statErr != nil {
		t.Fatal(statErr)
	}
}

func TestAuditorKillSwitchCannotBypassRedProjectVerification(t *testing.T) {
	// Validation contract 1: disabling the model auditor is not authority to
	// turn a process-proven red project suite into status=pass.
	t.Setenv("CODEAF_AUDITOR", "0")
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")
	workspace := t.TempDir()
	if err := writeFile(filepath.Join(workspace, "go.mod"), "module example.test/red\n\ngo 1.23\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "red.go"), "package red\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "red_test.go"), `package red

import "testing"

func TestRed(t *testing.T) { t.Fatal("still red") }
`); err != nil {
		t.Fatal(err)
	}
	var events bytes.Buffer
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Events: newEventWriter(&events), Notes: io.Discard,
	})
	defer runner.runtime.Close()
	audit, _, err := runner.auditFixLoop(context.Background(), "verify", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if audit.Status != auditorgate.StatusFail || audit.Verdict == nil ||
		len(audit.Verdict.Blockers) != 1 ||
		!strings.Contains(audit.Verdict.Blockers[0].Detail, "go test ./...") {
		t.Fatalf("auditor-disabled red suite = %#v", audit)
	}
	if audit.Reason == nil || !strings.Contains(*audit.Reason, "test") ||
		!strings.Contains(*audit.Reason, "go test ./...") {
		t.Fatalf("terminal verification reason = %#v", audit.Reason)
	}
	if !strings.Contains(events.String(), `"stage":"verification"`) ||
		!strings.Contains(events.String(), `"status":"fail"`) ||
		!strings.Contains(events.String(), `go test ./...`) {
		t.Fatalf("verification NDJSON event = %s", events.String())
	}
}

func TestDetectedBuildWithoutTestEntrypointCannotPass(t *testing.T) {
	// Validation contracts C3.1 and C5.4: detecting only a green build is
	// insufficient, and an interactive script is not a usable test entrypoint.
	t.Setenv("CODEAF_AUDITOR", "0")
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")
	workspace := t.TempDir()
	if err := writeFile(filepath.Join(workspace, "package.json"), `{"scripts":{"test":"vitest --watch"}}`); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "Makefile"), "build:\n\tprintf built > build.marker\n"); err != nil {
		t.Fatal(err)
	}
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	defer runner.runtime.Close()
	audit, _, err := runner.auditFixLoop(context.Background(), "verify", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if audit.Status != auditorgate.StatusFail || audit.Verdict == nil ||
		len(audit.Verdict.Blockers) != 1 ||
		!strings.Contains(audit.Verdict.Blockers[0].Detail, "no standard test entrypoint") {
		t.Fatalf("build-only project = %#v", audit)
	}
	if body, err := os.ReadFile(filepath.Join(workspace, "build.marker")); err != nil || string(body) != "built" {
		t.Fatalf("real build fixture did not execute: body=%q err=%v", body, err)
	}
}

func TestUndiscoveredBuildAndTestCannotPassWithAuditorDisabled(t *testing.T) {
	// Validation contracts C3.1, C3.3, and C3.5: disabling the model does not
	// disable the process floor, and an empty plan names both missing roles.
	//
	// The fixture is a TypeScript project with nothing runnable in it: a
	// manifest makes the workspace accountable, and neither role resolves to a
	// command, which is the state this contract is about. A workspace with no
	// manifest at all is a different case entirely — see
	// TestBareWorkspacePassesVacuouslyInsteadOfDemandingTheImpossible.
	t.Setenv("CODEAF_AUDITOR", "0")
	workspace := t.TempDir()
	if err := writeFile(filepath.Join(workspace, "tsconfig.json"), `{"compilerOptions":{"strict":true}}`); err != nil {
		t.Fatal(err)
	}
	var events bytes.Buffer
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Events: newEventWriter(&events), Notes: io.Discard,
	})
	defer runner.runtime.Close()
	audit, _, err := runner.auditFixLoop(context.Background(), "verify", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if audit.Status != auditorgate.StatusFail || audit.Reason == nil ||
		!strings.Contains(*audit.Reason, "build/typecheck") ||
		!strings.Contains(*audit.Reason, "test entrypoint") {
		t.Fatalf("empty verification plan = %#v", audit)
	}
	if !strings.Contains(events.String(), `"stage":"verification"`) ||
		!strings.Contains(events.String(), `build/typecheck`) ||
		!strings.Contains(events.String(), `test entrypoint`) {
		t.Fatalf("empty-plan event = %s", events.String())
	}
}

func TestGreenTestWithoutBuildEntrypointCannotPass(t *testing.T) {
	// Validation contract C3.2: a green test process in an otherwise
	// unrecognized repository cannot substitute for a missing build/typecheck.
	// Keep the fixture outside Python's conventional test layouts so this does
	// not contradict the F4 plain-Python recognition contract.
	t.Setenv("CODEAF_AUDITOR", "0")
	workspace := t.TempDir()
	if err := writeFile(filepath.Join(workspace, "checks", "check_green.py"), `import unittest

class GreenCheck(unittest.TestCase):
    def test_green(self):
        self.assertTrue(True)
`); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "AGENTS.md"), "Run `python3 -m unittest discover -s checks -p 'check_*.py'`.\n"); err != nil {
		t.Fatal(err)
	}
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	defer runner.runtime.Close()
	audit, _, err := runner.auditFixLoop(context.Background(), "verify", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if audit.Status != auditorgate.StatusFail || audit.Verdict == nil ||
		len(audit.Verdict.Blockers) != 1 ||
		!strings.Contains(audit.Verdict.Blockers[0].Detail, "build/typecheck") {
		t.Fatalf("test-only project = %#v", audit)
	}
	if audit.Verdict.Step2Signal == nil || len(audit.Verdict.Step2Signal.Commands) != 1 ||
		auditorgate.CommandExit(audit.Verdict.Step2Signal.Commands[0]) != "0" {
		t.Fatalf("test-only process evidence = %#v", audit.Verdict.Step2Signal)
	}
}

// TestBareWorkspacePassesVacuouslyInsteadOfDemandingTheImpossible is the
// regression for the runaway repair loop. A `git init` plus a README has
// nothing to compile and nothing to test, so both "no entrypoint was
// discoverable" failures were unrepairable by construction: the audit-fix loop
// reran, the auto-resume supervisor re-entered, and the run burned its cost
// ceiling with the requested file already written. Verification now passes and
// says plainly that it proved nothing, so the audit stage is not silently
// blind to the fact that the acceptance contract is carrying the whole gate.
func TestBareWorkspacePassesVacuouslyInsteadOfDemandingTheImpossible(t *testing.T) {
	t.Setenv("CODEAF_AUDITOR", "0")
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")
	workspace := t.TempDir()
	if err := writeFile(filepath.Join(workspace, "README.md"), "# demo\n"); err != nil {
		t.Fatal(err)
	}
	var events bytes.Buffer
	var notes bytes.Buffer
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Events: newEventWriter(&events), Notes: &notes,
	})
	defer runner.runtime.Close()

	verification := runner.runProjectVerification(context.Background())
	if verification.Failed != nil || verification.Failure != "" {
		t.Fatalf("bare workspace verification = %#v, want no failure", verification)
	}
	if !strings.Contains(verification.Prompt, "VACUOUS") ||
		!strings.Contains(verification.Prompt, "acceptance contract is the effective gate") {
		t.Fatalf("bare workspace prompt must declare itself vacuous, got:\n%s", verification.Prompt)
	}
	if !strings.Contains(notes.String(), "found nothing to run") {
		t.Fatalf("bare workspace note = %q", notes.String())
	}
	if !strings.Contains(events.String(), `"vacuous":true`) ||
		!strings.Contains(events.String(), `"status":"pass"`) {
		t.Fatalf("bare workspace event = %s", events.String())
	}
	// The failure strings that drove the loop must not appear at all.
	for _, banned := range []string{
		"no standard build/typecheck entrypoint was discoverable",
		"no standard test entrypoint was discoverable",
	} {
		if strings.Contains(verification.Prompt, banned) || strings.Contains(events.String(), banned) {
			t.Errorf("bare workspace still reports %q", banned)
		}
	}

	audit, _, err := runner.auditFixLoop(context.Background(), "verify", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if audit.Status != auditorgate.StatusPass {
		t.Fatalf("bare workspace audit = %#v, want pass", audit)
	}
}

func TestAuditorDisabledGreenBuildAndTestPass(t *testing.T) {
	// Validation contract C3.4: the model-auditor switch still permits a pass
	// when both independently executed process entrypoints are green.
	t.Setenv("CODEAF_AUDITOR", "0")
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")
	workspace := t.TempDir()
	if err := writeFile(filepath.Join(workspace, "go.mod"), "module example.test/green\n\ngo 1.23\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "green.go"), "package green\n"); err != nil {
		t.Fatal(err)
	}
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	defer runner.runtime.Close()
	audit, _, err := runner.auditFixLoop(context.Background(), "verify", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if audit.Status != auditorgate.StatusPass || audit.Verdict == nil ||
		audit.Verdict.Verdict != auditorgate.VerdictPass {
		t.Fatalf("green process verification = %#v", audit)
	}
}

func TestShellControlFlowCannotManufactureGreenVerification(t *testing.T) {
	// F1.1-F1.4: unsafe candidates fall through to a real ecosystem test, and
	// the allowed tee form is guarded by the harness's pipefail execution.
	t.Setenv("CODEAF_AUDITOR", "0")
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")
	for _, command := range []string{
		"go test ./... || true",
		"true || go test ./...",
		"exit 0; go test ./...",
		"true; go test ./...",
		"go test ./... | cat",
		"go test ./... 2>&1 | tee test.log",
	} {
		t.Run(command, func(t *testing.T) {
			workspace := t.TempDir()
			if err := writeFile(filepath.Join(workspace, "go.mod"), "module example.test/red\n\ngo 1.23\n"); err != nil {
				t.Fatal(err)
			}
			if err := writeFile(filepath.Join(workspace, "red_test.go"), "package red\n\nimport \"testing\"\n\nfunc TestRed(t *testing.T) { t.Fatal(\"red\") }\n"); err != nil {
				t.Fatal(err)
			}
			if err := writeFile(filepath.Join(workspace, "AGENTS.md"), "Run `go build ./...` and `"+command+"`.\n"); err != nil {
				t.Fatal(err)
			}
			runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
				Events: newEventWriter(io.Discard), Notes: io.Discard,
			})
			defer runner.runtime.Close()
			audit, _, err := runner.auditFixLoop(context.Background(), "verify", "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if audit.Status == auditorgate.StatusPass {
				t.Fatalf("control-flow bypass passed verification: %#v", audit)
			}
		})
	}
}

func TestVerificationExecutesCIStepsFromPreservedWorkingDirectory(t *testing.T) {
	// C9.1-C9.4: these valid nested-module commands fail from the repository
	// root, so a pass proves every discovered CI workdir form was honored.
	t.Setenv("CODEAF_AUDITOR", "0")
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")
	tests := []struct {
		name      string
		moduleDir string
		workflow  string
	}{
		{
			name: "working-directory field", moduleDir: "frontend",
			workflow: `jobs:
  verify:
    steps:
      - working-directory: frontend
        run: go build ./...
      - run: go test ./...
        working-directory: frontend
`},
		{
			name: "multiline leading cd", moduleDir: "frontend",
			workflow: `jobs:
  verify:
    steps:
      - run: |
          cd frontend
          go build ./...
          go test ./...
`},
		{
			name: "inline cd", moduleDir: "frontend",
			workflow: `jobs:
  verify:
    steps:
      - run: cd frontend && go build ./...
      - run: cd frontend && go test ./...
`},
		{
			name: "working-directory then inline cd", moduleDir: filepath.Join("packages", "frontend"),
			workflow: `jobs:
  verify:
    steps:
      - working-directory: packages
        run: cd frontend && go build ./...
      - working-directory: packages
        run: cd frontend && go test ./...
`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			if err := writeFile(filepath.Join(workspace, test.moduleDir, "go.mod"), "module example.test/frontend\n\ngo 1.23\n"); err != nil {
				t.Fatal(err)
			}
			if err := writeFile(filepath.Join(workspace, test.moduleDir, "frontend.go"), "package frontend\n"); err != nil {
				t.Fatal(err)
			}
			if err := writeFile(filepath.Join(workspace, ".github", "workflows", "verify.yml"), test.workflow); err != nil {
				t.Fatal(err)
			}
			runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
				Events: newEventWriter(io.Discard), Notes: io.Discard,
			})
			defer runner.runtime.Close()
			audit, _, err := runner.auditFixLoop(context.Background(), "verify", "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if audit.Status != auditorgate.StatusPass || audit.Verdict == nil ||
				audit.Verdict.Step2Signal == nil || len(audit.Verdict.Step2Signal.Commands) != 2 {
				t.Fatalf("nested CI verification = %#v, verdict=%#v", audit, audit.Verdict)
			}
		})
	}
}

func TestHardVerificationBypassesTestMemo(t *testing.T) {
	// Validation contract C2.5: even an unchanged tree and identical test
	// command cannot supply the hard gate from the ordinary Bash memo.
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
	workspace := t.TempDir()
	marker := filepath.Join(t.TempDir(), "test-executions")
	if err := writeFile(filepath.Join(workspace, "go.mod"), "module example.test/fresh\n\ngo 1.23\n"); err != nil {
		t.Fatal(err)
	}
	testSource := fmt.Sprintf(`package fresh

import (
  "os"
  "testing"
)

func TestFresh(t *testing.T) {
  file, err := os.OpenFile(%q, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
  if err != nil { t.Fatal(err) }
  defer file.Close()
  if _, err := file.WriteString("x"); err != nil { t.Fatal(err) }
}
`, marker)
	if err := writeFile(filepath.Join(workspace, "fresh_test.go"), testSource); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "AGENTS.md"),
		"Run `go build ./...` and `go test -count=1 ./...`.\n"); err != nil {
		t.Fatal(err)
	}
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	defer runner.runtime.Close()
	input, _ := json.Marshal(map[string]any{"command": "go test -count=1 ./..."})
	if _, err := runner.runtime.registry.Execute(context.Background(), steploop.ToolCall{
		ID: "pre-gate", Name: "bash", Input: input, SessionID: runner.sessionID,
	}); err != nil {
		t.Fatal(err)
	}
	verification := runner.runProjectVerification(context.Background())
	if verification.Failed != nil {
		t.Fatalf("hard verification = %#v", verification)
	}
	body, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "xx" {
		t.Fatalf("test process executions = %q, want two", body)
	}
}

func TestPythonProjectPassesOnATestEntrypointAlone(t *testing.T) {
	// Validation contract C3.2 qualifier: the build requirement applies only
	// where a build/typecheck step is discoverable. A plain Python package has
	// tests to run and nothing to compile, so demanding a build entrypoint
	// would fail every such repo instead of verifying it.
	t.Setenv("CODEAF_AUDITOR", "0")
	workspace := t.TempDir()
	if err := writeFile(
		filepath.Join(workspace, "pyproject.toml"), "[project]\nname = \"demo\"\n",
	); err != nil {
		t.Fatal(err)
	}
	writePassingPythonUnitTest(t, workspace)
	if err := writeFile(
		filepath.Join(workspace, "AGENTS.md"), "Run `python3 -m unittest discover -s tests`.\n",
	); err != nil {
		t.Fatal(err)
	}
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	defer runner.runtime.Close()
	audit, _, err := runner.auditFixLoop(context.Background(), "verify", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if audit.Status != auditorgate.StatusPass {
		t.Fatalf("python test-only project = %#v", audit)
	}
	commands := audit.Verdict.Step2Signal.Commands
	row, ok := commands[0].(map[string]any)
	if !ok || row["buildExpected"] != false {
		t.Fatalf("python command evidence = %#v, want buildExpected=false", commands)
	}
}

func TestHarnessReconciliationHonorsBuildExpectation(t *testing.T) {
	// F4.3: harness-owned plan metadata permits a green test-only project to
	// refute an exact Step-2 absence blocker, while an expected build still
	// requires a green build row.
	step := float64(2)
	verdict := auditorgate.AuditorVerdict{
		Verdict: auditorgate.VerdictFail,
		Blockers: []auditorgate.Blocker{{
			Step: &step, Detail: "Verification was not performed",
		}},
	}
	for _, test := range []struct {
		name          string
		buildExpected bool
		wantRefuted   bool
	}{
		{name: "test-only plan", buildExpected: false, wantRefuted: true},
		{name: "build-expected plan", buildExpected: true, wantRefuted: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			commands := []any{map[string]any{
				"cmd": "python3 -m pytest", "exit": float64(0), "kind": "test",
				"buildExpected": test.buildExpected,
			}}
			got, refuted := auditorgate.ReconcileHarnessVerification(verdict, commands)
			if refuted != test.wantRefuted {
				t.Fatalf("reconciled verdict = %#v, refuted=%v, want %v", got, refuted, test.wantRefuted)
			}
		})
	}
}

func TestRequirementsPythonProjectPassesOnDefaultPytestEntrypoint(t *testing.T) {
	// Corrected F4 regression: a requirements/config/tests Python repository is
	// recognized even without pyproject.toml, setup.py, or setup.cfg.
	t.Setenv("CODEAF_AUDITOR", "0")
	workspace := t.TempDir()
	writePassingPythonUnitTest(t, workspace)
	if err := writeFile(filepath.Join(workspace, "requirements.txt"), "pytest\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "pytest.ini"), "[pytest]\n"); err != nil {
		t.Fatal(err)
	}
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	defer runner.runtime.Close()
	audit, _, err := runner.auditFixLoop(context.Background(), "verify", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if audit.Status != auditorgate.StatusPass {
		t.Fatalf("requirements Python project = %#v", audit)
	}
}

func TestShellNoOpCannotSupplyGreenTestEvidence(t *testing.T) {
	// C2.1-C2.2: a plain-Python project may omit a build, but a successful
	// `true` process cannot become its test evidence through a trailing comment.
	t.Setenv("CODEAF_AUDITOR", "0")
	workspace := t.TempDir()
	if err := writeFile(filepath.Join(workspace, "pyproject.toml"), "[project]\nname = \"demo\"\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "AGENTS.md"), "Run `true # pytest`.\n"); err != nil {
		t.Fatal(err)
	}
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	defer runner.runtime.Close()
	audit, _, err := runner.auditFixLoop(context.Background(), "verify", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if audit.Status != auditorgate.StatusFail || audit.Verdict == nil ||
		!strings.Contains(audit.Verdict.Blockers[0].Detail, "no standard test entrypoint") {
		t.Fatalf("comment-spoofed test = %#v, want missing-test failure", audit)
	}
	if audit.Verdict.Step2Signal == nil || len(audit.Verdict.Step2Signal.Commands) != 0 {
		t.Fatalf("comment-spoofed process evidence = %#v, want none", audit.Verdict.Step2Signal)
	}
}

func TestFailingPythonTypecheckBlocksPassingTests(t *testing.T) {
	// C3.2: the explicit compile step is mandatory process evidence even for a
	// Python project whose genuine unit-test entrypoint is green.
	t.Setenv("CODEAF_AUDITOR", "0")
	workspace := t.TempDir()
	if err := writeFile(filepath.Join(workspace, "pyproject.toml"), "[project]\nname = \"typed\"\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "src", "broken.py"), "def broken(:\n"); err != nil {
		t.Fatal(err)
	}
	writePassingPythonUnitTest(t, workspace)
	if err := writeFile(filepath.Join(workspace, "AGENTS.md"),
		"Run `python3 -m compileall src` and `python3 -m unittest discover -s tests`.\n"); err != nil {
		t.Fatal(err)
	}
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	defer runner.runtime.Close()
	audit, _, err := runner.auditFixLoop(context.Background(), "verify", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if audit.Status != auditorgate.StatusFail || audit.Verdict == nil ||
		!strings.Contains(audit.Verdict.Blockers[0].Detail, "python3 -m compileall src") {
		t.Fatalf("typed Python verification = %#v, want compile failure", audit)
	}
	commands := audit.Verdict.Step2Signal.Commands
	if len(commands) != 2 || auditorgate.CommandExit(commands[0]) == "0" ||
		auditorgate.CommandExit(commands[1]) != "0" {
		t.Fatalf("typed Python evidence = %#v, want red build and green test", commands)
	}
}

func writePassingPythonUnitTest(t *testing.T, workspace string) {
	t.Helper()
	const source = `import unittest

class GreenTest(unittest.TestCase):
    def test_green(self):
        self.assertEqual(2 + 2, 4)
`
	if err := writeFile(filepath.Join(workspace, "tests", "test_green.py"), source); err != nil {
		t.Fatal(err)
	}
}
