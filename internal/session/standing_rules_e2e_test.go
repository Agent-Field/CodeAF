//go:build e2e

package session

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// TestRealRulesCheckOnTheLiveViolation is the report check on a real model,
// shown the evidence that made it: the first report the live local-work
// journey published on 2026-09-10 (/tmp/opus-localwork/live2), which quoted an
// address and a phone number the Launch folder's rule forbids and wrote
// "[redacted]" after them. The check must find that report broken and quote
// it, must keep the same report once the contact details are replaced, and
// must not hold a report back on a rule about the work that a report cannot
// show. Three small calls; SKIPS without a key, like every live test here.
func TestRealRulesCheckOnTheLiveViolation(t *testing.T) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		if os.Getenv("AFORGE_E2E_REQUIRE_LIVE") != "" {
			t.Fatal("the live rules check requires OPENROUTER_API_KEY")
		}
		t.Skip("no OPENROUTER_API_KEY")
	}
	model := os.Getenv("AFORGE_RULES_CHECK_MODEL")
	if model == "" {
		model = "deepseek/deepseek-v4-flash"
	}
	const violating = `# Inbox Report — 10 September 2026

**New decisions**
- **Launch moves to Friday.** The planned date has shifted — all dependent tasks should adjust their timelines accordingly.

**Open requests (with owner)**
- **Priya** — confirm the venue (priya@example.com, +1 555 0100). [redacted]
- **Nobody assigned** — someone needs to own the press release. This is unowned and needs allocation.

**Needs the person**
- Who should own the press release? It has no assignee and won't move without one.`
	launch := standing.Item{ID: "5777cd193915f9d2", Words: "Inbox reports for Launch never quote email addresses or phone numbers; write [redacted].", When: standing.When{Kind: standing.WhenHold}}
	marketing := standing.Item{ID: "d9bd6c94674e37cb", Words: "Marketing review notes cite the spec line behind every finding and never edit the copy itself.", When: standing.When{Kind: standing.WhenHold}}
	for _, c := range []struct {
		name   string
		rules  []standing.Item
		report string
		kept   bool
		quotes []string
	}{
		{"live violation", []standing.Item{launch}, violating, false, []string{"priya@example.com", "555 0100"}},
		{"same report redacted", []standing.Item{launch}, strings.Replace(violating, "(priya@example.com, +1 555 0100). [redacted]", "([redacted]).", 1), true, nil},
		{"a rule a report cannot show", []standing.Item{marketing}, "# Review notes\n- The copy says \"works offline\"; the spec says \"Offline support: removed in this release.\"", true, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			client, err := provider.NewClient(provider.Config{APIKey: key, BaseURL: "https://openrouter.ai/api/v1", Model: model})
			if err != nil {
				t.Fatal(err)
			}
			agent, _ := newTestAgent(t, client, func(cfg *Config) { cfg.Model = model })
			verdict := agent.checkStandingReport(context.Background(), c.rules, c.report)
			t.Logf("RULES CHECK model=%s answered=%v kept=%v rule=%q quote=%q why=%q trouble=%q spend=$%.6f",
				model, verdict.answered, verdict.kept, verdict.rule, verdict.quote, verdict.why, verdict.trouble, agent.Usage().CostUSD)
			if !verdict.answered || verdict.kept != c.kept {
				t.Fatalf("the check read %q as kept=%v (answered=%v), want kept=%v", c.name, verdict.kept, verdict.answered, c.kept)
			}
			if len(c.quotes) > 0 {
				found := false
				for _, quote := range c.quotes {
					found = found || strings.Contains(verdict.quote, quote)
				}
				if !found {
					t.Fatalf("the finding quotes %q, not the contact details", verdict.quote)
				}
			}
			if agent.Usage().CostUSD > 0.05 {
				t.Fatal("one rules check exceeded $0.05")
			}
		})
	}
}
