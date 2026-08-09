// Package fixgenerator ports src/session/fix-generator.ts:1-352 from the
// frozen swe-pro commit 3b25a1a. It renders the model-visible repair brief,
// dispatches the strict JSON decision agent through agentjson, tracks the
// durable audit-fix cycle count, and applies emitted fixes through a narrow
// PlanDB runner seam.
package fixgenerator

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/plandb"
	"github.com/Agent-Field/swe-pro-go/internal/session/agentjson"
	"github.com/Agent-Field/swe-pro-go/internal/session/artifactregistry"
	"github.com/Agent-Field/swe-pro-go/internal/session/auditorgate"
	"github.com/Agent-Field/swe-pro-go/internal/session/hardmode"
)

type FixKind string

const (
	FixCode     FixKind = "code"
	FixTest     FixKind = "test"
	FixResearch FixKind = "research"
)

type Fix struct {
	Title       string   `json:"title"`
	Kind        FixKind  `json:"kind"`
	Deps        []string `json:"deps"`
	Description string   `json:"description"`
}

type DecisionAction string

const (
	ActionDispatchFixes DecisionAction = "dispatch_fixes"
	ActionGiveUp        DecisionAction = "give_up"
)

type FixGeneratorDecision struct {
	Action  DecisionAction `json:"action"`
	Reason  string         `json:"reason"`
	Fixes   []Fix          `json:"fixes"`
	Summary *string        `json:"summary"`
}

var fallbackSummary = "fix-generator failed to emit a valid decision; original audit blockers remain unaddressed"

var FixGenFallback = FixGeneratorDecision{
	Action:  ActionGiveUp,
	Reason:  "Fix Generator produced no parseable JSON after retries. Defaulting to give_up — better to surface the audit verdict than spawn tasks from an unparseable plan.",
	Fixes:   nil,
	Summary: &fallbackSummary,
}

const cyclesFile = ".codeaf/audit-cycles.txt"

func ReadAuditCycles(workspace string) float64 {
	raw, err := os.ReadFile(filepath.Join(workspace, cyclesFile))
	if err != nil {
		return 0
	}
	number := parseInt10(jscompat.Trim(strings.ToValidUTF8(string(raw), "\uFFFD")))
	if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
		return 0
	}
	return number
}

func WriteAuditCycles(workspace string, number float64) error {
	path := filepath.Join(workspace, cyclesFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(jscompat.FormatNumber(number)), 0o666)
}

func MaxAuditFixCycles() float64 {
	fallback := float64(hardmode.DefaultAuditFixCycles)
	if hardmode.IsHardMode() {
		fallback = hardmode.HardAuditFixCycles
	}
	raw, exists := os.LookupEnv("MAX_AUDIT_FIX_CYCLES")
	if !exists {
		return fallback
	}
	number := parseInt10(raw)
	if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
		return fallback
	}
	return number
}

func parseInt10(text string) float64 {
	text = strings.TrimLeftFunc(text, func(r rune) bool { return isJSWhitespace(r) })
	if text == "" {
		return math.NaN()
	}
	index := 0
	if text[0] == '+' || text[0] == '-' {
		index++
	}
	digits := index
	for index < len(text) && text[index] >= '0' && text[index] <= '9' {
		index++
	}
	if index == digits {
		return math.NaN()
	}
	number, err := strconv.ParseFloat(text[:index], 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return math.NaN()
	}
	return number
}

func utf16Units(text string) []uint16 { return utf16.Encode([]rune(text)) }
func utf16Len(text string) int        { return len(utf16Units(text)) }

func sliceUTF16(text string, start, end int) string {
	units := utf16Units(text)
	start = max(0, min(start, len(units)))
	end = max(0, min(end, len(units)))
	if start > end {
		start = end
	}
	return string(utf16.Decode(units[start:end]))
}

func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ',
		0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func collapseWhitespace(text string) string {
	var out strings.Builder
	inRun := false
	for _, r := range text {
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

func BuildAuditEvidenceSections(verdict auditorgate.AuditorVerdict, limits ...float64) string {
	maxChars := 1500
	if len(limits) > 0 {
		maxChars = int(math.Trunc(limits[0]))
	}
	sections := []string{}
	used := 0
	push := func(header string, lines []string) {
		if len(lines) == 0 || used >= maxChars {
			return
		}
		body := []string{}
		for _, line := range lines {
			if used+utf16Len(header)+utf16Len(strings.Join(body, "\n"))+utf16Len(line) > maxChars {
				body = append(body, "  … (truncated)")
				break
			}
			body = append(body, line)
		}
		block := header + "\n" + strings.Join(body, "\n")
		sections = append(sections, block)
		used += utf16Len(block)
	}

	commands := verdict.Commands
	if verdict.Step2Signal != nil && verdict.Step2Signal.Commands != nil {
		commands = verdict.Step2Signal.Commands
	}
	commandLines := make([]string, 0, len(commands))
	for _, command := range commands {
		name := collapseWhitespace(auditorgate.CommandName(command))
		name = sliceUTF16(name, 0, min(120, utf16Len(name)))
		tail := auditorgate.CommandOutputTail(command, 160)
		line := "  - `" + name + "` exit=" + auditorgate.CommandExit(command)
		if tail != "" {
			line += " | " + tail
		}
		commandLines = append(commandLines, line)
	}
	push("## Auditor probe commands (cmd + exit + output tail)", commandLines)

	scopeLines := []string{}
	if verdict.Step3Scope != nil {
		for _, site := range verdict.Step3Scope.MissingSites {
			scopeLines = append(scopeLines, "  - missing: "+site)
		}
		for _, regression := range verdict.Step3Scope.Regressions {
			scopeLines = append(scopeLines, "  - regression: "+regression)
		}
	}
	push("## Scope gaps (uncovered sites / regressions)", scopeLines)

	structuralLines := []string{}
	if verdict.Step4Structural != nil {
		for _, concern := range verdict.Step4Structural.Concerns {
			structuralLines = append(structuralLines, "  - "+concern)
		}
	}
	push("## Structural concerns", structuralLines)

	weakLines := []string{}
	for _, coverage := range verdict.ClauseCoverage {
		if coverage.Clause == "" {
			continue
		}
		if coverage.Evidence == "" || utf16Len(jscompat.Trim(coverage.Evidence)) < 8 {
			line := "  - " + coverage.Clause
			if coverage.Evidence == "" {
				line += " (no evidence)"
			} else {
				line += " (evidence: " + coverage.Evidence + ")"
			}
			weakLines = append(weakLines, line)
		}
	}
	push("## Clauses with weak/empty probe evidence", weakLines)

	acceptanceLines := []string{}
	for _, row := range verdict.Step2CAcceptance {
		claimed, verified := "", ""
		if row.Claimed != nil {
			claimed = *row.Claimed
		}
		if row.Verified != nil {
			verified = *row.Verified
		}
		if claimed == verified {
			continue
		}
		if row.Claimed == nil {
			claimed = "(none)"
		}
		if row.Verified == nil {
			verified = "(none)"
		}
		line := "  - " + row.Criterion + ": claimed=\"" + claimed + "\" verified=\"" + verified + "\""
		if row.Evidence != nil && *row.Evidence != "" {
			line += " | " + *row.Evidence
		}
		acceptanceLines = append(acceptanceLines, line)
	}
	push("## Acceptance criteria: claimed ≠ verified", acceptanceLines)
	return strings.Join(sections, "\n\n")
}

type PromptInput struct {
	Workspace string                     `json:"workspace"`
	UserGoal  string                     `json:"userGoal"`
	Verdict   auditorgate.AuditorVerdict `json:"verdict"`
	Cycle     float64                    `json:"cycle"`
}

func BuildFixGenPrompt(input PromptInput, outputPath, frozen string) string {
	blockers := []string{}
	for i, blocker := range input.Verdict.Blockers {
		location := "(no file)"
		if blocker.File != nil && *blocker.File != "" {
			location = *blocker.File
			if blocker.Line != nil && *blocker.Line != 0 && !math.IsNaN(*blocker.Line) {
				location += ":" + jscompat.FormatNumber(*blocker.Line)
			}
		}
		step := "?"
		if blocker.Step != nil {
			step = jscompat.FormatNumber(*blocker.Step)
		}
		blockers = append(blockers,
			jscompat.FormatNumber(float64(i+1))+". step="+step+" | "+
				location+" — "+blocker.Detail,
		)
	}
	hints := []string{}
	for i, hint := range input.Verdict.RepairHints {
		hints = append(hints, "  "+jscompat.FormatNumber(float64(i+1))+". "+hint)
	}
	evidenceSections := BuildAuditEvidenceSections(input.Verdict)
	lines := []string{
		"# Fix Generator — translate audit blockers to plandb tasks",
		"",
		"Cycle: " + jscompat.FormatNumber(input.Cycle+1) + " of " +
			jscompat.FormatNumber(MaxAuditFixCycles()+1) + " (this is cycle " +
			jscompat.FormatNumber(input.Cycle+1) + ")",
		"",
		"## Original user goal",
		"",
	}
	if artifactregistry.ArtifactRefsEnabled() && utf16Len(input.UserGoal) > 4000 {
		head, tail := 3000.0, 1000.0
		lines = append(lines, artifactregistry.EmbedArtifact(
			input.Workspace,
			"spec",
			input.UserGoal,
			&artifactregistry.RenderRefOptions{
				ExcerptHead: &head,
				ExcerptTail: &tail,
				Note:        "excerpt only — full goal in the artifact file",
			},
		))
	} else {
		lines = append(lines, input.UserGoal)
	}
	lines = append(lines,
		"",
		"## Auditor verdict summary",
		"",
		"Verdict: "+string(input.Verdict.Verdict),
	)
	if input.Verdict.Step1Goal != nil && *input.Verdict.Step1Goal != "" {
		lines = append(lines, "Goal as auditor understood it: "+*input.Verdict.Step1Goal)
	}
	lines = append(lines,
		"",
		"## Blockers (each is a real audit failure)",
		"",
	)
	if len(blockers) == 0 {
		lines = append(lines, "(no blockers — should not happen on verdict=fail)")
	} else {
		lines = append(lines, strings.Join(blockers, "\n"))
	}
	lines = append(lines,
		"",
		"## Repair hints from auditor",
		"",
	)
	if len(hints) == 0 {
		lines = append(lines, "(none)")
	} else {
		lines = append(lines, strings.Join(hints, "\n"))
	}
	lines = append(lines, "")
	if evidenceSections != "" {
		lines = append(lines,
			"## Corroborating audit evidence (command output, scope gaps, unverified acceptance)",
			"",
			evidenceSections,
			"",
		)
	}
	lines = append(lines,
		"## Frozen leaves — file_scopes you MUST NOT re-target",
		"",
		frozen,
		"",
		"## Your output",
		"",
		"Write a single JSON FixGeneratorDecision object to "+outputPath+" per the contract in your role.",
		"Pick `dispatch_fixes` and emit one task per blocker (or batch trivially-related blockers), OR pick `give_up` if blockers are structural.",
		"",
		"NEVER give_up on an ENVIRONMENTAL excuse (toolchain/command 'unavailable', 'cargo not found', 'cannot run tests in this environment') without VERIFYING it first: run a probe (`which <tool> || ls ~/.cargo/bin ~/.rustup 2>/dev/null`). Nearly all such claims are PATH/invocation issues in one sub-shell, not missing toolchains — the same run usually executed the tool successfully minutes earlier. If the tool exists anywhere, emit a task instructing fixers/auditor to invoke it by absolute path (or export PATH) instead of giving up. give_up on environmental grounds is only valid with probe output proving the tool is absent from the machine.",
		"For fields not relevant to your action, emit `null` (do not omit the key).",
		"Each `description` MUST include the verbatim blocker text so the fixer that claims the task has full context.",
		"",
		"Hard constraint on frozen leaves: if a blocker's `file` falls within the file_scope of any frozen leaf (per the section above), do NOT emit a dispatch_fixes task that rewrites that file. Either choose `give_up` (if every blocker is in frozen territory) or scope the fix to non-frozen files only. Frozen leaves are contract-complete and merged; auditor concerns about them go in a follow-up sprint, not a fix-loop.",
	)
	filtered := lines[:0]
	for _, line := range lines {
		if line != "" {
			filtered = append(filtered, line)
		}
	}
	return strings.Join(filtered, "\n")
}

type PlanDBResult struct {
	Stdout []byte
}

type PlanDBRunner interface {
	RunPlanDB(args []string) (PlanDBResult, error)
}

type PlanDBRunnerFunc func(args []string) (PlanDBResult, error)

func (f PlanDBRunnerFunc) RunPlanDB(args []string) (PlanDBResult, error) { return f(args) }

type NativePlanDBRunner struct{}

func (NativePlanDBRunner) RunPlanDB(args []string) (PlanDBResult, error) {
	result := plandb.RunPlanDB(args)
	return PlanDBResult{Stdout: result.Stdout}, nil
}

func readFrozenLeaves(runner PlanDBRunner) string {
	if runner == nil {
		return "(no frozen leaves)"
	}
	result, err := runner.RunPlanDB([]string{"contexts", "--kind", "frozen"})
	if err != nil {
		return "(no frozen leaves)"
	}
	text := jscompat.Trim(strings.ToValidUTF8(string(result.Stdout), "\uFFFD"))
	if text == "" {
		return "(no frozen leaves)"
	}
	return sliceUTF16(text, 0, min(4000, utf16Len(text)))
}

type DispatchInput struct {
	Workspace       string
	ParentSessionID string
	UserGoal        string
	Verdict         auditorgate.AuditorVerdict
	Cycle           float64
}

type Dependencies struct {
	AgentJSON agentjson.Dependencies
	PlanDB    PlanDBRunner
}

func DispatchFixGenerator(
	ctx context.Context, input DispatchInput, deps Dependencies,
) (agentjson.Result[FixGeneratorDecision], error) {
	outputPath := filepath.Join(
		input.Workspace,
		".codeaf",
		"agents",
		"fix-generator",
		"cycle-"+jscompat.FormatNumber(input.Cycle+1)+".json",
	)
	frozen := readFrozenLeaves(deps.PlanDB)
	taskPrompt := BuildFixGenPrompt(PromptInput{
		Workspace: input.Workspace,
		UserGoal:  input.UserGoal,
		Verdict:   input.Verdict,
		Cycle:     input.Cycle,
	}, outputPath, frozen)
	maxRetries := 1
	timeoutMS := int64(15 * 60_000)
	if raw, exists := os.LookupEnv("CODEAF_FIX_GEN_TIMEOUT_MS"); exists {
		number := jscompat.ToNumber(raw)
		if !math.IsNaN(number) && !math.IsInf(number, 0) {
			timeoutMS = int64(number)
		}
	}
	label := "fix-generator"
	return agentjson.DispatchJSON(ctx, agentjson.Input[FixGeneratorDecision]{
		Agent:           "fix-generator",
		ParentSessionID: input.ParentSessionID,
		Workspace:       input.Workspace,
		TaskPrompt:      taskPrompt,
		OutputPath:      outputPath,
		Schema:          DecisionSchema{},
		Fallback:        &FixGenFallback,
		MaxRetries:      &maxRetries,
		TimeoutMS:       &timeoutMS,
		Label:           &label,
	}, deps.AgentJSON)
}

type ApplyFixGenResult struct {
	TasksAdded bool    `json:"tasksAdded"`
	Count      float64 `json:"count"`
	Summary    string  `json:"summary"`
}

func ApplyFixGeneratorDecision(
	decision FixGeneratorDecision, runner PlanDBRunner,
) ApplyFixGenResult {
	if decision.Action == ActionGiveUp {
		summary := decision.Reason
		if decision.Summary != nil {
			summary = *decision.Summary
		}
		return ApplyFixGenResult{TasksAdded: false, Count: 0, Summary: summary}
	}
	added := 0
	fixes := decision.Fixes
	for _, fix := range fixes {
		args := []string{
			"plandb", "add", fix.Title,
			"--kind", string(fix.Kind),
			"--description", fix.Description,
		}
		for _, dependency := range fix.Deps {
			args = append(args, "--dep", dependency+":feeds_into")
		}
		if runner == nil {
			continue
		}
		if _, err := runner.RunPlanDB(args); err == nil {
			added++
		}
	}
	return ApplyFixGenResult{
		TasksAdded: added > 0,
		Count:      float64(added),
		Summary: "fix-generator added " + strconv.Itoa(added) + "/" +
			strconv.Itoa(len(fixes)) + " new tasks",
	}
}

type DecisionSchema struct{}

func (DecisionSchema) SafeParse(raw json.RawMessage) agentjson.Validation[FixGeneratorDecision] {
	var decision FixGeneratorDecision
	issues := validateDecisionJSON(raw, &decision)
	return agentjson.Validation[FixGeneratorDecision]{Data: decision, Issues: issues}
}

func validateDecisionJSON(raw []byte, decision *FixGeneratorDecision) []agentjson.Issue {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return []agentjson.Issue{{Message: "Expected object"}}
	}
	issues := strictKeys(object, []string{"action", "reason", "fixes", "summary"}, nil)
	if err := json.Unmarshal(raw, decision); err != nil {
		return append(issues, agentjson.Issue{Message: "Invalid decision fields"})
	}
	if decision.Action != ActionDispatchFixes && decision.Action != ActionGiveUp {
		issues = append(issues, agentjson.Issue{Path: []string{"action"}, Message: "Invalid enum value"})
	}
	if utf16Len(decision.Reason) < 1 || utf16Len(decision.Reason) > 4000 {
		issues = append(issues, agentjson.Issue{Path: []string{"reason"}, Message: "String length out of range"})
	}
	if rawSummary, ok := object["summary"]; ok && string(rawSummary) != "null" {
		var summary string
		if json.Unmarshal(rawSummary, &summary) != nil {
			issues = append(issues, agentjson.Issue{Path: []string{"summary"}, Message: "Expected string or null"})
		}
	}
	if rawFixes, ok := object["fixes"]; ok && string(rawFixes) != "null" {
		var rawItems []json.RawMessage
		if err := json.Unmarshal(rawFixes, &rawItems); err != nil {
			issues = append(issues, agentjson.Issue{Path: []string{"fixes"}, Message: "Expected array or null"})
		} else {
			for index, item := range rawItems {
				var fixObject map[string]json.RawMessage
				path := []string{"fixes", strconv.Itoa(index)}
				if json.Unmarshal(item, &fixObject) != nil {
					issues = append(issues, agentjson.Issue{Path: path, Message: "Expected object"})
					continue
				}
				issues = append(issues, strictKeys(
					fixObject,
					[]string{"title", "kind", "deps", "description"},
					path,
				)...)
				var fix Fix
				if json.Unmarshal(item, &fix) != nil {
					issues = append(issues, agentjson.Issue{Path: path, Message: "Invalid fix fields"})
					continue
				}
				if utf16Len(fix.Title) < 1 || utf16Len(fix.Title) > 200 {
					issues = append(issues, agentjson.Issue{Path: append(path, "title"), Message: "String length out of range"})
				}
				if fix.Kind != FixCode && fix.Kind != FixTest && fix.Kind != FixResearch {
					issues = append(issues, agentjson.Issue{Path: append(path, "kind"), Message: "Invalid enum value"})
				}
				if utf16Len(fix.Description) < 1 || utf16Len(fix.Description) > 8000 {
					issues = append(issues, agentjson.Issue{Path: append(path, "description"), Message: "String length out of range"})
				}
				if rawDeps, ok := fixObject["deps"]; ok && string(rawDeps) != "null" {
					var deps []string
					if json.Unmarshal(rawDeps, &deps) != nil {
						issues = append(issues, agentjson.Issue{Path: append(path, "deps"), Message: "Expected string array or null"})
					}
				}
			}
		}
	}
	return issues
}

func strictKeys(
	object map[string]json.RawMessage, allowed []string, prefix []string,
) []agentjson.Issue {
	issues := []agentjson.Issue{}
	set := map[string]bool{}
	for _, key := range allowed {
		set[key] = true
		if _, exists := object[key]; !exists {
			issues = append(issues, agentjson.Issue{
				Path:    append(append([]string{}, prefix...), key),
				Message: "Required",
			})
		}
	}
	for key := range object {
		if !set[key] {
			issues = append(issues, agentjson.Issue{
				Path:    append(append([]string{}, prefix...), key),
				Message: "Unrecognized key",
			})
		}
	}
	return issues
}
