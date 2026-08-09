// Package validity ports src/session/validity.ts lines 1-358.
package validity

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type ValidityStatus string

const (
	StatusValid   ValidityStatus = "valid"
	StatusStale   ValidityStatus = "stale"
	StatusInvalid ValidityStatus = "invalid"
	StatusUnclear ValidityStatus = "unclear"
)

type ValidityConfidence string

const (
	ConfidenceHigh ValidityConfidence = "high"
	ConfidenceLow  ValidityConfidence = "low"
)

type ValidityVerdict struct {
	Status                 ValidityStatus     `json:"status"`
	Confidence             ValidityConfidence `json:"confidence"`
	Evidence               string             `json:"evidence"`
	RecommendedDeliverable string             `json:"recommendedDeliverable"`
}

type ValidityPhase string

const (
	PhaseIntake   ValidityPhase = "intake"
	PhaseContract ValidityPhase = "contract"
)

type ValidityMode string

const (
	ModeNormal      ValidityMode = "normal"
	ModeStaleReport ValidityMode = "stale-report"
	ModeHaltInvalid ValidityMode = "halt-invalid"
)

type ValidityDecision struct {
	Proceed bool         `json:"proceed"`
	Mode    ValidityMode `json:"mode"`
	Note    string       `json:"note"`
}

type IntakeValidityPromptInput struct {
	TaskText string `json:"taskText"`
}

type StalenessPromptInput struct {
	TaskText        string `json:"taskText"`
	ContractCommand string `json:"contractCommand"`
	ContractOutput  string `json:"contractOutput"`
	// CopiedFiles lists the files the harness copied into the base worktree so
	// the contract could run there, and whether each already existed at the
	// base commit. Empty means nothing was copied.
	CopiedFiles []CopiedFile `json:"copiedFiles,omitempty"`
}

// CopiedFile is one base-worktree copy and its provenance at the base commit.
type CopiedFile struct {
	Path          string `json:"path"`
	ExistedAtBase bool   `json:"existedAtBase"`
}

const jsSpace = `[\t\n\x0b\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

var (
	jsSpaceRunRe = regexp.MustCompile(jsSpace + `+`)
	jsonFenceRe  = regexp.MustCompile(
		`(?i)` + "```" + `(?:json)?` + jsSpace + `*((?s:.*?))` + "```",
	)
)

// ApplyValidityPolicy applies the non-negotiable policy floor to a validity
// verdict at intake or after a contract unexpectedly passes.
func ApplyValidityPolicy(verdict ValidityVerdict, phase ValidityPhase) ValidityDecision {
	if phase == PhaseIntake {
		if verdict.Status == StatusInvalid && verdict.Confidence == ConfidenceHigh {
			return ValidityDecision{
				Proceed: false,
				Mode:    ModeHaltInvalid,
				Note: "Intake judged the issue clearly invalid (high confidence): " +
					oneLine(verdict.Evidence),
			}
		}
		return ValidityDecision{
			Proceed: true,
			Mode:    ModeNormal,
			Note: "Intake proceeds normally (status=" + string(verdict.Status) +
				", confidence=" + string(verdict.Confidence) + ").",
		}
	}
	if verdict.Status == StatusStale && verdict.Confidence == ConfidenceHigh {
		return ValidityDecision{
			Proceed: true,
			Mode:    ModeStaleReport,
			Note: "Contract could not fail and the issue is judged stale (high confidence): " +
				"switching to report + regression test, no behavioral change.",
		}
	}
	return ValidityDecision{
		Proceed: true,
		Mode:    ModeNormal,
		Note: "Contract-phase verdict does not meet the stale/high floor (status=" +
			string(verdict.Status) + ", confidence=" + string(verdict.Confidence) +
			"); proceeding normally.",
	}
}

// BuildIntakeValidityPrompt builds the model-visible validity intake prompt.
func BuildIntakeValidityPrompt(input IntakeValidityPromptInput) string {
	return strings.Join([]string{
		"# Validity intake — is this issue clearly not real work?",
		"",
		"You are judging a single issue/task on its TEXT ALONE, before any code is",
		"read or run. You have no reproduction and no repo evidence yet — only words.",
		"Your job is a narrow one: catch the rare issue that is obviously not real",
		"work, and otherwise get out of the way.",
		"",
		"## Rules (a strict floor — do not exceed your evidence)",
		"",
		"- Rule INVALID only on a CLEAR contradiction with documented behavior/specs",
		"  or conventions, a self-contradictory request, or something that is simply",
		"  not actionable as written. Quote the contradicting spec/convention or the",
		"  self-contradiction in `evidence`.",
		"- Rule STALE only with concrete evidence that it is already fixed / cannot",
		"  reproduce. On text alone you almost never have this — prefer `valid`.",
		"- DEFAULT to valid. When in doubt, it is valid.",
		"- If your confidence is anything short of high, output `valid` (or",
		"  `unclear`) — never a low-confidence `invalid`. A low confidence verdict",
		"  must not block real work.",
		"",
		"Remember: people-pleasing is NOT your failure mode here, and neither is",
		"over-flagging. A false `invalid` kills legitimate work; only flag what you",
		"can point at.",
		"",
		"## Issue text",
		"",
		fence(input.TaskText),
		"",
		"## Output — strict JSON only",
		"",
		"Emit exactly one JSON object, no prose around it, matching:",
		"",
		schemaHint(),
		"",
		"For a normal issue: {\"status\":\"valid\",\"confidence\":\"high\",\"evidence\":",
		"\"nothing contradicts documented behavior; actionable as written\",",
		"\"recommendedDeliverable\":\"proceed with the normal fix\"}.",
	}, "\n")
}

// copiedFilesSection renders the base-worktree copy provenance. The contract
// runs in a detached checkout of the base commit, into which the harness copies
// the files the contract says it lives in — otherwise a newly written test
// could not run there at all. A copied file that did NOT exist at base is
// therefore expected for a regression test, and disqualifying for anything the
// contract asserts about: the check would be reading the answer rather than
// the base state, and "it passed at base" would mean nothing.
//
// Returns nil when nothing was copied, so the prompt stays byte-identical to
// the TS original on every path the fixtures cover.
func copiedFilesLines(files []CopiedFile) []string {
	if len(files) == 0 {
		return nil
	}
	lines := []string{
		"## Files the harness copied into the base checkout",
		"",
		"The contract ran in a pristine checkout of the base commit. These files",
		"were copied in from the working tree so the command could execute:",
		"",
	}
	contaminated := false
	for _, file := range files {
		if file.ExistedAtBase {
			lines = append(lines, "- `"+file.Path+"` — already existed at base")
			continue
		}
		contaminated = true
		lines = append(lines,
			"- `"+file.Path+"` — DID NOT EXIST at base; it was created during this run")
	}
	if contaminated {
		lines = append(lines,
			"",
			"Weigh this before ruling stale. If a file that did not exist at base is",
			"the very thing the issue asked to create or change, then the contract",
			"passed at base only because that file was copied in — the check was",
			"handed its own answer. That is NOT evidence the issue is stale; it is a",
			"mis-declared contract. In that case answer status `unclear`, and say in",
			"`evidence` that the deliverable was copied into the base checkout and",
			"belongs in `asserted_paths` rather than `paths`.",
			"A file that did not exist at base is only innocuous when it is purely",
			"the test/scaffolding — a new regression test is expected to be new.",
		)
	}
	return append(lines, "")
}

// BuildStalenessPrompt builds the contract-cannot-fail adjudication prompt.
func BuildStalenessPrompt(input StalenessPromptInput) string {
	lines := []string{
		"# Staleness adjudication — the contract that must fail PASSED",
		"",
		"A coder registered a machine-checkable acceptance contract for this issue.",
		"By protocol that contract MUST FAIL before any fix — it encodes the bug's",
		"reproduction, so a failing run is the proof the bug is real and present.",
		"",
		"It did NOT fail. On its very first registration run, BEFORE any code change,",
		"the contract PASSED. That is strong machine evidence that the bug does not",
		"reproduce in this repo state — most likely the issue is already fixed here,",
		"or was never reproducible as written.",
		"",
		"Judge: is this issue stale (already fixed / not reproducible here), or is",
		"the contract simply too weak / mis-targeted to have caught the real bug?",
		"",
		"- If the contract genuinely exercises the reported behavior and still passes",
		"  → status `stale`, confidence `high`. The recommendedDeliverable is:",
		"  \"report + regression test only, no behavioral change\" — document that",
		"  the issue does not reproduce, add a regression test that guards the",
		"  current (correct) behavior, and change no product behavior.",
		"- If the contract looks too shallow to have reproduced the bug (wrong file,",
		"  trivial assertion, tests the wrong path) → status `unclear` (or `valid`),",
		"  and say so in `evidence`; the coder should strengthen the contract.",
		"- Do NOT people-please into inventing a fix. \"This issue is stale\" is a",
		"  correct and valuable verdict when the evidence supports it.",
		"",
		"## Issue text",
		"",
		fence(input.TaskText),
		"",
		"## Registered contract command (this is what was expected to FAIL)",
		"",
		fence(input.ContractCommand),
		"",
		"## Contract output from its first run (it PASSED)",
		"",
		fence(input.ContractOutput),
		"",
	}
	// Appended only when the harness actually copied something, so a prompt
	// with no copies stays byte-identical to the TS original (fixture parity).
	lines = append(lines, copiedFilesLines(input.CopiedFiles)...)
	lines = append(lines,
		"## Output — strict JSON only",
		"",
		"Emit exactly one JSON object, no prose around it, matching:",
		"",
		schemaHint(),
	)
	return strings.Join(lines, "\n")
}

// ParseValidityVerdict returns the first valid strict verdict from fenced JSON
// candidates, then from the widest bare object span.
func ParseValidityVerdict(text string) *ValidityVerdict {
	if jscompat.Trim(text) == "" {
		return nil
	}
	for _, candidate := range jsonCandidates(text) {
		if verdict, ok := decodeVerdict(candidate); ok {
			return verdict
		}
	}
	return nil
}

func decodeVerdict(candidate string) (*ValidityVerdict, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(candidate), &fields); err != nil || fields == nil || len(fields) != 4 {
		return nil, false
	}
	for _, key := range []string{"status", "confidence", "evidence", "recommendedDeliverable"} {
		if _, ok := fields[key]; !ok {
			return nil, false
		}
	}
	var verdict ValidityVerdict
	if !decodeStrictString(fields["status"], &verdict.Status) ||
		!decodeStrictString(fields["confidence"], &verdict.Confidence) ||
		!decodeStrictString(fields["evidence"], &verdict.Evidence) ||
		!decodeStrictString(fields["recommendedDeliverable"], &verdict.RecommendedDeliverable) {
		return nil, false
	}
	switch verdict.Status {
	case StatusValid, StatusStale, StatusInvalid, StatusUnclear:
	default:
		return nil, false
	}
	if verdict.Confidence != ConfidenceHigh && verdict.Confidence != ConfidenceLow {
		return nil, false
	}
	return &verdict, true
}

func decodeStrictString[T ~string](raw json.RawMessage, out *T) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) >= 2 && trimmed[0] == '"' &&
		json.Unmarshal(trimmed, out) == nil
}

func jsonCandidates(text string) []string {
	out := []string{}
	for _, match := range jsonFenceRe.FindAllStringSubmatch(text, -1) {
		inner := jscompat.Trim(match[1])
		if inner != "" {
			out = append(out, inner)
		}
	}
	first := strings.Index(text, "{")
	last := strings.LastIndex(text, "}")
	if first >= 0 && last > first {
		out = append(out, text[first:last+1])
	}
	return out
}

// StaleReportContextBlock builds the model-visible replacement deliverable
// injected after a stale/high contract verdict.
func StaleReportContextBlock(verdict ValidityVerdict) string {
	return strings.Join([]string{
		"<system-reminder>",
		"STALENESS DETECTED — deliverable changed.",
		"",
		"Your acceptance contract was required to FAIL before the fix (it encodes",
		"the reproduction). Instead it PASSED on its first run. Independent judgment",
		"concludes this issue is STALE: the bug does not reproduce in this repo",
		"state (most likely already fixed here).",
		"",
		"Evidence: " + oneLine(verdict.Evidence),
		"",
		"Do NOT invent a behavioral change to make it look like you fixed something.",
		"There is nothing to fix. Your deliverable is now:",
		"",
		"1. Document the non-reproducibility: state plainly that the issue does not",
		"   reproduce here, cite the passing contract as the evidence, and note the",
		"   likely reason (already fixed on this branch / never reproducible as",
		"   written).",
		"2. Add a regression test that GUARDS THE CURRENT (correct) behavior, so a",
		"   future regression that reintroduces the bug would be caught.",
		"3. Make NO behavioral change to product code. Zero. If you find yourself",
		"   editing non-test source, stop — that is the failure mode this gate exists",
		"   to prevent.",
		"",
		"Recommended deliverable: " + oneLine(verdict.RecommendedDeliverable),
		"</system-reminder>",
	}, "\n")
}

func fence(body string) string {
	return strings.Join([]string{"```", body, "```"}, "\n")
}

func schemaHint() string {
	return strings.Join([]string{
		"```ts",
		"type ValidityVerdict = {",
		"  status: \"valid\" | \"stale\" | \"invalid\" | \"unclear\"",
		"  confidence: \"high\" | \"low\"",
		"  evidence: string             // concrete grounding: a spec citation, the",
		"                               // contract output, a self-contradiction quote",
		"  recommendedDeliverable: string",
		"}",
		"```",
	}, "\n")
}

func oneLine(value string) string {
	return jscompat.Trim(jsSpaceRunRe.ReplaceAllString(value, " "))
}
