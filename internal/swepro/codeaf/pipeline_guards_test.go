package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/session/auditorgate"
)

type backendFunc func(context.Context, turn) (turnResult, error)

func (f backendFunc) Run(ctx context.Context, request turn) (turnResult, error) {
	return f(ctx, request)
}

func guardWorkspace(t *testing.T) (string, string) {
	t.Helper()
	workspace := t.TempDir()
	if err := gitRun(workspace, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "README.md"), "base\n"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "add", "README.md"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "commit", "-m", "base"); err != nil {
		t.Fatal(err)
	}
	return workspace, strings.TrimSpace(gitOutput(context.Background(), workspace, "rev-parse", "HEAD"))
}

func passAudit() auditorgate.GateResult {
	verdict := auditorgate.AuditorVerdict{Verdict: auditorgate.VerdictPass}
	return auditorgate.GateResult{Status: auditorgate.StatusPass, Verdict: &verdict}
}

func TestTamperGuardRunsAfterAuditAndFeedsRepairVerdict(t *testing.T) {
	// Round 3 contract 1: an unrequested verification-config edit changes the
	// effective post-audit verdict consumed by the repair path.
	t.Setenv("CODEAF_SPEC_IDS", "0")
	workspace, base := guardWorkspace(t)
	if err := writeFile(filepath.Join(workspace, "package.json"), `{"scripts":{"test":"go test ./..."}}`); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "add", "package.json"); err != nil {
		t.Fatal(err)
	}
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	got := runner.applyAuditGuards(
		context.Background(), "Improve parser behavior", base, passAudit(),
	)
	if got.Status != auditorgate.StatusFail || got.Verdict == nil ||
		len(got.Verdict.Blockers) != 1 ||
		!strings.Contains(got.Verdict.Blockers[0].Detail, "package.json") {
		t.Fatalf("tamper effective verdict = %#v", got)
	}
}

func TestSpecIdentifierGuardRunsAfterAuditAndFeedsRepairVerdict(t *testing.T) {
	// Round 3 contract 2: a missing exact spec name becomes a correctness
	// blocker before cycle accounting and fix generation.
	t.Setenv("CODEAF_TAMPER", "0")
	workspace, base := guardWorkspace(t)
	if err := writeFile(filepath.Join(workspace, "feature.go"), "package feature\n"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "add", "feature.go"); err != nil {
		t.Fatal(err)
	}
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	got := runner.applyAuditGuards(
		context.Background(), `Expose "Exact Display Name" in the implementation.`,
		base, passAudit(),
	)
	if got.Status != auditorgate.StatusFail || got.Verdict == nil ||
		len(got.Verdict.Blockers) != 1 ||
		!strings.Contains(got.Verdict.Blockers[0].Detail, "Exact Display Name") {
		t.Fatalf("spec-id effective verdict = %#v", got)
	}
}

func TestSpecIdentifierGuardIncludesConventionScoutPredictionsContract(t *testing.T) {
	// Parity audit contract: high-confidence W12b sibling predictions are
	// machine-enforced alongside identifiers stated directly in the goal.
	t.Setenv("CODEAF_TAMPER", "0")
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
	t.Setenv("CODEAF_GLOSSARY", "1")
	workspace, base := guardWorkspace(t)
	if err := os.MkdirAll(filepath.Join(workspace, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "src", "sibling.ts"), `export const displayName = "Full Convention Name"`); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "src", "changed.ts"), "export const value = 1\n"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "add", "src/changed.ts"); err != nil {
		t.Fatal(err)
	}
	backend := backendFunc(func(_ context.Context, request turn) (turnResult, error) {
		if request.Agent != "convention-scout" || !strings.Contains(request.Prompt, "Full Convention Name") {
			t.Fatalf("convention scout request = %+v", request)
		}
		return turnResult{Text: `{"expected_identifiers":[{"value":"Full Convention Name","reason":"sibling displayName","confidence":"high"}]}`}, nil
	})
	runner := newPipeline(cliArgs{High: "provider/high"}, workspace, pipelineDeps{
		Backend: backend, Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	defer runner.runtime.Close()
	if err := runner.runtime.ensureRootSession(context.Background(), runner.sessionID, "root", "orchestrator"); err != nil {
		t.Fatal(err)
	}
	goal := "Add `src/new.ts` following sibling conventions."
	runner.runConventionScout(context.Background(), goal)
	if len(runner.predictedIdentifiers) != 1 || runner.predictedIdentifiers[0].Value != "Full Convention Name" {
		t.Fatalf("scout predictions = %#v", runner.predictedIdentifiers)
	}
	got := runner.applyAuditGuards(context.Background(), goal, base, passAudit())
	details := ""
	if got.Verdict != nil {
		for _, blocker := range got.Verdict.Blockers {
			details += blocker.Detail + "\n"
		}
	}
	if got.Status != auditorgate.StatusFail || got.Verdict == nil ||
		!strings.Contains(details, "Full Convention Name") {
		t.Fatalf("predicted identifier verdict = %#v", got)
	}
}

func TestHygieneCleanupFiresOnceAndHonorsKillSwitch(t *testing.T) {
	// Round 3 contract 3: otherwise-shippable work gets one model cleanup pass,
	// and CODEAF_HYGIENE=0 suppresses it.
	for _, test := range []struct {
		name      string
		kill      string
		wantCalls int
		wantGone  bool
	}{
		{name: "cleanup", wantCalls: 1, wantGone: true},
		{name: "kill-switch", kill: "0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CODEAF_HYGIENE", test.kill)
			workspace, base := guardWorkspace(t)
			scratch := filepath.Join(workspace, "src", "scratch.go")
			if err := os.MkdirAll(filepath.Dir(scratch), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := writeFile(scratch, "package src\n"); err != nil {
				t.Fatal(err)
			}
			if err := gitRun(workspace, "add", "src/scratch.go"); err != nil {
				t.Fatal(err)
			}
			calls := 0
			backend := backendFunc(func(_ context.Context, request turn) (turnResult, error) {
				calls++
				if !strings.Contains(request.Prompt, "Remove exactly these files") {
					t.Fatalf("cleanup prompt = %q", request.Prompt)
				}
				if err := os.Remove(scratch); err != nil {
					return turnResult{}, err
				}
				return turnResult{Text: "removed scratch file"}, nil
			})
			runner := newPipeline(cliArgs{
				High: "provider/high", EntryAgent: "coder",
			}, workspace, pipelineDeps{
				Backend: backend, Events: newEventWriter(io.Discard), Notes: io.Discard,
			})
			runner.entryAgent = "coder"
			runner.runHygieneCleanup(context.Background(), base, auditorgate.StatusPass)
			if calls != test.wantCalls {
				t.Fatalf("cleanup calls = %d, want %d", calls, test.wantCalls)
			}
			_, err := os.Stat(scratch)
			if gotGone := os.IsNotExist(err); gotGone != test.wantGone {
				t.Fatalf("scratch removed=%v, want %v (err=%v)", gotGone, test.wantGone, err)
			}
		})
	}
}
