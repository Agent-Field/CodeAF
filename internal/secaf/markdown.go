package secaf

// sec's readable report, `sec-report.md`, in the words the conversation's
// account uses.
//
// sec-af wrote its own (internal/secaf/output's GenerateReport, kept byte for
// byte with its Python original and its goldens), and it spoke sec-af's
// vocabulary — a finding's "Verdict: inconclusive", "not exploitable",
// "noise reduction" — beside figures that are not true here: its cost was
// always $0.00, its commit `HEAD` and its provider `harness`. A person read
// "unclear" in the chat and "inconclusive" in the file about the same finding.
// So the readable file is written here, from the same result, in the same
// words as the account: confirmed, likely, unclear, ruled out. The JSON and the
// SARIF keep sec-af's field names, because tools read them.
//
// THE EMPTINESS LAW HOLDS IN THE FILE TOO: a figure nobody measured, a field
// the audit left empty, is not written, never written as zero.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/secaf/schemas"
)

// runFacts is what the run knows about itself that the audit's result does
// not: what it audited, what it spent and how long it took.
type runFacts struct {
	scope    Scope
	changes  *Changes
	spent    float64
	sessions int
	calls    int
	took     time.Duration
	// cutAt is the phase the run's time ceiling stopped it in, and wall that
	// ceiling in words; empty for a run that finished in its time.
	cutAt, wall string
}

// markdownReport is the readable report.
func markdownReport(result schemas.SecurityAuditResult, run runFacts) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# sec — security audit of %s\n\n", run.scope.Describe())
	var facts []string
	if result.Repository != "" {
		facts = append(facts, "`"+result.Repository+"`")
	}
	if result.Branch != nil && strings.TrimSpace(*result.Branch) != "" {
		facts = append(facts, "branch `"+*result.Branch+"`")
	}
	if run.changes != nil {
		facts = append(facts, fmt.Sprintf("%d changed file%s since `%s`, %d near them",
			len(run.changes.Changed), plural(len(run.changes.Changed)), run.changes.BaseName, len(run.changes.Nearby)))
	}
	if stamp := strings.TrimSpace(result.Timestamp.String()); stamp != "" {
		facts = append(facts, stamp)
	}
	if len(facts) > 0 {
		b.WriteString(strings.Join(facts, " · ") + "\n\n")
	}

	if run.cutAt != "" {
		fmt.Fprintf(&b, "> **Cut short.** %s\n\n", cutSentence(run.cutAt, run.wall))
	}
	b.WriteString("## What it found\n\n")
	standing, unclear, ruledOut := sortFindings(result.Findings)
	if len(standing) == 0 {
		b.WriteString("Nothing it could show exploitable.")
	} else {
		fmt.Fprintf(&b, "%d problem%s: %d confirmed and %d likely", len(standing), plural(len(standing)), result.Confirmed, result.Likely)
		if counts := severityCounts(standing); counts != "" {
			b.WriteString(" (" + counts + ")")
		}
		b.WriteString(".")
	}
	if len(unclear) > 0 {
		fmt.Fprintf(&b, " %d stayed unclear: it could neither show them exploitable nor rule them out.", len(unclear))
	}
	if len(ruledOut) > 0 {
		fmt.Fprintf(&b, " %d %s ruled out.", len(ruledOut), wasWere(len(ruledOut)))
	}
	b.WriteString("\n\n")

	if len(standing) > 0 {
		b.WriteString("## Confirmed and likely\n\n")
		for _, finding := range standing {
			writeFinding(&b, finding)
		}
	}
	if len(unclear) > 0 {
		b.WriteString("## Unclear\n\nWorth a look by someone who knows the code; the audit could not settle these either way.\n\n")
		for _, finding := range unclear {
			writeFinding(&b, finding)
		}
	}
	if len(ruledOut) > 0 {
		b.WriteString("## Ruled out\n\n")
		for _, finding := range ruledOut {
			fmt.Fprintf(&b, "- %s", oneLine(finding.Title))
			if where := locationWord(finding.Location); where != "" {
				b.WriteString(" · `" + where + "`")
			}
			if why := oneLine(finding.Rationale); why != "" {
				b.WriteString(" — " + why)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if len(result.AttackChains) > 0 {
		titles := map[string]string{}
		for _, finding := range result.Findings {
			titles[finding.ID] = oneLine(finding.Title)
		}
		b.WriteString("## Attack chains\n\nFindings that are worse together than apart.\n\n")
		for _, chain := range result.AttackChains {
			fmt.Fprintf(&b, "### %s", oneLine(chain.Title))
			if chain.CombinedSeverity != "" {
				fmt.Fprintf(&b, " · %s", chain.CombinedSeverity)
			}
			b.WriteString("\n\n")
			if text := oneLine(chain.Description); text != "" {
				b.WriteString(text + "\n\n")
			}
			if impact := oneLine(chain.CombinedImpact); impact != "" {
				b.WriteString("- Impact: " + impact + "\n")
			}
			for _, id := range chain.Findings {
				if title := titles[id]; title != "" {
					b.WriteString("- " + title + "\n")
				}
			}
			for _, mapping := range chain.MitreAttackMapping {
				fmt.Fprintf(&b, "- MITRE ATT&CK %s (%s): %s\n", mapping.TechniqueID, mapping.Tactic, mapping.TechniqueName)
			}
			b.WriteString("\n")
		}
	}

	if len(result.ComplianceGaps) > 0 {
		b.WriteString("## Compliance gaps\n\n")
		for _, gap := range result.ComplianceGaps {
			fmt.Fprintf(&b, "- %s %s: %s · %d finding%s, worst %s\n", gap.Framework, gap.ControlID, gap.ControlName,
				gap.FindingCount, plural(gap.FindingCount), gap.MaxSeverity)
		}
		b.WriteString("\n")
	}

	var ran []string
	if run.sessions > 0 {
		ran = append(ran, fmt.Sprintf("%d agent session%s", run.sessions, plural(run.sessions)))
	}
	if run.calls > 0 {
		ran = append(ran, fmt.Sprintf("%d single call%s", run.calls, plural(run.calls)))
	}
	if run.spent > 0 {
		ran = append(ran, fmt.Sprintf("$%.2f", run.spent))
	}
	if run.took >= time.Second {
		ran = append(ran, run.took.Round(time.Second).String())
	}
	if len(ran) > 0 {
		b.WriteString("## How it ran\n\n" + strings.Join(ran, " · ") + ", at " + run.scope.Depth + " depth.\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// sortFindings splits the findings by how they came out, each most serious
// first.
func sortFindings(findings []schemas.VerifiedFinding) (standing, unclear, ruledOut []schemas.VerifiedFinding) {
	for _, finding := range findings {
		switch finding.Verdict {
		case schemas.VerdictConfirmed, schemas.VerdictLikely:
			standing = append(standing, finding)
		case schemas.VerdictNotExploitable:
			ruledOut = append(ruledOut, finding)
		default:
			unclear = append(unclear, finding)
		}
	}
	for _, list := range [][]schemas.VerifiedFinding{standing, unclear, ruledOut} {
		sort.SliceStable(list, func(i, j int) bool {
			if a, b := severityRank(list[i].Severity), severityRank(list[j].Severity); a != b {
				return a < b
			}
			return list[i].Verdict == schemas.VerdictConfirmed && list[j].Verdict != schemas.VerdictConfirmed
		})
	}
	return standing, unclear, ruledOut
}

// writeFinding is one finding in full: where, what weakness, why it stands,
// how data reaches it, the attack, and the fix.
func writeFinding(b *strings.Builder, finding schemas.VerifiedFinding) {
	fmt.Fprintf(b, "### %s · %s · %s\n\n", finding.Severity, findingWord(finding.Verdict), oneLine(finding.Title))
	if where := locationWord(finding.Location); where != "" {
		line := "- Where: `" + where + "`"
		if fn := finding.Location.FunctionName; fn != nil && strings.TrimSpace(*fn) != "" {
			line += " in `" + strings.TrimSpace(*fn) + "`"
		}
		b.WriteString(line + "\n")
	}
	if finding.CweID != "" {
		line := "- Weakness: " + finding.CweID
		if name := oneLine(finding.CweName); name != "" {
			line += " (" + name + ")"
		}
		if owasp := finding.OwaspCategory; owasp != nil && strings.TrimSpace(*owasp) != "" {
			line += " · OWASP " + strings.TrimSpace(*owasp)
		}
		b.WriteString(line + "\n")
	}
	if finding.ExploitabilityScore > 0 {
		fmt.Fprintf(b, "- Exploitability: %.1f of 10\n", finding.ExploitabilityScore)
	}
	if why := oneLine(finding.Rationale); why != "" {
		b.WriteString("- Why: " + why + "\n")
	}
	if proof := finding.Proof; proof != nil {
		if len(proof.DataFlowTrace) > 0 {
			b.WriteString("- How the data gets there:\n")
			for _, step := range proof.DataFlowTrace {
				where := step.File
				if step.Line > 0 {
					where = fmt.Sprintf("%s:%d", step.File, step.Line)
				}
				if strings.TrimSpace(where) != "" {
					fmt.Fprintf(b, "  - `%s` %s\n", where, oneLine(step.Description))
				} else {
					fmt.Fprintf(b, "  - %s\n", oneLine(step.Description))
				}
			}
		}
		if attack := oneLine(proof.ExploitHypothesis); attack != "" {
			b.WriteString("- The attack: " + attack + "\n")
		}
		if payload := proof.ExploitPayload; payload != nil && strings.TrimSpace(*payload) != "" {
			b.WriteString("- Payload: `" + oneLine(*payload) + "`\n")
		}
	}
	if fix := finding.Remediation; fix != nil {
		if text := oneLine(fix.FixDescription); text != "" {
			b.WriteString("- Fix: " + text + "\n")
		}
		if patch := strings.TrimSpace(fix.PatchDiff); patch != "" {
			b.WriteString("\n```diff\n" + patch + "\n```\n")
		}
	}
	if len(finding.Compliance) > 0 {
		var controls []string
		for _, mapping := range finding.Compliance {
			controls = append(controls, strings.TrimSpace(mapping.Framework+" "+mapping.ControlID))
		}
		b.WriteString("- Compliance: " + strings.Join(controls, ", ") + "\n")
	}
	b.WriteString("\n")
}

func wasWere(n int) string {
	if n == 1 {
		return "was"
	}
	return "were"
}

func areIs(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}
