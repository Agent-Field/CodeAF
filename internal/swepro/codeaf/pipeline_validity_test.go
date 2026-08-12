// This file covers the validity and decision-ledger wiring from
// swe-pro/src/cli/cmd/run.ts:626-714,1055-1114,1611-1614,1717-1843,2285-2300.
package codeaf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/knobs"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/ledgers"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/validity"
)

type validityBackend struct {
	mu sync.Mutex

	intakeReply string
	staleReply  string
	onCoder     func(call int, request turn) error

	calls      []turn
	coderCalls int
	rootCalls  int
}

func (backend *validityBackend) Run(
	_ context.Context, request turn,
) (turnResult, error) {
	backend.mu.Lock()
	backend.calls = append(backend.calls, request)
	backend.mu.Unlock()

	switch request.Agent {
	case "input-classifier":
		if err := writeFile(
			filepath.Join(request.Workspace, ".codeaf", "plan", "classification.json"),
			`{"class":"trivial","reason":"no decomposition phases required"}`,
		); err != nil {
			return turnResult{}, err
		}
		return turnResult{Text: "classified"}, nil
	case "validity-judge":
		if strings.HasPrefix(request.Prompt, "# Staleness adjudication") {
			return turnResult{Text: backend.staleReply}, nil
		}
		return turnResult{Text: backend.intakeReply}, nil
	case "root-orchestrator":
		backend.mu.Lock()
		backend.rootCalls++
		backend.mu.Unlock()
		return turnResult{Text: "proceeded"}, nil
	case "coder":
		backend.mu.Lock()
		backend.coderCalls++
		call := backend.coderCalls
		backend.mu.Unlock()
		if backend.onCoder != nil {
			if err := backend.onCoder(call, request); err != nil {
				return turnResult{}, err
			}
		}
		return turnResult{Text: "coder completed"}, nil
	case "auditor", "auditor-light":
		reproduced := true
		notes := "verified"
		command := map[string]any{
			"cmd": "go test ./...", "exitCode": 0,
		}
		body, err := json.Marshal(map[string]any{
			"verdict":  "pass",
			"commands": []any{command},
			"notes":    notes,
			"step2_signal": map[string]any{
				"reproduced":            reproduced,
				"commands":              []any{command},
				"spec_examples_matched": true,
				"notes":                 notes,
			},
			"blockers":     []any{},
			"repair_hints": []any{},
		})
		if err != nil {
			return turnResult{}, err
		}
		if err := writeFile(
			filepath.Join(request.Workspace, ".codeaf", "auditor-verdict.json"),
			string(body),
		); err != nil {
			return turnResult{}, err
		}
		return turnResult{Text: "audit passed"}, nil
	default:
		return turnResult{}, fmt.Errorf(
			"validity backend has no script for agent %q", request.Agent,
		)
	}
}

func (backend *validityBackend) count(agent string) int {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	total := 0
	for _, call := range backend.calls {
		if call.Agent == agent {
			total++
		}
	}
	return total
}

func (backend *validityBackend) callsFor(agent string) []turn {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	out := []turn{}
	for _, call := range backend.calls {
		if call.Agent == agent {
			out = append(out, call)
		}
	}
	return out
}

func TestIntakeValidityHaltReturnsSuccessWithExactNotes(t *testing.T) {
	workspace := validityTestRepo(t)
	setValidityTestEnv(t, workspace)
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")

	backend := &validityBackend{intakeReply: validityJSON(
		"invalid", "high", "contradicts the documented contract",
		"do not implement",
	)}
	var stdout, stderr bytes.Buffer
	err := runCLI(
		context.Background(),
		[]string{
			"run", "--dir", workspace,
			"--high", "provider/high", "--frontier", "provider/frontier",
			"Implement the contradiction.",
		},
		backend,
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("halt-invalid returned an error instead of exit 0: %v", err)
	}
	if backend.count("validity-judge") != 1 {
		t.Fatalf("validity judge calls = %d, want 1", backend.count("validity-judge"))
	}
	if len(backend.calls) < 2 || backend.calls[0].Agent != "input-classifier" ||
		backend.calls[1].Agent != "validity-judge" {
		t.Fatalf("pre-root gate order = %v, want classifier then validity", backend.calls)
	}
	if tasks := plandb.GetPlanDB().ListTasks(nil); len(tasks) != 0 {
		t.Fatalf("validity halt bootstrapped root work: %#v", tasks)
	}
	if backend.count("coder") != 0 || backend.count("root-orchestrator") != 0 {
		t.Fatalf(
			"work dispatched after halt: coder=%d root=%d",
			backend.count("coder"), backend.count("root-orchestrator"),
		)
	}
	judge := backend.callsFor("validity-judge")[0]
	if judge.ModelID != "frontier" || judge.ProviderID != "provider" {
		t.Fatalf("judge model = %s/%s, want provider/frontier", judge.ProviderID, judge.ModelID)
	}

	wantNotes := strings.Join([]string{
		"[codeaf] intake validity gate: dispatching validity-judge",
		"[codeaf] intake validity: status=invalid confidence=high → halt-invalid",
		"[codeaf] Intake judged the issue clearly invalid (high confidence): contradicts the documented contract",
		"[codeaf] intake evidence: contradicts the documented contract",
		"[codeaf] halting run: issue judged clearly invalid — NOT dispatching the coder",
		"[codeaf] exiting (process.exit 0)",
		"",
	}, "\n")
	if stderr.String() != wantNotes {
		t.Fatalf("notes mismatch\nwant:\n%s\ngot:\n%s", wantNotes, stderr.String())
	}
	var terminal event
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		var value event
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			t.Fatalf("decode event %q: %v", line, err)
		}
		if value.Type == "terminal" {
			terminal = value
		}
	}
	if terminal.Status != "refused" || terminal.Message != "issue judged clearly invalid" {
		t.Fatalf("terminal event = %#v, want successful refusal distinct from pass", terminal)
	}

	rows := readDecisionLines(t, workspace)
	if len(rows) != 1 {
		t.Fatalf("decision rows = %d, want 1:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	var decision ledgers.DecisionRecord
	if err := json.Unmarshal([]byte(rows[0]), &decision); err != nil {
		t.Fatal(err)
	}
	if decision.Kind != ledgers.DecisionTriage ||
		decision.TaskID != "intake-validity" ||
		decision.Model != "high" ||
		decision.Band != "unknown" ||
		decision.ReliableBand != "unknown" ||
		decision.Chosen != "halt-invalid" ||
		decision.Reason != "contradicts the documented contract" ||
		decision.KnobsHash != "" {
		t.Fatalf("intake decision = %+v", decision)
	}
}

func TestIntakeValidityUnparseableProceedsNormally(t *testing.T) {
	workspace := validityTestRepo(t)
	setValidityTestEnv(t, workspace)
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")
	t.Setenv("CODEAF_PRE_GATES", "0")
	t.Setenv("CODEAF_AUDITOR", "0")

	backend := &validityBackend{intakeReply: "not json"}
	var events, notes bytes.Buffer
	args := mustValidityArgs(t, workspace)
	runner := newPipeline(args, workspace, pipelineDeps{
		Backend: backend, Events: newEventWriter(&events), Notes: &notes,
	})
	result, err := runner.run(context.Background(), args.Message, pipelineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "pass" || backend.rootCalls != 1 {
		t.Fatalf("result=%+v root calls=%d", result, backend.rootCalls)
	}
	want := "[codeaf] intake validity gate: dispatching validity-judge\n" +
		"[codeaf] intake validity: no parseable verdict — proceeding normally\n" +
		// The pre-run photograph is taken after the validity gate and before
		// anything is written — see baseline.go. A green repository photographs
		// green, and the verification below is judged as a delta against it.
		"[codeaf] baseline build: make build — green\n" +
		"[codeaf] baseline test: make test — green\n" +
		"[codeaf] full verification build: make build (exit=0, source=Makefile#build)\n" +
		"[codeaf] full verification test: make test (exit=0, source=Makefile#test)\n"
	if notes.String() != want {
		t.Fatalf("notes = %q, want %q", notes.String(), want)
	}
	if rows := readDecisionLines(t, workspace); len(rows) != 0 {
		t.Fatalf("unparseable verdict wrote decisions: %v", rows)
	}
	judge := backend.callsFor("validity-judge")[0]
	if judge.ModelID != "high" {
		t.Fatalf("empty frontier should fall back to HIGH, got %q", judge.ModelID)
	}
}

func TestContractStalenessRepromptsOnceAndWritesGoldenDecisions(t *testing.T) {
	workspace := validityTestRepo(t)
	setValidityTestEnv(t, workspace)
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
	t.Setenv("CODEAF_ADMISSIBILITY", "0")
	t.Setenv("CODEAF_AUDITOR_MAX_ATTEMPTS", "1")
	t.Setenv("CODEAF_CONTRACT", "1")

	staleEvidence := "the registered reproduction passes on the base tree"
	staleDeliverable := "report + regression test only, no behavioral change"
	staleVerdict := validity.ValidityVerdict{
		Status: validity.StatusStale, Confidence: validity.ConfidenceHigh,
		Evidence: staleEvidence, RecommendedDeliverable: staleDeliverable,
	}
	backend := &validityBackend{
		intakeReply: validityJSON(
			"valid", "high", "actionable as written", "implement normally",
		),
		staleReply: validityJSON(
			"stale", "high", staleEvidence, staleDeliverable,
		),
	}
	backend.onCoder = func(call int, request turn) error {
		switch call {
		case 1:
			if err := writeFile(
				filepath.Join(workspace, "changed.txt"), "implementation\n",
			); err != nil {
				return err
			}
			if err := writeFile(
				filepath.Join(workspace, "contract-check.sh"),
				"#!/usr/bin/env bash\nexit 0\n",
			); err != nil {
				return err
			}
			return writeFile(
				filepath.Join(workspace, ".codeaf", "contract.json"),
				`{"command":"bash contract-check.sh","paths":["contract-check.sh"]}`,
			)
		case 2:
			if request.Prompt != validity.StaleReportContextBlock(staleVerdict) {
				return fmt.Errorf("stale re-prompt mismatch:\n%s", request.Prompt)
			}
			return writeFile(
				filepath.Join(workspace, "regression.txt"), "guard current behavior\n",
			)
		default:
			return fmt.Errorf("unexpected coder call %d", call)
		}
	}

	fixedNow := time.UnixMilli(1_700_000_000_123)
	var events, notes bytes.Buffer
	args := mustValidityArgs(t, workspace)
	args.Frontier = "provider/frontier"
	runner := newPipeline(args, workspace, pipelineDeps{
		Backend: backend, Events: newEventWriter(&events), Notes: &notes,
		Now: func() time.Time {
			return fixedNow
		},
	})
	result, err := runner.run(context.Background(), args.Message, pipelineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "pass" {
		t.Fatalf("result = %+v\nevents:\n%s\nnotes:\n%s", result, events.String(), notes.String())
	}
	if !runner.rootCutLeaf || runner.rootCutBand != "xs" {
		t.Fatalf("root cut = %v band=%q, want true/xs", runner.rootCutLeaf, runner.rootCutBand)
	}
	if backend.coderCalls != 2 {
		t.Fatalf("coder calls = %d, want initial + one stale re-prompt", backend.coderCalls)
	}
	if backend.count("validity-judge") != 2 {
		t.Fatalf("judge calls = %d, want intake + staleness", backend.count("validity-judge"))
	}
	if got := strings.Count(notes.String(), "re-prompting the coder once"); got != 1 {
		t.Fatalf("stale re-prompt note count = %d:\n%s", got, notes.String())
	}
	for _, fragment := range []string{
		"[codeaf] contract-cannot-fail: contract PASSES at base too — the acceptance check never failed anywhere; adjudicating staleness\n",
		"[codeaf] staleness verdict: status=stale confidence=high → stale-report\n",
		"[codeaf] Contract could not fail and the issue is judged stale (high confidence): switching to report + regression test, no behavioral change.\n",
	} {
		if !strings.Contains(notes.String(), fragment) {
			t.Errorf("notes missing %q:\n%s", fragment, notes.String())
		}
	}

	rows := readDecisionLines(t, workspace)
	if len(rows) != 3 {
		t.Fatalf("decision rows = %d, want 3:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	knobsHash := knobs.KnobsSnapshotHash(knobs.ResolveProjectKnobs(workspace, nil).Values)
	wantRows := []string{
		`{"ts":1700000000123,"kind":"triage","taskID":"intake-validity","model":"high","band":"xs","reliableBand":"unknown","chosen":"normal","reason":"actionable as written","knobsHash":""}`,
		`{"ts":1700000000123,"kind":"triage","taskID":"contract-staleness","model":"high","band":"xs","reliableBand":"unknown","chosen":"stale-report","reason":"the registered reproduction passes on the base tree","knobsHash":""}`,
		`{"ts":1700000000123,"kind":"convergence","taskID":"session-audit","model":"high","band":"xs","reliableBand":"unknown","chosen":"continue","reason":"n/a: latest cycle has no blockers","knobsHash":"` + knobsHash + `"}`,
	}
	for index := range wantRows {
		if rows[index] != wantRows[index] {
			t.Errorf(
				"decision row %d mismatch\nwant: %s\n got: %s",
				index, wantRows[index], rows[index],
			)
		}
	}
}

func TestRootCutPromptRequiresAndEnforcesAcceptanceContract(t *testing.T) {
	// Validation contracts 5-6: the root-cut user prompt includes the complete
	// DoD/contract/red-green/fact/script brief, and a missing contract cannot be
	// bypassed by an auditor pass.
	workspace := validityTestRepo(t)
	setValidityTestEnv(t, workspace)
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
	t.Setenv("CODEAF_CONTRACT", "1")
	t.Setenv("CODEAF_ADMISSIBILITY", "0")
	t.Setenv("CODEAF_TAMPER", "0")
	t.Setenv("CODEAF_SPEC_IDS", "0")
	t.Setenv("CODEAF_GLOSSARY", "0")

	backend := &validityBackend{intakeReply: validityJSON(
		"valid", "high", "actionable as written", "implement normally",
	)}
	backend.onCoder = func(_ int, request turn) error {
		for _, fragment := range []string{
			"Fix x.\n\n## Definition of done (root-cut fast path)",
			"## Acceptance contract (required first step)",
			"`.codeaf/contract.json`",
			"confirm it FAILS (red)",
			"same command PASSES (green)",
			// The two path lists and the DON'T that names the exact failure
			// from issue #22: a deliverable listed in `paths` is copied into
			// the base checkout and cancels the task as already-done.
			"\"asserted_paths\": [\"<file(s) the check asserts ABOUT>\"]",
			"file(s) the CHECK ITSELF lives in",
			"DON'T: for \"create a file hello.txt containing X\"",
			"`\"paths\": [\"test-hello.sh\"], \"asserted_paths\": [\"hello.txt\"]`",
			"## Repo facts (precomputed — do not re-derive)",
			"## Emit a script (one turn) instead of many tool calls",
		} {
			if !strings.Contains(request.Prompt, fragment) {
				return fmt.Errorf("root-cut prompt missing %q:\n%s", fragment, request.Prompt)
			}
		}
		return nil
	}

	var events, notes bytes.Buffer
	args := mustValidityArgs(t, workspace)
	runner := newPipeline(args, workspace, pipelineDeps{
		Backend: backend, Events: newEventWriter(&events), Notes: &notes,
	})
	result, err := runner.run(context.Background(), args.Message, pipelineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "fail" || !runner.rootCutLeaf {
		t.Fatalf("missing-contract result = %+v, root-cut=%v", result, runner.rootCutLeaf)
	}
	if backend.count("auditor") != 0 || backend.count("auditor-light") != 0 {
		t.Fatalf("missing contract reached auditor: calls=%#v", backend.calls)
	}
	for _, fragment := range []string{
		"acceptance contract MISSING OR INVALID (.codeaf/contract.json is required)",
		"acceptance contract failing — skipping audit LLM, routing to repair",
	} {
		if !strings.Contains(notes.String(), fragment) {
			t.Errorf("notes missing %q:\n%s", fragment, notes.String())
		}
	}
}

func TestDecisionEvidenceUsesJavaScriptUTF16Slice(t *testing.T) {
	workspace := t.TempDir()
	fixedNow := time.UnixMilli(1_700_000_000_123)
	args := cliArgs{High: "provider/high"}
	runner := newPipeline(args, workspace, pipelineDeps{
		Backend: &validityBackend{}, Events: newEventWriter(&bytes.Buffer{}),
		Now: func() time.Time {
			return fixedNow
		},
	})
	runner.appendDecision(
		ledgers.DecisionTriage,
		"intake-validity",
		"halt-invalid",
		prefixUTF16(strings.Repeat("x", 199)+"😀tail", 200),
		"",
	)
	rows := readDecisionLines(t, workspace)
	if len(rows) != 1 {
		t.Fatalf("decision rows = %v", rows)
	}
	wantReason := `"reason":"` + strings.Repeat("x", 199) + `\ud83d"`
	if !strings.Contains(rows[0], wantReason) {
		t.Fatalf("UTF-16 split surrogate was not preserved:\n%s", rows[0])
	}
}

func validityJSON(status, confidence, evidence, deliverable string) string {
	raw, _ := json.Marshal(map[string]string{
		"status": status, "confidence": confidence, "evidence": evidence,
		"recommendedDeliverable": deliverable,
	})
	return string(raw)
}

func validityTestRepo(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	if err := gitRun(workspace, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "config", "user.name", "codeaf-validity"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "config", "user.email", "codeaf@example.test"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "README.md"), "base\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "Makefile"), "build:\n\t@true\n\ntest:\n\t@true\n"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "add", "README.md", "Makefile"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "commit", "-m", "base"); err != nil {
		t.Fatal(err)
	}
	return workspace
}

func setValidityTestEnv(t *testing.T, workspace string) {
	t.Helper()
	t.Setenv("PLANDB_DB", filepath.Join(workspace, ".plandb.db"))
	t.Setenv("CODEAF_VALIDITY", "1")
	t.Setenv("CODEAF_AUTORESUME", "0")
	t.Setenv("CODEAF_PLANDB_PERSIST", "0")
	t.Setenv("CODEAF_OUTCOME_CACHE", "0")
	t.Setenv("CODEAF_REVIEW_REPAIR_CAP", "0")
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)
}

func mustValidityArgs(t *testing.T, workspace string) cliArgs {
	t.Helper()
	args, err := parseArgs([]string{
		"run", "--dir", workspace, "--high", "provider/high", "Fix x.",
	})
	if err != nil {
		t.Fatal(err)
	}
	return args
}

func readDecisionLines(t *testing.T, workspace string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(workspace, ".codeaf", "decisions.jsonl"))
	if os.IsNotExist(err) {
		return []string{}
	}
	if err != nil {
		t.Fatal(err)
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return []string{}
	}
	return strings.Split(trimmed, "\n")
}
