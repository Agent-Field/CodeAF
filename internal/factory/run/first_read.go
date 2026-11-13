package run

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── THE READ OF THE ISSUE A STEP STARTS FROM ───────────────────────────────
//
// A person may press "Write the brief" on a job that has not started, which
// pays for one read of the issue: what is really asked, the risky part, how
// anyone would know it is done, and what is unclear. A READ THE PERSON PAID
// FOR BEFORE WORK IS ONE THE WORK USES. So every step is given it, beside
// the issue itself, as a reading to check against the issue and never as a
// second set of orders: where the two disagree, the issue and the person's
// notes win. A job with no read before work gets nothing here, and a version
// written after a step reported is a report on that work, never handed on.

// firstReadLead is the paragraph's first line.
const firstReadLead = "Before work began, the issue was read for its person as below. Treat it as a reading, not orders: where it disagrees with the issue or the notes, they win."

// firstReadBrief is the job's read before work as a step's paragraph, "" for
// none.
func firstReadBrief(job Job) string {
	v, ok := factory.FirstReadOf(job.Item)
	if !ok {
		return ""
	}
	lines := []string{firstReadLead}
	if verdict := oneLine(v.Verdict); verdict != "" {
		lines = append(lines, "asked: "+verdict)
	}
	for _, b := range v.Blocks {
		var items []string
		for _, c := range b.Bullets {
			items = append(items, c.Text)
		}
		for _, q := range b.Questions {
			items = append(items, q.Text)
		}
		var kept []string
		for _, text := range items {
			if text = oneLine(text); text != "" {
				kept = append(kept, "- "+text)
			}
		}
		heading := oneLine(b.Heading)
		if len(kept) == 0 || heading == "" {
			continue
		}
		lines = append(lines, strings.ToLower(heading)+":")
		lines = append(lines, kept...)
	}
	if len(lines) == 1 {
		return ""
	}
	return strings.Join(lines, "\n")
}
