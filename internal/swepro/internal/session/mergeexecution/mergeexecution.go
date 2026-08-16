// Package mergeexecution is a bug-for-bug port of
// src/session/merge-execution.ts:1-303 (swe-pro 3b25a1a). It decides whether
// anti-correlated attempt coverage justifies a reconciliation child, builds
// that child's exact task prompt, applies blast-mode stagger delays, detects
// an abandoned auditor draft, and orchestrates one injected reconciliation
// pass.
//
// The source imports four pure helpers from session/portfolio.ts. That package
// is outside this bundle and has no Go port, so the narrow helper subset used
// here is kept package-local. Golden fixtures execute the real
// merge-execution.ts exports and therefore pin both this module and that
// compatibility subset together.
package mergeexecution

import (
	"math"
	"math/rand"
	"path/filepath"
	"reflect"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

const (
	// AuditorDraftFilename is the durable in-progress auditor draft basename.
	AuditorDraftFilename = "auditor-verdict.draft.json"
	// StaggerMinMS is the lower blast-mode delay bound.
	StaggerMinMS = 2000
	// StaggerMaxMS is the upper blast-mode delay bound.
	StaggerMaxMS = 4000

	minClauseLen   = 4
	minEvidenceLen = 8
)

// ClauseCoverage is one auditor coverage record.
type ClauseCoverage struct {
	Clause   string `json:"clause"`
	Evidence string `json:"evidence"`
}

// AttemptResult is the narrow session/portfolio AttemptResult shape consumed
// by merge-execution.ts and its imported prompt helpers.
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

// LoserDelta is one other attempt's unique substantive coverage.
type LoserDelta struct {
	Tag        string           `json:"tag"`
	Workspace  string           `json:"workspace"`
	GateStatus string           `json:"gateStatus"`
	Clauses    []ClauseCoverage `json:"clauses"`
}

// ReconcileDecision is the flattened representation of the source union.
type ReconcileDecision struct {
	Reconcile bool         `json:"reconcile"`
	Reason    string       `json:"reason"`
	Skip      string       `json:"skip,omitempty"`
	Deltas    []LoserDelta `json:"deltas"`
}

// DecideOptions are the pure reconciliation trigger inputs.
type DecideOptions struct {
	Attempts    []AttemptResult `json:"attempts"`
	Best        *AttemptResult  `json:"best"`
	NoReconcile bool            `json:"noReconcile"`
}

// DecideReconcile applies the failure-evidence spending policy.
func DecideReconcile(opts DecideOptions) ReconcileDecision {
	attempts := opts.Attempts
	if opts.NoReconcile {
		return ReconcileDecision{
			Reconcile: false,
			Reason:    "reconciliation disabled (--no-reconcile)",
			Skip:      "disabled",
			Deltas:    []LoserDelta{},
		}
	}
	if len(attempts) < 2 {
		return ReconcileDecision{
			Reconcile: false,
			Reason: "only " + jscompat.FormatNumber(float64(len(attempts))) +
				" attempt launched — nothing to merge",
			Skip:   "single-attempt",
			Deltas: []LoserDelta{},
		}
	}
	if opts.Best == nil {
		return ReconcileDecision{
			Reconcile: false, Reason: "no winner selected",
			Skip: "no-winner", Deltas: []LoserDelta{},
		}
	}

	best := *opts.Best
	others := make([]AttemptResult, 0, len(attempts))
	for _, attempt := range attempts {
		if attempt.Tag == best.Tag && attempt.Workspace == best.Workspace {
			continue
		}
		others = append(others, attempt)
	}
	deltas := loserDeltas(best, others)
	if len(deltas) == 0 {
		return ReconcileDecision{
			Reconcile: false,
			Reason:    "winner dominates on coverage — no loser covers a clause it lacks",
			Skip:      "winner-dominates",
			Deltas:    []LoserDelta{},
		}
	}
	clauseTotal := 0
	for _, delta := range deltas {
		clauseTotal += len(delta.Clauses)
	}
	return ReconcileDecision{
		Reconcile: true,
		Reason: jscompat.FormatNumber(float64(len(deltas))) + " loser attempt(s) cover " +
			jscompat.FormatNumber(float64(clauseTotal)) +
			" clause(s) the winner lacks — failure evidence, reconciling",
		Deltas: deltas,
	}
}

// BuildReconciliationMessage builds the exact task handed to the child
// harness inside best.Workspace.
func BuildReconciliationMessage(best AttemptResult, others []AttemptResult) string {
	all := make([]AttemptResult, 0, len(others)+1)
	all = append(all, best)
	all = append(all, others...)
	gaps := unionGaps(all)
	lines := []string{
		"# Reconciliation pass — port anti-correlated coverage into THIS workspace",
		"",
		"You are running inside the portfolio's WINNING workspace: " + best.Workspace,
		"It already covers the most clauses of any attempt. Your job is a NARROW",
		"merge, not a rewrite: independent attempts missed DIFFERENT clauses, so the",
		"union of their work covers clauses no single attempt did. Port that unique",
		"work here.",
		"",
		"## Contract (mandatory)",
		"- Port ONLY the clauses listed in the merge brief below, from the listed",
		"  attempt workspaces. Do NOT expand scope or invent new work.",
		"- Each listed attempt workspace is an absolute path that is READABLE. To",
		"  locate the relevant edits, DIFF each loser against its base",
		"  (`git -C <attemptWorkspace> diff` — or diff it against this workspace),",
		"  read the change that satisfies the clause, and port the minimal equivalent",
		"  into this workspace.",
		"- Do NOT regress any clause this workspace already covers. Re-verify the",
		"  winner's existing behavior after porting.",
		"- Every ported clause needs VERIFICATION EVIDENCE: an executed probe command",
		"  with its exit status, or a file:line where the clause is implemented. A",
		"  ported clause with no evidence does not count as done.",
		"- The residual-gap clauses (if any, listed under the brief) are SECONDARY",
		"  targets — no attempt covered them; close them from scratch only after the",
		"  primary ports are in and verified.",
		"",
	}
	if len(gaps) > 0 {
		lines = append(lines,
			"Primary targets: the per-attempt port lists below. Secondary targets: "+
				jscompat.FormatNumber(float64(len(gaps)))+" residual gap(s).",
		)
	} else {
		lines = append(lines,
			"Primary targets: the per-attempt port lists below. No residual gaps.",
		)
	}
	lines = append(lines, "", "---", "")
	return strings.Join(lines, "\n") + buildMergeBrief(best, others)
}

// AuditorDraftPath returns the workspace's absolute-or-relative draft path
// using node:path's POSIX behavior.
func AuditorDraftPath(workspace string) string {
	return filepath.Join(workspace, ".codeaf", AuditorDraftFilename)
}

// DraftPresenceInput is the durable-draft telemetry signal.
type DraftPresenceInput struct {
	VerdictParsed bool `json:"verdictParsed"`
	DraftExists   bool `json:"draftExists"`
}

// DraftPresenceResult is the pure suspect classification.
type DraftPresenceResult struct {
	Suspect bool   `json:"suspect"`
	Reason  string `json:"reason,omitempty"`
}

// DraftPresenceSuspect identifies audits that died after writing a draft but
// before writing a parseable verdict.
func DraftPresenceSuspect(input DraftPresenceInput) DraftPresenceResult {
	if !input.VerdictParsed && input.DraftExists {
		return DraftPresenceResult{
			Suspect: true,
			Reason:  "audit died mid-run (draft present, verdict missing)",
		}
	}
	return DraftPresenceResult{Suspect: false}
}

// StaggerMS returns the delay before launching child index. Omitting randFn
// uses Math.random's Go runtime counterpart; an explicit nil function retains
// the source's callable failure.
func StaggerMS(index float64, randFn ...func() float64) float64 {
	if index <= 0 {
		return 0
	}
	var random func() float64
	if len(randFn) == 0 {
		random = rand.Float64
	} else {
		random = randFn[0]
	}
	span := float64(StaggerMaxMS - StaggerMinMS)
	return StaggerMinMS + math.Floor(random()*(span+1))
}

// Sleep is the injectable delay seam.
type Sleep func(ms float64) error

// Launch is one staggered child invocation.
type Launch[T any] func(tag string, index float64) (T, error)

// StaggerDeps are WithStagger's optional dependencies.
type StaggerDeps struct {
	Sleep Sleep
	Rand  func() float64
}

// RealSleep waits for approximately ms milliseconds.
func RealSleep(ms float64) error {
	if ms < 0 || math.IsNaN(ms) {
		ms = 0
	}
	time.Sleep(time.Duration(ms * float64(time.Millisecond)))
	return nil
}

// WithStagger wraps a launch so every positive-index child waits first.
func WithStagger[T any](launch Launch[T], deps StaggerDeps) Launch[T] {
	sleep := deps.Sleep
	if sleep == nil {
		sleep = RealSleep
	}
	return func(tag string, index float64) (T, error) {
		var ms float64
		if deps.Rand == nil {
			ms = StaggerMS(index)
		} else {
			ms = StaggerMS(index, deps.Rand)
		}
		if ms > 0 {
			if err := sleep(ms); err != nil {
				var zero T
				return zero, err
			}
		}
		return launch(tag, index)
	}
}

// DiskEnvelope is the narrow resource-guard response used before spawning.
type DiskEnvelope struct {
	FreeBytes float64 `json:"freeBytes"`
	FreeGB    float64 `json:"freeGB"`
	FloorGB   float64 `json:"floorGB"`
	OK        bool    `json:"ok"`
}

// ReconcileDeps contains every side effect performed by RunReconciliation.
type ReconcileDeps struct {
	CheckDisk   func(path string) (DiskEnvelope, error)
	SpawnChild  func(winnerWorkspace, message string) (int, error)
	ReadVerdict func(workspace string) (any, error)
	Log         func(line string)
}

// RunOptions configures one reconciliation pass.
type RunOptions struct {
	Attempts    []AttemptResult
	Best        *AttemptResult
	NoReconcile bool
	Deps        ReconcileDeps
}

// ReconcileOutcome is the structured CLI-facing orchestration result.
type ReconcileOutcome struct {
	Ran    bool
	Reason string
	Skip   string

	Envelope       *DiskEnvelope
	Message        string
	ChildCode      int
	PostGateStatus string
	PostVerdict    any
}

// RunReconciliation runs at most one injected child.
func RunReconciliation(opts RunOptions) (ReconcileOutcome, error) {
	log := opts.Deps.Log
	if log == nil {
		log = func(string) {}
	}
	decision := DecideReconcile(DecideOptions{
		Attempts: opts.Attempts, Best: opts.Best, NoReconcile: opts.NoReconcile,
	})
	if !decision.Reconcile {
		log("[codeaf] reconcile: skipped — " + decision.Reason)
		return ReconcileOutcome{
			Ran: false, Reason: decision.Reason, Skip: decision.Skip,
		}, nil
	}

	best := *opts.Best
	others := make([]AttemptResult, 0, len(opts.Attempts))
	for _, attempt := range opts.Attempts {
		if attempt.Tag == best.Tag && attempt.Workspace == best.Workspace {
			continue
		}
		others = append(others, attempt)
	}
	envelope, err := opts.Deps.CheckDisk(best.Workspace)
	if err != nil {
		return ReconcileOutcome{}, err
	}
	if !envelope.OK {
		reason := "disk below floor before reconciliation spawn " +
			"(free=" + jscompat.ToFixed(envelope.FreeGB, 2) + "GB floor=" +
			jscompat.FormatNumber(envelope.FloorGB) + "GB) — skipping"
		log("[codeaf] reconcile: " + reason)
		return ReconcileOutcome{
			Ran: false, Reason: reason, Skip: "disk-floor", Envelope: &envelope,
		}, nil
	}

	message := BuildReconciliationMessage(best, others)
	log("[codeaf] reconcile: " + decision.Reason +
		"; launching child on winner " + best.Workspace)
	childCode, err := opts.Deps.SpawnChild(best.Workspace, message)
	if err != nil {
		return ReconcileOutcome{}, err
	}
	postVerdict, err := opts.Deps.ReadVerdict(best.Workspace)
	if err != nil {
		return ReconcileOutcome{}, err
	}
	postGateStatus := verdictString(postVerdict)
	log("[codeaf] reconcile: child exited code=" +
		jscompat.FormatNumber(float64(childCode)) +
		"; post-reconciliation gate=" + postGateStatus)
	return ReconcileOutcome{
		Ran: true, Reason: decision.Reason, Envelope: &envelope,
		Message: message, ChildCode: childCode,
		PostGateStatus: postGateStatus, PostVerdict: postVerdict,
	}, nil
}

func verdictString(value any) string {
	if value == nil {
		return "unknown"
	}
	if object, ok := value.(map[string]any); ok {
		if verdict, ok := object["verdict"].(string); ok {
			return verdict
		}
		return "unknown"
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return "unknown"
		}
		rv = rv.Elem()
	}
	if rv.Kind() == reflect.Struct {
		for _, name := range []string{"Verdict", "verdict"} {
			field := rv.FieldByName(name)
			if field.IsValid() && field.Kind() == reflect.String {
				return field.String()
			}
		}
	}
	return "unknown"
}

func isSubstantive(entry ClauseCoverage) bool {
	return utf16Length(jscompat.Trim(entry.Clause)) >= minClauseLen &&
		utf16Length(jscompat.Trim(entry.Evidence)) >= minEvidenceLen
}

func substantiveCoverage(attempt AttemptResult) []ClauseCoverage {
	out := make([]ClauseCoverage, 0, len(attempt.ClauseCoverage))
	for _, entry := range attempt.ClauseCoverage {
		if isSubstantive(entry) {
			out = append(out, entry)
		}
	}
	return out
}

func clauseKey(clause string) string {
	trimmed := jscompat.Trim(clause)
	lowered := cases.Lower(language.Und).String(trimmed)
	return collapseJSWhitespace(lowered)
}

func coveredKeys(attempt AttemptResult) map[string]struct{} {
	out := map[string]struct{}{}
	for _, entry := range substantiveCoverage(attempt) {
		out[clauseKey(entry.Clause)] = struct{}{}
	}
	return out
}

func unionGaps(attempts []AttemptResult) []string {
	covered := map[string]struct{}{}
	display := map[string]string{}
	order := []string{}
	for _, attempt := range attempts {
		for _, entry := range attempt.ClauseCoverage {
			trimmed := jscompat.Trim(entry.Clause)
			if trimmed == "" {
				continue
			}
			key := clauseKey(entry.Clause)
			if _, found := display[key]; !found {
				display[key] = trimmed
				order = append(order, key)
			}
			if isSubstantive(entry) {
				covered[key] = struct{}{}
			}
		}
	}
	out := []string{}
	for _, key := range order {
		if _, found := covered[key]; !found {
			out = append(out, display[key])
		}
	}
	return out
}

func loserDeltas(best AttemptResult, others []AttemptResult) []LoserDelta {
	winnerCovered := coveredKeys(best)
	out := []LoserDelta{}
	for _, other := range others {
		if other.Tag == best.Tag && other.Workspace == best.Workspace {
			continue
		}
		emitted := map[string]struct{}{}
		clauses := []ClauseCoverage{}
		for _, entry := range substantiveCoverage(other) {
			key := clauseKey(entry.Clause)
			if _, found := winnerCovered[key]; found {
				continue
			}
			if _, found := emitted[key]; found {
				continue
			}
			emitted[key] = struct{}{}
			clauses = append(clauses, ClauseCoverage{
				Clause: jscompat.Trim(entry.Clause), Evidence: jscompat.Trim(entry.Evidence),
			})
		}
		if len(clauses) > 0 {
			out = append(out, LoserDelta{
				Tag: other.Tag, Workspace: other.Workspace,
				GateStatus: other.GateStatus, Clauses: clauses,
			})
		}
	}
	return out
}

func suspectPasses(attempts []AttemptResult) []AttemptResult {
	out := []AttemptResult{}
	for _, attempt := range attempts {
		if attempt.GateStatus == "pass" && attempt.TelemetrySuspect {
			out = append(out, attempt)
		}
	}
	return out
}

func buildSuspectWarning(attempts []AttemptResult) string {
	suspects := suspectPasses(attempts)
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

func buildMergeBrief(best AttemptResult, others []AttemptResult) string {
	all := make([]AttemptResult, 0, len(others)+1)
	all = append(all, best)
	all = append(all, others...)
	suspectWarning := buildSuspectWarning(all)
	lines := []string{}
	if suspectWarning != "" {
		lines = append(lines, suspectWarning, "")
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
	gaps := unionGaps(all)
	if len(gaps) > 0 {
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

func utf16Length(value string) int {
	return len(utf16.Encode([]rune(value)))
}
