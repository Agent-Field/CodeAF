// This file ports the verdict schema, deterministic extraction, contract
// parsing, and model-visible prompt assembly from
// src/session/review-gate.ts:56-245 and 361-577.
package reviewgate

import (
	"bytes"
	"encoding/json"
	"math"
	"sort"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/session/agentjson"
)

type ReviewBug struct {
	File     string   `json:"file"`
	Line     *float64 `json:"line,omitempty"`
	Severity string   `json:"severity"`
	Detail   string   `json:"detail"`
}

type ReviewVerdict struct {
	Verdict      string      `json:"verdict"`
	Confidence   string      `json:"confidence"`
	Done         bool        `json:"done"`
	SpecCoverage string      `json:"spec_coverage"`
	Bugs         []ReviewBug `json:"bugs"`
	RepairHints  []string    `json:"repair_hints"`
	Evidence     string      `json:"evidence"`
}

// ReviewSchema is reviewSchema's z.object boundary. Unknown object keys are
// stripped, matching non-strict Zod objects, and omitted done defaults true.
type ReviewSchema struct{}

func (ReviewSchema) SafeParse(raw json.RawMessage) agentjson.Validation[ReviewVerdict] {
	verdict, issues := ParseReviewVerdict(raw)
	return agentjson.Validation[ReviewVerdict]{Data: verdict, Issues: issues}
}

func ParseReviewVerdict(raw json.RawMessage) (ReviewVerdict, []agentjson.Issue) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return ReviewVerdict{}, []agentjson.Issue{{Message: "Expected object, received invalid"}}
	}
	issues := []agentjson.Issue{}
	readString := func(key string) string {
		value, ok := object[key]
		if !ok {
			issues = append(issues, agentjson.Issue{Path: []string{key}, Message: "Required"})
			return ""
		}
		var text string
		if json.Unmarshal(value, &text) != nil {
			issues = append(issues, agentjson.Issue{Path: []string{key}, Message: "Expected string"})
		}
		return text
	}

	out := ReviewVerdict{Done: true}
	out.Verdict = readString("verdict")
	if out.Verdict != "pass" && out.Verdict != "fail" {
		issues = append(issues, agentjson.Issue{Path: []string{"verdict"}, Message: "Invalid enum value"})
	}
	out.Confidence = readString("confidence")
	if out.Confidence != "high" && out.Confidence != "medium" && out.Confidence != "low" {
		issues = append(issues, agentjson.Issue{Path: []string{"confidence"}, Message: "Invalid enum value"})
	}
	if rawDone, ok := object["done"]; ok {
		if json.Unmarshal(rawDone, &out.Done) != nil {
			issues = append(issues, agentjson.Issue{Path: []string{"done"}, Message: "Expected boolean"})
		}
	}
	out.SpecCoverage = readString("spec_coverage")
	out.Evidence = readString("evidence")

	if value, ok := object["repair_hints"]; !ok || bytes.Equal(value, []byte("null")) {
		issues = append(issues, agentjson.Issue{Path: []string{"repair_hints"}, Message: "Expected array"})
	} else {
		var values []json.RawMessage
		if json.Unmarshal(value, &values) != nil {
			issues = append(issues, agentjson.Issue{Path: []string{"repair_hints"}, Message: "Expected array"})
		} else {
			out.RepairHints = make([]string, 0, len(values))
			for index, item := range values {
				var text string
				if json.Unmarshal(item, &text) != nil {
					issues = append(issues, agentjson.Issue{
						Path:    []string{"repair_hints", jscompat.FormatNumber(float64(index))},
						Message: "Expected string",
					})
					continue
				}
				out.RepairHints = append(out.RepairHints, text)
			}
		}
	}

	if value, ok := object["bugs"]; !ok || bytes.Equal(value, []byte("null")) {
		issues = append(issues, agentjson.Issue{Path: []string{"bugs"}, Message: "Expected array"})
	} else {
		var values []json.RawMessage
		if json.Unmarshal(value, &values) != nil {
			issues = append(issues, agentjson.Issue{Path: []string{"bugs"}, Message: "Expected array"})
		} else {
			out.Bugs = make([]ReviewBug, 0, len(values))
			for index, item := range values {
				bug, bugIssues := parseReviewBug(item, index)
				issues = append(issues, bugIssues...)
				out.Bugs = append(out.Bugs, bug)
			}
		}
	}
	return out, issues
}

func parseReviewBug(raw json.RawMessage, index int) (ReviewBug, []agentjson.Issue) {
	var object map[string]json.RawMessage
	base := []string{"bugs", jscompat.FormatNumber(float64(index))}
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return ReviewBug{}, []agentjson.Issue{{Path: base, Message: "Expected object"}}
	}
	issues := []agentjson.Issue{}
	read := func(key string) string {
		value, ok := object[key]
		if !ok {
			issues = append(issues, agentjson.Issue{Path: appendPath(base, key), Message: "Required"})
			return ""
		}
		var text string
		if json.Unmarshal(value, &text) != nil {
			issues = append(issues, agentjson.Issue{Path: appendPath(base, key), Message: "Expected string"})
		}
		return text
	}
	bug := ReviewBug{
		File: read("file"), Severity: read("severity"), Detail: read("detail"),
	}
	if bug.Severity != "blocker" && bug.Severity != "major" && bug.Severity != "minor" {
		issues = append(issues, agentjson.Issue{Path: appendPath(base, "severity"), Message: "Invalid enum value"})
	}
	if value, ok := object["line"]; ok {
		var line float64
		if json.Unmarshal(value, &line) != nil {
			issues = append(issues, agentjson.Issue{Path: appendPath(base, "line"), Message: "Expected number"})
		} else {
			bug.Line = &line
		}
	}
	return bug, issues
}

func appendPath(base []string, item string) []string {
	out := append([]string(nil), base...)
	return append(out, item)
}

func SynthesizeFailVerdict(reason, evidence string) ReviewVerdict {
	return ReviewVerdict{
		Verdict:      "fail",
		Confidence:   "low",
		Done:         false,
		SpecCoverage: "verdict not extracted: " + utf16Slice(reason, 0, 180),
		Bugs: []ReviewBug{{
			File: "", Severity: "blocker", Detail: reason,
		}},
		RepairHints: []string{
			"Reviewer must end with a structured verdict. Preferred path: write `.codeaf/review-verdict.json` matching reviewSchema as the final shell action. Fallback path: produce a parseable final message.",
		},
		Evidence: evidence,
	}
}

// ExtractVerdictFromProse intentionally keeps the source's brace scanner,
// including its failure to understand braces inside JSON strings.
func ExtractVerdictFromProse(transcript string) *ReviewVerdict {
	if transcript == "" {
		return nil
	}
	candidates := fencedCandidates(transcript)
	depth, start := 0, -1
	for index := 0; index < len(transcript); index++ {
		switch transcript[index] {
		case '{':
			if depth == 0 {
				start = index
			}
			depth++
		case '}':
			depth--
			if depth == 0 && start >= 0 {
				candidates = append(candidates, transcript[start:index+1])
				start = -1
			} else if depth < 0 {
				depth, start = 0, -1
			}
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return utf16Length(candidates[i]) > utf16Length(candidates[j])
	})
	for _, candidate := range candidates {
		verdict, issues := ParseReviewVerdict(json.RawMessage(candidate))
		if len(issues) == 0 {
			return &verdict
		}
	}
	return nil
}

func fencedCandidates(transcript string) []string {
	out := []string{}
	for offset := 0; offset < len(transcript); {
		start := strings.Index(transcript[offset:], "```")
		if start < 0 {
			break
		}
		start += offset + 3
		if strings.HasPrefix(transcript[start:], "json") {
			start += 4
		}
		for start < len(transcript) {
			cp, size := utf8Decode(transcript, start)
			if !isJSSpace(cp) {
				break
			}
			start += size
		}
		end := strings.Index(transcript[start:], "```")
		if end < 0 {
			break
		}
		end += start
		if end > start {
			out = append(out, transcript[start:end])
		}
		offset = end + 3
	}
	return out
}

func utf8Decode(value string, index int) (rune, int) {
	return decodeWTF8(value, index)
}

func isJSSpace(cp rune) bool {
	switch cp {
	case '\t', '\n', '\v', '\f', '\r', ' ',
		0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return cp >= 0x2000 && cp <= 0x200a
}

type Contract struct {
	FileScope  *string `json:"fileScope,omitempty"`
	Acceptance *string `json:"acceptance,omitempty"`
	Outputs    *string `json:"outputs,omitempty"`
	TaskRole   *string `json:"taskRole,omitempty"`
	IssueFile  *string `json:"issueFile,omitempty"`
}

func ReadContract(description string) Contract {
	lines := strings.Split(description, "\n")
	find := func(key string) *string {
		prefix := strings.ToLower(key) + ":"
		for _, line := range lines {
			if !strings.HasPrefix(strings.ToLower(line), prefix) {
				continue
			}
			original := line[len(prefix):]
			if original == "" {
				continue
			}
			value := original
			for value != "" {
				cp, size := utf8Decode(value, 0)
				if !isJSSpace(cp) {
					break
				}
				value = value[size:]
			}
			// \s* backtracks one character when the rest of the line is all
			// non-line-breaking whitespace so (.+) can still match. trim()
			// then turns that capture into the empty string.
			if value == "" && !containsJSLineTerminator(original) {
				empty := ""
				return &empty
			}
			if value == "" || containsJSLineTerminator(value) {
				continue
			}
			trimmed := jscompat.Trim(value)
			return &trimmed
		}
		return nil
	}
	return Contract{
		FileScope: find("file_scope"), Acceptance: find("acceptance"),
		Outputs: find("outputs"), TaskRole: find("task_role"),
		IssueFile: find("issue_file"),
	}
}

func containsJSLineTerminator(value string) bool {
	return strings.ContainsAny(value, "\n\r") ||
		strings.ContainsRune(value, '\u2028') ||
		strings.ContainsRune(value, '\u2029')
}

var policyNames = map[string]struct{}{
	"access": {}, "parallel": {}, "worktree": {}, "file_scope": {},
	"task_role": {}, "context_inputs": {}, "outputs": {}, "agent": {},
	"acceptance": {}, "session_id": {}, "message_id": {},
	"parent_session_id": {}, "subagent_type": {}, "command": {},
	"issue_file": {},
}

func StripPolicyLines(description string) string {
	lines := strings.Split(description, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := jscompat.Trim(line)
		colon := strings.IndexByte(trimmed, ':')
		if colon >= 0 {
			if _, ok := policyNames[strings.ToLower(trimmed[:colon])]; ok {
				continue
			}
		}
		out = append(out, line)
	}
	joined := strings.Join(out, "\n")
	for strings.Contains(joined, "\n\n\n") {
		joined = strings.ReplaceAll(joined, "\n\n\n", "\n\n")
	}
	return jscompat.Trim(joined)
}

type ReviewPromptArgs struct {
	RootExcerpt  string
	ImplTitle    string
	ImplContract Contract
	ImplSummary  string
	Diff         string
	Config       Config
}

func BuildReviewPrompt(args ReviewPromptArgs) string {
	lines := []string{
		"You are gating a code merge. The implementation below claims to satisfy the root issue.",
		"Your verdict decides whether it merges to main.",
		"",
	}
	if args.RootExcerpt != "" {
		lines = append(lines, "# Root issue", args.RootExcerpt, "")
	}
	lines = append(lines, "# Leaf impl: "+args.ImplTitle)
	lines = appendContractLine(lines, "task_role", args.ImplContract.TaskRole)
	lines = appendContractLine(lines, "file_scope", args.ImplContract.FileScope)
	lines = appendContractLine(lines, "outputs", args.ImplContract.Outputs)
	lines = appendContractLine(lines, "acceptance", args.ImplContract.Acceptance)
	lines = append(lines,
		"", "# Self-reported summary (verify, do not trust blindly)", args.ImplSummary, "",
		"# Diff (base..HEAD)", "```diff", args.Diff, "```", "",
		"# Your job",
		"1. Read the diff carefully. Cross-check against acceptance criteria.",
		"2. Run whatever verification is appropriate for THIS diff:",
		"   - Go: `go build ./...`, `go vet ./...`, `go test ./...`",
		"   - TypeScript: `bun run typecheck` if available",
		"   - Python: `python3 -m py_compile <files>`, `ruff check` if available",
		"   - Tests added? READ them — do they actually exercise the new code paths?",
		"   - API contracts changed? Check call sites.",
		"   - Implementation claims a file was modified? READ that file and confirm.",
		"3. Be specific. 'Looks good' is not a verdict.",
		"4. You have ~"+strconvInt(args.Config.MaxToolCalls)+" tool calls and ~"+
			strconvInt(int(math.Floor(float64(args.Config.TimeoutMS)/60_000+0.5)))+
			" min wall-time. Efficient triage, not exhaustive testing.",
		"5. You are READ-ONLY: write/edit/apply_patch/plandb tools are disabled. You cannot fix things — only flag them.",
		"",
		"When you have enough evidence to decide, stop and the harness will extract your structured verdict.",
		"Be strict:",
		"- verdict=pass ONLY if acceptance is met AND any build/test you ran exited 0.",
		"- done=true (default) — set if this leaf's contract is delivered and the worker should NOT iterate further. The merge happens and the branch is frozen. Set done=false only when verdict=pass but the leaf is genuinely incomplete and downstream depends on follow-up commits.",
		"- bugs[] must be concrete (file:line where possible, NOT 'somewhere in the code').",
		"- repair_hints[] must be specific (NOT 'improve error handling').",
		"- evidence must list what you actually did, with command exit codes if you ran any.",
	)
	return strings.Join(lines, "\n")
}

func appendContractLine(lines []string, key string, value *string) []string {
	if value != nil && *value != "" {
		lines = append(lines, key+": "+*value)
	}
	return lines
}

func strconvInt(value int) string {
	return jscompat.FormatNumber(float64(value))
}

type RepairDescriptionArgs struct {
	ImplTaskID   string
	ImplTitle    string
	Verdict      ReviewVerdict
	ImplContract *Contract
}

func BuildRepairDescription(args RepairDescriptionArgs) string {
	bugLines := []string{}
	for index, bug := range args.Verdict.Bugs {
		if index >= 10 {
			break
		}
		location := "(no file)"
		if bug.File != "" {
			location = bug.File
			if bug.Line != nil && *bug.Line != 0 && !math.IsNaN(*bug.Line) {
				location += ":" + jscompat.FormatNumber(*bug.Line)
			}
		}
		bugLines = append(bugLines, "- ["+bug.Severity+"] "+location+": "+bug.Detail)
	}
	hintLines := []string{}
	for index, hint := range args.Verdict.RepairHints {
		if index >= 10 {
			break
		}
		hintLines = append(hintLines, "- "+hint)
	}
	specLines := []string{}
	if args.ImplContract != nil {
		if args.ImplContract.IssueFile != nil && *args.ImplContract.IssueFile != "" {
			specLines = append(specLines,
				"- Full spec: "+*args.ImplContract.IssueFile+
					" (read this file first; it is the source of truth)",
			)
		}
		if args.ImplContract.Acceptance != nil && *args.ImplContract.Acceptance != "" {
			specLines = append(specLines,
				"- Acceptance: "+compact(*args.ImplContract.Acceptance, 600),
			)
		}
	}
	lines := []string{
		"Repair attempt for impl leaf " + args.ImplTaskID + " (" + compact(args.ImplTitle, 80) + ").",
		"Reviewer verdict: FAIL (confidence: " + args.Verdict.Confidence + ").",
		"",
	}
	if len(specLines) > 0 {
		lines = append(lines,
			"# Original contract (do NOT regress clauses the reviewer did not mention)",
		)
		lines = append(lines, specLines...)
		lines = append(lines, "")
	}
	lines = append(lines,
		"Spec coverage assessment: "+args.Verdict.SpecCoverage,
		"",
		"# Bugs to fix",
	)
	if len(bugLines) > 0 {
		lines = append(lines, bugLines...)
	} else {
		lines = append(lines, "(reviewer did not list specific bugs)")
	}
	lines = append(lines, "", "# Repair hints from reviewer")
	if len(hintLines) > 0 {
		lines = append(lines, hintLines...)
	} else {
		lines = append(lines, "(none)")
	}
	lines = append(lines,
		"",
		"# Reviewer evidence",
		compact(args.Verdict.Evidence, 1500),
		"",
		"task_role: repair",
		"access: write",
		"parallel: serial",
		"agent: fixer",
		"outputs: patch",
		"acceptance: all blocker-severity bugs above are resolved; build/tests pass",
	)
	return strings.Join(lines, "\n")
}

func BuildRepairPrompt(repairTitle, description string) string {
	return StripPolicyLines(strings.Join([]string{
		"Task: " + repairTitle, "", description,
	}, "\n"))
}
