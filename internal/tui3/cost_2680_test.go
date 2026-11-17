package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

func costModels2680() []Model {
	return []Model{
		{
			ID:             "deepseek/deepseek-v4-flash",
			ContextLength:  128_000,
			PromptPrice:    0.000001,
			CacheReadPrice: 0.0000002,
		},
		{
			ID:             "z-ai/glm-5.3-flash",
			ContextLength:  128_000,
			PromptPrice:    0.0000002,
			CacheReadPrice: 0.0000001,
		},
		{
			ID:             "openai/gpt-5.4-mini",
			ContextLength:  128_000,
			PromptPrice:    0.000003,
			CacheReadPrice: 0.0000015,
		},
	}
}

func TestCacheSavingsDoNotChangeWhenSelectedModelChanges2680(t *testing.T) {
	agent := &fakeAgent{
		model: "deepseek/deepseek-v4-flash",
		usage: session.Usage{
			Input:     31_600,
			Output:    420,
			CacheRead: 13_600,
			CostUSD:   0.0034,
			Calls:     2,
			Turns:     2,
		},
	}
	a := newTestApp(agent)
	a.width = 200
	a.dismissWelcome()
	a.models = costModels2680
	a.model = "deepseek/deepseek-v4-flash"
	a.refreshUsage()

	// 13,600 cached tokens at deepseek/deepseek-v4-flash's ($0.000001 - $0.0000002) = $0.01088 -> $0.0109.
	const wantSaved = "saved $0.0109"
	if got := a.warmSegment(); !strings.Contains(got, wantSaved) {
		t.Fatalf("warmSegment before model switch = %q, want %q", got, wantSaved)
	}
	if line := plain(a.legend(a.width)); !strings.Contains(line, wantSaved) {
		t.Fatalf("legend before model switch = %q, want %q", line, wantSaved)
	}
	if cost := plain(a.costText()); !strings.Contains(cost, "13.6k read · "+wantSaved) {
		t.Fatalf("/cost before model switch missing %q:\n%s", wantSaved, cost)
	}

	// Switch the selected model to z-ai/glm-5.3-flash without sending a new turn.
	a.slash("/model z-ai/glm-5.3-flash")
	if a.model != "z-ai/glm-5.3-flash" {
		t.Fatalf("selected model = %q, want z-ai/glm-5.3-flash", a.model)
	}

	// Earned cache savings must stay at $0.0109, never repriced to $0.0014.
	if got := a.warmSegment(); !strings.Contains(got, wantSaved) {
		t.Fatalf("warmSegment after /model repriced earned savings: %q, want %q", got, wantSaved)
	}
	if line := plain(a.legend(a.width)); !strings.Contains(line, wantSaved) {
		t.Fatalf("legend after /model repriced earned savings: %q, want %q", line, wantSaved)
	}
	costAfter := plain(a.costText())
	if !strings.Contains(costAfter, "13.6k read · "+wantSaved) {
		t.Fatalf("/cost after /model repriced earned savings:\n%s", costAfter)
	}
	if strings.Contains(costAfter, "$0.0014") {
		t.Fatalf("/cost used the newly selected model's rate for past turns:\n%s", costAfter)
	}
}

func TestReopenedConversationPricesEachLedgerCallAtItsOwnModel2680(t *testing.T) {
	now := time.Now()
	// Call 1 on deepseek/deepseek-v4-flash: 10,000 * $0.0000008 = $0.0080.
	// Call 2 on z-ai/glm-5.3-flash: 4,000 * $0.0000001 = $0.0004.
	// Total saved = $0.0084 (whereas pricing all 14,000 at openai/gpt-5.4-mini's $0.0000015 gap would be $0.0210).
	lines := []session.UsageLine{
		{
			At:        now.Add(-time.Minute),
			Session:   treeConversation,
			Model:     "deepseek/deepseek-v4-flash",
			Calls:     1,
			Input:     20_000,
			Output:    500,
			CacheRead: 10_000,
			USD:       0.012,
		},
		{
			At:        now,
			Session:   treeConversation,
			Model:     "z-ai/glm-5.3-flash",
			Calls:     1,
			Input:     5_000,
			Output:    100,
			CacheRead: 4_000,
			USD:       0.001,
		},
	}

	// Live conversation that made both calls and then switched its selected model to openai/gpt-5.4-mini.
	live := spendTreeLab(t, 0.013, lines[:1])
	live.width = 200
	live.dismissWelcome()
	live.models = costModels2680
	live.model = "deepseek/deepseek-v4-flash"
	live.take(session.Usage{Input: 20_000, Output: 500, CacheRead: 10_000, CostUSD: 0.012, Calls: 1})
	live.model = "z-ai/glm-5.3-flash"
	appendLedger(t, live.usageLedger, lines[1:])
	live.take(session.Usage{Input: 25_000, Output: 600, CacheRead: 14_000, CostUSD: 0.013, Calls: 2})
	live.slash("/model openai/gpt-5.4-mini")

	liveWarm := live.warmSegment()
	liveCost := plain(live.costText())
	if !strings.Contains(liveCost, "14k read · saved $0.0084") {
		t.Fatalf("live /cost did not sum each call at its own model's rate:\n%s", liveCost)
	}

	// Reopened conversation on openai/gpt-5.4-mini backed by the ledger and journal.
	reopened := spendTreeLab(t, 0.013, lines)
	reopened.width = 200
	reopened.dismissWelcome()
	reopened.models = costModels2680
	reopened.model = "openai/gpt-5.4-mini"
	reopened.agent = &fakeAgent{
		model: "openai/gpt-5.4-mini",
		usage: session.Usage{Input: 25_000, Output: 600, CacheRead: 14_000, CostUSD: 0.013, Calls: 2},
	}
	journalLines := strings.Join([]string{
		`{"type":"session","version":1,"id":"` + treeConversation + `","cwd":"` + filepath.Dir(reopened.file) + `","model":"openai/gpt-5.4-mini","timestamp":"2026-11-16T12:00:00Z"}`,
		`{"type":"usage","usage":{"model":"deepseek/deepseek-v4-flash","input":20000,"output":500,"cacheRead":10000,"costUsd":0.012,"calls":1},"timestamp":"2026-11-16T12:00:05Z"}`,
		`{"type":"usage","usage":{"model":"z-ai/glm-5.3-flash","input":5000,"output":100,"cacheRead":4000,"costUsd":0.001,"calls":1,"aux":true},"timestamp":"2026-11-16T12:00:10Z"}`,
		"",
	}, "\n")
	if err := os.WriteFile(reopened.file, []byte(journalLines), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened.resetMeters()
	reopened.refreshUsage()
	reopened.readTreeSpend()

	reopenedWarm := reopened.warmSegment()
	reopenedCost := plain(reopened.costText())
	if reopenedWarm != liveWarm {
		t.Fatalf("reopened warmSegment = %q, want live %q", reopenedWarm, liveWarm)
	}
	if reopenedCost != liveCost {
		t.Fatalf("reopened /cost disagrees with live /cost:\nreopened:\n%s\nlive:\n%s", reopenedCost, liveCost)
	}
}

func TestCostCountsAndTokensMatchLedgerAfterLateRowAppended2680(t *testing.T) {
	now := time.Now()
	initial := []session.UsageLine{
		{
			At:        now.Add(-2 * time.Second),
			Session:   treeConversation,
			Model:     "deepseek/deepseek-v4-flash",
			Calls:     2,
			Input:     14_200,
			Output:    240,
			CacheRead: 8_000,
			USD:       0.0016,
		},
		{
			At:      now.Add(-time.Second),
			Session: treeConversation,
			Model:   "deepseek/deepseek-v4-flash",
			Role:    "title",
			Calls:   1,
			Input:   700,
			Output:  25,
			USD:     0.0002,
		},
	}

	// Snapshot at EventTurnDone holds only the first 3 calls (14.9k in, 265 out, $0.0018).
	a := spendTreeLab(t, 0.0018, initial)
	a.models = costModels2680
	a.model = "deepseek/deepseek-v4-flash"
	a.agent = &fakeAgent{
		model: "deepseek/deepseek-v4-flash",
		usage: session.Usage{
			Input:     14_900,
			Output:    265,
			CacheRead: 8_000,
			CostUSD:   0.0018,
			Calls:     3,
			Turns:     2,
		},
	}
	a.refreshUsage()
	a.readTreeSpend()

	// A post-turn reflex call and a late reconciled receipt land in usage.jsonl after the turn settled.
	appendLedger(t, a.usageLedger, []session.UsageLine{
		{
			At:        now,
			Session:   treeConversation,
			Model:     "deepseek/deepseek-v4-flash",
			Role:      "reflex",
			Calls:     1,
			Input:     1_200,
			Output:    150,
			CacheRead: 600,
			USD:       0.0002,
		},
		{
			At:         now.Add(time.Second),
			Session:    treeConversation,
			Model:      "deepseek/deepseek-v4-flash",
			Calls:      1,
			Input:      3_000,
			Output:     2_653,
			CacheRead:  1_400,
			USD:        0.0004,
			Reconciled: true,
		},
	})

	// Without reopening or running /model, /cost must report all 5 calls and their summed tokens and cache reads.
	cost := plain(a.costText())
	for _, want := range []string{
		"$0.0024",
		"19.1k in · 3.1k out",
		"10k read · saved $0.0080",
		"model calls",
		"5",
	} {
		if !strings.Contains(cost, want) {
			t.Fatalf("/cost after late ledger rows is missing %q:\n%s", want, cost)
		}
	}
}
