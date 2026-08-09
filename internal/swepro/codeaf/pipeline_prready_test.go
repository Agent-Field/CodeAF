package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/session/prreadyphase"
)

func TestPipelineReverifiesOnlyWhenPostAuditTreeChanges(t *testing.T) {
	tests := []struct {
		name              string
		formatterMutation string
		writeSummary      bool
		selfMutating      bool
		wantStatus        string
		wantReason        string
		wantVerifications int
	}{
		{
			name: "unchanged tree avoids redundant verification", writeSummary: true,
			wantStatus: "pass", wantVerifications: 2,
		},
		{
			name: "changed tree is reverified", formatterMutation: "formatted.txt",
			writeSummary: true, wantStatus: "pass", wantVerifications: 4,
		},
		{
			name: "red final tree revokes pass", formatterMutation: "BROKEN",
			writeSummary: true, wantStatus: "fail", wantVerifications: 4,
		},
		{
			name:       "reported formatter failure revokes pass",
			wantStatus: "fail", wantVerifications: 2,
		},
		{
			name: "self-mutating verification is bounded", formatterMutation: "formatted.txt",
			writeSummary: true, selfMutating: true, wantStatus: "fail",
			wantReason: "self-mutating", wantVerifications: 6,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := validityTestRepo(t)
			setValidityTestEnv(t, workspace)
			t.Setenv("CODEAF_VALIDITY", "0")
			t.Setenv("CODEAF_PRE_GATES", "0")
			t.Setenv("CODEAF_AUDITOR", "0")
			t.Setenv("CODEAF_HYGIENE", "0")
			t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")

			countPath := filepath.Join(t.TempDir(), "verification-count")
			mutationCommand := "@true"
			if test.selfMutating {
				if err := writeFile(filepath.Join(workspace, "generated.txt"), "initial\n"); err != nil {
					t.Fatal(err)
				}
				mutationCommand = "@printf 'mutation\\n' >> generated.txt"
			}
			makefile := fmt.Sprintf(
				"build:\n\t@printf 'build\\n' >> %q\n\t@test ! -f BROKEN\n\n"+
					"test:\n\t@printf 'test\\n' >> %q\n\t@test ! -f BROKEN\n\t%s\n",
				countPath, countPath, mutationCommand,
			)
			if err := writeFile(filepath.Join(workspace, "Makefile"), makefile); err != nil {
				t.Fatal(err)
			}

			backend := backendFunc(func(_ context.Context, request turn) (turnResult, error) {
				switch request.Agent {
				case "coder":
					return turnResult{Text: "implementation complete"}, nil
				case "pr-ready-planner":
					if err := writeFile(
						filepath.Join(workspace, prreadyphase.PlanRelativePath), "plan\n",
					); err != nil {
						return turnResult{}, err
					}
					return turnResult{Text: "planned"}, nil
				case "pr-formatter":
					if test.formatterMutation != "" {
						if err := writeFile(
							filepath.Join(workspace, test.formatterMutation), "mutation\n",
						); err != nil {
							return turnResult{}, err
						}
					}
					if test.writeSummary {
						if err := writeFile(
							filepath.Join(workspace, prreadyphase.SummaryRelativePath), "summary\n",
						); err != nil {
							return turnResult{}, err
						}
					}
					return turnResult{Text: "formatted"}, nil
				default:
					return turnResult{}, fmt.Errorf("unexpected agent %q", request.Agent)
				}
			})
			args := mustValidityArgs(t, workspace)
			args.EntryAgent = "coder"
			args.PRReady = true
			runner := newPipeline(args, workspace, pipelineDeps{
				Backend: backend, Events: newEventWriter(io.Discard), Notes: io.Discard,
			})
			t.Cleanup(runner.runtime.Close)

			result, err := runner.run(context.Background(), args.Message, pipelineOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != test.wantStatus {
				t.Fatalf("result = %#v, want status %q", result, test.wantStatus)
			}
			if test.wantReason != "" && !strings.Contains(result.Reason, test.wantReason) {
				t.Fatalf("result reason = %q, want %q", result.Reason, test.wantReason)
			}
			raw, err := os.ReadFile(countPath)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(strings.Fields(string(raw))); got != test.wantVerifications {
				t.Fatalf("verification command count = %d, want %d; log=%q", got, test.wantVerifications, raw)
			}
		})
	}
}

func TestWorktreeFingerprintBudgetIsFailSafeChanged(t *testing.T) {
	// F10.2: an over-budget tree yields a fresh changed marker rather than a
	// stable hash of a silent subset, forcing the bounded re-verification path.
	workspace := validityTestRepo(t)
	large := make([]byte, worktreeFingerprintMaxBytes+1)
	if err := os.WriteFile(filepath.Join(workspace, "large.bin"), large, 0o644); err != nil {
		t.Fatal(err)
	}
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{Events: newEventWriter(io.Discard), Notes: io.Discard})
	t.Cleanup(runner.runtime.Close)
	first, firstOK := runner.worktreeFingerprint(context.Background())
	second, secondOK := runner.worktreeFingerprint(context.Background())
	if !firstOK || !secondOK || first == second ||
		!strings.Contains(first, "worktree-fingerprint-budget") ||
		!strings.Contains(second, "worktree-fingerprint-budget") {
		t.Fatalf("over-budget fingerprints = %q/%v, %q/%v", first, firstOK, second, secondOK)
	}
}

func TestFailedAuditSkipsWorktreeFingerprinting(t *testing.T) {
	// F10.1: once machine verification has already made the audit non-pass,
	// final-tree fingerprinting cannot improve the outcome and must not scan.
	workspace := validityTestRepo(t)
	setValidityTestEnv(t, workspace)
	t.Setenv("CODEAF_VALIDITY", "0")
	t.Setenv("CODEAF_PRE_GATES", "0")
	t.Setenv("CODEAF_AUDITOR", "0")
	t.Setenv("CODEAF_HYGIENE", "0")
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")
	if err := writeFile(filepath.Join(workspace, "Makefile"), "build:\n\t@false\n\ntest:\n\t@true\n"); err != nil {
		t.Fatal(err)
	}
	args := mustValidityArgs(t, workspace)
	args.EntryAgent = "coder"
	runner := newPipeline(args, workspace, pipelineDeps{
		Backend: backendFunc(func(context.Context, turn) (turnResult, error) {
			return turnResult{Text: "implementation complete"}, nil
		}),
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	t.Cleanup(runner.runtime.Close)
	result, err := runner.run(context.Background(), args.Message, pipelineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status == "pass" {
		t.Fatalf("failing verification passed: %#v", result)
	}
	if runner.fingerprintFiles != nil || runner.fingerprintNonce != 0 {
		t.Fatalf("failed audit scanned worktree: files=%d nonce=%d", len(runner.fingerprintFiles), runner.fingerprintNonce)
	}
}
