package praf

// What a review hands back: the files of its full report, written in the
// run's record folder, and the short account of them the conversation reads.
//
// THE ACCOUNT'S FIRST LINE SAYS WHAT IT FOUND, because the first line is what
// the project's index keeps of a run and all another conversation reads of it
// there. The rest is the findings one per line, blocking first, with where
// they are, and where the full report is.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/praf/schemas"
)

// Report file names in the record folder: the program's own name on each.
const (
	reportMarkdown = "pr-report.md"
	reportJSON     = "pr-report.json"
)

// accountFindings is how many findings the account lists one per line; the
// rest are counted, and the report holds them all.
const accountFindings = 12

// evidenceCap bounds one finding's quoted evidence in the report.
const evidenceCap = 1200

// Saved is the review as pr-report.json keeps it: the pull request it is of,
// down to the commit that was reviewed, and the review pr-af built, including
// the GitHub review a later `/pr post` sends as it stands.
type Saved struct {
	Program     string               `json:"program"`
	PullRequest SavedPullRequest     `json:"pull_request"`
	Focus       string               `json:"focus,omitempty"`
	Review      schemas.ReviewResult `json:"review"`
}

// SavedPullRequest is the pull request a saved review is of.
type SavedPullRequest struct {
	Owner   string `json:"owner"`
	Repo    string `json:"repo"`
	Number  int    `json:"number"`
	URL     string `json:"url"`
	Title   string `json:"title,omitempty"`
	HeadSHA string `json:"head_sha"`
}

// Target is the saved pull request as a Target.
func (p SavedPullRequest) Target() Target {
	return Target{Owner: p.Owner, Repo: p.Repo, Number: p.Number}
}

// runFacts is what the run measured about itself, for the report.
type runFacts struct {
	target   Target
	title    string
	focus    string
	checkout string
	spent    float64
	sessions int
	calls    int
	cutAt    string
	wall     string
	failNote string
}

// writeReport writes the full report into folder and answers the paths it
// wrote. A folder of "" writes nothing.
func writeReport(folder string, saved Saved, run runFacts) ([]string, error) {
	if strings.TrimSpace(folder) == "" {
		return nil, nil
	}
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return nil, err
	}
	var written []string
	for _, file := range []struct {
		name string
		body []byte
	}{
		{reportMarkdown, []byte(markdownReport(saved.Review, run))},
		{reportJSON, append(data, '\n')},
	} {
		path := filepath.Join(folder, file.name)
		if err := os.WriteFile(path, file.body, 0o600); err != nil {
			return written, err
		}
		written = append(written, path)
	}
	return written, nil
}

// account is a finished review as the conversation reads it.
func account(review schemas.ReviewResult, run runFacts, files []string) string {
	findings := sortedFindings(review.Findings)
	var b strings.Builder
	b.WriteString(outcomeLine(review, run) + "\n")
	if run.cutAt != "" {
		b.WriteString(cutSentence(run.cutAt, run.wall) + "\n")
	}
	if run.failNote != "" {
		b.WriteString(run.failNote + "\n")
	}
	if len(findings) > 0 {
		b.WriteString("\n")
	}
	for index, f := range findings {
		if index == accountFindings {
			fmt.Fprintf(&b, "- and %d more in the report\n", len(findings)-index)
			break
		}
		label := string(f.Severity)
		if f.Blocking {
			label += " · blocking"
		}
		fmt.Fprintf(&b, "- %s · %s", label, plainWords(oneLine(f.Title)))
		if where := location(f, run.checkout); where != "" {
			b.WriteString(" · " + where)
		}
		if f.Suggestion != nil {
			if fix := oneLine(*f.Suggestion); fix != "" {
				if runes := []rune(fix); len(runes) > 220 {
					fix = string(runes[:220]) + "…"
				}
				b.WriteString("\n  fix: " + plainWords(fix))
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("\nNothing was posted to GitHub.")
	if len(files) > 0 {
		b.WriteString("\nFull report: " + strings.Join(files, ", "))
	}
	return strings.TrimSpace(b.String())
}

// outcomeLine is the account's first line: the pull request, and what the
// review found in counts, blocking first.
func outcomeLine(review schemas.ReviewResult, run runFacts) string {
	what := "Review of " + run.target.String()
	if title := oneLine(run.title); title != "" {
		what += " (" + title + ")"
	}
	s := review.Summary
	if s.TotalFindings == 0 {
		return what + ": no findings."
	}
	line := fmt.Sprintf("%s: %d finding%s, %d blocking the merge", what, s.TotalFindings, plural(s.TotalFindings), s.BlockingCount)
	if counts := severityCounts(s.BySeverity); counts != "" {
		line += " (" + counts + ")"
	}
	return line + "."
}

// cutSentence says the run's time ceiling stopped it, where, and what that
// left undone.
func cutSentence(stage, wall string) string {
	ceiling := "its time ceiling"
	if wall != "" {
		ceiling += " of " + wall
	}
	undone := "the checks after it did not run, so it covers part of the change"
	switch stage {
	case stageWords[stageReview]:
		undone = "the reviewers still running were stopped and their parts of the change were not reviewed"
	case stageWords[stageIntake], stageWords[stageAnatomy], stageWords[stagePlan]:
		undone = "it stopped before reviewing most of the change"
	}
	return fmt.Sprintf("It reached %s while %s, so %s.", ceiling, stage, undone)
}

func severityCounts(bySeverity map[string]int) string {
	var parts []string
	for _, severity := range []string{"critical", "important", "suggestion", "nitpick"} {
		if n := bySeverity[severity]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, severity))
		}
	}
	return strings.Join(parts, ", ")
}

// markdownReport is the full review as plain Markdown: every finding, most
// urgent first, each with where, why and what to do. It is not the GitHub
// summary pr-af posts (HTML details blocks and badges for a pull request's
// page); it is for reading here.
func markdownReport(review schemas.ReviewResult, run runFacts) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# pr review of %s\n\n", run.target)
	if url := run.target.URL(); url != "" {
		fmt.Fprintf(&b, "%s\n\n", url)
	}
	b.WriteString(outcomeLine(review, run) + "\n\n")
	if run.cutAt != "" {
		b.WriteString(cutSentence(run.cutAt, run.wall) + "\n\n")
	}
	if run.failNote != "" {
		b.WriteString(run.failNote + "\n\n")
	}
	if run.focus != "" {
		b.WriteString("Asked to focus on: " + plainWords(oneLine(run.focus)) + "\n\n")
	}
	b.WriteString("Nothing was posted to GitHub: this review exists only here.\n")
	if summary, _ := review.Metadata.Intake["pr_summary"].(string); strings.TrimSpace(summary) != "" {
		b.WriteString("\n## What the pull request does\n\n" + plainWords(strings.TrimSpace(summary)) + "\n")
	}
	b.WriteString("\n## Findings\n")
	findings := sortedFindings(review.Findings)
	if len(findings) == 0 {
		b.WriteString("\nNo issues found across the parts of the change that were reviewed.\n")
	}
	for i, f := range findings {
		b.WriteString(markdownFinding(i+1, f, run.checkout))
	}
	fmt.Fprintf(&b, "\n---\n%d review dimension%s · %d agent session%s · %d single call%s · $%.4f\n",
		review.Summary.DimensionsRun, plural(review.Summary.DimensionsRun),
		run.sessions, plural(run.sessions), run.calls, plural(run.calls), run.spent)
	return b.String()
}

func markdownFinding(n int, f schemas.ScoredFinding, checkout string) string {
	var b strings.Builder
	label := string(f.Severity)
	if f.Blocking {
		label += ", blocking"
	}
	fmt.Fprintf(&b, "\n### %d. [%s] %s\n\n", n, label, plainWords(strings.TrimSpace(f.Title)))
	var where []string
	if loc := location(f, checkout); loc != "" {
		where = append(where, "`"+loc+"`")
	}
	if f.DimensionName != "" {
		where = append(where, plainWords(f.DimensionName))
	}
	where = append(where, fmt.Sprintf("confidence %d%%", int(f.Confidence*100)))
	b.WriteString(strings.Join(where, " · ") + "\n")
	if body := strings.TrimSpace(f.Body); body != "" {
		b.WriteString("\n" + plainWords(body) + "\n")
	}
	if reason := strings.TrimSpace(f.BlockingReason); reason != "" {
		if f.Blocking {
			b.WriteString("\nWhy it blocks the merge: " + plainWords(reason) + "\n")
		} else {
			b.WriteString("\nWhy it does not block the merge: " + plainWords(reason) + "\n")
		}
	}
	if f.Suggestion != nil {
		if s := strings.TrimSpace(*f.Suggestion); s != "" {
			b.WriteString("\nSuggested fix:\n\n" + plainWords(s) + "\n")
		}
	}
	if ev := strings.TrimSpace(f.Evidence); ev != "" {
		if runes := []rune(ev); len(runes) > evidenceCap {
			ev = string(runes[:evidenceCap]) + " …"
		}
		b.WriteString("\nEvidence:\n\n")
		for _, line := range strings.Split(ev, "\n") {
			b.WriteString("> " + line + "\n")
		}
	}
	return b.String()
}

// location is path:line or path:start-end, relative to the review's checkout.
func location(f schemas.ScoredFinding, checkout string) string {
	path := f.FilePath
	if checkout != "" {
		path = strings.TrimPrefix(path, strings.TrimSuffix(checkout, "/")+"/")
	}
	switch {
	case path == "":
		return ""
	case f.LineStart <= 0:
		return path
	case f.LineEnd > f.LineStart:
		return fmt.Sprintf("%s:%d-%d", path, f.LineStart, f.LineEnd)
	default:
		return fmt.Sprintf("%s:%d", path, f.LineStart)
	}
}

var severityOrder = map[string]int{"critical": 0, "important": 1, "suggestion": 2, "nitpick": 3}

// sortedFindings orders findings blocking first, then by severity, then by
// score: the order a person should read them in.
func sortedFindings(in []schemas.ScoredFinding) []schemas.ScoredFinding {
	out := append([]schemas.ScoredFinding(nil), in...)
	rank := func(s schemas.Severity) int {
		if r, ok := severityOrder[string(s)]; ok {
			return r
		}
		return len(severityOrder)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Blocking != b.Blocking {
			return a.Blocking
		}
		if ra, rb := rank(a.Severity), rank(b.Severity); ra != rb {
			return ra < rb
		}
		return a.Score > b.Score
	})
	return out
}
