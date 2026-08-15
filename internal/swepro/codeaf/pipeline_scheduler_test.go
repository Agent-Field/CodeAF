package codeaf

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/capability"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/sizeband"
)

func TestBuildCapabilityTierMapUsesModelIDsAndHighMembershipWins(t *testing.T) {
	pool := poolResolver{
		low:  []string{"openrouter/vendor/shared", "other/vendor/low"},
		high: []string{"openrouter/vendor/shared", "openrouter/vendor/high"},
	}
	want := map[string]capability.ModelTierName{
		"vendor/shared": capability.TierHigh,
		"vendor/low":    capability.TierLow,
		"vendor/high":   capability.TierHigh,
	}
	if got := buildCapabilityTierMap(pool); !reflect.DeepEqual(got, want) {
		t.Fatalf("tier map = %#v, want %#v", got, want)
	}
}

func TestPipelineWarmStartsOneCapabilityTrackerForRootCutAndScheduler(t *testing.T) {
	// Round-2 tracker contract: prior run outcomes warm one tracker instance;
	// the root-cut decision and scheduler both consume that same live state.
	workspace := t.TempDir()
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
	t.Setenv("CODEAF_HARD", "0")
	if err := os.MkdirAll(filepath.Join(workspace, ".codeaf"), 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"taskID":"t","model":{"providerID":"openrouter","modelID":"vendor/high"},` +
		`"sizeBand":"m","verdict":"fail","repairRounds":0,"turns":1,"toolErrors":0,` +
		`"costUsd":0.01,"wallMs":1,"mergeConflict":false,"timestamp":1}`
	if err := os.WriteFile(filepath.Join(workspace, ".codeaf", "outcomes.jsonl"),
		[]byte(strings.Repeat(line+"\n", 8)), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := newPipeline(cliArgs{
		High: "openrouter/vendor/high", Low: "openrouter/vendor/low",
	}, workspace, pipelineDeps{})
	tracker := runner.capabilityTracker(true)
	model := capability.ModelRef{ProviderID: "openrouter", ModelID: "vendor/high"}
	if got := tracker.MaxReliableBand(model); got == sizeband.BandM || got == sizeband.BandL || got == sizeband.BandXL {
		t.Fatalf("warm failure evidence did not lower reliable band: %q", got)
	}
	goal := strings.Join([]string{
		"Refactor the retry loop in the client so it backs off exponentially.",
		"", "file_scope: src/client/retry.ts", "",
		"- [ ] backoff doubles each attempt", "- [ ] capped at 30s", "- [ ] unit tests cover the cap",
	}, "\n")
	if band := sizeband.EstimateSizeBand(sizeband.EstimateSizeBandInput{Description: goal}); band != sizeband.BandM {
		t.Fatalf("test goal band = %q, want m", band)
	}
	cold := newPipeline(cliArgs{
		High: "openrouter/vendor/high", Low: "openrouter/vendor/low",
	}, t.TempDir(), pipelineDeps{})
	if !cold.shouldRootCut(goal) {
		t.Fatal("prior-only high tracker should accept the medium root")
	}
	if runner.shouldRootCut(goal) {
		t.Fatal("warm failure evidence did not influence the root-cut consumer")
	}
	options := runner.schedulerOptions(true)
	if options.Capability != tracker || runner.capability != tracker {
		t.Fatalf("scheduler/root trackers differ: scheduler=%p root=%p", options.Capability, tracker)
	}
}

func TestLiveSchedulerOptionsWireAdaptiveServicesAndDeferEnvFlags(t *testing.T) {
	runner := newPipeline(cliArgs{
		High: "openrouter/vendor/high", Low: "openrouter/vendor/low",
	}, t.TempDir(), pipelineDeps{})
	options := runner.schedulerOptions(true)
	// Contracts 1-5: the live constructor supplies every adaptive seam while
	// leaving cache/frontier unset for NewScheduler's environment defaults.
	if options.Agents == nil || options.Capability == nil || options.Briefing == nil ||
		options.Replanner == nil || options.Planner == nil || options.OutcomeObserver == nil {
		t.Fatalf("incomplete live scheduler options: %#v", options)
	}
	if options.OutcomeCache != nil || options.FrontierEnabled != nil {
		t.Fatalf("environment flags overridden: cache=%v frontier=%v", options.OutcomeCache, options.FrontierEnabled)
	}
	agent, err := options.Agents.Get(context.Background(), "fixer")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{}
	for _, rule := range agent.Permission {
		if rule.Action == "allow" {
			allowed[rule.Permission] = true
		}
	}
	if !allowed["task"] || !allowed["plandb"] {
		t.Fatalf("fixer permissions missing scheduler tools: %#v", agent.Permission)
	}
}
