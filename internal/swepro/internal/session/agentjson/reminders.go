// Package agentjson ports the model-visible assembly paths from
// src/session/agent-json.ts:197-308 and 432-484.
package agentjson

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type BuildPromptArgs struct {
	TaskPrompt string
	OutputPath string
	Workspace  string
	Label      string
	RetryNote  *string
	SchemaHint *string
}

func BuildSystemReminder(args BuildPromptArgs) string {
	lines := []string{
		"<system-reminder>",
		"You are running as the " + args.Label + " subagent. Your output is consumed programmatically.",
		"Workspace: " + args.Workspace,
		"Output file: " + args.OutputPath,
		"",
		"Hard rules:",
		"  1. Write your final answer as a single JSON object to the output file above.",
		"  2. Use `write` (with absolute path) to create the file. Do not echo via bash.",
		"  3. The JSON must match the schema given below EXACTLY. No extra keys.",
		"  4. For optional fields not relevant to your decision, emit the key with value `null`.",
		"  5. Do not emit prose outside the JSON file. Your assistant text can be empty.",
		"  6. After writing, end your turn.",
	}
	if args.SchemaHint != nil && *args.SchemaHint != "" {
		lines = append(lines, "", "Schema (TypeScript):", "```ts", *args.SchemaHint, "```")
	}
	if args.RetryNote != nil && *args.RetryNote != "" {
		lines = append(lines, "", "Retry note:", *args.RetryNote)
	}
	lines = append(lines, "</system-reminder>")
	return strings.Join(lines, "\n")
}

type ParseFixReminderArgs struct {
	OutputPath string
	ParseError string
	Attempt    int
	Budget     int
	Label      string
}

func BuildParseFixReminder(args ParseFixReminderArgs) string {
	return strings.Join([]string{
		"<system-reminder>",
		"PARSE-FIX MODE (" + intString(args.Attempt) + "/" + intString(args.Budget) + ") — agent: " + args.Label,
		"",
		"Your output file at " + args.OutputPath + " does NOT parse as JSON.",
		"Parse error: " + args.ParseError,
		"",
		"This is your ONLY task this turn:",
		"  1. `read` " + args.OutputPath + " to see the current bytes",
		"  2. Identify the structural problem (likely: duplicate `}` then re-opened object,",
		"     missing close, trailing comma, or two top-level JSON objects concatenated)",
		"  3. Use `write` (absolute path) to overwrite " + args.OutputPath + " with a single,",
		"     valid JSON object. Keep all data you already produced — just fix the shape.",
		"  4. Verify with: bash `python3 -c \"import json; json.load(open('" + args.OutputPath + "'))\"`",
		"  5. End your turn.",
		"",
		"Do NOT add new content. Do NOT append. Do NOT explain. Repair the structure.",
		"</system-reminder>",
	}, "\n")
}

type SchemaFixReminderArgs struct {
	OutputPath   string
	SchemaErrors string
	Attempt      int
	Budget       int
	Label        string
}

func BuildSchemaFixReminder(args SchemaFixReminderArgs) string {
	return strings.Join([]string{
		"<system-reminder>",
		"SCHEMA-FIX MODE (" + intString(args.Attempt) + "/" + intString(args.Budget) + ") — agent: " + args.Label,
		"",
		"Your file at " + args.OutputPath + " parses as JSON but does not match the required schema.",
		"",
		"Failing fields:",
		args.SchemaErrors,
		"",
		"This is your ONLY task this turn:",
		"  1. `read` the file to see current values",
		"  2. Fix ONLY the listed fields. Leave everything else alone.",
		"  3. Use `write` (absolute path) to overwrite with the corrected file.",
		"  4. End your turn.",
		"",
		"Do NOT rewrite the whole structure. Do NOT remove unrelated keys. Surgical edit only.",
		"</system-reminder>",
	}, "\n")
}

func BuildWatcherReminder(outputPath, parseError string) string {
	return strings.Join([]string{
		"<system-reminder>",
		"Your output file at " + outputPath + " does NOT currently parse as valid JSON.",
		"Parser error: " + parseError,
		"",
		"Before adding more content, fix the structural issue. Read the file to see the current state, then either:",
		"  - rewrite the file from scratch with the correct shape (use `write`), OR",
		"  - if you were mid-append, finish the structural close (`]}` etc.) so it parses.",
		"",
		"You will keep getting this reminder until the file parses.",
		"</system-reminder>",
	}, "\n")
}

type Issue struct {
	Path    []string `json:"path"`
	Message string   `json:"message"`
}

func FormatSchemaErrors(issues []Issue, limit ...int) string {
	max := 8
	if len(limit) > 0 {
		max = limit[0]
	}
	if max < 0 {
		max = 0
	}
	if max > len(issues) {
		max = len(issues)
	}
	lines := make([]string, 0, max)
	for _, issue := range issues[:max] {
		path := strings.Join(issue.Path, ".")
		if path == "" {
			path = "(root)"
		}
		lines = append(lines, "  - "+path+": "+issue.Message)
	}
	return strings.Join(lines, "\n")
}

func CompactSchemaErrors(issues []Issue) string {
	lines := make([]string, 0, len(issues))
	for _, issue := range issues {
		lines = append(lines, strings.Join(issue.Path, ".")+": "+issue.Message)
	}
	return sliceUTF16(strings.Join(lines, "; "), 400)
}

func intString(value int) string {
	return jscompat.FormatNumber(float64(value))
}
