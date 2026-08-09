package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/loopguard"
)

// dirtyRunner is phase2Runner plus a non-empty `git diff --name-only`, i.e. a
// leaf that actually wrote something.
type dirtyRunner struct{ phase2Runner }

func (r dirtyRunner) Run(ctx context.Context, argv []string, opts runOptions) runResult {
	if containsSubsequence(argv, []string{"diff", "--name-only"}) {
		return runResult{Code: 0, Stdout: []byte("errors.go\nerrors_test.go\n")}
	}
	return r.phase2Runner.Run(ctx, argv, opts)
}

type committedLeafStep struct{}

func (committedLeafStep) RunLeaf(_ context.Context, input LeafRunRequest) (LeafRunResult, error) {
	path := filepath.Join(input.Worktree, "src", "leaf.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return LeafRunResult{}, err
	}
	if err := os.WriteFile(path, []byte("package leaf\n"), 0o666); err != nil {
		return LeafRunResult{}, err
	}
	if err := runGitCommand(input.Worktree, "add", "src/leaf.go"); err != nil {
		return LeafRunResult{}, err
	}
	if err := runGitCommand(input.Worktree, "commit", "-m", "complete leaf"); err != nil {
		return LeafRunResult{}, err
	}
	return LeafRunResult{
		Messages: assistantMessages(9),
		Parts:    []LeafPart{{Type: "text", Text: "implemented the leaf"}},
	}, nil
}

func TestCancelledContextDoesNotHideALeafDiff(t *testing.T) {
	workspace := newGitRepository(t)
	baseSHA := gitRun(t, workspace, "rev-parse", "HEAD")
	oracleWorktree := filepath.Join(t.TempDir(), "oracle-worktree")
	gitRun(t, workspace, "worktree", "add", "-b", "plandb/oracle", oracleWorktree, "HEAD")
	if err := os.WriteFile(filepath.Join(oracleWorktree, "committed.txt"), []byte("real work\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	gitRun(t, oracleWorktree, "add", "committed.txt")
	gitRun(t, oracleWorktree, "commit", "-m", "committed leaf diff")

	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	files := listChangedFiles(osCommandRunner{}, expired, oracleWorktree, baseSHA)
	if len(files) != 1 || files[0] != "committed.txt" {
		t.Fatalf("changed files under expired context = %v, want [committed.txt]", files)
	}

	h := newTerminalHarness(t, LeafRunResult{})
	h.scheduler.workspace = workspace
	h.scheduler.runner = osCommandRunner{}
	h.scheduler.stepLoop = committedLeafStep{}
	h.scheduler.adaptiveCuts = true
	h.scheduler.loopGuard = func(loopguard.LoopGuardOptions) loopguard.LoopGuard {
		return permissiveLoopGuard{}
	}
	got := h.scheduler.dispatchOne(expired, SchedulerInput{
		RootTaskID: "root", ProjectID: "project", DBPath: "/ignored",
		ParentSessionID: "parent",
	}, dispatchItem{task: phase2Task("root"), agentID: "scheduler:parent"}, nil)

	if h.gateCalls == 0 {
		t.Fatal("committed leaf diff was hidden after budget expiry; review gate was not called")
	}
	if h.db.hasCommand("what-unlocks", "leaf") {
		t.Fatalf("committed leaf was cascade-failed; commands=%#v", h.db.commands)
	}
	if !got.Success {
		t.Fatalf("expired-context leaf did not complete through the gate: %#v", got)
	}
}

func TestUnadjudicatedGateFailureDeliversInsteadOfCascading(t *testing.T) {
	testsPassed := true
	tests := []struct {
		name             string
		status           GateStatus
		unadjudicated    bool
		hasDiff          bool
		wantMergeCalls   int
		wantCascade      bool
		wantReplanCalls  int
		wantOutcome      string
		wantSuccess      bool
		wantInfraBlocker bool
	}{
		{
			name:   "unadjudicated failure with a diff",
			status: GateFail, unadjudicated: true, hasDiff: true,
			wantMergeCalls: 1, wantOutcome: "fail", wantSuccess: true,
			wantInfraBlocker: true,
		},
		{
			name:   "unadjudicated escalation with a diff",
			status: GateEscalated, unadjudicated: true, hasDiff: true,
			wantMergeCalls: 1, wantOutcome: "escalated", wantSuccess: true,
			wantInfraBlocker: true,
		},
		{
			name:   "unadjudicated failure without a diff",
			status: GateFail, unadjudicated: true,
			wantCascade: true, wantOutcome: "fail",
		},
		{
			name:   "authored reviewer rejection with a diff",
			status: GateFail, hasDiff: true,
			wantCascade: true, wantOutcome: "fail",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newTerminalHarness(t, LeafRunResult{
				Parts:      []LeafPart{{Type: "text", Text: "implemented"}},
				TestPassed: &testsPassed,
			})
			h.scheduler.outcomeCache = true
			if test.hasDiff {
				h.scheduler.runner = dirtyRunner{}
			}
			h.scheduler.gate = gateFunc(func(context.Context, GateInput) (GateResult, error) {
				h.gateCalls++
				return GateResult{
					Status: test.status, Reason: "gate backend unavailable",
					Unadjudicated: test.unadjudicated,
				}, nil
			})
			replanner := &phase2Replanner{
				enabled: true,
				result:  ReplanResult{Abort: true, Summary: "should not run"},
			}
			h.scheduler.replanner = replanner

			got := h.dispatch(phase2Task("root"))

			if got.Success != test.wantSuccess {
				t.Fatalf("success = %t, want %t; result=%#v", got.Success, test.wantSuccess, got)
			}
			if h.mergeCalls != test.wantMergeCalls {
				t.Fatalf("merge calls = %d, want %d", h.mergeCalls, test.wantMergeCalls)
			}
			cascaded := h.db.hasCommand("what-unlocks", "leaf")
			if cascaded != test.wantCascade {
				t.Fatalf("cascade = %t, want %t; commands=%#v", cascaded, test.wantCascade, h.db.commands)
			}
			if replanner.calls != test.wantReplanCalls {
				t.Fatalf("replanner calls = %d, want %d", replanner.calls, test.wantReplanCalls)
			}
			if h.lastOutcome == nil || string(h.lastOutcome.Verdict) != test.wantOutcome {
				t.Fatalf("outcome = %#v, want verdict %q", h.lastOutcome, test.wantOutcome)
			}

			infraBlocker := false
			for _, command := range h.db.commands {
				if len(command) > 2 && command[1] == "context" &&
					strings.Contains(command[2], "Infrastructure cause: gate backend unavailable; no component read this diff.") {
					infraBlocker = true
				}
			}
			if infraBlocker != test.wantInfraBlocker {
				t.Fatalf("infrastructure blocker = %t, want %t; commands=%#v", infraBlocker, test.wantInfraBlocker, h.db.commands)
			}
			if test.wantMergeCalls > 0 {
				cachePath := filepath.Join(h.scheduler.workspace, ".codeaf", "outcome-cache.json")
				if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
					t.Fatalf("unadjudicated outcome entered cache at %s: %v", cachePath, err)
				}
			}
		})
	}
}

// Validation contract for the adaptive leaf guard, derived from the urfave/cli
// #2263 and node-semver #775 benchmark runs, which each delivered an empty diff
// while a correct patch sat in .codeaf/rejected-work/:
//
//   - the adaptive guard measures trajectory COST (turns, tool errors, repair
//     rounds) and never reads the diff, so it must not be the thing that
//     decides a patch is worthless;
//   - a leaf that exhausts its retry budget but left real changes must reach
//     the review gate — the one component that judges patches;
//   - a leaf that left nothing still fails closed, because there is nothing to
//     salvage;
//   - a leaf stopped by the LOOP guard still fails closed even with changes:
//     spinning is not made trustworthy by having touched files;
//   - the quarantine copy is written on every path, so no route loses work.
func TestDispatchAdaptiveGuardSalvagesRealWork(t *testing.T) {
	// Three attempts whose first overruns the band turn budget: this is the
	// exact shape that drives contextpolicy to ModeGiveUp.
	exhaustingResults := func() []LeafRunResult {
		return []LeafRunResult{
			{
				Messages: assistantMessages(9),
				Parts:    []LeafPart{{Type: "text", Text: "still investigating"}},
			},
			{Parts: []LeafPart{{Type: "text", Text: "implemented after retry"}}},
			{Parts: []LeafPart{{Type: "text", Text: "implemented after second retry"}}},
		}
	}

	t.Run("guarded leaf with a real diff reaches the review gate", func(t *testing.T) {
		h := newTerminalHarness(t, LeafRunResult{})
		h.scheduler.adaptiveCuts = true
		h.scheduler.runner = dirtyRunner{}
		h.scheduler.loopGuard = func(loopguard.LoopGuardOptions) loopguard.LoopGuard {
			return permissiveLoopGuard{}
		}
		h.step.results = exhaustingResults()

		got := h.dispatch(phase2Task("root"))

		if h.gateCalls == 0 {
			t.Errorf("gateCalls = 0; guarded work never reached the review gate")
		}
		if !got.Success {
			t.Errorf("Success = false; a gate-passing patch must ship, result=%#v", got)
		}
		// No quarantine copy is expected here: the intermediate-retry guard now
		// breaks out before any reset, so the work is never discarded and there
		// is nothing to preserve. Quarantine is asserted on the paths that DO
		// discard — the empty-diff and loop-guard cases below.
		if h.db.hasCommand("what-unlocks", "leaf") {
			t.Errorf("task was cascade-failed despite producing a diff; commands=%#v", h.db.commands)
		}
	})

	t.Run("guarded leaf with no diff still fails closed", func(t *testing.T) {
		h := newTerminalHarness(t, LeafRunResult{})
		h.scheduler.adaptiveCuts = true
		h.scheduler.loopGuard = func(loopguard.LoopGuardOptions) loopguard.LoopGuard {
			return permissiveLoopGuard{}
		}
		h.step.results = exhaustingResults()

		got := h.dispatch(phase2Task("root"))

		if got.Success {
			t.Errorf("Success = true; there was nothing to salvage, result=%#v", got)
		}
		if h.gateCalls != 0 {
			t.Errorf("gateCalls = %d; an empty attempt must not occupy the gate", h.gateCalls)
		}
		if h.preserved != 3 {
			t.Errorf("preserved = %d, want 3", h.preserved)
		}
	})

	t.Run("loop-guard stop fails closed even with a real diff", func(t *testing.T) {
		h := newTerminalHarness(t, LeafRunResult{Parts: []LeafPart{
			{Type: "tool", Tool: "read", ArgsKey: "same"},
			{Type: "tool", Tool: "read", ArgsKey: "same"},
			{Type: "tool", Tool: "read", ArgsKey: "same"},
			{Type: "text", Text: "still looping"},
		}})
		h.scheduler.adaptiveCuts = true
		h.scheduler.runner = dirtyRunner{}

		got := h.dispatch(phase2Task("root"))

		if got.Success {
			t.Errorf("Success = true; a spinning leaf must not ship, result=%#v", got)
		}
		if h.preserved != 1 || h.emitted != 1 {
			t.Errorf("preserved=%d emitted=%d, want 1/1", h.preserved, h.emitted)
		}
		if !h.db.hasCommand("what-unlocks", "leaf") {
			t.Errorf("loop-guard stop must still cascade; commands=%#v", h.db.commands)
		}
	})
}

// transcriptTail feeds classifyFailure's "did the agent mention this file or
// symbol" checks. For a long leaf it must show what the leaf just DID, not the
// session's opening chatter — node-semver-775 was triaged "localization" twice,
// each verdict resetting a correct patch, because the symbol it had just
// written was absent from the transcript's first 6000 units.
func TestTranscriptTailShowsRecentWork(t *testing.T) {
	parts := []LeafPart{}
	for i := 0; i < 400; i++ {
		parts = append(parts, LeafPart{Type: "tool", Tool: "read", ArgsKey: "docs/intro.md"})
	}
	parts = append(parts,
		LeafPart{Type: "tool", Tool: "edit", ArgsKey: "internal/re.js"},
		LeafPart{Type: "text", Text: "added PRERELEASECOERCE to internal/re.js"},
	)

	tail := transcriptTail(LeafRunResult{Parts: parts})

	if !strings.Contains(tail, "PRERELEASECOERCE") {
		t.Errorf("tail omits the symbol the leaf just wrote; got %d chars ending %q",
			len(tail), tail[max(0, len(tail)-80):])
	}
	if !strings.Contains(tail, "internal/re.js") {
		t.Errorf("tail omits the file the leaf just edited")
	}
	if !strings.HasPrefix(tail, "...") {
		t.Errorf("elided tail must mark the omission at the FRONT; got prefix %q", tail[:min(8, len(tail))])
	}
}

func TestTailJS(t *testing.T) {
	if got := tailJS("short", 100); got != "short" {
		t.Errorf("under the limit must pass through unchanged, got %q", got)
	}
	if got := tailJS("abcdefghij", 8); got != "...fghij" {
		t.Errorf("tailJS = %q, want %q", got, "...fghij")
	}
	// A surrogate pair must never be split: 🚀 is two UTF-16 units, so a cut
	// landing mid-pair has to step forward rather than emit U+FFFD.
	if got := tailJS("ab🚀🚀", 6); strings.ContainsRune(got, '�') {
		t.Errorf("tailJS split a surrogate pair: %q", got)
	}
}

// The intermediate fresh-context retry must not reset a worktree that holds
// real work either. node-semver-775 lost two correct patches this way: its
// ledger reads outcome=unknown (classifyFailure's DEFAULT verdict, "no strong
// heuristic matched") for both attempts, each a `git reset --hard` over a
// working fix, and the run then ran out of wall clock and delivered nothing.
func TestFreshContextRetryDoesNotResetRealWork(t *testing.T) {
	t.Run("a leaf that produced changes goes to the gate, not a reset", func(t *testing.T) {
		h := newTerminalHarness(t, LeafRunResult{})
		h.scheduler.adaptiveCuts = true
		h.scheduler.runner = dirtyRunner{}
		h.scheduler.loopGuard = func(loopguard.LoopGuardOptions) loopguard.LoopGuard {
			return permissiveLoopGuard{}
		}
		h.step.results = []LeafRunResult{
			{
				Messages: assistantMessages(9),
				Parts:    []LeafPart{{Type: "text", Text: "implemented the fix"}},
			},
			{Parts: []LeafPart{{Type: "text", Text: "second attempt"}}},
			{Parts: []LeafPart{{Type: "text", Text: "third attempt"}}},
		}

		got := h.dispatch(phase2Task("root"))

		if h.step.calls != 1 {
			t.Errorf("leaf ran %d times; work-bearing attempts must not be retried away", h.step.calls)
		}
		if h.gateCalls == 0 {
			t.Errorf("gateCalls = 0; the diff never reached the review gate")
		}
		if !got.Success {
			t.Errorf("Success = false; a gate-passing patch must ship: %#v", got)
		}
	})

	t.Run("a leaf that produced nothing is still retried", func(t *testing.T) {
		h := newTerminalHarness(t, LeafRunResult{})
		h.scheduler.adaptiveCuts = true
		h.scheduler.loopGuard = func(loopguard.LoopGuardOptions) loopguard.LoopGuard {
			return permissiveLoopGuard{}
		}
		h.step.results = []LeafRunResult{
			{
				Messages: assistantMessages(9),
				Parts:    []LeafPart{{Type: "text", Text: "still investigating"}},
			},
			{Parts: []LeafPart{{Type: "text", Text: "retry"}}},
			{Parts: []LeafPart{{Type: "text", Text: "second retry"}}},
		}

		h.dispatch(phase2Task("root"))

		if h.step.calls != 3 {
			t.Errorf("leaf ran %d times, want 3; the rescue path must survive for empty attempts", h.step.calls)
		}
	})
}
