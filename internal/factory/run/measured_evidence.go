package run

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// Numeric totals belong to command receipts. Preserve a model's findings but
// remove guessed totals from its prose, including claims passed to later steps.
var proseTestTotals = regexp.MustCompile(`(?i)\b(?:all\s+)?[0-9]+\+?\s+(?:(?:named|retrieval|package)\s+)*(?:tests?|outcomes?)\s+(?:pass(?:ed)?|are\s+passing)\b`)

func measuredTestEvidence(r factory.StageResult) factory.StageResult {
	if r.Tests == nil {
		return r
	}
	normalize := func(s string) string { return proseTestTotals.ReplaceAllString(s, "tests passed") }
	r.Output = normalize(r.Output)
	for i := range r.Claims {
		r.Claims[i].Text = normalize(r.Claims[i].Text)
		r.Claims[i].Evidence = normalize(r.Claims[i].Evidence)
	}
	c := r.Tests
	line := fmt.Sprintf("Measured tests: %d passed, %d failed, %d skipped (largest measured command in this step; includes named subtests).", c.Passed, c.Failed, c.Skipped)
	if c.Command != "" {
		line += " Command: `" + c.Command + "`."
	}
	r.Output = strings.TrimSpace(r.Output) + "\n\n" + line
	return r
}
