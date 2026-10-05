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
	"time"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/secaf/output"
	"github.com/Agent-Field/codeaf/internal/secaf/schemas"
)

// Report file names in the record folder: the program's own name on each, as
// it is on every surface.
const (
	reportJSON     = "sec-report.json"
	reportMarkdown = "sec-report.md"
	reportSARIF    = "sec-report.sarif"
	// reportCompliance is written only when the audit was asked to map its
	// findings to compliance frameworks.
	reportCompliance = "sec-compliance.md"
)

// sarifTool is who sec's SARIF says produced it. A code-scanning tool shows
// the driver's name beside every result, and the program a person ran is sec,
// so the log names it, its build and its home rather than sec-af's.
func sarifTool() output.SarifTool {
	return output.SarifTool{Name: Name, Version: buildinfo.Revision(),
		InformationURI: "https://github.com/Agent-Field/CodeAF", PropertyPrefix: Name}
}

// complianceBrand is how sec signs its compliance report.
var complianceBrand = output.ComplianceBrand{
	Heading:   "# sec — compliance report",
	Signature: "*Written by sec, the security audit codeaf carries.*",
}

// reportFiles writes the full report into folder and answers the paths it
// wrote, in a fixed order. A folder of "" writes nothing.
func reportFiles(folder string, result schemas.SecurityAuditResult, compliance bool, run runFacts) ([]string, error) {
	if strings.TrimSpace(folder) == "" {
		return nil, nil
	}
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return nil, err
	}
	// THE FILES SAY WHAT THIS RUN MEASURED. sec-af's own result carries a
	// cost and an agent count it never filled — $0.00 and the calls between
	// its own parts — and a provider word ("harness") that is not true here.
	// The run's own figures go in their place, and the SARIF is written again
	// under sec's name.
	result.CostUsd = run.spent
	result.AgentInvocations = run.sessions + run.calls
	result.Provider = "codeaf"
	result.Findings = output.RuleIDsAs(result.Findings, sarifTool())
	result.Sarif = output.GenerateSarifAs(result, sarifTool())
	files := []struct {
		name string
		body []byte
	}{
		{reportMarkdown, []byte(markdownReport(result, run))},
		{reportJSON, []byte(output.GenerateJSON(result, true) + "\n")},
		{reportSARIF, []byte(result.Sarif)},
	}
	if compliance {
		files = append(files, struct {
			name string
			body []byte
		}{reportCompliance, []byte(output.GenerateComplianceReportAs(result, time.Now().UTC(), complianceBrand))})
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
func summary(scope Scope, changes *Changes, result schemas.SecurityAuditResult, files []string, cutAt, wall string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Security audit of %s", scope.Describe())
	if changes != nil {
		fmt.Fprintf(&b, " (%d changed files since %s, %d files near them)", len(changes.Changed), changes.BaseName, len(changes.Nearby))
	}
	b.WriteString(".\n")
	if cutAt != "" {
		b.WriteString(cutSentence(cutAt, wall) + "\n")
	}
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

// outcomeLine is a finished audit in one line, for its ending: how many
// problems stand, or that none does.
func outcomeLine(result schemas.SecurityAuditResult, cutAt string) string {
	standing := result.Confirmed + result.Likely
	line := "it found nothing it could show exploitable"
	if standing > 0 {
		line = fmt.Sprintf("it found %d problem%s: %d confirmed, %d likely", standing, plural(standing), result.Confirmed, result.Likely)
	}
	if cutAt != "" {
		line += ", before its time ceiling cut it short during " + cutAt
	}
	return line
}

// cutSentence says that the run's time ceiling stopped it, in which phase, and
// what that left undone.
func cutSentence(phase, wall string) string {
	ceiling := "its time ceiling"
	if wall != "" {
		ceiling += " of " + wall
	}
	undone := "the work after it did not run"
	switch phase {
	case stageWords[stageProve]:
		undone = "findings whose tests had not finished are marked unclear, and no fixes were written"
	case stageWords[stageRemediation]:
		undone = "the fixes it had not finished are missing"
	case stageWords[stageHunt]:
		undone = "the hunters still running were stopped, and their findings were not tested"
	}
	return fmt.Sprintf("It reached %s during %s, so %s.", ceiling, phase, undone)
}
