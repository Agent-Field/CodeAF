package main

import (
	"strings"
	"testing"
)

// A section's body runs to the next heading at its own level or above, so a
// report that files its findings under one sub-heading per document is read
// whole rather than as an empty section.
func TestSectionBodySpansSubheadings(t *testing.T) {
	text := "# Report\n\n## Summary\n\nfive documents, one quarter.\n\n## Findings\n\n### deploys.md\n\n47 deploys, 3 rollbacks.\n\n### spend.md\n\n$4,210 a month.\n\n## Risks\n\nthe search cluster is a single point of failure.\n\n## Recommendations\n\n1. gate migrations on their backfills\n2. right-size the search cluster\n3. tune the staging alert\n"
	body, ok := sectionBody(text, "Findings")
	if !ok {
		t.Fatal("no Findings section")
	}
	for _, want := range []string{"deploys.md", "47 deploys", "spend.md", "$4,210"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Findings body lost %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Risks") || strings.Contains(body, "single point") {
		t.Fatalf("Findings body ran into the next section:\n%s", body)
	}
	if g := gradeC4Text(text); !g.Pass {
		t.Fatalf("a report with sub-headed findings should pass: %s", g.Detail)
	}
}
