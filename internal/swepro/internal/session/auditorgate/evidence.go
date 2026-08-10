// This file ports auditor evidence reuse, retry reminders, delta/adjudication
// selection, and frontier verdict aggregation from
// src/session/auditor-gate.ts:623-1313.
package auditorgate

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/auditconvergence"
)

type AuditCarryForwardArgs struct {
	PriorSHA        *string         `json:"priorSha,omitempty"`
	CurrentSHA      *string         `json:"currentSha,omitempty"`
	PreviousVerdict *AuditorVerdict `json:"previousVerdict,omitempty"`
	ChangedFiles    []string        `json:"changedFiles"`
	MaxLines        *float64        `json:"maxLines,omitempty"`
}

func BuildAuditCarryForwardBlock(args AuditCarryForwardArgs) string {
	maxLines := 30
	if args.MaxLines != nil {
		maxLines = int(math.Max(1, *args.MaxLines))
	}
	var summary *AuditEvidenceSummary
	notes := ""
	if args.PreviousVerdict != nil {
		value := BuildAuditEvidenceSummary(args.PreviousVerdict)
		summary = &value
		noteParts := []string{}
		if args.PreviousVerdict.Notes != nil && jscompat.Trim(*args.PreviousVerdict.Notes) != "" {
			noteParts = append(noteParts, *args.PreviousVerdict.Notes)
		}
		if args.PreviousVerdict.Step2Signal != nil &&
			args.PreviousVerdict.Step2Signal.Notes != nil &&
			jscompat.Trim(*args.PreviousVerdict.Step2Signal.Notes) != "" {
			noteParts = append(noteParts, *args.PreviousVerdict.Step2Signal.Notes)
		}
		notes = strings.Join(noteParts, " ")
	}
	priorSHA := "unknown"
	if args.PriorSHA != nil {
		priorSHA = *args.PriorSHA
	}
	shaLine := "Prior audit SHA: " + priorSHA
	if args.CurrentSHA != nil {
		shaLine += "; current HEAD: " + *args.CurrentSHA
	}
	lines := []string{
		"# Verified evidence from previous audit cycle",
		shaLine,
	}
	if summary != nil {
		reproduced := "unknown"
		if args.PreviousVerdict.Step2Signal != nil {
			reproduced = boolString(args.PreviousVerdict.Step2Signal.Reproduced)
		}
		lines = append(lines,
			"Prior verdict: "+string(args.PreviousVerdict.Verdict)+
				"; reproduced="+reproduced+
				"; commands="+jscompat.FormatNumber(summary.AuditCommandsRun)+
				"; blockers="+jscompat.FormatNumber(summary.AuditBlockers),
		)
	} else {
		lines = append(lines, "Prior verdict: unavailable (treat prior work as unverified).")
	}
	noteLine := compact(collapseJSWhitespace(notes), 240)
	if noteLine == "" {
		noteLine = "(none)"
	}
	lines = append(lines, "Prior notes: "+noteLine, "Prior commands (command + exit code):")
	if args.PreviousVerdict == nil {
		lines = append(lines, "- (none)")
	} else {
		for _, command := range verdictCommands(*args.PreviousVerdict) {
			lines = append(lines,
				"- "+compact(collapseJSWhitespace(CommandName(command)), 150)+
					" exit="+CommandExit(command),
			)
		}
	}
	lines = append(lines, "Prior blockers:")
	if args.PreviousVerdict != nil && len(args.PreviousVerdict.Blockers) > 0 {
		for _, blocker := range args.PreviousVerdict.Blockers {
			prefix := ""
			if blocker.File != nil {
				prefix = *blocker.File
				if truthyNumber(blocker.Line) {
					prefix += ":" + jscompat.FormatNumber(*blocker.Line)
				}
				prefix += " — "
			}
			lines = append(lines, "- "+prefix+compact(collapseJSWhitespace(blocker.Detail), 160))
		}
	} else {
		lines = append(lines, "- (none)")
	}
	lines = append(lines, "Prior clause coverage (clause — evidence):")
	if args.PreviousVerdict != nil && len(args.PreviousVerdict.ClauseCoverage) > 0 {
		for _, coverage := range args.PreviousVerdict.ClauseCoverage {
			lines = append(lines,
				"- "+compact(collapseJSWhitespace(coverage.Clause), 100)+
					" — "+compact(collapseJSWhitespace(coverage.Evidence), 100),
			)
		}
	} else {
		lines = append(lines, "- (none)")
	}
	lines = append(lines, "Files changed since that audit (INVALIDATION KEY):")
	if len(args.ChangedFiles) > 0 {
		for _, file := range args.ChangedFiles {
			lines = append(lines, "- "+file)
		}
	} else {
		lines = append(lines, "- (none)")
	}
	instruction := "Instruction: re-verify only what changed or was blocked. You may carry forward prior probe evidence ONLY for clauses whose implementing files are NOT in the changed-files list above; re-probe every clause whose files changed. Prior verified evidence stands unless contradicted."
	lines = append(lines, instruction)
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	keep := maxLines - 2
	if keep < 0 {
		keep = 0
	}
	capped := append([]string{}, lines[:min(keep, len(lines))]...)
	capped = append(capped, "- (additional evidence omitted by 30-line cap)", instruction)
	if len(capped) > maxLines {
		capped = capped[:maxLines]
	}
	return strings.Join(capped, "\n")
}

func BuildInadmissibleRetryReminder(
	inventory []string, rejected AuditorVerdict, rejectionReason ...*string,
) []string {
	reasonBlock := []string{}
	if len(rejectionReason) > 0 && rejectionReason[0] != nil {
		reasonBlock = []string{
			"",
			"PREVIOUS VERDICT REJECTED AS INADMISSIBLE. Exact gate reason:",
			"  " + compact(collapseJSWhitespace(*rejectionReason[0]), 400),
			"Fix THIS specific defect in your next verdict:",
			"- \"no executed command/repro evidence\" ⇒ run build/tests in THIS session and list every command in step2_signal.commands.",
			"- \"draft or incomplete\" ⇒ your FINAL rewrite must contain no draft/placeholder/pending-status language about the audit itself.",
			"- \"clause_coverage\" ⇒ populate clause_coverage with one substantive {clause, evidence} entry per spec clause (evidence = concrete probe output).",
		}
	}
	if len(inventory) == 0 {
		return reasonBlock
	}
	missing := UncoveredInventoryClauses(rejected.ClauseCoverage, inventory)
	missingSet := map[string]bool{}
	for _, clause := range missing {
		missingSet[clause] = true
	}
	coveredNums, missingNums := []string{}, []string{}
	for i, clause := range inventory {
		number := strconv.Itoa(i + 1)
		if missingSet[clause] {
			missingNums = append(missingNums, number)
		} else {
			coveredNums = append(coveredNums, number)
		}
	}
	header := []string{
		"",
		"PREVIOUS VERDICT REJECTED AS INADMISSIBLE — it did not carry per-clause",
		"evidence for every spec clause. The FULL clause inventory is repeated below",
		"at maximal recency (it first appeared far above, near the prompt top). Re-",
		"probe each MISSING clause with an independent input and record it in",
		"clause_coverage before emitting your next verdict; a pass that leaves any",
		"inventory clause uncovered is inadmissible again.",
		"",
		"# Spec clause inventory (re-injected at verdict-time)",
	}
	coveredText, missingText := "(none)", "(none)"
	if len(coveredNums) > 0 {
		coveredText = strings.Join(coveredNums, ", ")
	}
	if len(missingNums) > 0 {
		missingText = strings.Join(missingNums, ", ")
	}
	footer := []string{
		"",
		"Covered last attempt (clause_coverage entry present): " + coveredText,
		"MISSING last attempt (probe these now): " + missingText,
	}
	const capChars = 2400
	fixedLen := utf16Len(strings.Join(append(append([]string{}, header...), footer...), "\n")) + 1
	budget := max(0, capChars-fixedLen-48)
	kept := []string{}
	used := 0
	for i, clause := range inventory {
		line := strconv.Itoa(i+1) + ". " + compact(collapseJSWhitespace(clause), 120)
		if used+utf16Len(line)+1 > budget {
			kept = append(kept, "…[remaining clauses omitted to fit the recency budget]")
			break
		}
		kept = append(kept, line)
		used += utf16Len(line) + 1
	}
	out := append([]string{}, reasonBlock...)
	out = append(out, header...)
	out = append(out, kept...)
	out = append(out, footer...)
	return out
}

type ResolveAuditModeArgs struct {
	Light               *bool   `json:"light,omitempty"`
	AdaptiveCutsEnabled bool    `json:"adaptiveCutsEnabled"`
	SizeBand            *string `json:"sizeBand,omitempty"`
	SpecClauseCount     float64 `json:"specClauseCount,omitempty"`
}

func ResolveAuditMode(args ResolveAuditModeArgs) string {
	bandAllowsLight := args.SizeBand == nil || *args.SizeBand == "xs" || *args.SizeBand == "s"
	dense := args.SpecClauseCount >= DenseSpecClauseThreshold
	if args.Light != nil && *args.Light && args.AdaptiveCutsEnabled && bandAllowsLight && !dense {
		return "light"
	}
	return "full"
}

type ShouldAdjudicateArgs struct {
	AuditCycle          float64 `json:"auditCycle"`
	VerdictFlipped      bool    `json:"verdictFlipped"`
	DenseSpecForcedFull bool    `json:"denseSpecForcedFull"`
}

func ShouldAdjudicate(args ShouldAdjudicateArgs) bool {
	return args.DenseSpecForcedFull || (args.AuditCycle >= 2 && args.VerdictFlipped)
}

type ShouldDeltaScopeArgs struct {
	AuditCycle   float64 `json:"auditCycle"`
	ChangedSince float64 `json:"changedSince"`
	TotalChanged float64 `json:"totalChanged"`
}

func ShouldDeltaScope(args ShouldDeltaScopeArgs) bool {
	if args.AuditCycle < 2 || args.TotalChanged <= 0 || args.ChangedSince <= 0 {
		return false
	}
	return args.ChangedSince <= args.TotalChanged*0.5
}

var contractPassRE = regexp.MustCompile(`result: *pass\b`)

func ContractEvidenceIndicatesPass(evidence *string) bool {
	return evidence != nil && contractPassRE.MatchString(asciiLower(*evidence))
}

type AdjudicationRuling string

const (
	RulingConfirm  AdjudicationRuling = "CONFIRM"
	RulingOverturn AdjudicationRuling = "OVERTURN"
)

type ApplyAdjudicationArgs struct {
	Disputed       AuditorVerdict `json:"disputed"`
	Adjudicator    AuditorVerdict `json:"adjudicator"`
	ContractPassed *bool          `json:"contractPassed,omitempty"`
}

type ApplyAdjudicationResult struct {
	Verdict AuditorVerdict     `json:"verdict"`
	Ruling  AdjudicationRuling `json:"ruling"`
	Outcome VerdictStatus      `json:"outcome"`
	Note    string             `json:"note"`
}

func ApplyAdjudication(args ApplyAdjudicationArgs) ApplyAdjudicationResult {
	ruling := RulingOverturn
	if args.Adjudicator.Verdict == args.Disputed.Verdict {
		ruling = RulingConfirm
	}
	adjNote := ""
	if args.Adjudicator.Notes != nil {
		adjNote = sliceUTF16(collapseJSWhitespace(*args.Adjudicator.Notes), 0,
			min(400, utf16Len(collapseJSWhitespace(*args.Adjudicator.Notes))))
	}
	if args.Disputed.Verdict == VerdictPass {
		if ruling == RulingConfirm {
			notes := jscompat.Trim("[adjudicated: CONFIRM pass] " + adjNote)
			return ApplyAdjudicationResult{
				Verdict: args.Disputed.cloneSet(field("notes", notes)),
				Ruling:  ruling, Outcome: VerdictPass,
				Note: "frontier adjudicator confirmed the pass",
			}
		}
		blockers := make([]Blocker, 0, len(args.Adjudicator.Blockers))
		for _, blocker := range args.Adjudicator.Blockers {
			blockers = append(blockers, blocker.withSeverity(auditconvergence.SeverityCorrectness))
		}
		if len(blockers) == 0 {
			detail := "[adjudicated] frontier overturned pass: "
			if adjNote == "" {
				detail += "correctness concern"
			} else {
				detail += adjNote
			}
			blockers = []Blocker{newBlockerOrdered(
				field("step", 0),
				field("detail", detail),
				field("severity", auditconvergence.SeverityCorrectness),
			)}
		}
		notes := jscompat.Trim("[adjudicated: OVERTURN pass→fail] " + adjNote)
		return ApplyAdjudicationResult{
			Verdict: args.Disputed.cloneSet(
				field("verdict", VerdictFail),
				field("blockers", blockers),
				field("notes", notes),
			),
			Ruling: ruling, Outcome: VerdictFail,
			Note: "frontier adjudicator overturned the pass to a fail",
		}
	}
	if ruling == RulingConfirm {
		notes := jscompat.Trim("[adjudicated: CONFIRM fail] " + adjNote)
		return ApplyAdjudicationResult{
			Verdict: args.Disputed.cloneSet(field("notes", notes)),
			Ruling:  ruling, Outcome: VerdictFail,
			Note: "frontier adjudicator confirmed the fail",
		}
	}
	if args.ContractPassed != nil && *args.ContractPassed {
		notes := jscompat.Trim("[adjudicated: OVERTURN fail→pass, contract PASS] " + adjNote)
		fields := []orderedField{
			field("verdict", VerdictPass),
			field("blockers", []Blocker{}),
		}
		if args.Disputed.Step2Signal != nil {
			fields = append(fields, field("step2_signal", args.Disputed.Step2Signal))
		} else {
			fields = append(fields, field("step2_signal", nil))
		}
		fields = append(fields, field("notes", notes))
		return ApplyAdjudicationResult{
			Verdict: newVerdictOrdered(fields...),
			Ruling:  ruling, Outcome: VerdictPass,
			Note: "frontier adjudicator dismissed the blockers as cosmetic and the machine contract passes",
		}
	}
	notes := jscompat.Trim("[adjudicated: OVERTURN denied — no passing machine evidence] " + adjNote)
	return ApplyAdjudicationResult{
		Verdict: args.Disputed.cloneSet(field("notes", notes)),
		Ruling:  ruling, Outcome: VerdictFail,
		Note: "frontier overturn denied: no passing contract evidence — machine floor keeps the fail",
	}
}

type AdjudicationEvidencePackArgs struct {
	Disputed         AuditorVerdict `json:"disputed"`
	DiffStat         string         `json:"diffStat"`
	TopHunks         string         `json:"topHunks"`
	ContractEvidence *string        `json:"contractEvidence,omitempty"`
	CarryForward     *string        `json:"carryForward,omitempty"`
	MaxChars         *float64       `json:"maxChars,omitempty"`
}

func BuildAdjudicationEvidencePack(args AdjudicationEvidencePackArgs) string {
	capChars := 12000
	if args.MaxChars != nil {
		capChars = int(math.Max(2000, *args.MaxChars))
	}
	blockers := []string{}
	for i, blocker := range args.Disputed.Blockers {
		severity := auditconvergence.SeverityCorrectness
		if blocker.Severity != nil {
			severity = *blocker.Severity
		}
		location := ""
		if blocker.File != nil {
			location = *blocker.File
			if truthyNumber(blocker.Line) {
				location += ":" + jscompat.FormatNumber(*blocker.Line)
			}
			location += " — "
		}
		blockers = append(blockers,
			strconv.Itoa(i+1)+". ["+string(severity)+"] "+location+
				compact(collapseJSWhitespace(blocker.Detail), 240),
		)
	}
	if len(blockers) == 0 {
		blockers = []string{"(none)"}
	}
	reproduced := "unknown"
	if args.Disputed.Step2Signal != nil {
		reproduced = boolString(args.Disputed.Step2Signal.Reproduced)
	}
	sections := []string{
		"# Verdict under dispute",
		"verdict: " + string(args.Disputed.Verdict),
		"reproduced: " + reproduced,
		"",
		"# Blockers cited by the audit under dispute",
	}
	sections = append(sections, blockers...)
	sections = append(sections,
		"",
		"# Diff stat (base → now)",
	)
	diffStat := jscompat.Trim(args.DiffStat)
	if diffStat == "" {
		diffStat = "(no stat)"
	}
	sections = append(sections,
		diffStat,
		"",
		"# Top diff hunks (capped)",
		"```diff",
	)
	hunks := jscompat.Trim(args.TopHunks)
	if hunks == "" {
		hunks = "(no diff)"
	}
	sections = append(sections, hunks, "```")
	if args.ContractEvidence != nil && *args.ContractEvidence != "" {
		sections = append(sections, "", *args.ContractEvidence)
	}
	if args.CarryForward != nil && *args.CarryForward != "" {
		sections = append(sections, "", *args.CarryForward)
	}
	body := strings.Join(sections, "\n")
	if utf16Len(body) <= capChars {
		return body
	}
	return sliceUTF16(body, 0, capChars-24) + "\n…[evidence truncated]"
}

func TopDiffHunks(diff string, maxLines ...float64) string {
	limit := 200
	if len(maxLines) > 0 {
		limit = int(math.Trunc(maxLines[0]))
	}
	lines := splitCRLF(diff)
	if len(lines) <= limit {
		return diff
	}
	return strings.Join(lines[:limit], "\n") +
		"\n… [" + strconv.Itoa(len(lines)-limit) + " more diff lines omitted]"
}

func ParseAdjudicatorVerdict(messages []any) *AuditorVerdict {
	texts := []string{}
	for _, rawMessage := range messages {
		message, _ := rawMessage.(map[string]any)
		role := ""
		if info, ok := message["info"].(map[string]any); ok {
			role, _ = info["role"].(string)
		}
		if role == "" {
			role, _ = message["role"].(string)
		}
		if role != "" && role != "assistant" {
			continue
		}
		parts, _ := message["parts"].([]any)
		for _, rawPart := range parts {
			part, _ := rawPart.(map[string]any)
			text, textOK := part["text"].(string)
			if part["type"] == "text" && textOK {
				texts = append(texts, text)
			}
		}
	}
	for i := len(texts) - 1; i >= 0; i-- {
		if verdict := extractVerdictFromText(texts[i]); verdict != nil {
			return verdict
		}
	}
	return nil
}

var fencedJSONRE = regexp.MustCompile("(?s)```(?:json)?[\\t\\n\\v\\f\\r \\x{00a0}\\x{1680}\\x{2000}-\\x{200a}\\x{2028}\\x{2029}\\x{202f}\\x{205f}\\x{3000}\\x{feff}]*([\\s\\S]*?)[\\t\\n\\v\\f\\r \\x{00a0}\\x{1680}\\x{2000}-\\x{200a}\\x{2028}\\x{2029}\\x{202f}\\x{205f}\\x{3000}\\x{feff}]*```")

func extractVerdictFromText(text string) *AuditorVerdict {
	candidates := []string{}
	for _, match := range fencedJSONRE.FindAllStringSubmatch(text, -1) {
		if len(match) > 1 {
			candidates = append(candidates, match[1])
		}
	}
	first, last := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if first >= 0 && last > first {
		candidates = append(candidates, text[first:last+1])
	}
	for i := len(candidates) - 1; i >= 0; i-- {
		var verdict AuditorVerdict
		if err := json.Unmarshal([]byte(candidates[i]), &verdict); err != nil {
			continue
		}
		if validateAuditorVerdict(verdict) == nil {
			return &verdict
		}
	}
	return nil
}
