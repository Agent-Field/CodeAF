package tui3

import (
	"math"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// #2679: A ledger row with `"role":"knowledge"` and no session, task or
// automation id appears in `/spend` under `by topic` as
// `learning from your conversations`, and the topic rows sum to the page's
// `14 days` total across a mixed ledger.
func TestSpendByTopicIncludesKnowledgeCallsAndSumsToTotal2679(t *testing.T) {
	win := session.LastDays(spendTestNow, 14)

	// 1. A ledger with a single id-less knowledge row appears in the topic rollup.
	knowledgeOnly := []session.UsageLine{{
		At:     spendTestNow,
		Model:  "deepseek/deepseek-v4.1-flash",
		Role:   "knowledge",
		Calls:  2,
		Input:  1500,
		Output: 200,
		USD:    0.06,
	}}
	single := readSpend(knowledgeOnly, win, spendTestNow)
	if len(single.subjects) != 1 {
		t.Fatalf("single knowledge ledger produced %d topic rows, want 1: %+v", len(single.subjects), single.subjects)
	}
	if single.subjects[0].ID != "learning from your conversations" {
		t.Fatalf("knowledge topic label = %q, want %q", single.subjects[0].ID, "learning from your conversations")
	}
	if math.Abs(single.subjects[0].USD-single.totals.USD) > 1e-9 {
		t.Fatalf("knowledge topic USD = %v, header total = %v", single.subjects[0].USD, single.totals.USD)
	}
	singleText := strings.Join(plainSpendRows(single.rows(120, newPalette(tokens.NoColor, false))), "\n")
	if !strings.Contains(singleText, "learning from your conversations") {
		t.Fatalf("by topic did not draw `learning from your conversations`:\n%s", singleText)
	}

	// 2. A mixed ledger (one conversation row, one task row, one automation row,
	// one knowledge row) rolls every row into `by topic` and sums to the header.
	mixed := []session.UsageLine{
		{
			At: spendTestNow, Model: "deepseek/deepseek-v4-flash",
			Session: "conv-1", Workspace: "/work/ws1",
			Calls: 2, Input: 14000, Output: 100, USD: 1.25,
		},
		{
			At: spendTestNow, Model: "deepseek/deepseek-v4-flash",
			Session: "task-node-1", Root: "conv-1", Task: "task-1", Workspace: "/work/ws1",
			Calls: 3, Input: 8000, Output: 400, USD: 2.50,
		},
		{
			At: spendTestNow, Model: "gemini/gemini-2.5-flash",
			Automation: "auto-1", Workspace: "/work/ws1",
			Calls: 1, Input: 1000, Output: 50, USD: 0.40,
		},
		{
			At: spendTestNow, Model: "deepseek/deepseek-v4.1-flash",
			Role:  "knowledge",
			Calls: 2, Input: 1500, Output: 200, USD: 0.60,
		},
	}
	reading := readSpend(mixed, win, spendTestNow)
	if len(reading.subjects) != 4 {
		t.Fatalf("mixed ledger produced %d topic rows, want 4: %+v", len(reading.subjects), reading.subjects)
	}

	var ledgerSum, topicSum float64
	for _, line := range mixed {
		ledgerSum += line.USD
	}
	foundKnowledge := false
	for _, subject := range reading.subjects {
		topicSum += subject.USD
		if subject.ID == "learning from your conversations" {
			foundKnowledge = true
		}
	}
	if !foundKnowledge {
		t.Fatalf("mixed topic rollup missing `learning from your conversations`: %+v", reading.subjects)
	}
	if math.Abs(topicSum-ledgerSum) > 1e-9 || math.Abs(topicSum-reading.totals.USD) > 1e-9 {
		t.Fatalf("topic sum = %.6f, ledger sum = %.6f, header total = %.6f", topicSum, ledgerSum, reading.totals.USD)
	}
	if dollars(topicSum) != dollars(reading.totals.USD) {
		t.Fatalf("topic sum to the cent = %s, header 14 days figure = %s", dollars(topicSum), dollars(reading.totals.USD))
	}

	pageText := strings.Join(plainSpendRows(reading.rows(120, newPalette(tokens.NoColor, false))), "\n")
	for _, want := range []string{
		"14 days came to " + dollars(ledgerSum),
		"task-1",
		"conv-1",
		"auto-1",
		"learning from your conversations",
	} {
		if !strings.Contains(pageText, want) {
			t.Fatalf("drawn spend page missing %q:\n%s", want, pageText)
		}
	}
}
