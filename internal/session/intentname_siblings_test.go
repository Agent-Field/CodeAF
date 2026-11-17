package session

import (
	"strings"
	"testing"
)

func TestIntentNamesReadSiblingTitlesForEachCall(t *testing.T) {
	names := []string{"Old sibling"}
	c := intentFixture(2, func(n int) intentAnswer {
		if n < 2 {
			return intentAnswer{title: "KEEP_NAME"}
		}
		return intentAnswer{title: "Sourdough apple smell"}
	})
	a, _ := intentAgent(t, c, func(cfg *Config) { cfg.IntentSiblingNames = func() []string { return names } })
	submitMessage(t, a, Submission{Text: "hi"})
	settleNaming(a)
	names = []string{"Renamed sibling", "New sibling"}
	submitMessage(t, a, Submission{Text: "my sourdough smells like apples"})
	settleNaming(a)
	if c.asks() != 3 {
		t.Fatalf("naming calls = %d, want 3", c.asks())
	}
	if !strings.Contains(c.ask(0).request(), "Old sibling") {
		t.Fatal("first naming request lacks original sibling")
	}
	body := c.ask(2).request()
	if strings.Contains(body, "Old sibling") || !strings.Contains(body, "Renamed sibling") || !strings.Contains(body, "New sibling") {
		t.Fatalf("naming request used stale siblings: %s", body)
	}
}

func TestIntentShorterRepairReadsCurrentSiblingTitles(t *testing.T) {
	names := []string{"Old sibling"}
	c := intentFixture(1, func(n int) intentAnswer {
		if n == 0 {
			names = []string{"New sibling"}
			return intentAnswer{title: strings.Repeat("x", 25)}
		}
		return intentAnswer{title: "Mira release review"}
	})
	a, _ := intentAgent(t, c, func(cfg *Config) { cfg.IntentSiblingNames = func() []string { return names } })
	submitMessage(t, a, Submission{Text: "Review the release with Mira"})
	settleNaming(a)
	if c.asks() != 2 {
		t.Fatalf("naming calls = %d, want original plus repair", c.asks())
	}
	if body := c.ask(1).request(); strings.Contains(body, "Old sibling") || !strings.Contains(body, "New sibling") {
		t.Fatalf("repair reused old sibling names: %s", body)
	}
}
