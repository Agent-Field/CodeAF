//go:build e2e

package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE LIVE WRITER. factory.job_brief_write on this machine's factory, the
// job's agent a real in-process conversation, one real model for every role
// ([liveFactoryModel] through [liveRecorder], which refuses any other). The
// job is a fixture with a plan, a test check, a review and a proof, so the
// draft has evidence to point at. It says the model the reply named, the
// cost and what validation dropped. Opt-in (CODEAF_LIVE_FACTORY=1), under a
// cent.
func TestLiveBriefWriterKeepsACheckedBrief(t *testing.T) {
	if os.Getenv(liveFactoryOptIn) != "1" {
		t.Skipf("opt-in live lane: set %s=1 (spends under a cent on OpenRouter)", liveFactoryOptIn)
	}
	if liveFactoryKey == "" {
		t.Fatal("BLOCKED: no OpenRouter key by any road the product reads; not faking a pass")
	}
	s := newLocalSetup(t)
	profile := v3Profile(t, map[string]any{
		config.KeyTaskModelsMode: "one", config.KeyTaskModelsOne: liveFactoryModel,
		"models.roles": "", "models.tiers.low": liveFactoryModel, "models.tiers.high": liveFactoryModel,
		"models.tiers.reflex": liveFactoryModel, "models.tiers.mastermind": liveFactoryModel,
		"models.tiers.worker": liveFactoryModel, "task.model": liveFactoryModel, "models.fallbacks": "",
	})
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("OPENROUTER_API_KEY", liveFactoryKey)
	recorder := newLiveRecorder(t, config.DefaultBaseURL)
	t.Setenv("CODEAF_BASE_URL", recorder.server.URL)
	freshEngineProcess(t)
	s.backend.open = appOpenInProcess
	st := s.start()
	floor := s.backend.local.host.floor
	it, err := floor.Create(factory.Item{Tier: factory.TierOwner, Repo: "acme/api", Num: 7, Kind: factory.KindIssue, State: factory.StateNeedsYou, Model: liveFactoryModel,
		Title: "Retries double-send after a worker restart",
		Body:  "When a worker restarts mid-send, the retry loop sends the message again although the first send landed.",
		Stages: []factory.Stage{
			{Name: "plan", Kind: factory.StageChat, On: true}, {Name: "write", Kind: factory.StageChat, On: true},
			{Name: "test", Kind: factory.StageCheck, Until: factory.UntilGreen, On: true},
			{Name: "review", Kind: factory.StageChat, Until: factory.UntilClean, On: true},
			{Name: "proof", Kind: factory.StageChat, Until: factory.UntilProven, On: true},
		},
		RuleChecks: &factory.RuleChecks{Start: factory.CheckPassed},
		Change: &factory.Change{Stat: &factory.ChangeStat{Files: 2, Added: 14, Removed: 3}, Files: []factory.ChangedFile{
			{Path: "retry/send.go", Added: 9, Removed: 3}, {Path: "retry/send_test.go", Added: 5}}, Patch: "diff --git a/retry/send.go b/retry/send.go\n--- a/retry/send.go\n+++ b/retry/send.go\n@@ -10,4 +10,9 @@ func Send(ctx context.Context) error {\n-\treturn resend()\n+\tif lease.Landed(id) {\n+\t\treturn nil\n+\t}\n+\treturn resend()\n"},
		Stream: &factory.Stream{Spent: 0.4, Phases: []factory.Phase{
			{Name: "plan", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1, Result: &factory.StageResult{Done: true, Output: "Check the send ledger before a retry resends; add a test that restarts a worker mid-send."}},
			{Name: "write", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1, Result: &factory.StageResult{Done: true, Output: "Send now asks the ledger whether the message landed before resending."}},
			{Name: "test", Kind: factory.StageCheck, State: factory.PhaseDone, Round: 1, Result: &factory.StageResult{Done: true, Tests: &factory.TestCounts{Command: "go test ./retry/...", Passed: 12}}},
			{Name: "review", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1, Result: &factory.StageResult{Done: true, Findings: 0, Output: "Clean. The ledger read happens before the resend."}},
			{Name: "proof", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1, Result: &factory.StageResult{Done: true, Claims: []factory.Claim{
				{Text: "a restarted worker does not resend", OK: true, Evidence: "TestRestartMidSend", Medium: "test"},
				{Text: "latency unchanged under load", OK: false}}}},
		}},
		Question: "Approve the change for publishing?", QKind: "approve",
	})
	if err != nil {
		t.Fatal(err)
	}
	var got appwire.FactoryJobBriefWritten
	s.must("factory.job_brief_write", map[string]any{"factory": st.Factory, "job": it.ID}, &got)
	var reasons []string
	for _, d := range got.Drops {
		reasons = append(reasons, d.Part+": "+d.Reason)
	}
	t.Logf("LIVE BRIEF model=%s cost_usd=%.6f kept=%v status=%s rev=%d blocks=%d drops=%d failure=%q",
		got.Model, got.CostUSD, got.Kept, got.Current.Status, got.Current.Brief.Rev, len(got.Current.Brief.Blocks), len(got.Drops), got.Current.Failure)
	t.Logf("LIVE BRIEF verdict=%q", got.Current.Brief.Verdict)
	for _, b := range got.Current.Brief.Blocks {
		t.Logf("LIVE BRIEF block %s", b.Key())
	}
	for _, r := range reasons {
		t.Logf("LIVE BRIEF drop %s", r)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	t.Logf("LIVE BRIEF recorder calls=%d replies=%v cost=%.6f problems=%v", recorder.calls, recorder.replies, recorder.cost, recorder.problems)
	if len(recorder.problems) > 0 || len(recorder.replies) != 1 || recorder.replies[liveFactoryModel] == 0 {
		t.Fatalf("the model was not the pinned one alone: %v %v", recorder.replies, recorder.problems)
	}
	if !got.Kept || got.Current.Status != factory.BriefStatusReady || !strings.Contains(got.Model, "deepseek-v4.1-flash") {
		t.Fatalf("no brief kept: %+v", got)
	}
}

// THE LIVE FIRST READ. Real issues from the file CODEAF_BRIEF_ISSUES names
// ([{"number","title","body"}]), each a job that has not run, through
// factory.job_brief_write on one real model for every role. It says the old
// page's words (the title) beside the new read, with what each cost, and holds
// the read's overlap with its issue to [factory.BriefOverlapMost]. A thin issue
// spends nothing. Opt-in, and the whole run stays under $0.20.
func TestLiveBriefFirstReads(t *testing.T) {
	if os.Getenv(liveFactoryOptIn) != "1" || os.Getenv("CODEAF_BRIEF_ISSUES") == "" {
		t.Skipf("opt-in live lane: set %s=1 and CODEAF_BRIEF_ISSUES", liveFactoryOptIn)
	}
	if liveFactoryKey == "" {
		t.Fatal("BLOCKED: no OpenRouter key by any road the product reads; not faking a pass")
	}
	raw, err := os.ReadFile(os.Getenv("CODEAF_BRIEF_ISSUES"))
	if err != nil {
		t.Fatal(err)
	}
	var issues []struct {
		Number      int
		Title, Body string
	}
	if err := json.Unmarshal(raw, &issues); err != nil {
		t.Fatal(err)
	}
	s := newLocalSetup(t)
	profile := v3Profile(t, map[string]any{
		config.KeyTaskModelsMode: "one", config.KeyTaskModelsOne: liveFactoryModel,
		"models.roles": "", "models.tiers.low": liveFactoryModel, "models.tiers.high": liveFactoryModel,
		"models.tiers.reflex": liveFactoryModel, "models.tiers.mastermind": liveFactoryModel,
		"models.tiers.worker": liveFactoryModel, "task.model": liveFactoryModel, "models.fallbacks": "",
	})
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("OPENROUTER_API_KEY", liveFactoryKey)
	recorder := newLiveRecorder(t, config.DefaultBaseURL)
	t.Setenv("CODEAF_BASE_URL", recorder.server.URL)
	freshEngineProcess(t)
	s.backend.open = appOpenInProcess
	st := s.start()
	floor := s.backend.local.host.floor
	total, worst := 0.0, 0.0
	for _, issue := range issues {
		it, err := floor.Create(factory.Item{Tier: factory.TierOwner, Repo: "acme/api", Num: issue.Number, Kind: factory.KindIssue, State: factory.StateNew, Model: liveFactoryModel,
			Title: issue.Title, Body: issue.Body})
		if err != nil {
			t.Fatal(err)
		}
		var got appwire.FactoryJobBriefWritten
		s.must("factory.job_brief_write", map[string]any{"factory": st.Factory, "job": it.ID}, &got)
		cur, _ := floor.Get(it.ID)
		overlap := factory.BriefIssueOverlap(got.Current.Brief, cur)
		total += got.CostUSD
		worst = max(worst, overlap)
		t.Logf("FIRST READ #%d words=%d skip=%q kept=%v model=%s cost_usd=%.6f job_spent=%.6f overlap=%.3f drops=%d failure=%q", issue.Number, factory.BriefWords(issue.Body), got.Skip, got.Kept, got.Model, got.CostUSD, factory.JobSpent(cur), overlap, len(got.Drops), got.Current.Failure)
		t.Logf("  OLD %q", issue.Title)
		t.Logf("  NEW %q", got.Current.Brief.Verdict)
		for _, b := range got.Current.Brief.Blocks {
			for _, c := range b.Bullets {
				t.Logf("    %s: %s", b.Heading, c.Text)
			}
			for _, q := range b.Questions {
				t.Logf("    %s: %s", b.Heading, q.Text)
			}
		}
		for _, d := range got.Drops {
			t.Logf("    DROP %s: %s", d.Part, d.Reason)
		}
		if got.Skip == "" && overlap > factory.BriefOverlapMost {
			t.Errorf("#%d overlap %.3f is over %.2f", issue.Number, overlap, factory.BriefOverlapMost)
		}
		if total > 0.20 {
			t.Fatalf("over the run's cap: $%.4f", total)
		}
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	t.Logf("FIRST READS total_usd=%.6f worst_overlap=%.3f recorder calls=%d replies=%v cost=%.6f problems=%v", total, worst, recorder.calls, recorder.replies, recorder.cost, recorder.problems)
	if len(recorder.problems) > 0 || len(recorder.replies) != 1 || recorder.replies[liveFactoryModel] == 0 {
		t.Fatalf("the model was not the pinned one alone: %v %v", recorder.replies, recorder.problems)
	}
}
