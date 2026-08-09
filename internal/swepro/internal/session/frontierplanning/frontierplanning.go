// Package frontierplanning ports src/session/frontier-planning.ts lines 1-402
// from swe-pro commit 3b25a1a. It contains the pure W12 frontier-planning
// budgets, prompt builders, reply parsers, and structural plan comparisons.
//
// JavaScript-sensitive behavior is intentional: string caps and slices count
// UTF-16 code units, trimming uses the ECMAScript whitespace set, JSON reply
// extraction is greedy from the first "{" through the last "}", path folding
// follows String.prototype.toLowerCase, and set iteration preserves first
// occurrence order.
package frontierplanning

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// FrontierPlanningRole mirrors keyof typeof FRONTIER_PLANNING_CAPS.
type FrontierPlanningRole string

const (
	RoleGlossary          FrontierPlanningRole = "glossary"
	RoleContractReview    FrontierPlanningRole = "contractReview"
	RoleSketchArbitration FrontierPlanningRole = "sketchArbitration"
	RoleRootCause         FrontierPlanningRole = "rootCause"
)

// FrontierPlanningLimits preserves the TS object-literal property order.
type FrontierPlanningLimits struct {
	Glossary          int `json:"glossary"`
	ContractReview    int `json:"contractReview"`
	SketchArbitration int `json:"sketchArbitration"`
	RootCause         int `json:"rootCause"`
}

// FrontierPlanningCaps mirrors FRONTIER_PLANNING_CAPS.
var FrontierPlanningCaps = FrontierPlanningLimits{
	Glossary:          1,
	ContractReview:    1,
	SketchArbitration: 1,
	RootCause:         2,
}

// FrontierPlanningUsage is the mutable `used` record exposed by the TS ledger.
type FrontierPlanningUsage struct {
	Glossary          int `json:"glossary"`
	ContractReview    int `json:"contractReview"`
	SketchArbitration int `json:"sketchArbitration"`
	RootCause         int `json:"rootCause"`
	extraRoles        []FrontierPlanningRole
}

// MarshalJSON preserves the object-literal property order and the TS runtime
// behavior for an off-union role: `undefined++` creates an own NaN property,
// which JSON.stringify emits as null.
func (u FrontierPlanningUsage) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`{"glossary":`)
	b.WriteString(strconv.Itoa(u.Glossary))
	b.WriteString(`,"contractReview":`)
	b.WriteString(strconv.Itoa(u.ContractReview))
	b.WriteString(`,"sketchArbitration":`)
	b.WriteString(strconv.Itoa(u.SketchArbitration))
	b.WriteString(`,"rootCause":`)
	b.WriteString(strconv.Itoa(u.RootCause))
	for _, role := range u.extraRoles {
		key, err := json.Marshal(string(role))
		if err != nil {
			return nil, err
		}
		b.WriteByte(',')
		b.Write(key)
		b.WriteString(":null")
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// FrontierPlanningLedger mirrors createFrontierPlanningLedger's return value.
type FrontierPlanningLedger struct {
	Used FrontierPlanningUsage `json:"used"`
}

// CreateFrontierPlanningLedger returns a fresh per-run role ledger.
func CreateFrontierPlanningLedger() *FrontierPlanningLedger {
	return &FrontierPlanningLedger{}
}

// TryTake returns false once the selected role's cap has been reached.
func (l *FrontierPlanningLedger) TryTake(role FrontierPlanningRole) bool {
	switch role {
	case RoleGlossary:
		if l.Used.Glossary >= FrontierPlanningCaps.Glossary {
			return false
		}
		l.Used.Glossary++
	case RoleContractReview:
		if l.Used.ContractReview >= FrontierPlanningCaps.ContractReview {
			return false
		}
		l.Used.ContractReview++
	case RoleSketchArbitration:
		if l.Used.SketchArbitration >= FrontierPlanningCaps.SketchArbitration {
			return false
		}
		l.Used.SketchArbitration++
	case RoleRootCause:
		if l.Used.RootCause >= FrontierPlanningCaps.RootCause {
			return false
		}
		l.Used.RootCause++
	default:
		for _, prior := range l.Used.extraRoles {
			if prior == role {
				return true
			}
		}
		l.Used.extraRoles = append(l.Used.extraRoles, role)
	}
	return true
}

func capString(s string, n int) string {
	if utf16Length(s) > n {
		return utf16SliceTo(s, n) + "\n…[truncated]"
	}
	return s
}

// SpecIdentifierKind is the narrow dependency frontier-planning needs from
// src/session/spec-identifiers.ts. That module is outside this bundle.
type SpecIdentifierKind string

// SpecIdentifierContext is the identifier's provenance.
type SpecIdentifierContext string

const (
	IdentifierKindCode           SpecIdentifierKind    = "code"
	IdentifierContextRequirement SpecIdentifierContext = "requirement"
)

// SpecIdentifier mirrors the dependency's public record shape.
type SpecIdentifier struct {
	Value   string                 `json:"value"`
	Kind    SpecIdentifierKind     `json:"kind"`
	Source  string                 `json:"source"`
	Context *SpecIdentifierContext `json:"context,omitempty"`
}

const (
	// GlossaryMaxPredictions mirrors GLOSSARY_MAX_PREDICTIONS.
	GlossaryMaxPredictions = 5
	glossaryValueMin       = 3
	glossaryValueMax       = 80
)

// GlossaryPrediction mirrors the TS interface.
type GlossaryPrediction struct {
	Value      string `json:"value"`
	Reason     string `json:"reason"`
	Confidence string `json:"confidence"`
}

// BuildGlossaryPrompt mirrors buildGlossaryPrompt.
func BuildGlossaryPrompt(input struct {
	TaskText       string           `json:"taskText"`
	Identifiers    []SpecIdentifier `json:"identifiers"`
	SiblingContext string           `json:"siblingContext"`
}) string {
	idLines := make([]string, 0, len(input.Identifiers))
	for _, id := range input.Identifiers {
		idLines = append(idLines, "- ("+string(id.Kind)+") `"+id.Value+"`")
	}
	idList := strings.Join(idLines, "\n")
	if idList == "" {
		idList = "(none extracted)"
	}
	return strings.Join([]string{
		"# Convention glossary request",
		"",
		"A spec states some identifiers EXPLICITLY (listed below). Repos also demand",
		"identifiers the spec leaves IMPLICIT — user-facing display names, registry",
		"keys, locale entries — that sibling components define by convention. A",
		"paraphrased implicit identifier (e.g. display name `Auto TOC` where sibling",
		"components spell out `Auto Table of Contents`) fails held-out verification",
		"even when behavior is perfect.",
		"",
		"From the spec and the sibling-file conventions below, predict the EXACT",
		"additional identifier strings the implementation must contain. Only predict",
		"what the conventions genuinely determine; mark anything uncertain as low",
		"confidence. Prefer FULLY SPELLED-OUT forms when siblings spell out.",
		"",
		"## Spec (truncated)",
		capString(input.TaskText, 4000),
		"",
		"## Explicit spec identifiers",
		idList,
		"",
		"## Sibling conventions (dir listings + excerpts)",
		capString(input.SiblingContext, 10000),
		"",
		"## Output — strict JSON only, no prose:",
		`{"expected_identifiers": [{"value": "<exact string>", "reason": "<≤120 chars, cite the sibling convention>", "confidence": "high" | "low"}], "notes": "<≤200 chars>"}`,
	}, "\n")
}

// ParseGlossary parses the scout reply and returns an empty slice on failure.
func ParseGlossary(reply string) []GlossaryPrediction {
	root, ok := parseGreedyObject(reply)
	if !ok {
		return []GlossaryPrediction{}
	}
	raw, ok := root["expected_identifiers"].([]any)
	if !ok {
		return []GlossaryPrediction{}
	}
	out := []GlossaryPrediction{}
	for _, entry := range raw {
		obj, _ := entry.(map[string]any)
		value, _ := obj["value"].(string)
		value = jscompat.Trim(value)
		confidence := "low"
		if obj["confidence"] == "high" {
			confidence = "high"
		}
		reason, _ := obj["reason"].(string)
		reason = utf16SliceTo(reason, 120)
		n := utf16Length(value)
		if n < glossaryValueMin || n > glossaryValueMax {
			continue
		}
		out = append(out, GlossaryPrediction{Value: value, Reason: reason, Confidence: confidence})
	}
	return out
}

// EnforceableGlossaryIdentifiers returns the bounded, high-confidence,
// non-duplicate predictions that become machine-enforced code identifiers.
func EnforceableGlossaryIdentifiers(predictions []GlossaryPrediction, explicit []SpecIdentifier) []SpecIdentifier {
	out := []SpecIdentifier{}
	for _, p := range predictions {
		if p.Confidence != "high" {
			continue
		}
		if len(out) >= GlossaryMaxPredictions {
			break
		}
		duplicate := false
		for _, e := range explicit {
			if e.Value == p.Value || strings.Contains(e.Value, p.Value) || strings.Contains(p.Value, e.Value) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		sourceReason := p.Reason
		if sourceReason == "" {
			sourceReason = "derived from sibling conventions"
		}
		context := IdentifierContextRequirement
		out = append(out, SpecIdentifier{
			Value:   p.Value,
			Kind:    IdentifierKindCode,
			Source:  "convention-scout (frontier): " + sourceReason,
			Context: &context,
		})
	}
	return out
}

// GlossaryContextBlock renders the coder-facing convention reminder.
func GlossaryContextBlock(predictions []GlossaryPrediction) string {
	if len(predictions) == 0 {
		return ""
	}
	lines := []string{
		"<system-reminder>",
		"Convention glossary (derived from this repo's sibling-file conventions by a",
		"reviewing model). Use these EXACT strings where the implementation needs the",
		"corresponding identifier — do not paraphrase or abbreviate them:",
	}
	for _, p := range predictions {
		reason := p.Reason
		if reason == "" {
			reason = "sibling convention"
		}
		lines = append(lines, "- `"+p.Value+"` ("+p.Confidence+" confidence — "+reason+")")
	}
	lines = append(lines, "</system-reminder>")
	return strings.Join(lines, "\n")
}

// ContractReview mirrors the TS interface. A nil Suggestion is JavaScript
// `undefined`, so JSON.stringify omits it.
type ContractReview struct {
	Verdict    string   `json:"verdict"`
	Reasons    []string `json:"reasons"`
	Suggestion *string  `json:"suggestion,omitempty"`
}

// BuildContractReviewPrompt mirrors buildContractReviewPrompt.
func BuildContractReviewPrompt(input struct {
	TaskText              string `json:"taskText"`
	ContractJSON          string `json:"contractJson"`
	ContractFileText      string `json:"contractFileText"`
	ContractResultSummary string `json:"contractResultSummary"`
}) string {
	return strings.Join([]string{
		"# Acceptance-contract review",
		"",
		"A coding agent registered a machine-run acceptance contract BEFORE editing:",
		"the harness's done-gate, staleness check, and convergence floor all key off",
		"it. Judge whether PASSING this contract genuinely proves the spec below is",
		"implemented — and whether it could have FAILED on the pre-fix tree (a",
		"contract that cannot fail proves nothing).",
		"",
		"## Spec (truncated)",
		capString(input.TaskText, 4000),
		"",
		"## Registered contract",
		capString(input.ContractJSON, 1000),
		"",
		"## Contract check file(s)",
		capString(input.ContractFileText, 6000),
		"",
		"## First machine run of the contract",
		capString(input.ContractResultSummary, 800),
		"",
		"Judge coverage, not style: name spec requirements a passing run would NOT",
		"prove (missed edge cases, tautological assertions, wrong target). If the",
		"contract is a faithful proxy for the spec, say sound.",
		"",
		"## Output — strict JSON only, no prose:",
		`{"verdict": "sound" | "weak", "reasons": ["<≤140 chars each>"], "suggestion": "<one concrete strengthening, ≤200 chars, or empty>"}`,
	}, "\n")
}

// ParseContractReview parses the model reply, returning nil on failure.
func ParseContractReview(reply string) *ContractReview {
	obj, ok := parseGreedyObject(reply)
	if !ok {
		return nil
	}
	verdict, _ := obj["verdict"].(string)
	if verdict != "sound" && verdict != "weak" {
		return nil
	}
	reasons := []string{}
	if raw, ok := obj["reasons"].([]any); ok {
		for _, value := range raw {
			if text, ok := value.(string); ok {
				reasons = append(reasons, text)
				if len(reasons) == 6 {
					break
				}
			}
		}
	}
	var suggestion *string
	if text, ok := obj["suggestion"].(string); ok && jscompat.Trim(text) != "" {
		clipped := utf16SliceTo(text, 200)
		suggestion = &clipped
	}
	return &ContractReview{Verdict: verdict, Reasons: reasons, Suggestion: suggestion}
}

// ContractReviewEvidenceBlock renders the auditor evidence section.
func ContractReviewEvidenceBlock(review ContractReview) string {
	lines := []string{"## Frontier contract review: " + strings.ToUpper(review.Verdict)}
	if len(review.Reasons) > 0 {
		lines = append(lines, "Coverage gaps a passing contract run would NOT prove:")
		for _, reason := range review.Reasons {
			lines = append(lines, "- "+reason)
		}
	}
	if review.Suggestion != nil && *review.Suggestion != "" {
		lines = append(lines, "Suggested strengthening: "+*review.Suggestion)
	}
	if review.Verdict == "weak" {
		lines = append(lines, "Weigh contract-pass evidence accordingly: verify the flagged gaps directly instead of trusting the pass.")
	}
	return strings.Join(lines, "\n")
}

// PlanSketch mirrors the TS interface.
type PlanSketch struct {
	Files    []string `json:"files"`
	Approach string   `json:"approach"`
}

// SketchDisagreementJaccard mirrors SKETCH_DISAGREEMENT_JACCARD.
const SketchDisagreementJaccard = 0.4

// BuildSketchPrompt mirrors buildSketchPrompt.
func BuildSketchPrompt(taskText string) string {
	return strings.Join([]string{
		"# Plan sketch",
		"",
		"Sketch a MINIMAL implementation plan for the task below. You may use at most",
		"SIX read/grep/glob calls to locate the real files — spend them on locating,",
		"not understanding everything. Then STOP and output the sketch.",
		"",
		"## Task",
		capString(taskText, 4000),
		"",
		"## Output — your FINAL message must be strict JSON only:",
		`{"files": ["<repo-relative paths you would create or edit>"], "approach": "<≤400 chars: the core mechanism of the fix/feature>"}`,
	}, "\n")
}

// ParseSketch parses one plan sketch, returning nil on failure or empty content.
func ParseSketch(reply string) *PlanSketch {
	obj, ok := parseGreedyObject(reply)
	if !ok {
		return nil
	}
	files := []string{}
	if raw, ok := obj["files"].([]any); ok {
		for _, value := range raw {
			if text, ok := value.(string); ok && jscompat.Trim(text) != "" {
				files = append(files, jscompat.Trim(text))
			}
		}
	}
	approach, _ := obj["approach"].(string)
	approach = utf16SliceTo(approach, 600)
	if len(files) == 0 && approach == "" {
		return nil
	}
	return &PlanSketch{Files: files, Approach: approach}
}

func normalizePath(path string) string {
	if strings.HasPrefix(path, "./") {
		path = path[2:]
	}
	path = strings.ReplaceAll(path, `\`, "/")
	return jsLowerCase(path)
}

// SketchJaccard returns the file-set Jaccard similarity in [0,1].
func SketchJaccard(a, b PlanSketch) float64 {
	sa := make(map[string]struct{}, len(a.Files))
	sb := make(map[string]struct{}, len(b.Files))
	for _, file := range a.Files {
		sa[normalizePath(file)] = struct{}{}
	}
	for _, file := range b.Files {
		sb[normalizePath(file)] = struct{}{}
	}
	if len(sa) == 0 && len(sb) == 0 {
		return 1
	}
	intersection := 0
	for file := range sa {
		if _, ok := sb[file]; ok {
			intersection++
		}
	}
	union := len(sa) + len(sb) - intersection
	if union == 0 {
		return 1
	}
	return float64(intersection) / float64(union)
}

// SketchesDisagree applies the structural-disagreement threshold.
func SketchesDisagree(a, b PlanSketch) bool {
	return SketchJaccard(a, b) < SketchDisagreementJaccard
}

// BuildSketchArbitrationPrompt mirrors buildSketchArbitrationPrompt.
func BuildSketchArbitrationPrompt(input struct {
	TaskText string     `json:"taskText"`
	SketchA  PlanSketch `json:"sketchA"`
	SketchB  PlanSketch `json:"sketchB"`
}) string {
	render := func(sketch PlanSketch, name string) string {
		files := strings.Join(sketch.Files, ", ")
		if files == "" {
			files = "(none)"
		}
		return strings.Join([]string{
			"## " + name,
			"files: " + files,
			"approach: " + sketch.Approach,
		}, "\n")
	}
	return strings.Join([]string{
		"# Plan arbitration",
		"",
		"Two models independently sketched plans for the same task and STRUCTURALLY",
		"DISAGREE on which files the work lives in. Pick the correct plan — or merge",
		"them if each holds part of the truth. Selection is your job, not generation:",
		"judge which sketch reflects how this kind of change is actually made.",
		"",
		"## Task",
		capString(input.TaskText, 4000),
		"",
		render(input.SketchA, "Sketch A"),
		"",
		render(input.SketchB, "Sketch B"),
		"",
		"## Output — ≤250 words, plain text:",
		"The chosen/merged plan: the files to touch, the core mechanism, and the one",
		"risk the coder must check first. No preamble.",
	}, "\n")
}

// PlanContextBlock renders the plan reminder injected into the coder prompt.
func PlanContextBlock(planText string) string {
	trimmed := jscompat.Trim(planText)
	if trimmed == "" {
		return ""
	}
	return strings.Join([]string{
		"<system-reminder>",
		"Pre-flight plan (independent sketches reviewed before you started). Verify",
		"against the actual code before following — it is a head start, not an order:",
		trimmed,
		"</system-reminder>",
	}, "\n")
}

// RootCauseBlocker mirrors the inline blocker record.
type RootCauseBlocker struct {
	File   string             `json:"file,omitempty"`
	Line   *jscompat.JSNumber `json:"line,omitempty"`
	Detail string             `json:"detail"`
}

// BuildRootCausePrompt mirrors buildRootCausePrompt.
func BuildRootCausePrompt(input struct {
	TaskText     string             `json:"taskText"`
	Cycle        jscompat.JSNumber  `json:"cycle"`
	Blockers     []RootCauseBlocker `json:"blockers"`
	DiffStat     string             `json:"diffStat"`
	ContractTail string             `json:"contractTail"`
}) string {
	blockerLines := make([]string, 0, min(len(input.Blockers), 10))
	for _, blocker := range input.Blockers[:min(len(input.Blockers), 10)] {
		prefix := ""
		if blocker.File != "" {
			prefix = blocker.File
			if blocker.Line != nil && jscompat.Truthy(float64(*blocker.Line)) {
				prefix += ":" + jscompat.FormatNumber(float64(*blocker.Line))
			}
			prefix += " — "
		}
		blockerLines = append(blockerLines, "- "+prefix+blocker.Detail)
	}
	standing := strings.Join(blockerLines, "\n")
	if standing == "" {
		standing = "(none listed)"
	}
	return strings.Join([]string{
		"# Root-cause diagnosis",
		"",
		"A coding agent is entering fix cycle " + jscompat.FormatNumber(float64(input.Cycle)+1) + ": previous fix rounds did",
		"not clear the audit. Blind iteration is failing — diagnose WHY before more of",
		"it. From the evidence, name the single most likely root cause and the",
		"minimal fix strategy. If the blockers themselves look wrong (misread spec,",
		"phantom issue), say that instead — that IS a diagnosis.",
		"",
		"## Task (truncated)",
		capString(input.TaskText, 3000),
		"",
		"## Standing blockers",
		standing,
		"",
		"## Work so far (diff stat vs base)",
		capString(input.DiffStat, 1200),
		"",
		"## Latest acceptance-contract output (tail)",
		capString(input.ContractTail, 800),
		"",
		"## Output — ≤200 words, plain text:",
		"1) root cause, 2) minimal fix strategy (which file, what change), 3) what",
		"would prove it fixed. No preamble.",
	}, "\n")
}

// RootCauseRepairHint flattens and caps a diagnosis for the fix generator.
func RootCauseRepairHint(diagnosis string) string {
	return "[frontier root-cause diagnosis] " + utf16SliceTo(collapseJSWhitespace(jscompat.Trim(diagnosis)), 900)
}

func parseGreedyObject(reply string) (map[string]any, bool) {
	start := strings.IndexByte(reply, '{')
	end := strings.LastIndexByte(reply, '}')
	if start < 0 || end < start {
		return nil, false
	}
	var value any
	if err := json.Unmarshal([]byte(reply[start:end+1]), &value); err != nil {
		return nil, false
	}
	obj, ok := value.(map[string]any)
	return obj, ok
}

func collapseJSWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inSpace := false
	for _, r := range s {
		if isJSWhitespace(r) {
			if !inSpace {
				b.WriteByte(' ')
				inSpace = true
			}
			continue
		}
		b.WriteRune(r)
		inSpace = false
	}
	return b.String()
}

func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ',
		0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func jsLowerCase(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		return strings.ToLower(s)
	}
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range runes {
		switch {
		case r == 0x0130:
			b.WriteRune('i')
			b.WriteRune(0x0307)
		case r == 0x03a3 && isFinalSigma(runes, i):
			b.WriteRune(0x03c2)
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func isFinalSigma(runes []rune, i int) bool {
	j := i - 1
	for j >= 0 && isCaseIgnorable(runes[j]) {
		j--
	}
	if j < 0 || !isCased(runes[j]) {
		return false
	}
	k := i + 1
	for k < len(runes) && isCaseIgnorable(runes[k]) {
		k++
	}
	return k >= len(runes) || !isCased(runes[k])
}

func isCased(r rune) bool {
	return unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r) ||
		unicode.Is(unicode.Other_Lowercase, r) || unicode.Is(unicode.Other_Uppercase, r)
}

func isCaseIgnorable(r rune) bool {
	switch r {
	case '\'', 0x2019, 0x00ad, 0x02b9, 0x0385, 0x1fbf, 0x1fc1, 0x1fcd, 0x1fce,
		0x1fcf, 0x1fdd, 0x1fde, 0x1fdf, 0x1fed, 0x1fee, 0x1fef, 0x1ffd, 0x1ffe, 0x2027:
		return true
	}
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) ||
		unicode.Is(unicode.Lm, r) || unicode.Is(unicode.Sk, r)
}

func decodeWTF8(s string, i int) (rune, int) {
	b := s[i]
	switch {
	case b < 0x80:
		return rune(b), 1
	case b&0xe0 == 0xc0:
		if i+1 < len(s) && s[i+1]&0xc0 == 0x80 {
			cp := rune(b&0x1f)<<6 | rune(s[i+1]&0x3f)
			if cp >= 0x80 {
				return cp, 2
			}
		}
	case b&0xf0 == 0xe0:
		if i+2 < len(s) && s[i+1]&0xc0 == 0x80 && s[i+2]&0xc0 == 0x80 {
			cp := rune(b&0x0f)<<12 | rune(s[i+1]&0x3f)<<6 | rune(s[i+2]&0x3f)
			if cp >= 0x800 {
				return cp, 3
			}
		}
	case b&0xf8 == 0xf0:
		if i+3 < len(s) && s[i+1]&0xc0 == 0x80 && s[i+2]&0xc0 == 0x80 && s[i+3]&0xc0 == 0x80 {
			cp := rune(b&0x07)<<18 | rune(s[i+1]&0x3f)<<12 | rune(s[i+2]&0x3f)<<6 | rune(s[i+3]&0x3f)
			if cp >= 0x10000 && cp <= 0x10ffff {
				return cp, 4
			}
		}
	}
	return utf8.RuneError, 1
}

func appendWTF8(dst []byte, cp rune) []byte {
	if cp >= 0xd800 && cp <= 0xdfff {
		return append(dst,
			byte(0xe0|cp>>12),
			byte(0x80|(cp>>6)&0x3f),
			byte(0x80|cp&0x3f))
	}
	return utf8.AppendRune(dst, cp)
}

func utf16Length(s string) int {
	length := 0
	for i := 0; i < len(s); {
		cp, size := decodeWTF8(s, i)
		i += size
		if cp >= 0x10000 {
			length += 2
		} else {
			length++
		}
	}
	return length
}

func utf16SliceTo(s string, n int) string {
	if n <= 0 {
		return ""
	}
	units := 0
	for i := 0; i < len(s); {
		cp, size := decodeWTF8(s, i)
		width := 1
		if cp >= 0x10000 {
			width = 2
		}
		if units+width > n {
			high := rune(0xd800 + ((cp - 0x10000) >> 10))
			return s[:i] + string(appendWTF8(nil, high))
		}
		units += width
		i += size
		if units == n {
			return s[:i]
		}
	}
	return s
}
