// Package portfolio ports the pure selector, reporting, and schedule subset of
// src/session/portfolio.ts reached by the portfolio CLI command at swe-pro
// commit 3b25a1a. The already-ported mergeexecution package remains the owner
// of reconciliation execution.
package portfolio

import (
	"math"
	"reflect"
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

const (
	minClauseLen        = 4
	minEvidenceLen      = 8
	suspectPassWithin   = 1e5
	statusTierScale     = 1e6
	costTieBreakPenalty = 1e-3
)

// ClauseCoverage is one auditor-recorded clause and its evidence.
type ClauseCoverage struct {
	Clause   string `json:"clause"`
	Evidence string `json:"evidence"`
}

// AttemptResult is the harness-visible result assembled from one clone.
type AttemptResult struct {
	Tag        string `json:"tag"`
	Workspace  string `json:"workspace"`
	GateStatus string `json:"gateStatus"`

	Verdict        any              `json:"verdict,omitempty"`
	ClauseCoverage []ClauseCoverage `json:"clauseCoverage,omitempty"`
	CostUSD        *float64         `json:"costUsd,omitempty"`
	WallMS         *float64         `json:"wallMs,omitempty"`

	TelemetrySuspect       bool    `json:"telemetrySuspect,omitempty"`
	TelemetrySuspectReason *string `json:"telemetrySuspectReason,omitempty"`
}

// TelemetryInput contains the mechanical telemetry-corruption signals.
type TelemetryInput struct {
	GateStatus      string
	VerdictParsed   bool
	ClauseCoverage  []ClauseCoverage
	LogWriteFailure bool
}

// TelemetryResult is DetectTelemetrySuspect's classification.
type TelemetryResult struct {
	Suspect bool
	Reason  string
}

// DetectTelemetrySuspect flags missing verdicts, evidence-free passes, and
// disk-write failure markers without making a quality judgment.
func DetectTelemetrySuspect(input TelemetryInput) TelemetryResult {
	reasons := []string{}
	if !input.VerdictParsed {
		reasons = append(reasons, "verdict file missing or has unparseable fields")
	}
	substantive := 0
	for _, entry := range input.ClauseCoverage {
		if isSubstantive(entry) {
			substantive++
		}
	}
	if input.GateStatus == "pass" && substantive == 0 {
		reasons = append(reasons,
			"pass verdict carries no substantive clause_coverage (an admissible pass must carry evidence)",
		)
	}
	if input.LogWriteFailure {
		reasons = append(reasons, "disk-full / ENOSPC write-failure marker in attempt log")
	}
	return TelemetryResult{
		Suspect: len(reasons) > 0,
		Reason:  strings.Join(reasons, "; "),
	}
}

func statusTier(status string) float64 {
	switch status {
	case "pass":
		return 3
	case "skipped", "escalated":
		return 2
	case "fail":
		return 1
	default:
		return 0
	}
}

func blockerCount(attempt AttemptResult) int {
	if attempt.Verdict == nil {
		return 0
	}
	if object, ok := attempt.Verdict.(map[string]any); ok {
		if blockers, ok := object["blockers"].([]any); ok {
			return len(blockers)
		}
	}
	value := reflect.ValueOf(attempt.Verdict)
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return 0
		}
		value = value.Elem()
	}
	if value.Kind() == reflect.Struct {
		field := value.FieldByName("Blockers")
		if field.IsValid() && (field.Kind() == reflect.Slice || field.Kind() == reflect.Array) {
			return field.Len()
		}
	}
	return 0
}

func substantiveCoverage(attempt AttemptResult) []ClauseCoverage {
	out := []ClauseCoverage{}
	for _, entry := range attempt.ClauseCoverage {
		if isSubstantive(entry) {
			out = append(out, entry)
		}
	}
	return out
}

// ScoreAttempt applies the source's tier, evidence, blocker, and cost order.
func ScoreAttempt(attempt AttemptResult) float64 {
	within := float64(-blockerCount(attempt))
	if attempt.GateStatus == "pass" {
		within = float64(len(substantiveCoverage(attempt)))
		if attempt.TelemetrySuspect {
			within = suspectPassWithin
		}
	}
	costPenalty := 0.0
	if attempt.CostUSD != nil && !math.IsNaN(*attempt.CostUSD) && !math.IsInf(*attempt.CostUSD, 0) {
		costPenalty = *attempt.CostUSD * costTieBreakPenalty
	}
	return statusTier(attempt.GateStatus)*statusTierScale + within - costPenalty
}

// SelectBest returns the highest-scoring result, preserving input order on a
// tie. An empty portfolio has no winner.
func SelectBest(attempts []AttemptResult) *AttemptResult {
	if len(attempts) == 0 {
		return nil
	}
	best := attempts[0]
	bestScore := ScoreAttempt(best)
	for _, attempt := range attempts[1:] {
		if score := ScoreAttempt(attempt); score > bestScore {
			best = attempt
			bestScore = score
		}
	}
	return &best
}

// Launch invokes one attempt for a schedule position.
type Launch func(tag string, index int) (AttemptResult, error)

// RunLadder launches sequentially until stop accepts a result.
func RunLadder(
	tags []string,
	launch Launch,
	stop func(AttemptResult) bool,
	onEscalate func(previous AttemptResult, nextTag string),
) ([]AttemptResult, error) {
	launched := []AttemptResult{}
	for index, tag := range tags {
		result, err := launch(tag, index)
		if err != nil {
			return nil, err
		}
		launched = append(launched, result)
		if stop(result) {
			break
		}
		if index+1 < len(tags) && onEscalate != nil {
			onEscalate(result, tags[index+1])
		}
	}
	return launched, nil
}

// RunBlast launches every tag concurrently and returns results in tag order,
// matching Promise.all.
func RunBlast(tags []string, launch Launch) ([]AttemptResult, error) {
	results := make([]AttemptResult, len(tags))
	errs := make([]error, len(tags))
	var group sync.WaitGroup
	for index, tag := range tags {
		group.Add(1)
		go func() {
			defer group.Done()
			results[index], errs[index] = launch(tag, index)
		}()
	}
	group.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

// BuildSuspectWarning constructs the warning emitted to both streams.
func BuildSuspectWarning(attempts []AttemptResult) string {
	suspects := []AttemptResult{}
	for _, attempt := range attempts {
		if attempt.GateStatus == "pass" && attempt.TelemetrySuspect {
			suspects = append(suspects, attempt)
		}
	}
	if len(suspects) == 0 {
		return ""
	}
	bar := "!!! " + strings.Repeat("=", 60) + " !!!"
	lines := []string{
		bar,
		"!!! TELEMETRY-SUSPECT PASS DETECTED - SELECTION IS NOT TRUSTWORTHY",
		bar,
		"",
		"One or more attempts PASSED the auditor gate but their evidence telemetry",
		"is missing or empty - a signature of telemetry LOSS (e.g. ENOSPC disk",
		"exhaustion corrupting the verdict), NOT of zero work. Such a pass may be a",
		"genuine full solve whose coverage record was destroyed, so it is floated to",
		"the TOP of the pass tier and can BE the selection - but the auditor signal",
		"backing it is UNRELIABLE.",
		"",
		"Suspect attempt(s):",
	}
	for _, suspect := range suspects {
		lines = append(lines, "  - attempt "+suspect.Tag+" @ "+suspect.Workspace)
		reason := "pass verdict with empty/absent clause_coverage"
		if suspect.TelemetrySuspectReason != nil {
			reason = *suspect.TelemetrySuspectReason
		}
		lines = append(lines, "      why: "+reason)
	}
	lines = append(lines,
		"",
		"REQUIRED: externally verify the suspect attempt(s) above (run the real",
		"acceptance checks against the workspace) BEFORE trusting this selection or",
		"merging. Do not ship on the auditor verdict alone.",
		bar,
	)
	return strings.Join(lines, "\n") + "\n"
}

type loserDelta struct {
	Tag, Workspace, GateStatus string
	Clauses                    []ClauseCoverage
}

func loserDeltas(best AttemptResult, others []AttemptResult) []loserDelta {
	winnerCovered := coveredKeys(best)
	out := []loserDelta{}
	for _, other := range others {
		if other.Tag == best.Tag && other.Workspace == best.Workspace {
			continue
		}
		emitted := map[string]bool{}
		clauses := []ClauseCoverage{}
		for _, entry := range substantiveCoverage(other) {
			key := clauseKey(entry.Clause)
			if winnerCovered[key] || emitted[key] {
				continue
			}
			emitted[key] = true
			clauses = append(clauses, ClauseCoverage{
				Clause: jscompat.Trim(entry.Clause), Evidence: jscompat.Trim(entry.Evidence),
			})
		}
		if len(clauses) > 0 {
			out = append(out, loserDelta{other.Tag, other.Workspace, other.GateStatus, clauses})
		}
	}
	return out
}

// BuildMergeBrief emits the exact deterministic reconciliation brief.
func BuildMergeBrief(best AttemptResult, others []AttemptResult) string {
	all := append([]AttemptResult{best}, others...)
	warning := BuildSuspectWarning(all)
	lines := []string{}
	if warning != "" {
		lines = append(lines, warning, "")
	}
	lines = append(lines,
		"## Merge brief — reconcile the portfolio into the winning workspace",
		"",
		"Winning attempt: "+best.Tag+" (gate: "+best.GateStatus+")",
		"Winning workspace: "+best.Workspace,
		"",
		"This is the base to reconcile INTO. Below, each other attempt lists the",
		"spec clauses IT covered that the winner did not. Open that attempt's",
		"workspace, inspect how it satisfied each clause, and port that behavior",
		"into the winning workspace — without regressing what the winner already",
		"covers.",
		"",
	)
	deltas := loserDeltas(best, others)
	for _, delta := range deltas {
		lines = append(lines,
			"### From attempt "+delta.Tag+" ("+delta.Workspace+")",
			"Gate: "+delta.GateStatus+". Port these "+
				jscompat.FormatNumber(float64(len(delta.Clauses)))+" clause(s):",
		)
		for _, clause := range delta.Clauses {
			lines = append(lines, "- "+clause.Clause+" — evidence: "+clause.Evidence)
		}
		lines = append(lines, "")
	}
	if len(deltas) == 0 {
		lines = append(lines,
			"No other attempt covers a clause the winner lacks — the winner",
			"dominates on coverage. Nothing to port; ship the winner as-is.",
			"",
		)
	}
	if gaps := unionGaps(all); len(gaps) > 0 {
		lines = append(lines,
			"### Residual gaps (NO attempt covered these)",
			"These clauses were mentioned but never substantively proven by any",
			"attempt — the merge pass must close them from scratch:",
		)
		for _, gap := range gaps {
			lines = append(lines, "- "+gap)
		}
		lines = append(lines, "")
	}
	return trimEndJS(strings.Join(lines, "\n")) + "\n"
}

func isSubstantive(entry ClauseCoverage) bool {
	return len(utf16.Encode([]rune(jscompat.Trim(entry.Clause)))) >= minClauseLen &&
		len(utf16.Encode([]rune(jscompat.Trim(entry.Evidence)))) >= minEvidenceLen
}

func clauseKey(clause string) string {
	return collapseJSWhitespace(cases.Lower(language.Und).String(jscompat.Trim(clause)))
}

func coveredKeys(attempt AttemptResult) map[string]bool {
	out := map[string]bool{}
	for _, entry := range substantiveCoverage(attempt) {
		out[clauseKey(entry.Clause)] = true
	}
	return out
}

func unionGaps(attempts []AttemptResult) []string {
	covered := map[string]bool{}
	display := map[string]string{}
	order := []string{}
	for _, attempt := range attempts {
		for _, entry := range attempt.ClauseCoverage {
			trimmed := jscompat.Trim(entry.Clause)
			if trimmed == "" {
				continue
			}
			key := clauseKey(entry.Clause)
			if _, ok := display[key]; !ok {
				display[key] = trimmed
				order = append(order, key)
			}
			if isSubstantive(entry) {
				covered[key] = true
			}
		}
	}
	out := []string{}
	for _, key := range order {
		if !covered[key] {
			out = append(out, display[key])
		}
	}
	return out
}

func collapseJSWhitespace(value string) string {
	var out strings.Builder
	inWhitespace := false
	for _, r := range value {
		if isJSWhitespace(r) {
			if !inWhitespace {
				out.WriteByte(' ')
				inWhitespace = true
			}
			continue
		}
		inWhitespace = false
		out.WriteRune(r)
	}
	return out.String()
}

func trimEndJS(value string) string {
	end := len(value)
	for end > 0 {
		r, size := utf8.DecodeLastRuneInString(value[:end])
		if !isJSWhitespace(r) {
			break
		}
		end -= size
	}
	return value[:end]
}

func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ',
		0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}
