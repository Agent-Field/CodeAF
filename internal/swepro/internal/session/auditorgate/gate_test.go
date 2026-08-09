package auditorgate

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/baked"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/agentjson"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/auditconvergence"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/observer"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/specclauses"
)

type auditObserver struct {
	tracked   []observer.ObservedSession
	untracked []string
}

func (o *auditObserver) Track(input observer.ObservedSession) {
	o.tracked = append(o.tracked, input)
}

func (o *auditObserver) Untrack(sessionID string) {
	o.untracked = append(o.untracked, sessionID)
}

func TestVerdictFileLifecycleAndBoundedWalk(t *testing.T) {
	workspace := t.TempDir()
	primary := filepath.Join(workspace, verdictRelative)
	if err := os.MkdirAll(filepath.Dir(primary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(primary, []byte(
		`{"notes":"n","verdict":"fail","extra":"drop","step2_signal":{"notes":"x","extra":"drop"},"step2c_acceptance":[{"criterion":"c","claimed":"yes","verified":"no"}]}`,
	), 0o600); err != nil {
		t.Fatal(err)
	}
	verdict, err := ReadVerdictFile(workspace)
	if err != nil || verdict == nil {
		t.Fatalf("verdict=%#v err=%v", verdict, err)
	}
	encoded, _ := jscompat.Stringify(verdict)
	want := `{"verdict":"fail","notes":"n","step2_signal":{"notes":"x"},"step2c_acceptance":[{"criterion":"c","claimed":"yes","verified":"no"}]}`
	if string(encoded) != want {
		t.Fatalf("normalized verdict\n got: %s\nwant: %s", encoded, want)
	}

	UnlinkVerdictFile(workspace)
	if _, err := os.Stat(primary); !os.IsNotExist(err) {
		t.Fatalf("stale verdict still exists: %v", err)
	}
	plain := filepath.Join(workspace, "plain", ".codeaf")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plain, "auditor-verdict.json"),
		[]byte(`{"verdict":"pass","commands":["bun test exited 0"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	verdict, _ = ReadVerdictFile(workspace)
	if verdict == nil || verdict.Verdict != VerdictPass {
		t.Fatalf("plain nested verdict = %#v", verdict)
	}

	if err := os.RemoveAll(filepath.Join(workspace, "plain")); err != nil {
		t.Fatal(err)
	}
	vendored := filepath.Join(workspace, "vendor", "repo")
	if err := os.MkdirAll(filepath.Join(vendored, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(vendored, ".codeaf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vendored, ".codeaf", "auditor-verdict.json"),
		[]byte(`{"verdict":"pass"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	verdict, _ = ReadVerdictFile(workspace)
	if verdict != nil {
		t.Fatalf("nested repo verdict leaked: %#v", verdict)
	}
}

func TestDraftAndProvenancePersistence(t *testing.T) {
	workspace := t.TempDir()
	WriteVerdictDraft(workspace, AuditorVerdictDraft{
		StartedAt: 123, Mode: "light", ClauseCount: 5, MatrixCellCount: 2,
	})
	raw, err := os.ReadFile(filepath.Join(workspace, draftRelative))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"startedAt":123,"mode":"light","clauseCount":5,"matrixCellCount":2}` {
		t.Fatalf("draft = %s", raw)
	}
	RemoveVerdictDraft(workspace)
	RemoveVerdictDraft(workspace)
	if _, err := os.Stat(filepath.Join(workspace, draftRelative)); !os.IsNotExist(err) {
		t.Fatalf("draft still exists: %v", err)
	}

	provenance := AuditProvenance{
		AuditSHA: "abc", AuditCycle: 2,
		Verdict: AuditorVerdict{
			Verdict:  VerdictFail,
			Blockers: []Blocker{{Detail: "wrong"}},
		},
	}
	if err := WriteAuditProvenance(workspace, provenance); err != nil {
		t.Fatal(err)
	}
	loaded := ReadAuditProvenance(workspace)
	if loaded == nil || loaded.AuditSHA != "abc" || loaded.Verdict.Blockers[0].Detail != "wrong" {
		t.Fatalf("loaded provenance = %#v", loaded)
	}
	meta, err := os.ReadFile(filepath.Join(workspace, provenanceRelative))
	if err != nil || len(meta) == 0 || meta[len(meta)-1] != '\n' {
		t.Fatalf("meta newline err=%v body=%q", err, meta)
	}
	artifacts, err := filepath.Glob(filepath.Join(workspace, ".codeaf", "artifacts", "*.txt"))
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("verdict history artifacts=%v err=%v", artifacts, err)
	}
}

func TestCaptureSessionDiffRealGit(t *testing.T) {
	workspace := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = workspace
		command.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	git("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(workspace, "a.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "base")
	if err := os.WriteFile(filepath.Join(workspace, "a.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !hasChanges(ExecProcessRunner{}, workspace, nil) {
		t.Fatal("unstaged change was not staged/seen")
	}
	diff := captureSessionDiff(ExecProcessRunner{}, workspace, nil)
	if !strings.Contains(diff, "+two") {
		t.Fatalf("diff = %q", diff)
	}
	files := changedFilesForAudit(ExecProcessRunner{}, workspace, nil)
	if !reflect.DeepEqual(files, []string{"a.txt"}) {
		t.Fatalf("changed files = %#v", files)
	}
}

func TestGateSessionDispatchesAuditorThroughAgentJSON(t *testing.T) {
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
	t.Setenv("CODEAF_ADMISSIBILITY", "1")
	workspace := t.TempDir()
	var requests []agentjson.Request
	observerHook := &auditObserver{}
	git := fakeGateGit()
	deps := GateDependencies{
		Git:      git,
		Now:      func() float64 { return 1234 },
		Observer: observerHook,
		Clauses: ClauseJudgerFunc(func(
			context.Context, string, string,
		) (specclauses.SpecClauseJudgment, error) {
			return specclauses.SpecClauseJudgment{
				Clauses: []string{"tests pass"}, Count: 1, Source: "fallback",
			}, nil
		}),
		ExecutedCommands: func(int) []string { return []string{"bun test"} },
		AgentJSON: agentjson.Dependencies{
			Resolver: agentjson.ResolverFunc(func(tier baked.Tier) []string {
				if tier != baked.TierHigh {
					t.Fatalf("tier = %q", tier)
				}
				return []string{"provider/model"}
			}),
			Client: agentjson.ClientFunc(func(_ context.Context, request agentjson.Request) error {
				requests = append(requests, request)
				body := `{"verdict":"pass","step2_signal":{"reproduced":true,"commands":[{"cmd":"bun test","exit":0,"output":"tests passed"}],"notes":"green"},"blockers":[],"clause_coverage":[{"clause":"tests pass","evidence":"bun test -> tests passed"}]}`
				return os.WriteFile(
					filepath.Join(workspace, verdictRelative), []byte(body), 0o600,
				)
			}),
			NewID: func(prefix string) string { return prefix + "-id" },
		},
	}
	verificationEvidence := "# Harness-executed full project verification\n- [test] `bun test` exit=0"
	result, err := GateSession(context.Background(), GateInput{
		Workspace: workspace, UserPrompt: "Ensure tests pass.",
		ParentSessionID: "parent", VerificationEvidence: &verificationEvidence,
	}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusPass || result.Verdict == nil ||
		result.Verdict.Verdict != VerdictPass {
		t.Fatalf("result = %#v", result)
	}
	if len(requests) != 1 || requests[0].Agent != "auditor" {
		t.Fatalf("requests = %#v", requests)
	}
	if len(observerHook.tracked) != 1 || len(observerHook.untracked) != 1 {
		t.Fatalf("observer lifecycle = %#v", observerHook)
	}
	observed := observerHook.tracked[0]
	if observed.SessionID != "session-id" || observed.AgentRole != "auditor" ||
		observed.TaskSummary != "Audit completion of: Ensure tests pass." ||
		observed.StartedAt != 1234 ||
		observed.ArtifactPath != filepath.Join(workspace, verdictRelative) ||
		observed.Workspace != workspace || observed.ParentSessionID != "parent" ||
		observerHook.untracked[0] != observed.SessionID {
		t.Fatalf("observer payload=%#v untracked=%v", observed, observerHook.untracked)
	}
	for _, want := range []string{
		"Original task spec", "Ensure tests pass.", "diff --git",
		"Completion criteria", workspace + "/.codeaf/auditor-verdict.json",
		"Harness-executed full project verification", "[test] `bun test` exit=0",
	} {
		if !strings.Contains(requests[0].TaskPrompt, want) {
			t.Errorf("task prompt missing %q", want)
		}
	}
	if result.Evidence == nil || !strings.Contains(*result.Evidence, "tests passed") {
		t.Fatalf("harvested evidence = %#v", result.Evidence)
	}
	if result.Convergence == nil {
		t.Fatal("missing convergence assessment")
	}
	if _, err := os.Stat(filepath.Join(workspace, draftRelative)); !os.IsNotExist(err) {
		t.Fatalf("draft was not removed: %v", err)
	}
	if ReadAuditProvenance(workspace) == nil {
		t.Fatal("provenance not written")
	}
}

func TestGateSessionRetriesInadmissiblePassThenConverts(t *testing.T) {
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
	t.Setenv("CODEAF_ADMISSIBILITY", "1")
	workspace := t.TempDir()
	attempts := 0
	deps := GateDependencies{
		Git: fakeGateGit(),
		AgentJSON: agentjson.Dependencies{
			Resolver: agentjson.ResolverFunc(func(baked.Tier) []string {
				return []string{"provider/model"}
			}),
			Client: agentjson.ClientFunc(func(_ context.Context, request agentjson.Request) error {
				attempts++
				return os.WriteFile(
					filepath.Join(workspace, verdictRelative),
					[]byte(`{"verdict":"pass"}`), 0o600,
				)
			}),
			NewID: func(prefix string) string { return prefix + "-id" },
		},
	}
	result, err := GateSession(context.Background(), GateInput{
		Workspace: workspace, UserPrompt: "Do it", ParentSessionID: "p",
	}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d", attempts)
	}
	if result.Status != StatusFail || result.Verdict == nil ||
		len(result.Verdict.Blockers) != 1 ||
		!strings.Contains(result.Verdict.Blockers[0].Detail, "[unproven]") {
		t.Fatalf("result = %#v", result)
	}
}

func TestGateSessionRetriesUnderfilledTemplateVerdict(t *testing.T) {
	// Round 4 contract C1: response completeness is not optional admissibility
	// policy. Even with that policy disabled, the live runH template shape must
	// consume another slot from auditor-gate.ts:2071-2076's bounded attempts.
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")
	t.Setenv("CODEAF_ADMISSIBILITY", "0")
	t.Setenv("CODEAF_AUDITOR_MAX_ATTEMPTS", "2")
	workspace := t.TempDir()
	attempts := 0
	requests := []agentjson.Request{}
	deps := GateDependencies{
		Git: fakeGateGit(),
		AgentJSON: agentjson.Dependencies{
			Resolver: agentjson.ResolverFunc(func(baked.Tier) []string {
				return []string{"provider/model"}
			}),
			Client: agentjson.ClientFunc(func(_ context.Context, request agentjson.Request) error {
				attempts++
				requests = append(requests, request)
				body := `{"verdict":"fail","step2_signal":{"reproduced":false,"commands":[{"cmd":"go build ./...","exit":0,"kind":"build","source":"go.mod"},{"cmd":"go test ./...","exit":0,"kind":"test","source":"go.mod"}],"spec_examples_matched":"n/a","notes":"Not yet run"},"step3_scope":{},"step4_structural":{"shape_matches_spec":false},"blockers":[{"file":"unknown","line":0,"step":2,"detail":"Build and tests not yet verified independently"}],"repair_hints":[]}`
				if attempts == 2 {
					body = `{"verdict":"fail","step2_signal":{"reproduced":true,"commands":[{"cmd":"go test ./...","exit":1}],"notes":"parser regression reproduced"},"step3_scope":{"callers_checked":["Parse"]},"blockers":[{"file":"parser.go","line":42,"step":2,"detail":"go test ./... fails in TestParseUnary"}],"repair_hints":["Fix unary parsing."]}`
				}
				return os.WriteFile(filepath.Join(workspace, verdictRelative), []byte(body), 0o600)
			}),
			NewID: func(prefix string) string { return prefix + "-id" },
		},
	}
	result, err := GateSession(context.Background(), GateInput{
		Workspace: workspace, UserPrompt: "Fix the parser", ParentSessionID: "p",
	}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if len(requests) != 2 || !strings.Contains(
		requests[1].TaskPrompt, "draft or incomplete audit",
	) {
		t.Fatalf("retry requests = %#v", requests)
	}
	if result.Status != StatusFail || result.Verdict == nil ||
		len(result.Verdict.Blockers) != 1 ||
		result.Verdict.Blockers[0].Detail != "go test ./... fails in TestParseUnary" {
		t.Fatalf("result = %#v", result)
	}
}

func TestReconcileHarnessVerificationRefutesOnlyAbsenceBlockers(t *testing.T) {
	// Round 4 contracts C2-C3: both independently process-proven entrypoint
	// kinds are required before the exact runH Step 2 absence claim can clear.
	verdict := mustAuditorVerdict(t, `{"verdict":"fail","step2_signal":{"reproduced":false,"commands":[],"notes":"Not yet run"},"step3_scope":{},"step4_structural":{"shape_matches_spec":false},"blockers":[{"file":"unknown","line":0,"step":2,"detail":"Build and tests not yet verified independently"}],"repair_hints":[]}`)
	green := []any{
		map[string]any{"cmd": "go build ./...", "exit": float64(0), "kind": "build", "source": "go.mod"},
		map[string]any{"cmd": "go test ./...", "exit": float64(0), "kind": "test", "source": "go.mod"},
	}

	got, refuted := ReconcileHarnessVerification(verdict, green)
	if !refuted || got.Verdict != VerdictPass || len(got.Blockers) != 0 {
		t.Fatalf("reconciled verdict = %#v, refuted=%v", got, refuted)
	}
	if got.Step2Signal == nil || len(got.Step2Signal.Commands) != 2 ||
		got.Step2Signal.Notes == nil || strings.Contains(strings.ToLower(*got.Step2Signal.Notes), "not yet run") {
		t.Fatalf("reconciled signal = %#v", got.Step2Signal)
	}
	if got.Step2Signal.Reproduced == nil || *got.Step2Signal.Reproduced {
		t.Fatalf("model reproduction judgment was overwritten: %#v", got.Step2Signal.Reproduced)
	}
}

func TestReconcileHarnessVerificationRequiresBothGreenKinds(t *testing.T) {
	// Round 4 contract C2: one green kind, an untyped command, or any red row is
	// not authority to contradict the auditor.
	verdict := mustAuditorVerdict(t, `{"verdict":"fail","blockers":[{"step":2,"detail":"Verification was not performed"}]}`)
	tests := []struct {
		name     string
		commands []any
	}{
		{name: "build-only", commands: []any{map[string]any{"cmd": "go build ./...", "exit": float64(0), "kind": "build"}}},
		{name: "test-only", commands: []any{map[string]any{"cmd": "go test ./...", "exit": float64(0), "kind": "test"}}},
		{name: "untyped", commands: []any{
			map[string]any{"cmd": "go build ./...", "exit": float64(0)},
			map[string]any{"cmd": "go test ./...", "exit": float64(0)},
		}},
		{name: "red-test", commands: []any{
			map[string]any{"cmd": "go build ./...", "exit": float64(0), "kind": "build"},
			map[string]any{"cmd": "go test ./...", "exit": float64(1), "kind": "test"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, refuted := ReconcileHarnessVerification(verdict, test.commands)
			if refuted || got.Verdict != VerdictFail || len(got.Blockers) != 1 {
				t.Fatalf("reconciled verdict = %#v, refuted=%v", got, refuted)
			}
		})
	}
}

func TestReconcileHarnessVerificationPreservesSubstantiveStep2Failures(t *testing.T) {
	// Round 4 contracts C4 and C6: green project entrypoints refute only the
	// claim that no check occurred, never the result of a substantive probe.
	green := []any{
		map[string]any{"cmd": "go build ./...", "exit": float64(0), "kind": "build"},
		map[string]any{"cmd": "go test ./...", "exit": float64(0), "kind": "test"},
	}
	for _, detail := range []string{
		"go test ./... failed in TestParseUnary",
		"Spec example '1 + 2' produced 4 instead of 3",
		"Required crash reproduction was not reproduced",
		"Tests were not run because package compilation failed",
	} {
		t.Run(detail, func(t *testing.T) {
			verdict := mustAuditorVerdict(t, `{"verdict":"fail","step2_signal":{"reproduced":true,"commands":[{"cmd":"targeted probe","exit":1}],"notes":"audit complete"},"step3_scope":{"callers_checked":["Parse"]},"blockers":[{"file":"parser.go","line":42,"step":2,"detail":`+string(mustJSON(t, detail))+`}],"repair_hints":["Fix the defect."]}`)
			got, refuted := ReconcileHarnessVerification(verdict, green)
			if refuted || got.Verdict != VerdictFail || len(got.Blockers) != 1 ||
				got.Blockers[0].Detail != detail || !reflect.DeepEqual(got.RepairHints, verdict.RepairHints) {
				t.Fatalf("reconciled verdict = %#v, refuted=%v", got, refuted)
			}
		})
	}
}

func mustAuditorVerdict(t *testing.T, raw string) AuditorVerdict {
	t.Helper()
	var verdict AuditorVerdict
	if err := json.Unmarshal([]byte(raw), &verdict); err != nil {
		t.Fatal(err)
	}
	return verdict
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestBranchBAdjudicationKeepsRawLiveVerdict(t *testing.T) {
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
	t.Setenv("CODEAF_ADMISSIBILITY", "1")
	workspace := t.TempDir()
	cycle := 2.0
	contract := "Result: PASS"
	deps := GateDependencies{
		Git: fakeGateGit(),
		AgentJSON: agentjson.Dependencies{
			Resolver: agentjson.ResolverFunc(func(baked.Tier) []string {
				return []string{"provider/model"}
			}),
			Client: agentjson.ClientFunc(func(_ context.Context, request agentjson.Request) error {
				return os.WriteFile(
					filepath.Join(workspace, verdictRelative),
					[]byte(`{"verdict":"fail","commands":[{"cmd":"bun test","exit":0}],"blockers":[{"detail":"cosmetic concern","severity":"polish"}]}`),
					0o600,
				)
			}),
			NewID: func(prefix string) string { return prefix + "-id" },
		},
		Adjudicator: AdjudicatorFunc(func(
			context.Context, AdjudicatorInput,
		) (*AuditorVerdict, error) {
			verdict := AuditorVerdict{Verdict: VerdictPass}
			return &verdict, nil
		}),
	}
	result, err := GateSession(context.Background(), GateInput{
		Workspace: workspace, UserPrompt: "Do it", ParentSessionID: "p",
		AuditCycle: &cycle, ContractEvidence: &contract,
	}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusPass {
		t.Fatalf("adjudicated result = %#v", result)
	}
	live, err := ReadVerdictFile(workspace)
	if err != nil || live == nil || live.Verdict != VerdictFail {
		t.Fatalf("live verdict=%#v err=%v; branch-B mismatch was repaired", live, err)
	}
	provenance := ReadAuditProvenance(workspace)
	if provenance == nil || provenance.Verdict.Verdict != VerdictPass {
		t.Fatalf("applied provenance = %#v", provenance)
	}
}

func TestGateSessionSkipsNoDiff(t *testing.T) {
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")
	workspace := t.TempDir()
	called := false
	result, err := GateSession(context.Background(), GateInput{
		Workspace: workspace, UserPrompt: "x", ParentSessionID: "p",
	}, GateDependencies{
		Git: ProcessRunnerFunc(func(argv []string, cwd string) (ProcessResult, error) {
			return ProcessResult{}, nil
		}),
		AgentJSON: agentjson.Dependencies{
			Client: agentjson.ClientFunc(func(context.Context, agentjson.Request) error {
				called = true
				return nil
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusSkipped || called {
		t.Fatalf("result=%#v called=%v", result, called)
	}
}

func TestAssessVerdictConvergenceUsesPortedPolicy(t *testing.T) {
	severity := auditconvergence.SeverityHygiene
	verdict := AuditorVerdict{
		Verdict:  VerdictFail,
		Blockers: []Blocker{{Detail: "leftover scratch file", Severity: &severity}},
	}
	result := AssessVerdictConvergence(verdict, nil, nil, nil)
	if result.Action != auditconvergence.ActionCleanup {
		t.Fatalf("assessment = %#v", result)
	}
	history := []*auditconvergence.ConvergenceCycle{{
		HygieneCount: 1, BlockerKeys: []string{"other"},
	}}
	result = AssessVerdictConvergence(verdict, history, nil, nil)
	if result.Action != auditconvergence.ActionAccept {
		t.Fatalf("assessment after cleanup = %#v", result)
	}
}

func TestAgentJSONAdjudicator(t *testing.T) {
	workspace := t.TempDir()
	var request agentjson.Request
	service := AgentJSONAdjudicator{Dependencies: agentjson.Dependencies{
		Resolver: agentjson.ResolverFunc(func(tier baked.Tier) []string {
			if tier != baked.TierFrontier {
				t.Fatalf("tier = %q", tier)
			}
			return []string{"provider/frontier"}
		}),
		Client: agentjson.ClientFunc(func(_ context.Context, got agentjson.Request) error {
			request = got
			path := filepath.Join(workspace, ".codeaf", "agents", "adjudicator", "cycle-2.json")
			return os.WriteFile(path, []byte(
				`{"verdict":"fail","blockers":[{"detail":"real bug","severity":"correctness"}]}`,
			), 0o600)
		}),
		NewID: func(prefix string) string { return prefix + "-id" },
	}}
	verdict, err := service.Adjudicate(context.Background(), AdjudicatorInput{
		Workspace: workspace, ParentSessionID: "p",
		Disputed:     AuditorVerdict{Verdict: VerdictPass},
		EvidencePack: "evidence", TimeoutMS: 1000, AuditCycle: 2,
	})
	if err != nil || verdict == nil || verdict.Verdict != VerdictFail {
		t.Fatalf("verdict=%#v err=%v", verdict, err)
	}
	if request.Agent != "adjudicator" ||
		!strings.Contains(request.TaskPrompt, "Verdict under dispute") &&
			!strings.Contains(request.TaskPrompt, "verdict under dispute") {
		t.Fatalf("request = %#v", request)
	}
}

type fakeLowProvider struct {
	modelCalls []string
}

func (p *fakeLowProvider) GetModel(
	_ context.Context, providerID, modelID string,
) (any, error) {
	p.modelCalls = append(p.modelCalls, providerID+"/"+modelID)
	if providerID == "bad" {
		return nil, os.ErrNotExist
	}
	return providerID + "/" + modelID, nil
}

func (*fakeLowProvider) GetLanguage(_ context.Context, model any) (any, error) {
	return "language:" + model.(string), nil
}

func TestResolveLowLanguageWithSkipsInvalidAndFailedCandidates(t *testing.T) {
	provider := &fakeLowProvider{}
	got := ResolveLowLanguageWith(context.Background(), []string{
		"invalid", "bad/model", "good/model/name",
	}, provider)
	if got != "language:good/model/name" {
		t.Fatalf("language = %#v", got)
	}
	if !reflect.DeepEqual(provider.modelCalls, []string{"bad/model", "good/model/name"}) {
		t.Fatalf("model calls = %#v", provider.modelCalls)
	}
}

func fakeGateGit() ProcessRunner {
	return ProcessRunnerFunc(func(argv []string, _ string) (ProcessResult, error) {
		joined := strings.Join(argv, " ")
		switch {
		case joined == "git rev-parse HEAD":
			return ProcessResult{Stdout: []byte("abcdef1234567890\n")}, nil
		case strings.Contains(joined, "--stat"):
			return ProcessResult{Stdout: []byte(" src/a.go | 1 +\n")}, nil
		case strings.Contains(joined, "--name-only"):
			return ProcessResult{Stdout: []byte("src/a.go\n")}, nil
		case strings.Contains(joined, "--no-color"):
			return ProcessResult{Stdout: []byte("diff --git a/src/a.go b/src/a.go\n+change\n")}, nil
		default:
			return ProcessResult{}, nil
		}
	})
}

func TestAuditorSchemaStripsUnknownKeys(t *testing.T) {
	raw := json.RawMessage(`{"notes":"n","verdict":"pass","unknown":1}`)
	parsed := (AuditorSchema{}).SafeParse(raw)
	if !parsed.Success() {
		t.Fatalf("issues = %#v", parsed.Issues)
	}
	encoded, _ := jscompat.Stringify(parsed.Data)
	if string(encoded) != `{"verdict":"pass","notes":"n"}` {
		t.Fatalf("encoded = %s", encoded)
	}
}

// TestDoneCriteriaAcceptsPassingAcceptanceContract is the issue-#23 regression.
//
// The done-criteria overlay treats a PASSING auditor verdict as unproven unless
// it can see fresh test evidence, and VerifiedTestsPassed recognizes only
// ecosystem-standard runners (`go test`, `pytest`, `npm test`, …). A task in a
// repo with no test framework gets a bespoke acceptance script instead, so the
// overlay saw no test evidence, flipped a correct pass to fail with
// "done-criteria evidence incomplete", and looped: no edit the coder could make
// would ever spell `bash test-hello.sh` like a test runner.
//
// The registered acceptance contract closes the gap. It is not a weaker
// substitute: the harness runs it itself each cycle, and the base-contract
// check requires it to have FAILED at the base commit.
func TestDoneCriteriaAcceptsPassingAcceptanceContract(t *testing.T) {
	// The auditor verdict from the real failing run: pass, reproduced, every
	// command exit 0, no blockers — and not one command a test regex matches.
	const bespokeVerdict = `{"verdict":"pass","step2_signal":{"reproduced":true,` +
		`"commands":[{"cmd":"xxd hello.txt","exit":0,"tail":"hello engine"},` +
		`{"cmd":"bash test-hello.sh","exit":0,"tail":"PASS: criterion-2"}],` +
		`"notes":"file contains exactly 'hello engine'"},"blockers":[],` +
		`"clause_coverage":[{"clause":"create hello.txt","evidence":"xxd -> hello engine"}]}`
	// The historical shape: an ecosystem runner VerifiedTestsPassed recognizes.
	const ecosystemVerdict = `{"verdict":"pass","step2_signal":{"reproduced":true,` +
		`"commands":[{"cmd":"bun test","exit":0,"output":"tests passed"}],"notes":"green"},` +
		`"blockers":[],"clause_coverage":[{"clause":"create hello.txt","evidence":"bun test -> green"}]}`

	for _, test := range []struct {
		name           string
		verdictBody    string
		contractPassed bool
		prompt         string
		wantStatus     GateStatus
		wantVerdict    VerdictStatus
	}{
		{
			name:           "bespoke contract command with a passing contract",
			verdictBody:    bespokeVerdict,
			contractPassed: true,
			prompt:         "Create a file src/a.go containing exactly: hello engine",
			wantStatus:     StatusPass, wantVerdict: VerdictPass,
		},
		{
			// Without the contract signal the old starvation is still visible:
			// this is the exact bug, and it must stay failing on its own terms.
			name:           "bespoke contract command with no contract evidence",
			verdictBody:    bespokeVerdict,
			contractPassed: false,
			prompt:         "Create a file src/a.go containing exactly: hello engine",
			wantStatus:     StatusFail, wantVerdict: VerdictFail,
		},
		{
			// No regression for repos that do have a real runner: the
			// ecosystem path still passes with no contract at all.
			name:           "ecosystem test command without a contract",
			verdictBody:    ecosystemVerdict,
			contractPassed: false,
			prompt:         "Create a file src/a.go containing exactly: hello engine",
			wantStatus:     StatusPass, wantVerdict: VerdictPass,
		},
		{
			// The anti-rubber-stamp case. The prompt demands a file the diff
			// never touched (fakeGateGit only ever reports src/a.go), so the
			// file criterion is genuinely unmet and a passing contract must
			// NOT buy it a pass.
			name:           "genuinely unmet file criterion still fails",
			verdictBody:    bespokeVerdict,
			contractPassed: true,
			prompt:         "Create a file docs/absent.md describing the change",
			wantStatus:     StatusFail, wantVerdict: VerdictFail,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
			t.Setenv("CODEAF_ADMISSIBILITY", "0")
			workspace := t.TempDir()
			deps := GateDependencies{
				Git: fakeGateGit(),
				Now: func() float64 { return 1234 },
				Clauses: ClauseJudgerFunc(func(
					context.Context, string, string,
				) (specclauses.SpecClauseJudgment, error) {
					return specclauses.SpecClauseJudgment{
						Clauses: []string{"create hello.txt"}, Count: 1, Source: "fallback",
					}, nil
				}),
				ExecutedCommands: func(int) []string { return []string{"bash test-hello.sh"} },
				AgentJSON: agentjson.Dependencies{
					Resolver: agentjson.ResolverFunc(func(baked.Tier) []string {
						return []string{"provider/model"}
					}),
					Client: agentjson.ClientFunc(func(_ context.Context, _ agentjson.Request) error {
						return os.WriteFile(
							filepath.Join(workspace, verdictRelative),
							[]byte(test.verdictBody), 0o600,
						)
					}),
					NewID: func(prefix string) string { return prefix + "-id" },
				},
			}
			result, err := GateSession(context.Background(), GateInput{
				Workspace: workspace, UserPrompt: test.prompt,
				ParentSessionID: "parent", ContractPassed: test.contractPassed,
			}, deps)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != test.wantStatus ||
				result.Verdict == nil || result.Verdict.Verdict != test.wantVerdict {
				t.Fatalf("status=%q verdict=%#v, want status=%q verdict=%q",
					result.Status, result.Verdict, test.wantStatus, test.wantVerdict)
			}
			if test.wantVerdict == VerdictFail {
				found := false
				for _, blocker := range result.Verdict.Blockers {
					if strings.Contains(blocker.Detail, "done-criteria evidence incomplete") {
						found = true
					}
				}
				if !found {
					t.Fatalf("expected a done-criteria blocker, got %#v", result.Verdict.Blockers)
				}
			}
		})
	}
}

// TestEmptyDiffPassesOnlyWhenObligationsAreMet is the issue-#24 regression.
//
// "no changes to audit" is a legitimate skip when the run already satisfied its
// obligations — a passing acceptance contract (the no-behavioral-change
// deliverable the contract-cannot-fail path produces) or an audit that already
// accepted the tree. It was also reached when NOTHING had happened: a coder
// that wrote no code and no contract failed cycle 1 on the missing contract,
// the run resumed, cycle 2 found an empty diff with no blockers and no
// provenance, and the run terminated PASS having produced nothing at all.
func TestEmptyDiffPassesOnlyWhenObligationsAreMet(t *testing.T) {
	noDiffGit := ProcessRunnerFunc(func([]string, string) (ProcessResult, error) {
		return ProcessResult{}, nil
	})
	for _, test := range []struct {
		name           string
		contractPassed bool
		priorVerdict   *VerdictStatus
		wantStatus     GateStatus
	}{
		{
			// #24 itself: nothing delivered, nothing registered, nothing audited.
			name:       "nothing delivered fails instead of passing",
			wantStatus: StatusFail,
		},
		{
			// The stale / no-behavioral-change deliverable stays a pass.
			name:           "passing contract keeps the legitimate skip",
			contractPassed: true,
			wantStatus:     StatusSkipped,
		},
		{
			// An audit already accepted this tree; a later empty cycle is a skip.
			name:         "prior accepted audit keeps the legitimate skip",
			priorVerdict: verdictStatusPtr(VerdictPass),
			wantStatus:   StatusSkipped,
		},
		{
			// Resume must not upgrade a rejection into a pass: a prior FAIL
			// already suppressed the skip, and it must keep doing so.
			name:         "prior rejection is not upgraded to a pass",
			priorVerdict: verdictStatusPtr(VerdictFail),
			wantStatus:   StatusFail,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
			t.Setenv("CODEAF_ADMISSIBILITY", "0")
			workspace := t.TempDir()
			if test.priorVerdict != nil {
				if err := WriteAuditProvenance(workspace, AuditProvenance{
					AuditSHA: "deadbeef", AuditCycle: 1,
					Verdict: newVerdictOrdered(
						field("verdict", *test.priorVerdict),
						field("blockers", []Blocker{}),
					),
				}); err != nil {
					t.Fatal(err)
				}
			}
			dispatched := false
			cycle := 2.0
			result, err := GateSession(context.Background(), GateInput{
				Workspace: workspace, UserPrompt: "Create a file hello.txt containing exactly: hello engine",
				ParentSessionID: "p", AuditCycle: &cycle,
				ContractPassed: test.contractPassed,
			}, GateDependencies{
				Git: noDiffGit,
				Clauses: ClauseJudgerFunc(func(
					context.Context, string, string,
				) (specclauses.SpecClauseJudgment, error) {
					return specclauses.SpecClauseJudgment{
						Clauses: []string{"create hello.txt"}, Count: 1, Source: "fallback",
					}, nil
				}),
				AgentJSON: agentjson.Dependencies{
					Resolver: agentjson.ResolverFunc(func(baked.Tier) []string {
						return []string{"provider/model"}
					}),
					Client: agentjson.ClientFunc(func(context.Context, agentjson.Request) error {
						dispatched = true
						return nil
					}),
					NewID: func(prefix string) string { return prefix + "-id" },
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != test.wantStatus {
				t.Fatalf("status = %q, want %q (result=%#v)", result.Status, test.wantStatus, result)
			}
			if test.wantStatus == StatusSkipped && dispatched {
				t.Error("a legitimate skip must not dispatch the auditor")
			}
			if test.name == "nothing delivered fails instead of passing" {
				if dispatched {
					t.Error("the nothing-delivered fail must not cost an auditor call")
				}
				// A verdict must accompany the fail so the audit-fix loop
				// re-dispatches the coder instead of ending the run.
				if result.Verdict == nil || len(result.Verdict.Blockers) == 0 {
					t.Fatalf("expected an actionable verdict, got %#v", result.Verdict)
				}
				if !strings.Contains(result.Verdict.Blockers[0].Detail, "nothing was delivered") {
					t.Fatalf("blocker = %q", result.Verdict.Blockers[0].Detail)
				}
			}
		})
	}
}

func verdictStatusPtr(v VerdictStatus) *VerdictStatus { return &v }
