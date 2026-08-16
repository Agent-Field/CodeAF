// This file covers the I/O shell and bounded orchestration ported from
// src/session/review-gate.ts:247-395 and 616-1937.
package reviewgate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/baked"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/agentjson"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/scheduler"
)

type recordingPlanDB struct {
	mu       sync.Mutex
	commands [][]string
	reviews  int
	repairs  int
}

func (p *recordingPlanDB) Run(argv []string) plandb.RunResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.commands = append(p.commands, append([]string(nil), argv...))
	if len(argv) >= 3 && argv[0] == "plandb" && argv[1] == "add" {
		id := ""
		if strings.HasPrefix(argv[2], "Review #") {
			p.reviews++
			id = "review-" + strconvInt(p.reviews)
		} else if strings.HasPrefix(argv[2], "Repair #") {
			p.repairs++
			id = "repair-" + strconvInt(p.repairs)
		}
		raw, _ := jscompat.Stringify(struct {
			ID string `json:"id"`
		}{ID: id})
		return plandb.RunResult{Stdout: raw}
	}
	return plandb.RunResult{Stdout: []byte("{}")}
}

func (p *recordingPlanDB) snapshot() [][]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([][]string, len(p.commands))
	for index, command := range p.commands {
		out[index] = append([]string(nil), command...)
	}
	return out
}

type scriptedAgent struct {
	mu sync.Mutex

	requests       []agentjson.Request
	reviews        []ReviewVerdict
	reviewIndex    int
	retry          *RetryAdviceDecision
	issue          *IssueAdvisorDecision
	auditor        json.RawMessage
	synth          *SynthesizerDecision
	repairMutation func(worktree string) error
	blockRepair    bool
}

func (a *scriptedAgent) Run(ctx context.Context, request agentjson.Request) error {
	a.mu.Lock()
	a.requests = append(a.requests, request)
	a.mu.Unlock()
	switch request.Agent {
	case "superpowers-code-reviewer":
		a.mu.Lock()
		index := a.reviewIndex
		a.reviewIndex++
		if index >= len(a.reviews) {
			a.mu.Unlock()
			return errors.New("no scripted review verdict")
		}
		verdict := a.reviews[index]
		a.mu.Unlock()
		return writeAgentOutput(request, verdict)

	case "retry-advisor":
		if a.retry == nil {
			return errors.New("no retry decision")
		}
		return writeAgentOutput(request, *a.retry)

	case "issue-advisor":
		if a.issue == nil {
			return errors.New("no issue decision")
		}
		return writeAgentOutput(request, *a.issue)

	case "auditor":
		if len(a.auditor) == 0 {
			return errors.New("no auditor verdict")
		}
		return writeAgentRaw(request, a.auditor)

	case "gate-synthesizer":
		if a.synth == nil {
			return errors.New("no synth decision")
		}
		return writeAgentOutput(request, *a.synth)

	case "fixer":
		if a.blockRepair {
			<-ctx.Done()
			return ctx.Err()
		}
		if a.repairMutation != nil {
			return a.repairMutation(request.Workspace)
		}
	}
	return nil
}

func (a *scriptedAgent) snapshot() []agentjson.Request {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]agentjson.Request(nil), a.requests...)
}

func writeAgentOutput(request agentjson.Request, value any) error {
	raw, err := jscompat.Stringify(value)
	if err != nil {
		return err
	}
	return writeAgentRaw(request, raw)
}

func writeAgentRaw(request agentjson.Request, raw []byte) error {
	outputPath := ""
	for _, line := range strings.Split(request.Reminder, "\n") {
		if strings.HasPrefix(line, "Output file: ") {
			outputPath = strings.TrimPrefix(line, "Output file: ")
			break
		}
	}
	if outputPath == "" {
		return errors.New("output path missing from reminder")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o777); err != nil {
		return err
	}
	return os.WriteFile(outputPath, raw, 0o666)
}

func agentDeps(client agentjson.Client) agentjson.Dependencies {
	counter := 0
	return agentjson.Dependencies{
		Resolver: agentjson.ResolverFunc(func(baked.Tier) []string {
			return []string{"openai/test-model"}
		}),
		Client: client,
		NewID: func(prefix string) string {
			counter++
			return prefix + "-" + strconvInt(counter)
		},
	}
}

func passReview() ReviewVerdict {
	return ReviewVerdict{
		Verdict: "pass", Confidence: "high", Done: true,
		SpecCoverage: "all criteria met", Bugs: []ReviewBug{},
		RepairHints: []string{}, Evidence: "go test ./... exited 0",
	}
}

func failReview() ReviewVerdict {
	line := 2.0
	return ReviewVerdict{
		Verdict: "fail", Confidence: "high", Done: false,
		SpecCoverage: "criterion B missed",
		Bugs: []ReviewBug{{
			File: "app.go", Line: &line, Severity: "blocker",
			Detail: "wrong value",
		}},
		RepairHints: []string{"write the expected value"},
		Evidence:    "go test ./... exited 1",
	}
}

func newGitRepo(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	gitRun(t, root, "init", "-b", "main")
	gitRun(t, root, "config", "user.name", "Test")
	gitRun(t, root, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package app\nconst Value = 1\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-m", "base")
	base := strings.TrimSpace(gitRun(t, root, "rev-parse", "HEAD"))
	return root, base
}

func gitRun(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = cwd
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func modifyApp(t *testing.T, root string, value int) {
	t.Helper()
	content := "package app\nconst Value = " + strconvInt(value) + "\n"
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte(content), 0o666); err != nil {
		t.Fatal(err)
	}
}

func gateInput(root, base string, task *plandb.Task) scheduler.GateInput {
	return scheduler.GateInput{
		Workspace: root, DBPath: "/ignored", ProjectID: "project",
		RootTaskID: "root", RootExcerpt: "Set Value to 2.", Task: task,
		WorktreePath: root, Branch: "main", BaseSHA: base,
		Summary: "Changed Value.", ParentSessionID: "parent",
		SubagentType: "fixer",
	}
}

func codeTask(tags ...string) *plandb.Task {
	description := strings.Join([]string{
		"Implement the requested value.",
		"file_scope: app.go",
		"acceptance: Value is correct",
		"task_role: code",
	}, "\n")
	return &plandb.Task{
		ID: "impl", Title: "Change Value", Description: &description,
		Kind: "code", Tags: tags,
	}
}

func TestShouldSkipReviewUsesStagedDirtyAndUntrackedFiles(t *testing.T) {
	t.Run("no changes", func(t *testing.T) {
		root, base := newGitRepo(t)
		got := ShouldSkipReview(context.Background(), root, base, fixtureConfig(), nil)
		if !got.Skip || got.Reason != "no_changes" {
			t.Fatalf("got %#v", got)
		}
	})
	t.Run("docs only", func(t *testing.T) {
		root, base := newGitRepo(t)
		if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("docs\n"), 0o666); err != nil {
			t.Fatal(err)
		}
		got := ShouldSkipReview(context.Background(), root, base, fixtureConfig(), nil)
		if !got.Skip || got.Reason != "docs_only" {
			t.Fatalf("got %#v", got)
		}
	})
	t.Run("source change", func(t *testing.T) {
		root, base := newGitRepo(t)
		modifyApp(t, root, 2)
		got := ShouldSkipReview(context.Background(), root, base, fixtureConfig(), nil)
		if got.Skip {
			t.Fatalf("got %#v", got)
		}
	})
}

func TestAllocateGateWorktreeDistinguishesMissingRefFromKilledGit(t *testing.T) {
	root, _ := newGitRepo(t)
	service := New(Dependencies{})

	missing, err := service.allocateGateWorktree(
		context.Background(), root, "missing", "refs/heads/does-not-exist",
	)
	if missing != nil || err == nil {
		t.Fatalf("missing ref allocation = %#v, %v; want nil worktree and error", missing, err)
	}
	if !strings.Contains(err.Error(), "rev-parse refs/heads/does-not-exist") ||
		!strings.Contains(err.Error(), "code=128") {
		t.Fatalf("missing ref error does not preserve git failure: %q", err)
	}

	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	worktree, err := service.allocateGateWorktree(expired, root, "expired", "main")
	if err != nil || worktree == nil {
		t.Fatalf("valid ref allocation under expired context = %#v, %v", worktree, err)
	}
	if worktree.BaseSHA == "" || worktree.Branch != "plandb/expired" {
		t.Fatalf("expired-context allocation = %#v", worktree)
	}
	service.cleanupWorktree(context.Background(), root, worktree.Path, worktree.Branch)
}

func TestFabricatedAdvisorDecisionsAreReportedUnauthored(t *testing.T) {
	root := t.TempDir()
	fabricated := New(Dependencies{AgentJSON: agentDeps(&scriptedAgent{})})
	retry, retryAuthored := fabricated.dispatchRetryAdvisor(context.Background(), RetryAdvisorInput{
		TaskID: "impl", Workspace: root, ParentSessionID: "parent",
	})
	if retry.Action != "escalate_to_advisor" || retryAuthored {
		t.Fatalf("fabricated retry decision = %#v, authored=%t", retry, retryAuthored)
	}
	issue, issueAuthored := fabricated.dispatchIssueAdvisor(context.Background(), IssueAdvisorInput{
		TaskID: "impl", Workspace: root, ParentSessionID: "parent",
	})
	if issue.Action != "escalate_to_replan" || issueAuthored {
		t.Fatalf("fabricated issue decision = %#v, authored=%t", issue, issueAuthored)
	}

	hint := "change the implementation"
	blocker := "the reviewer found a concrete defect"
	parsedClient := &scriptedAgent{
		retry: &RetryAdviceDecision{
			Action: "retry_with_hint", Reason: "the defect is localized", StrategyHint: &hint,
		},
		issue: &IssueAdvisorDecision{
			Action: "escalate_to_replan", Reason: "the defect blocks delivery", Blocker: &blocker,
		},
	}
	parsed := New(Dependencies{AgentJSON: agentDeps(parsedClient)})
	retry, retryAuthored = parsed.dispatchRetryAdvisor(context.Background(), RetryAdvisorInput{
		TaskID: "impl", Workspace: root, ParentSessionID: "parent",
	})
	if retry.Action != "retry_with_hint" || !retryAuthored {
		t.Fatalf("parsed retry decision = %#v, authored=%t", retry, retryAuthored)
	}
	issue, issueAuthored = parsed.dispatchIssueAdvisor(context.Background(), IssueAdvisorInput{
		TaskID: "impl", Workspace: root, ParentSessionID: "parent",
	})
	if issue.Action != "escalate_to_replan" || !issueAuthored {
		t.Fatalf("parsed issue decision = %#v, authored=%t", issue, issueAuthored)
	}
}

func TestGateThatCannotAdjudicateIsNotAQualityFailure(t *testing.T) {
	const extractionFailure = "verdict extraction: reviewer did not write .codeaf/review-verdict.json AND no parseable verdict JSON in the reviewer's prose. The reviewer is responsible for writing the verdict file — see superpowers-code-reviewer.md."
	hint := "try repair"
	tests := []struct {
		name                  string
		config                func(*Config)
		client                func() *scriptedAgent
		wantStatus            scheduler.GateStatus
		wantReason            string
		wantExtractionFailure bool
		wantUnadjudicated     bool
	}{
		{
			name: "reviewer errors",
			config: func(config *Config) {
				config.RepairCap = 0
			},
			client: func() *scriptedAgent {
				blocker := "review output unavailable"
				return &scriptedAgent{issue: &IssueAdvisorDecision{
					Action: "escalate_to_replan", Reason: "cannot assess", Blocker: &blocker,
				}}
			},
			wantStatus:            scheduler.GateEscalated,
			wantReason:            "advisor:escalate_to_replan — review output unavailable",
			wantExtractionFailure: true,
			wantUnadjudicated:     true,
		},
		{
			name: "repair dispatch blocked",
			config: func(config *Config) {
				config.RepairCap = 1
				config.TimeoutMS = 10
			},
			client: func() *scriptedAgent {
				return &scriptedAgent{
					reviews: []ReviewVerdict{failReview()},
					retry: &RetryAdviceDecision{
						Action: "retry_with_hint", Reason: "retry", StrategyHint: &hint,
					},
					blockRepair: true,
				}
			},
			wantStatus:        scheduler.GateFail,
			wantReason:        "repair dispatch failed: repair errored or timed out: context deadline exceeded",
			wantUnadjudicated: true,
		},
		{
			name: "advisors error",
			config: func(config *Config) {
				config.RepairCap = 1
			},
			client: func() *scriptedAgent {
				return &scriptedAgent{reviews: []ReviewVerdict{failReview()}}
			},
			wantStatus: scheduler.GateEscalated,
			wantReason: "advisor:escalate_to_replan — advisor failed to emit a valid decision; " +
				"treat as unresolvable at the leaf level and consider broader replanning",
			wantUnadjudicated: true,
		},
		{
			name: "real reviewer rejection",
			config: func(config *Config) {
				config.RepairCap = 0
			},
			client: func() *scriptedAgent {
				blocker := "reviewer found a real defect"
				return &scriptedAgent{
					reviews: []ReviewVerdict{failReview()},
					issue: &IssueAdvisorDecision{
						Action: "escalate_to_replan", Reason: "quality rejection", Blocker: &blocker,
					},
				}
			},
			wantStatus:        scheduler.GateEscalated,
			wantReason:        "advisor:escalate_to_replan — reviewer found a real defect",
			wantUnadjudicated: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, base := newGitRepo(t)
			modifyApp(t, root, 2)
			config := fixtureConfig()
			test.config(&config)
			service := New(Dependencies{
				Config: &config, PlanDB: &recordingPlanDB{},
				AgentJSON: agentDeps(test.client()),
			})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			result, err := service.GateLeaf(ctx, gateInput(root, base, codeTask()))
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != test.wantStatus || result.Reason != test.wantReason ||
				result.Unadjudicated != test.wantUnadjudicated {
				t.Fatalf("result = %#v", result)
			}
			if test.wantExtractionFailure {
				if result.Verdict == nil || len(result.Verdict.Bugs) != 1 ||
					result.Verdict.Bugs[0].Detail != extractionFailure {
					t.Fatalf("fabricated reviewer verdict changed bytes: %#v", result.Verdict)
				}
			}
		})
	}
}

func TestGatePassDispatchesStructuredReviewerAndCleansReviewWorktree(t *testing.T) {
	root, base := newGitRepo(t)
	modifyApp(t, root, 2)
	planDB := &recordingPlanDB{}
	client := &scriptedAgent{reviews: []ReviewVerdict{passReview()}}
	service := New(Dependencies{
		Config: func() *Config { value := fixtureConfig(); return &value }(),
		PlanDB: planDB, AgentJSON: agentDeps(client),
	})

	result, err := service.GateLeaf(
		context.Background(), gateInput(root, base, codeTask()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != scheduler.GatePass || result.RepairAttempts != 0 ||
		result.FinalWorktreePath != root || result.FinalBranch != "main" {
		t.Fatalf("result = %#v", result)
	}
	if result.Done == nil || !*result.Done || result.Verdict == nil ||
		result.Verdict.Verdict != scheduler.VerdictPass {
		t.Fatalf("verdict = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(root, ".plandb", "wt-review-1")); !os.IsNotExist(err) {
		t.Fatalf("review worktree survived cleanup: %v", err)
	}
	branches := gitRun(t, root, "branch", "--list", "plandb/review-1")
	if strings.TrimSpace(branches) != "" {
		t.Fatalf("review branch survived: %q", branches)
	}
	requests := client.snapshot()
	if len(requests) != 1 || requests[0].Agent != "superpowers-code-reviewer" {
		t.Fatalf("requests = %#v", requests)
	}
	if !strings.Contains(requests[0].TaskPrompt, "# Diff (base..HEAD)") ||
		!strings.Contains(requests[0].TaskPrompt, "+const Value = 2") {
		t.Fatalf("review prompt missing diff:\n%s", requests[0].TaskPrompt)
	}
}

func TestGateRepairReturnsRepairBranchAndCommitsMutation(t *testing.T) {
	root, base := newGitRepo(t)
	modifyApp(t, root, 2)
	planDB := &recordingPlanDB{}
	hint := "Change the constant and rerun tests."
	client := &scriptedAgent{
		reviews: []ReviewVerdict{failReview(), passReview()},
		retry: &RetryAdviceDecision{
			Action: "retry_with_hint", Reason: "localized failure",
			StrategyHint: &hint,
		},
		repairMutation: func(worktree string) error {
			return os.WriteFile(
				filepath.Join(worktree, "app.go"),
				[]byte("package app\nconst Value = 3\n"),
				0o666,
			)
		},
	}
	config := fixtureConfig()
	config.RepairCap = 1
	service := New(Dependencies{
		Config: &config, PlanDB: planDB, AgentJSON: agentDeps(client),
	})

	result, err := service.GateLeaf(
		context.Background(), gateInput(root, base, codeTask()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != scheduler.GatePass || result.RepairAttempts != 1 ||
		result.FinalBranch != "plandb/repair-1" {
		t.Fatalf("result = %#v", result)
	}
	if result.FinalWorktreePath != filepath.Join(root, ".plandb", "wt-repair-1") {
		t.Fatalf("final worktree = %q", result.FinalWorktreePath)
	}
	content, err := os.ReadFile(filepath.Join(result.FinalWorktreePath, "app.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "package app\nconst Value = 3\n" {
		t.Fatalf("repair content = %q", content)
	}
	status := gitRun(t, result.FinalWorktreePath, "status", "--porcelain")
	if strings.TrimSpace(status) != "" {
		t.Fatalf("repair worktree dirty: %q", status)
	}
	tree := gitRun(t, result.FinalWorktreePath, "ls-tree", "-r", "--name-only", "HEAD")
	if strings.Contains(tree, ".codeaf") {
		t.Fatalf("internal verdict artifact committed:\n%s", tree)
	}
	requests := client.snapshot()
	agents := make([]string, len(requests))
	for index, request := range requests {
		agents[index] = request.Agent
	}
	want := []string{
		"superpowers-code-reviewer", "retry-advisor", "fixer",
		"superpowers-code-reviewer",
	}
	if strings.Join(agents, ",") != strings.Join(want, ",") {
		t.Fatalf("agents = %v, want %v", agents, want)
	}
	if strings.Contains(requests[2].TaskPrompt, hint) {
		t.Fatalf("kept source bug changed: retry hint unexpectedly reached repair prompt")
	}
	commands := planDB.snapshot()
	foundHint := false
	for _, command := range commands {
		if strings.Contains(strings.Join(command, "\x00"), "STRATEGY HINT (retry-advisor): "+hint) {
			foundHint = true
		}
	}
	if !foundHint {
		t.Fatal("retry-advisor hint was not persisted")
	}
	t.Cleanup(func() {
		command := exec.Command("git", "worktree", "remove", "--force", result.FinalWorktreePath)
		command.Dir = root
		_ = command.Run()
		command = exec.Command("git", "branch", "-D", result.FinalBranch)
		command.Dir = root
		_ = command.Run()
	})
}

func TestSupersededRepairWorktreeIsReaped(t *testing.T) {
	root, base := newGitRepo(t)
	modifyApp(t, root, 2)
	hint := "make another concrete edit"
	repairValue := 2
	client := &scriptedAgent{
		reviews: []ReviewVerdict{failReview(), failReview(), passReview()},
		retry: &RetryAdviceDecision{
			Action: "retry_with_hint", Reason: "localized failure", StrategyHint: &hint,
		},
		repairMutation: func(worktree string) error {
			repairValue++
			content := "package app\nconst Value = " + strconvInt(repairValue) + "\n"
			return os.WriteFile(filepath.Join(worktree, "app.go"), []byte(content), 0o666)
		},
	}
	config := fixtureConfig()
	config.RepairCap = 2
	service := New(Dependencies{
		Config: &config, PlanDB: &recordingPlanDB{}, AgentJSON: agentDeps(client),
	})

	result, err := service.GateLeaf(context.Background(), gateInput(root, base, codeTask()))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != scheduler.GatePass || result.RepairAttempts != 2 {
		t.Fatalf("result = %#v", result)
	}
	predecessor := filepath.Join(root, ".plandb", "wt-repair-1")
	if _, err := os.Stat(predecessor); !os.IsNotExist(err) {
		t.Fatalf("superseded repair worktree survived: %v", err)
	}
	if branch := strings.TrimSpace(gitRun(t, root, "branch", "--list", "plandb/repair-1")); branch != "" {
		t.Fatalf("superseded repair branch survived: %q", branch)
	}
	if _, err := os.Stat(filepath.Join(root, "app.go")); err != nil {
		t.Fatalf("caller implementation worktree was reaped: %v", err)
	}
	if _, err := os.Stat(result.FinalWorktreePath); err != nil {
		t.Fatalf("incoming final repair worktree was reaped: %v", err)
	}
	t.Cleanup(func() {
		service.cleanupWorktree(context.Background(), root, result.FinalWorktreePath, result.FinalBranch)
	})
}

func TestRepairTimeoutDegradesThroughSingleFailureBranch(t *testing.T) {
	root, base := newGitRepo(t)
	modifyApp(t, root, 2)
	hint := "try repair"
	client := &scriptedAgent{
		reviews: []ReviewVerdict{failReview()},
		retry: &RetryAdviceDecision{
			Action: "retry_with_hint", Reason: "retry",
			StrategyHint: &hint,
		},
		blockRepair: true,
	}
	config := fixtureConfig()
	config.RepairCap = 1
	config.TimeoutMS = 10
	service := New(Dependencies{
		Config: &config, PlanDB: &recordingPlanDB{},
		AgentJSON: agentDeps(client),
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := service.GateLeaf(ctx, gateInput(root, base, codeTask()))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != scheduler.GateFail ||
		result.Reason != "repair dispatch failed: repair errored or timed out: context deadline exceeded" {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(root, ".plandb", "wt-repair-1")); !os.IsNotExist(err) {
		t.Fatalf("timed-out repair worktree survived cleanup: %v", err)
	}
	if branch := strings.TrimSpace(gitRun(t, root, "branch", "--list", "plandb/repair-1")); branch != "" {
		t.Fatalf("timed-out repair branch survived: %q", branch)
	}
}

func TestHighRiskPathRunsReviewerAuditorThenSynthesizer(t *testing.T) {
	root, base := newGitRepo(t)
	modifyApp(t, root, 2)
	client := &scriptedAgent{
		reviews: []ReviewVerdict{passReview()},
		auditor: json.RawMessage(`{"verdict":"pass","blockers":[],"repair_hints":[]}`),
		synth: &SynthesizerDecision{
			Verdict: "pass", Reason: "both agents passed",
			Blockers: nil, RepairHints: nil, Confidence: "high",
		},
	}
	service := New(Dependencies{
		Config: func() *Config { value := fixtureConfig(); return &value }(),
		PlanDB: &recordingPlanDB{}, AgentJSON: agentDeps(client),
	})
	result, err := service.GateLeaf(
		context.Background(), gateInput(root, base, codeTask("risk:high")),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != scheduler.GatePass || result.Verdict == nil ||
		!strings.Contains(result.Verdict.Evidence, "synthesized from reviewer+auditor") {
		t.Fatalf("result = %#v", result)
	}
	requests := client.snapshot()
	agents := make([]string, len(requests))
	for index, request := range requests {
		agents[index] = request.Agent
	}
	want := []string{"superpowers-code-reviewer", "auditor", "gate-synthesizer"}
	if strings.Join(agents, ",") != strings.Join(want, ",") {
		t.Fatalf("agents = %v, want %v", agents, want)
	}
}

func TestIdenticalRepairDiffEscalatesBeforeSecondReview(t *testing.T) {
	root, base := newGitRepo(t)
	modifyApp(t, root, 2)
	hint := "retry without a concrete edit"
	blocker := "repair made no semantic progress"
	client := &scriptedAgent{
		reviews: []ReviewVerdict{failReview()},
		retry: &RetryAdviceDecision{
			Action: "retry_with_hint", Reason: "retry",
			StrategyHint: &hint,
		},
		issue: &IssueAdvisorDecision{
			Action: "escalate_to_replan", Reason: "stuck",
			Blocker: &blocker,
		},
	}
	config := fixtureConfig()
	config.RepairCap = 3
	service := New(Dependencies{
		Config: &config, PlanDB: &recordingPlanDB{},
		AgentJSON: agentDeps(client),
	})
	result, err := service.GateLeaf(
		context.Background(), gateInput(root, base, codeTask()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != scheduler.GateEscalated || result.RepairAttempts != 1 ||
		result.AdvisorBlocker != blocker {
		t.Fatalf("result = %#v", result)
	}
	requests := client.snapshot()
	agents := make([]string, len(requests))
	for index, request := range requests {
		agents[index] = request.Agent
	}
	want := []string{
		"superpowers-code-reviewer", "retry-advisor", "fixer", "issue-advisor",
	}
	if strings.Join(agents, ",") != strings.Join(want, ",") {
		t.Fatalf("agents = %v, want %v", agents, want)
	}
	t.Cleanup(func() {
		worktree := filepath.Join(root, ".plandb", "wt-repair-1")
		command := exec.Command("git", "worktree", "remove", "--force", worktree)
		command.Dir = root
		_ = command.Run()
		command = exec.Command("git", "branch", "-D", "plandb/repair-1")
		command.Dir = root
		_ = command.Run()
	})
}

func TestTestsRequiredConvertsPassToFailBeforeAdvisor(t *testing.T) {
	root, base := newGitRepo(t)
	modifyApp(t, root, 2)
	client := &scriptedAgent{
		reviews: []ReviewVerdict{passReview()},
		issue: &IssueAdvisorDecision{
			Action: "accept_with_debt", Reason: "ship with tracked gap",
			Debt: []IssueAdvisorDebt{{
				Gap: "missing regression test", Severity: "medium",
			}},
		},
	}
	config := fixtureConfig()
	config.RepairCap = 0
	service := New(Dependencies{
		Config: &config, PlanDB: &recordingPlanDB{},
		AgentJSON: agentDeps(client),
	})
	result, err := service.GateLeaf(
		context.Background(),
		gateInput(root, base, codeTask("tests:required")),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != scheduler.GateDonePartial || result.Verdict == nil ||
		result.Verdict.Verdict != scheduler.VerdictFail ||
		len(result.Verdict.Bugs) != 1 ||
		!strings.Contains(result.Verdict.Bugs[0].Detail, "zero new test files") {
		t.Fatalf("result = %#v", result)
	}
}

func TestTestsRequiredAcceptsAddedCasesInAnExistingTestFile(t *testing.T) {
	root, _ := newGitRepo(t)
	testPath := filepath.Join(root, "app_test.go")
	if err := os.WriteFile(testPath, []byte("package app\n\nfunc ExampleValue() {\n\t_ = Value\n}\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", "app_test.go")
	gitRun(t, root, "commit", "-m", "add existing test file")
	base := strings.TrimSpace(gitRun(t, root, "rev-parse", "HEAD"))
	modifyApp(t, root, 2)
	file, err := os.OpenFile(testPath, os.O_APPEND|os.O_WRONLY, 0o666)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("\nfunc ExampleValue_addedCase() {\n\t_ = Value + 1\n}\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	config := fixtureConfig()
	service := New(Dependencies{
		Config: &config, PlanDB: &recordingPlanDB{},
		AgentJSON: agentDeps(&scriptedAgent{reviews: []ReviewVerdict{passReview()}}),
	})
	result, err := service.GateLeaf(
		context.Background(), gateInput(root, base, codeTask("tests:required")),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != scheduler.GatePass || result.Verdict == nil ||
		result.Verdict.Verdict != scheduler.VerdictPass {
		t.Fatalf("result = %#v", result)
	}
}

func TestGateSkipsOwnMachineryAndSpecialistModes(t *testing.T) {
	for _, test := range []struct {
		name       string
		kind       plandb.TaskKind
		subagent   string
		wantReason string
	}{
		{name: "review kind", kind: "review", subagent: "fixer", wantReason: "gate-machinery kind: review"},
		{name: "repair kind", kind: "repair", subagent: "fixer", wantReason: "gate-machinery kind: repair"},
		{name: "specialist", kind: "code", subagent: "review-prover", wantReason: "review-mode agent: review-prover"},
	} {
		t.Run(test.name, func(t *testing.T) {
			task := codeTask()
			task.Kind = test.kind
			input := scheduler.GateInput{
				Task: task, WorktreePath: "/work", Branch: "branch",
				SubagentType: test.subagent,
			}
			service := New(Dependencies{
				Config: func() *Config { value := fixtureConfig(); return &value }(),
				PlanDB: &recordingPlanDB{},
			})
			result, err := service.GateLeaf(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != scheduler.GateSkipped || result.Reason != test.wantReason {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestApplyAdvisorDecisionCoversEveryTypedAction(t *testing.T) {
	depends := true
	hint := strings.Repeat("h", 125)
	blocker := strings.Repeat("b", 125)
	tests := []struct {
		name           string
		decision       IssueAdvisorDecision
		wantStatus     scheduler.GateStatus
		wantReason     string
		wantCommand    string
		wantAction     string
		wantBlocker    string
		wantCommandTwo string
	}{
		{
			name: "retry modified",
			decision: IssueAdvisorDecision{
				Action: "retry_modified", Reason: "relax",
				RelaxCriteria: []string{"criterion A", "criterion B"},
			},
			wantStatus:  scheduler.GateFail,
			wantReason:  "advisor:retry_modified — 2 criteria relaxed",
			wantCommand: "RELAXED CRITERIA (advisor): criterion A; criterion B",
			wantAction:  "retry_modified",
		},
		{
			name: "retry approach clips hint",
			decision: IssueAdvisorDecision{
				Action: "retry_approach", Reason: "change course",
				StrategyHint: &hint,
			},
			wantStatus:  scheduler.GateFail,
			wantReason:  "advisor:retry_approach — " + strings.Repeat("h", 120),
			wantCommand: "ALTERNATE STRATEGY (advisor): " + hint,
			wantAction:  "retry_approach",
		},
		{
			name: "split preserves dependency grammar",
			decision: IssueAdvisorDecision{
				Action: "split", Reason: "decompose",
				Subtasks: []IssueAdvisorSubtask{
					{Title: "one"},
					{Title: "two", DependsOnAbove: &depends},
					{Title: "three"},
				},
			},
			wantStatus:  scheduler.GateEscalated,
			wantReason:  "advisor:split — 3 sub-tasks",
			wantCommand: "plandb\x00split\x00impl\x00--into\x00one > two, three",
			wantAction:  "split",
		},
		{
			name: "accept with debt",
			decision: IssueAdvisorDecision{
				Action: "accept_with_debt", Reason: "bounded gap",
				Debt: []IssueAdvisorDebt{{Gap: "document later", Severity: "low"}},
			},
			wantStatus:     scheduler.GateDonePartial,
			wantReason:     "advisor:accept_with_debt — 1 debt items recorded",
			wantCommand:    "[low] document later",
			wantAction:     "accept_with_debt",
			wantCommandTwo: `{"accepted_with_debt":1}`,
		},
		{
			name: "escalate clips reason and preserves blocker",
			decision: IssueAdvisorDecision{
				Action: "escalate_to_replan", Reason: "blocked", Blocker: &blocker,
			},
			wantStatus:  scheduler.GateEscalated,
			wantReason:  "advisor:escalate_to_replan — " + strings.Repeat("b", 120),
			wantCommand: "escalation blocker: " + blocker,
			wantAction:  "escalate_to_replan",
			wantBlocker: blocker,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			planDB := &recordingPlanDB{}
			service := New(Dependencies{PlanDB: planDB})
			verdict := failReview()
			result := service.applyAdvisorDecision(
				test.decision, "impl", "plandb/repair-1", "/repo/wt",
				&verdict, 2, false,
			)
			if result.Status != test.wantStatus || result.Reason != test.wantReason ||
				result.AdvisorAction != test.wantAction ||
				result.AdvisorBlocker != test.wantBlocker ||
				result.FinalBranch != "plandb/repair-1" ||
				result.FinalWorktreePath != "/repo/wt" ||
				result.RepairAttempts != 2 ||
				result.Verdict == nil {
				t.Fatalf("result = %#v", result)
			}
			joined := make([]string, 0, len(planDB.snapshot()))
			for _, command := range planDB.snapshot() {
				joined = append(joined, strings.Join(command, "\x00"))
			}
			all := strings.Join(joined, "\n")
			if !strings.Contains(all, test.wantCommand) {
				t.Fatalf("commands missing %q:\n%s", test.wantCommand, all)
			}
			if test.wantCommandTwo != "" && !strings.Contains(all, test.wantCommandTwo) {
				t.Fatalf("commands missing %q:\n%s", test.wantCommandTwo, all)
			}
		})
	}
}
