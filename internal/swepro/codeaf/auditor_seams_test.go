package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/session/auditorgate"
	"github.com/Agent-Field/swe-pro-go/internal/session/scheduler"
)

type seamBackend struct {
	t              *testing.T
	auditPrompts   []string
	lowCalls       int
	auditorCalls   int
	mismatch       bool
	confirmation   bool
	adjudicatorHit bool
}

func (backend *seamBackend) Run(_ context.Context, request turn) (turnResult, error) {
	switch request.Agent {
	case "low-judge":
		backend.lowCalls++
		return turnResult{Text: `{"clauses":["changed module must pass its impacted test"]}`}, nil
	case "auditor", "auditor-light":
		backend.auditorCalls++
		backend.auditPrompts = append(backend.auditPrompts, request.Prompt)
		command := "npm test -- src/value.test.ts"
		actual := command
		if backend.mismatch {
			actual = "go test ./..."
		}
		verdict := map[string]any{
			"verdict": "pass",
			"step2_signal": map[string]any{
				"reproduced": true,
				"commands": []any{map[string]any{
					"cmd": command, "exit": 0, "output": "12 tests passed",
				}},
				"notes": "impacted test passed",
			},
			"blockers": []any{},
			"clause_coverage": []any{map[string]any{
				"clause":   "changed module must pass its impacted test",
				"evidence": command + " -> 12 tests passed",
			}},
		}
		if backend.confirmation && backend.auditorCalls == 2 {
			verdict = map[string]any{
				"verdict":  "fail",
				"commands": []any{map[string]any{"cmd": command, "exit": 1}},
				"blockers": []any{map[string]any{
					"detail":   "confirmation found a real regression",
					"severity": "correctness",
				}},
			}
		}
		body, err := json.Marshal(verdict)
		if err != nil {
			return turnResult{}, err
		}
		if err := os.MkdirAll(filepath.Join(request.Workspace, ".codeaf"), 0o755); err != nil {
			return turnResult{}, err
		}
		if err := os.WriteFile(
			filepath.Join(request.Workspace, ".codeaf", "auditor-verdict.json"),
			body, 0o600,
		); err != nil {
			return turnResult{}, err
		}
		return turnResult{Parts: []scheduler.LeafPart{
			{Type: "tool", Tool: "bash", ArgsKey: `{"command":"` + actual + `"}`, Status: "completed"},
			{Type: "text", Text: "PASS: impacted test reports 12 tests passed"},
		}}, nil
	case "adjudicator":
		backend.adjudicatorHit = true
		path := filepath.Join(
			request.Workspace, ".codeaf", "agents", "adjudicator", "cycle-2.json",
		)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return turnResult{}, err
		}
		return turnResult{Text: "adjudicated"}, os.WriteFile(
			path,
			[]byte(`{"verdict":"fail","blockers":[{"detail":"frontier confirmed the regression","severity":"correctness"}]}`),
			0o600,
		)
	default:
		return turnResult{}, nil
	}
}

func tiaWorkspace(t *testing.T) (string, string) {
	t.Helper()
	workspace, base := guardWorkspace(t)
	if err := writeFile(filepath.Join(workspace, ".git", "info", "exclude"), ".codeaf/\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "src", "value.ts"), "export const value = 1\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(
		filepath.Join(workspace, "src", "value.test.ts"),
		`import { value } from "./value"; test("value", () => expect(value).toBe(1))`+"\n",
	); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "Makefile"), "build:\n\t@true\n\ntest:\n\t@true\n"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "add", "src", "Makefile"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "commit", "-m", "typescript base"); err != nil {
		t.Fatal(err)
	}
	base = gitOutput(context.Background(), workspace, "rev-parse", "HEAD")
	if err := writeFile(filepath.Join(workspace, "src", "value.ts"), "export const value = 2\n"); err != nil {
		t.Fatal(err)
	}
	return workspace, base
}

func TestAuditorTIAResultCapFallsBackToFullEntrypointContract(t *testing.T) {
	// Parity audit contract: exact TIA is used only for 1..20 tests; a wider
	// impact set returns nil so the auditor runs the full entrypoint.
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "src", "value.ts"), "export const value = 1\n"); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 21; index++ {
		name := filepath.Join(workspace, "src", fmt.Sprintf("value_%02d.test.ts", index))
		if err := writeFile(name, `import { value } from "./value"; test("v", () => value)`+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{Events: newEventWriter(io.Discard)})
	defer runner.runtime.Close()
	if got := runner.auditorDependencies().ImpactedTests(workspace, []string{"src/value.ts"}); got != nil {
		t.Fatalf("21 impacted tests = %v, want full-entrypoint fallback", got)
	}
}

func TestAuditorDepthSeamsReachLiveGateAndHardConfirmation(t *testing.T) {
	// Round 3 contract 6: the live auditor uses LOW clause judgment, exact TIA,
	// executed-command and extra-evidence observations, frontier adjudication,
	// and hard-mode confirmation blocker replacement.
	t.Run("live optional seams", func(t *testing.T) {
		t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
		t.Setenv("CODEAF_ADMISSIBILITY", "1")
		workspace, base := tiaWorkspace(t)
		backend := &seamBackend{t: t}
		runner := newPipeline(cliArgs{
			High: "provider/high", Low: "provider/low", Frontier: "provider/frontier",
		}, workspace, pipelineDeps{
			Backend: backend, Events: newEventWriter(io.Discard), Notes: io.Discard,
		})
		deps := runner.auditorDependencies()
		withoutDeadline := func(
			ctx context.Context, _ time.Duration,
		) (context.Context, context.CancelFunc) {
			return context.WithCancel(ctx)
		}
		deps.AgentJSON.TimeoutContext = withoutDeadline
		adjudicator := deps.Adjudicator.(auditorgate.AgentJSONAdjudicator)
		adjudicator.Dependencies.TimeoutContext = withoutDeadline
		deps.Adjudicator = adjudicator
		result, err := auditorgate.GateSession(context.Background(), auditorgate.GateInput{
			Workspace: workspace, UserPrompt: "The changed module must pass its impacted test.",
			ParentSessionID: runner.sessionID, BaseSHA: &base,
		}, deps)
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != auditorgate.StatusPass {
			t.Fatalf("evidence-backed result = %#v", result)
		}
		if backend.lowCalls != 1 || len(backend.auditPrompts) != 1 {
			t.Fatalf("low calls=%d audit prompts=%d", backend.lowCalls, len(backend.auditPrompts))
		}
		for _, want := range []string{
			"changed module must pass its impacted test", "src/value.test.ts",
		} {
			if !strings.Contains(backend.auditPrompts[0], want) {
				t.Errorf("audit prompt missing %q: %s", want, backend.auditPrompts[0])
			}
		}
		if result.Evidence == nil || !strings.Contains(*result.Evidence, "12 tests passed") {
			t.Fatalf("dynamic evidence = %#v", result.Evidence)
		}
		adjudicated, err := deps.Adjudicator.Adjudicate(
			context.Background(), auditorgate.AdjudicatorInput{
				Workspace: workspace, ParentSessionID: runner.sessionID,
				Disputed:     auditorgate.AuditorVerdict{Verdict: auditorgate.VerdictPass},
				EvidencePack: "tests failed", TimeoutMS: 1000, AuditCycle: 2,
			},
		)
		if err != nil || adjudicated == nil ||
			adjudicated.Verdict != auditorgate.VerdictFail || !backend.adjudicatorHit {
			t.Fatalf("adjudicated=%#v hit=%v err=%v", adjudicated, backend.adjudicatorHit, err)
		}
	})

	t.Run("executed command mismatch", func(t *testing.T) {
		t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
		t.Setenv("CODEAF_ADMISSIBILITY", "1")
		workspace, base := tiaWorkspace(t)
		backend := &seamBackend{t: t, mismatch: true}
		runner := newPipeline(cliArgs{
			High: "provider/high", Low: "provider/low",
		}, workspace, pipelineDeps{
			Backend: backend, Events: newEventWriter(io.Discard), Notes: io.Discard,
		})
		result, err := auditorgate.GateSession(context.Background(), auditorgate.GateInput{
			Workspace: workspace, UserPrompt: "The changed module must pass its impacted test.",
			ParentSessionID: runner.sessionID, BaseSHA: &base,
		}, runner.auditorDependencies())
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != auditorgate.StatusFail || result.Verdict == nil ||
			len(result.Verdict.Blockers) == 0 ||
			!strings.Contains(result.Verdict.Blockers[0].Detail, "unproven") {
			t.Fatalf("mismatched execution result = %#v", result)
		}
	})

	t.Run("hard confirmation dissent", func(t *testing.T) {
		t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
		t.Setenv("CODEAF_ADMISSIBILITY", "1")
		t.Setenv("CODEAF_HARD", "1")
		t.Setenv("CODEAF_TAMPER", "0")
		t.Setenv("CODEAF_SPEC_IDS", "0")
		workspace, base := tiaWorkspace(t)
		backend := &seamBackend{t: t, confirmation: true}
		runner := newPipeline(cliArgs{
			High: "provider/high",
		}, workspace, pipelineDeps{
			Backend: backend, Events: newEventWriter(io.Discard), Notes: io.Discard,
		})
		result, _, err := runner.auditFixLoop(
			context.Background(), "Change value behavior", base, "", "",
		)
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != auditorgate.StatusFail || result.Verdict == nil ||
			len(result.Verdict.Blockers) != 1 ||
			!strings.Contains(result.Verdict.Blockers[0].Detail, "confirmation") {
			t.Fatalf("hard confirmation result = %#v", result)
		}
	})
}
