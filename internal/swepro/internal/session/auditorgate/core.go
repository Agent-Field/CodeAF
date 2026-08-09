// This file ports the pure verdict assembly, evidence reconciliation, command
// rendering, adjudication, and retry policy from
// src/session/auditor-gate.ts:127-1313.
package auditorgate

import (
	"encoding/json"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/auditconvergence"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/specclauses"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/sprt"
)

const (
	DenseSpecClauseThreshold = 5
	denseSpecMinCoverage     = 5
	LightAuditTurnCap        = 7
	FrontierMaxCallsPerRun   = 3
)

const BlockerSeverityInstruction = "Tag each blocker with a `severity`: \"correctness\" (wrong behavior, a missing requirement, or a failing check), \"hygiene\" (leftover scratch/backup files, dead code, misplaced files), or \"polish\" (naming, comments, style). When unsure, use \"correctness\"."

const NamingConventionInstruction = "User-facing names/display strings: verify each follows the repo's EXISTING convention for sibling components (full spell-out vs. abbreviation — e.g. if sibling rules use full phrases like \"Auto Table of Contents\", a terse \"Auto TOC\" is wrong even though it reads fine). When a name diverges from the sibling convention, raise a correctness blocker that names the exact expected string."

// CountSpecClauses preserves auditor-gate.ts's re-exported import surface.
func CountSpecClauses(spec string) float64 { return specclauses.CountSpecClauses(spec) }

func utf16Units(s string) []uint16 { return utf16.Encode([]rune(s)) }
func utf16Len(s string) int        { return len(utf16Units(s)) }

func sliceUTF16(s string, start, end int) string {
	units := utf16Units(s)
	if start < 0 {
		start = 0
	}
	if end < 0 {
		end = 0
	}
	if start > len(units) {
		start = len(units)
	}
	if end > len(units) {
		end = len(units)
	}
	if start > end {
		start = end
	}
	return string(utf16.Decode(units[start:end]))
}

func compact(s string, limit int) string {
	if utf16Len(s) > limit {
		return sliceUTF16(s, 0, limit-3) + "..."
	}
	return s
}

func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ',
		0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func collapseJSWhitespace(s string) string {
	var out strings.Builder
	inRun := false
	for _, r := range s {
		if isJSWhitespace(r) {
			if !inRun {
				out.WriteByte(' ')
				inRun = true
			}
			continue
		}
		inRun = false
		out.WriteRune(r)
	}
	return jscompat.Trim(out.String())
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

func tokenSet(s string) map[string]struct{} {
	lower := strings.ToLower(s)
	out := map[string]struct{}{}
	start := -1
	flush := func(end int) {
		if start >= 0 && end-start >= 3 {
			out[lower[start:end]] = struct{}{}
		}
		start = -1
	}
	for i := 0; i < len(lower); i++ {
		c := lower[i]
		if c == '`' || c == '*' || c == '_' {
			flush(i)
			continue
		}
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			if start < 0 {
				start = i
			}
		} else {
			flush(i)
		}
	}
	flush(len(lower))
	return out
}

func clauseTokensOverlap(a, b string) bool {
	ta, tb := tokenSet(a), tokenSet(b)
	if len(ta) == 0 || len(tb) == 0 {
		return false
	}
	shared := 0
	for token := range ta {
		if _, ok := tb[token]; ok {
			shared++
		}
	}
	minSize := len(ta)
	if len(tb) < minSize {
		minSize = len(tb)
	}
	return shared >= 2 || float64(shared)/float64(minSize) >= 0.5
}

func UncoveredInventoryClauses(coverage []ClauseCoverage, inventory []string) []string {
	covered := make([]string, 0, len(coverage))
	for _, entry := range coverage {
		if jscompat.Trim(entry.Clause) != "" {
			covered = append(covered, entry.Clause+" "+entry.Evidence)
		}
	}
	uncovered := []string{}
	for _, clause := range inventory {
		found := false
		for _, text := range covered {
			if clauseTokensOverlap(clause, text) {
				found = true
				break
			}
		}
		if !found {
			uncovered = append(uncovered, clause)
		}
	}
	return uncovered
}

var interactionMarkerRoots = []string{
	"nest", "insid", "within", "embed", "enclos", "wrap", "contain",
	"negat", "invers", "invert", "disabl", "absent", "without", "empt", "suppress",
	"repeat", "repetit", "multipl", "adjacent", "duplicat", "recursi", "twice", "consecut",
	"combin", "together", "both", "alongside", "occur", "simultan", "interleav", "conjunction",
}

func markerRoots(text string) map[string]struct{} {
	low := strings.ToLower(text)
	out := map[string]struct{}{}
	for _, root := range interactionMarkerRoots {
		if strings.Contains(low, root) {
			out[root] = struct{}{}
		}
	}
	return out
}

func matrixCellAddressed(cell string, coverageTexts []string) bool {
	idx := strings.Index(cell, "⊗")
	clauseFragment, context := cell, cell
	if idx >= 0 {
		clauseFragment = cell[:idx]
		context = cell[idx+len("⊗"):]
	}
	cellMarkers := markerRoots(context)
	clauseTokens := tokenSet(clauseFragment)
	contextDistinct := map[string]struct{}{}
	for token := range tokenSet(context) {
		if _, exists := clauseTokens[token]; !exists {
			contextDistinct[token] = struct{}{}
		}
	}
	if len(cellMarkers) == 0 && len(contextDistinct) == 0 {
		for _, text := range coverageTexts {
			if clauseTokensOverlap(cell, text) {
				return true
			}
		}
		return false
	}
	for _, text := range coverageTexts {
		if !clauseTokensOverlap(clauseFragment, text) {
			continue
		}
		if len(cellMarkers) > 0 {
			entryMarkers := markerRoots(text)
			for marker := range cellMarkers {
				if _, exists := entryMarkers[marker]; exists {
					return true
				}
			}
		} else {
			entryTokens := tokenSet(text)
			for token := range contextDistinct {
				if _, exists := entryTokens[token]; exists {
					return true
				}
			}
		}
	}
	return false
}

func UncoveredMatrixCells(coverage []ClauseCoverage, matrix []string) []string {
	covered := make([]string, 0, len(coverage))
	for _, entry := range coverage {
		if jscompat.Trim(entry.Clause) != "" {
			covered = append(covered, entry.Clause+" "+entry.Evidence)
		}
	}
	uncovered := []string{}
	for _, cell := range matrix {
		if !matrixCellAddressed(cell, covered) {
			uncovered = append(uncovered, cell)
		}
	}
	return uncovered
}

func AdmissibilityEnabled() bool { return os.Getenv("CODEAF_ADMISSIBILITY") != "0" }

type AdmissibilityOptions struct {
	SpecClauseCount     float64  `json:"specClauseCount,omitempty"`
	SpecClauseInventory []string `json:"specClauseInventory,omitempty"`
	SpecMatrix          []string `json:"specMatrix,omitempty"`
}

type AdmissibilityResult struct {
	Admissible bool   `json:"admissible"`
	Reason     string `json:"reason"`
}

// IsUnderfilledTemplateVerdict identifies the conjunction observed in runH,
// not any one weak field in isolation. This is stricter than the draft-note
// policy at auditor-gate.ts:283-293 because it remains a response-completeness
// failure even when optional admissibility policy is disabled.
func IsUnderfilledTemplateVerdict(v AuditorVerdict) bool {
	if v.Step2Signal == nil || v.Step2Signal.Notes == nil ||
		!notRunPlaceholder(*v.Step2Signal.Notes) || v.Step3Scope == nil ||
		len(v.Step3Scope.CallersChecked) != 0 || len(v.Step3Scope.MissingSites) != 0 ||
		len(v.Step3Scope.Regressions) != 0 || len(v.Blockers) != 1 {
		return false
	}
	blocker := v.Blockers[0]
	return blocker.File != nil && strings.EqualFold(jscompat.Trim(*blocker.File), "unknown") &&
		blocker.Line != nil && *blocker.Line == 0 && blocker.Step != nil && *blocker.Step == 2
}

var (
	draftBareRE  = regexp.MustCompile(`\b(?:draft|placeholder|not yet run)\b`)
	draftStateRE = regexp.MustCompile(
		`\b(?:audit|verdict|step[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]*[0-9]+)` +
			`[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]+` +
			`(?:is[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]+|still[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]+)?` +
			`(?:incomplete|pending|in[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]+progress|unfinished)\b`,
	)
)

func IsAdmissibleVerdict(v AuditorVerdict, options ...*AdmissibilityOptions) AdmissibilityResult {
	notes := []string{}
	if v.Notes != nil {
		notes = append(notes, *v.Notes)
	}
	if v.Step2Signal != nil && v.Step2Signal.Notes != nil {
		notes = append(notes, *v.Step2Signal.Notes)
	}
	noteText := asciiLower(strings.Join(notes, "\n"))
	if draftBareRE.MatchString(noteText) || draftStateRE.MatchString(noteText) {
		return AdmissibilityResult{false, "verdict notes indicate a draft or incomplete audit"}
	}
	commands := verdictCommands(v)
	hasEvidence := len(commands) > 0
	if v.Verdict == VerdictFail && !hasEvidence && len(v.Blockers) == 0 {
		return AdmissibilityResult{false, "fail verdict has no executed command/repro evidence or blockers"}
	}
	if v.Verdict == VerdictPass && !hasEvidence {
		return AdmissibilityResult{false, "pass verdict has no executed command/repro evidence"}
	}
	opts := &AdmissibilityOptions{}
	if len(options) > 0 && options[0] != nil {
		opts = options[0]
	}
	clauseCount := opts.SpecClauseCount
	if v.Verdict == VerdictPass && clauseCount >= DenseSpecClauseThreshold {
		substantive := 0
		for _, entry := range v.ClauseCoverage {
			if utf16Len(jscompat.Trim(entry.Clause)) >= 4 &&
				utf16Len(jscompat.Trim(entry.Evidence)) >= 8 {
				substantive++
			}
		}
		required := int(math.Min(clauseCount, denseSpecMinCoverage))
		if substantive < required {
			return AdmissibilityResult{
				false,
				"dense spec (" + jscompat.FormatNumber(clauseCount) +
					" clauses): pass verdict carries " + strconv.Itoa(substantive) + "/" +
					strconv.Itoa(required) +
					" substantive clause_coverage entries — spec-derived probes required",
			}
		}
		if len(opts.SpecClauseInventory) > 0 {
			uncovered := UncoveredInventoryClauses(v.ClauseCoverage, opts.SpecClauseInventory)
			if len(uncovered) > 0 {
				names := make([]string, len(uncovered))
				for i, clause := range uncovered {
					names[i] = `"` + sliceUTF16(clause, 0, min(80, utf16Len(clause))) + `"`
				}
				return AdmissibilityResult{
					false,
					"dense spec: pass verdict leaves " + strconv.Itoa(len(uncovered)) +
						" inventory clause(s) uncovered by clause_coverage: " +
						strings.Join(names, "; "),
				}
			}
		}
		if len(opts.SpecMatrix) > 0 {
			uncovered := UncoveredMatrixCells(v.ClauseCoverage, opts.SpecMatrix)
			if len(uncovered) > 0 {
				names := make([]string, len(uncovered))
				for i, cell := range uncovered {
					names[i] = `"` + sliceUTF16(cell, 0, min(80, utf16Len(cell))) + `"`
				}
				return AdmissibilityResult{
					false,
					"dense spec: pass verdict is list-complete but product-incomplete — " +
						strconv.Itoa(len(uncovered)) +
						" interaction matrix cell(s) neither probed nor justified not-applicable in clause_coverage: " +
						strings.Join(names, "; "),
				}
			}
		}
	}
	return AdmissibilityResult{true, "verdict contains admissible evidence"}
}

func SynthesizeScopeBlockers(v AuditorVerdict, inventory, matrix []string) AuditorVerdict {
	if v.Verdict != VerdictFail {
		return v
	}
	seen := map[string]bool{}
	for _, blocker := range v.Blockers {
		seen[blocker.Detail] = true
	}
	added := []Blocker{}
	if v.Step3Scope != nil {
		for _, site := range v.Step3Scope.MissingSites {
			detail := "[scope] uncovered call/change site: " + site
			if !seen[detail] {
				added = append(added, newBlockerOrdered(
					field("step", 3),
					field("file", site),
					field("detail", detail),
				))
				seen[detail] = true
			}
		}
	}
	for _, clause := range UncoveredInventoryClauses(v.ClauseCoverage, inventory) {
		detail := "[clause] spec clause without probe evidence: " + clause
		if !seen[detail] {
			added = append(added, newBlockerOrdered(field("step", 2), field("detail", detail)))
			seen[detail] = true
		}
	}
	for _, cell := range UncoveredMatrixCells(v.ClauseCoverage, matrix) {
		detail := "[matrix] interaction cell unaddressed (behavior must hold here): " + cell
		if !seen[detail] {
			added = append(added, newBlockerOrdered(field("step", 2), field("detail", detail)))
			seen[detail] = true
		}
	}
	if len(added) == 0 {
		return v
	}
	blockers := append(append([]Blocker{}, v.Blockers...), added...)
	return v.cloneSet(field("blockers", blockers))
}

func ConvertInadmissiblePassToFail(verdict AuditorVerdict, reason string) AuditorVerdict {
	blockers := append([]Blocker{}, verdict.Blockers...)
	blockers = append(blockers, newBlockerOrdered(
		field("step", 2),
		field("detail", "[unproven] pass verdict was inadmissible: "+reason+
			". Prove or fix each named gap with fresh command evidence."),
	))
	return verdict.cloneSet(field("verdict", VerdictFail), field("blockers", blockers))
}

func NoVerdictRetryExhausted(consecutiveNoVerdict float64) bool {
	if consecutiveNoVerdict <= 0 {
		return false
	}
	decision, err := sprt.SprtDecision(sprt.SprtSnapshot{
		Successes: 0, Trials: consecutiveNoVerdict, P0: 0.5, P1: 0.95,
	})
	return err == nil && decision != "continue"
}

var (
	testCommandRE        = regexp.MustCompile(`\b(?:vitest|jest|bun +test|pytest|cargo +test|go +test|npm +test|pnpm +test)\b`)
	exitZeroRE           = regexp.MustCompile(`(?:exit(?:ed)?|code) *[:=]? *0\b`)
	exitCodeRE           = regexp.MustCompile(`(?:exit(?:ed)?|code) *[:=]? *(-?[0-9]+)`)
	verificationAbsentRE = regexp.MustCompile(
		`^(?:` +
			`(?:build (?:and|&) tests?|build/tests?|build and (?:the )?test suite) (?:was |were )?not (?:yet )?(?:run|performed|verified)(?: independently)?` +
			`|(?:independent )?verification (?:was |were )?not (?:yet )?(?:run|performed|verified)(?: independently)?` +
			`|not (?:yet )?(?:run|performed|verified)(?: independently)?` +
			`|no independent verification(?: was)?(?: run| performed| completed)?` +
			`)[.!]?$`,
	)
)

func verdictCommands(v AuditorVerdict) []any {
	if v.Step2Signal != nil && v.Step2Signal.Commands != nil {
		return v.Step2Signal.Commands
	}
	if v.Commands != nil {
		return v.Commands
	}
	return []any{}
}

// AttachVerificationCommands folds harness-executed project entrypoints into
// the auditor's Step 2 evidence. Matching model-authored command rows are
// replaced, so the process-derived exit code is authoritative on disk.
func AttachVerificationCommands(verdict AuditorVerdict, commands []any) AuditorVerdict {
	if len(commands) == 0 {
		return verdict
	}
	replacements := map[string]bool{}
	for _, command := range commands {
		replacements[strings.ToLower(collapseJSWhitespace(CommandName(command)))] = true
	}
	merged := []any{}
	for _, command := range verdictCommands(verdict) {
		key := strings.ToLower(collapseJSWhitespace(CommandName(command)))
		if !replacements[key] {
			merged = append(merged, command)
		}
	}
	merged = append(merged, commands...)
	signal := Step2Signal{Commands: merged}
	if verdict.Step2Signal != nil {
		signal.Reproduced = verdict.Step2Signal.Reproduced
		signal.SpecExamplesMatched = verdict.Step2Signal.SpecExamplesMatched
		signal.Notes = verdict.Step2Signal.Notes
	}
	return verdict.cloneSet(field("step2_signal", signal))
}

// ReconcileHarnessVerification is the narrow post-audit exception to the
// prompt-only evidence seam ported from auditor-gate.ts:2303-2328. Harness
// evidence can disprove only a Step 2 claim that no verification occurred; it
// cannot overturn what an auditor says a probe found.
func ReconcileHarnessVerification(
	verdict AuditorVerdict, commands []any,
) (AuditorVerdict, bool) {
	attached := AttachVerificationCommands(verdict, commands)
	if verdict.Verdict != VerdictFail || len(verdict.Blockers) == 0 ||
		!greenHarnessBuildAndTest(commands) {
		return attached, false
	}
	for _, blocker := range verdict.Blockers {
		if !verificationAbsenceBlocker(blocker) {
			return attached, false
		}
	}

	signal := Step2Signal{Commands: verdictCommands(attached)}
	if attached.Step2Signal != nil {
		signal.Reproduced = attached.Step2Signal.Reproduced
		signal.SpecExamplesMatched = attached.Step2Signal.SpecExamplesMatched
		signal.Notes = attached.Step2Signal.Notes
	}
	if signal.Notes != nil && notRunPlaceholder(*signal.Notes) {
		notes := "Harness independently ran every expected project verification entrypoint; all exited 0."
		signal.Notes = &notes
	}
	return attached.cloneSet(
		field("verdict", VerdictPass),
		field("step2_signal", signal),
		field("blockers", []Blocker{}),
		field("repair_hints", []string{}),
	), true
}

func greenHarnessBuildAndTest(commands []any) bool {
	sawBuild, sawTest := false, false
	buildExpected := true
	buildExpectationDeclared := false
	for _, command := range commands {
		obj, ok := command.(map[string]any)
		if !ok {
			continue
		}
		kind := asciiLower(jscompat.Trim(jsString(obj["kind"])))
		if kind != "build" && kind != "test" {
			continue
		}
		if expected, ok := obj["buildExpected"].(bool); ok {
			if buildExpectationDeclared && expected != buildExpected {
				return false
			}
			buildExpected = expected
			buildExpectationDeclared = true
		}
		code, ok := firstNumber(obj, "exit", "exitCode", "exit_code", "code")
		if !ok || code != 0 {
			return false
		}
		if kind == "build" {
			sawBuild = true
		} else {
			sawTest = true
		}
	}
	return sawTest && (sawBuild || buildExpectationDeclared && !buildExpected)
}

func verificationAbsenceBlocker(blocker Blocker) bool {
	if blocker.Step == nil || *blocker.Step != 2 {
		return false
	}
	detail := asciiLower(collapseJSWhitespace(jscompat.Trim(blocker.Detail)))
	return verificationAbsentRE.MatchString(detail)
}

func notRunPlaceholder(notes string) bool {
	normalized := asciiLower(collapseJSWhitespace(jscompat.Trim(notes)))
	return normalized == "not run" || normalized == "not yet run"
}

func VerifiedTestsPassed(verdict AuditorVerdict) bool {
	commands := verdictCommands(verdict)
	if len(commands) == 0 {
		return false
	}
	sawPassing := false
	for _, command := range commands {
		if text, ok := command.(string); ok {
			normalized := asciiLower(collapseJSWhitespace(text))
			if testCommandRE.MatchString(normalized) && exitZeroRE.MatchString(normalized) {
				sawPassing = true
			}
			continue
		}
		obj, _ := command.(map[string]any)
		text := jsString(nullish(obj["cmd"], obj["command"], ""))
		if !testCommandRE.MatchString(asciiLower(collapseJSWhitespace(text))) {
			continue
		}
		code, ok := firstNumber(obj, "exit", "exitCode", "exit_code", "code")
		if !ok {
			continue
		}
		if code != 0 {
			return false
		}
		sawPassing = true
	}
	return sawPassing
}

type AuditEvidenceSummary struct {
	AuditCommandsRun float64 `json:"auditCommandsRun"`
	AuditBlockers    float64 `json:"auditBlockers"`
	InRunTestsPassed bool    `json:"inRunTestsPassed"`
}

func BuildAuditEvidenceSummary(verdict ...*AuditorVerdict) AuditEvidenceSummary {
	if len(verdict) == 0 || verdict[0] == nil {
		return AuditEvidenceSummary{}
	}
	v := *verdict[0]
	return AuditEvidenceSummary{
		AuditCommandsRun: float64(len(verdictCommands(v))),
		AuditBlockers:    float64(len(v.Blockers)),
		InRunTestsPassed: VerifiedTestsPassed(v),
	}
}

type ChangedSinceArgs struct {
	PriorSHA   *string `json:"priorSha,omitempty"`
	CurrentSHA *string `json:"currentSha,omitempty"`
	DiffOutput string  `json:"diffOutput"`
}

func ComputeChangedSince(args ChangedSinceArgs) []string {
	if args.PriorSHA == nil || args.CurrentSHA == nil ||
		*args.PriorSHA == "" || *args.CurrentSHA == "" ||
		*args.PriorSHA == *args.CurrentSHA {
		return []string{}
	}
	out := []string{}
	seen := map[string]bool{}
	for _, line := range splitCRLF(args.DiffOutput) {
		line = jscompat.Trim(line)
		if line != "" && !seen[line] {
			seen[line] = true
			out = append(out, line)
		}
	}
	return out
}

func CommandExit(command any) string {
	if text, ok := command.(string); ok {
		match := exitCodeRE.FindStringSubmatch(asciiLower(text))
		if len(match) > 1 {
			return match[1]
		}
		return "?"
	}
	obj, _ := command.(map[string]any)
	if number, ok := firstNumber(obj, "exit", "exitCode", "exit_code", "code"); ok {
		return jscompat.FormatNumber(number)
	}
	return "?"
}

func CommandName(command any) string {
	if text, ok := command.(string); ok {
		return text
	}
	obj, _ := command.(map[string]any)
	return jsString(nullish(obj["cmd"], obj["command"], "audit command"))
}

func CommandOutputTail(command any, limits ...float64) string {
	if _, ok := command.(string); ok {
		return ""
	}
	limit := 240
	if len(limits) > 0 {
		limit = int(math.Trunc(limits[0]))
	}
	obj, _ := command.(map[string]any)
	raw := nullish(obj["output"], obj["stdout"], obj["tail"], obj["output_tail"], "")
	text, ok := raw.(string)
	if !ok {
		return ""
	}
	collapsed := collapseJSWhitespace(text)
	if utf16Len(collapsed) > limit {
		return "…" + sliceUTF16(collapsed, utf16Len(collapsed)-limit, utf16Len(collapsed))
	}
	return collapsed
}

type CrossCheckResult struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason"`
}

func CrossCheckExecutedCommands(v AuditorVerdict, executed []string) CrossCheckResult {
	if v.Verdict != VerdictPass {
		return CrossCheckResult{true, "cross-check n/a (not a pass)"}
	}
	if len(executed) == 0 {
		return CrossCheckResult{true, "cross-check skipped (no executed commands available)"}
	}
	norm := func(s string) string {
		return strings.ToLower(collapseJSWhitespace(s))
	}
	executedNorm := []string{}
	for _, command := range executed {
		if normalized := norm(command); normalized != "" {
			executedNorm = append(executedNorm, normalized)
		}
	}
	for _, command := range verdictCommands(v) {
		cited := norm(CommandName(command))
		if utf16Len(cited) < 3 || cited == "audit command" {
			continue
		}
		matched := false
		for _, item := range executedNorm {
			if strings.Contains(item, cited) || strings.Contains(cited, item) {
				matched = true
				break
			}
		}
		if !matched {
			return CrossCheckResult{
				false,
				`pass verdict cites command "` + sliceUTF16(cited, 0, min(80, utf16Len(cited))) +
					`" with no matching executed bash call in the audit session`,
			}
		}
	}
	return CrossCheckResult{true, "all cited commands were executed"}
}

func ExtractExecutedBashCommands(messages []any) []string {
	out := []string{}
	for _, rawMessage := range messages {
		message, _ := rawMessage.(map[string]any)
		parts, _ := message["parts"].([]any)
		for _, rawPart := range parts {
			part, _ := rawPart.(map[string]any)
			if part["type"] != "tool" || part["tool"] != "bash" {
				continue
			}
			state, _ := part["state"].(map[string]any)
			input, _ := state["input"].(map[string]any)
			command, ok := input["command"].(string)
			if ok && jscompat.Trim(command) != "" {
				out = append(out, command)
			}
		}
	}
	return out
}

func nullish(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstNumber(obj map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		value, exists := obj[key]
		if !exists || value == nil {
			continue
		}
		switch number := value.(type) {
		case float64:
			return number, true
		case json.Number:
			f, err := number.Float64()
			return f, err == nil
		default:
			return 0, false
		}
	}
	return 0, false
}

func jsString(value any) string {
	switch value := value.(type) {
	case nil:
		return "null"
	case string:
		return value
	case bool:
		if value {
			return "true"
		}
		return "false"
	case float64:
		return jscompat.FormatNumber(value)
	case json.Number:
		number, _ := value.Float64()
		return jscompat.FormatNumber(number)
	case []any:
		parts := make([]string, len(value))
		for i, item := range value {
			if item == nil {
				parts[i] = ""
			} else {
				parts[i] = jsString(item)
			}
		}
		return strings.Join(parts, ",")
	case map[string]any:
		return "[object Object]"
	default:
		return "[object Object]"
	}
}

func splitCRLF(s string) []string {
	return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}

func boolString(value *bool) string {
	if value == nil {
		return "unknown"
	}
	if *value {
		return "true"
	}
	return "false"
}

func truthyNumber(value *float64) bool {
	return value != nil && *value != 0 && !math.IsNaN(*value)
}

func ptrString(value string) *string { return &value }

func ptrSeverity(value auditconvergence.BlockerSeverity) *auditconvergence.BlockerSeverity {
	return &value
}

func runeByteIndex(s string, runeIndex int) int {
	if runeIndex <= 0 {
		return 0
	}
	count := 0
	for index := range s {
		if count == runeIndex {
			return index
		}
		count++
	}
	return len(s)
}

func validUTF8OrReplacement(data []byte) string {
	if utf8.Valid(data) {
		return string(data)
	}
	return strings.ToValidUTF8(string(data), "\uFFFD")
}
