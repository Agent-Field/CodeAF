package secaf

// What the audit hands back: the files of its full report, written in the
// run's record folder, and the short account of them the conversation reads.
//
// THE ACCOUNT IS IN A PERSON'S WORDS. sec-af's own vocabulary — a finding's
// "verdict", "verified", "not_exploitable" — is its machinery, and this house
// draws none of it: a finding here is confirmed, likely, unclear or ruled out.
// The JSON report keeps sec-af's field names, because it is data a tool reads.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/secaf/output"
	"github.com/Agent-Field/codeaf/internal/secaf/schemas"
)

// Report file names in the record folder.
const (
	reportJSON     = "security-audit.json"
	reportMarkdown = "security-audit.md"
	reportSARIF    = "security-audit.sarif"
	// reportCompliance is written only when the audit was asked to map its
	// findings to compliance frameworks.
	reportCompliance = "security-audit-compliance.md"
)

// reportFiles writes the full report into folder and answers the paths it
// wrote, in a fixed order. A folder of "" writes nothing.
func reportFiles(folder string, result schemas.SecurityAuditResult, compliance bool) ([]string, error) {
	if strings.TrimSpace(folder) == "" {
		return nil, nil
	}
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return nil, err
	}
	files := []struct {
		name string
		body []byte
	}{
		{reportMarkdown, []byte(output.GenerateReport(result))},
		{reportJSON, []byte(output.GenerateJSON(result, true) + "\n")},
		{reportSARIF, []byte(result.Sarif)},
	}
	if compliance {
		files = append(files, struct {
			name string
			body []byte
		}{reportCompliance, []byte(output.GenerateComplianceReport(result))})
	}
	var written []string
	for _, file := range files {
		if len(strings.TrimSpace(string(file.body))) == 0 {
			continue
		}
		path := filepath.Join(folder, file.name)
		if err := os.WriteFile(path, file.body, 0o600); err != nil {
			return written, err
		}
		written = append(written, path)
	}
	return written, nil
}

// findingWord is how a finding came out, in a person's words.
func findingWord(verdict schemas.ExploitVerdict) string {
	switch verdict {
	case schemas.VerdictConfirmed:
		return "confirmed"
	case schemas.VerdictLikely:
		return "likely"
	case schemas.VerdictNotExploitable:
		return "ruled out"
	}
	return "unclear"
}

// severityRank orders severities most serious first.
func severityRank(severity schemas.Severity) int {
	switch severity {
	case schemas.SeverityCritical:
		return 0
	case schemas.SeverityHigh:
		return 1
	case schemas.SeverityMedium:
		return 2
	case schemas.SeverityLow:
		return 3
	}
	return 4
}

// summaryFindings is how many findings the account lists one by one; the rest
// are counted, and the report holds them all.
const summaryFindings = 12

// summary is the account of a finished audit the conversation reads: what was
// audited, what it found in counts, the findings that stand one per line with
// where and how to fix, and where the full report is.
func summary(scope Scope, changes *Changes, result schemas.SecurityAuditResult, files []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Security audit of %s", scope.Describe())
	if changes != nil {
		fmt.Fprintf(&b, " (%d changed files since %s, %d files near them)", len(changes.Changed), changes.BaseName, len(changes.Nearby))
	}
	b.WriteString(".\n")
	standing := make([]schemas.VerifiedFinding, 0, len(result.Findings))
	for _, finding := range result.Findings {
		if finding.Verdict == schemas.VerdictConfirmed || finding.Verdict == schemas.VerdictLikely {
			standing = append(standing, finding)
		}
	}
	if len(standing) == 0 {
		b.WriteString("It found nothing it could show exploitable")
		if result.Inconclusive > 0 {
			fmt.Fprintf(&b, "; %d finding%s stayed unclear and are in the report", result.Inconclusive, plural(result.Inconclusive))
		}
		b.WriteString(".\n")
	} else {
		fmt.Fprintf(&b, "Found %d problem%s: %d confirmed and %d likely", len(standing), plural(len(standing)), result.Confirmed, result.Likely)
		if counts := severityCounts(standing); counts != "" {
			b.WriteString(" (" + counts + ")")
		}
		b.WriteString(".")
		if result.Inconclusive > 0 || result.NotExploitable > 0 {
			fmt.Fprintf(&b, " %d more stayed unclear and %d were ruled out.", result.Inconclusive, result.NotExploitable)
		}
		b.WriteString("\n\n")
		sort.SliceStable(standing, func(i, j int) bool {
			if a, b := severityRank(standing[i].Severity), severityRank(standing[j].Severity); a != b {
				return a < b
			}
			return standing[i].Verdict == schemas.VerdictConfirmed && standing[j].Verdict != schemas.VerdictConfirmed
		})
		for index, finding := range standing {
			if index == summaryFindings {
				fmt.Fprintf(&b, "- and %d more in the report\n", len(standing)-index)
				break
			}
			fmt.Fprintf(&b, "- %s · %s · %s", finding.Severity, findingWord(finding.Verdict), oneLine(finding.Title))
			if where := locationWord(finding.Location); where != "" {
				b.WriteString(" · " + where)
			}
			if finding.CweID != "" {
				b.WriteString(" · " + finding.CweID)
			}
			if fix := fixWord(finding.Remediation); fix != "" {
				b.WriteString("\n  fix: " + fix)
			}
			b.WriteString("\n")
		}
	}
	if len(result.AttackChains) > 0 {
		fmt.Fprintf(&b, "\n%d attack chain%s link findings together; the report draws them.\n", len(result.AttackChains), plural(len(result.AttackChains)))
	}
	if len(files) > 0 {
		b.WriteString("\nFull report: " + strings.Join(files, ", ") + "\n")
	}
	return strings.TrimSpace(b.String())
}

func severityCounts(findings []schemas.VerifiedFinding) string {
	counts := map[schemas.Severity]int{}
	for _, finding := range findings {
		counts[finding.Severity]++
	}
	var parts []string
	for _, severity := range []schemas.Severity{schemas.SeverityCritical, schemas.SeverityHigh, schemas.SeverityMedium, schemas.SeverityLow, schemas.SeverityInfo} {
		if counts[severity] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[severity], severity))
		}
	}
	return strings.Join(parts, ", ")
}

func locationWord(location schemas.Location) string {
	if strings.TrimSpace(location.FilePath) == "" {
		return ""
	}
	if location.StartLine > 0 {
		return fmt.Sprintf("%s:%d", location.FilePath, location.StartLine)
	}
	return location.FilePath
}

// fixWord is a finding's fix in one line, cut where it runs long.
func fixWord(remediation *schemas.RemediationSuggestion) string {
	if remediation == nil {
		return ""
	}
	fix := oneLine(remediation.FixDescription)
	if runes := []rune(fix); len(runes) > 220 {
		fix = string(runes[:220]) + "…"
	}
	return fix
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
