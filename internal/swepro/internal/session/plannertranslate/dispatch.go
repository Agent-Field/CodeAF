package plannertranslate

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/agentjson"
)

// DispatchPlannerTranslateInput mirrors planner-translate.ts:165-173.
type DispatchPlannerTranslateInput struct {
	Workspace       string
	ParentSessionID string
	PromptOps       any
	OutputPath      *string
	FrontierContext *string
	FrontierTick    *float64
}

// BuildPrompt is the byte-identical planner-translate task prompt.
func BuildPrompt(outputPath string, frontierContext *string, frontierTick *float64) string {
	lines := []string{
		"# Planner — translation task",
		"",
		"## What you must read",
		"",
		"Exactly these two files. Read both before writing your output:",
		"  - `.codeaf/plan/architecture.md` — the architect's blueprint.",
		"  - `.codeaf/plan/product.md` — context only (do not let it override architecture.md).",
		"",
		"## What you must write",
		"",
		"Write a single JSON object to `" + outputPath + "` matching the DAG schema.",
		"",
		"Shape (canonical example — copy exact field names):",
		"",
		"```json",
		"{",
		`  "summary": "Emitted N leaves with M edges from architecture.md §X-§Y.",`,
		`  "tasks": [`,
		"    {",
		`      "taskKey": "lib-types",`,
		`      "title": "Implement lib/types.go — Mlrval and Record types",`,
		`      "kind": "code",`,
		`      "description": "## Description\n<...>\n\n## Interface Contracts\n<verbatim from architecture.md>\n\n## Files\n- Create: lib/types.go\n\n## Acceptance\n- [ ] Compiles\n- [ ] Exposes declared interface\n",`,
		`      "tags": ["agent:fixer", "scope:medium", "risk:high"],`,
		`      "deps": []`,
		"    },",
		"    {",
		`      "taskKey": "io-reader-csv",`,
		`      "title": "Implement io/reader_csv.go — CSV input reader",`,
		`      "kind": "code",`,
		`      "description": "<see template>",`,
		`      "tags": ["agent:fixer", "scope:small"],`,
		`      "deps": [`,
		`        { "from_task": "lib-types", "kind": "feeds_into" },`,
		`        { "from_task": "io-interfaces", "kind": "feeds_into" }`,
		"      ]",
		"    }",
		"  ]",
		"}",
		"```",
		"",
		"## Mandatory translation rules",
		"",
		"1. **Enumerate every module.** Every component in architecture.md's `## Components`",
		"   section, OR every file in `## File layout` (if finer-grained), becomes one task.",
		"   Architecture.md is authoritative — if it names 27 modules, you produce 27 tasks.",
		"",
		"2. **Translate every edge.** Every `depends on` / `imports from` / arrow in",
		"   architecture.md's `## Module dependency graph` becomes one `feeds_into` entry",
		"   in the dependent task's `deps` array. Only direct edges (plandb computes the",
		"   transitive closure). Root tasks (no upstream) get `deps: []`.",
		"",
		"2a. **Prune edges, then add joins.** An architecture relation becomes",
		"`feeds_into` only when the dependent cannot compile, implement, or test against",
		"the upstream interface alone. Record the consumed interface in its description.",
		"Do not encode transitive, organizational, or phase-order edges. Emit explicit",
		"integration/QA tasks for shared wiring and multi-module verification.",
		"",
		"2b. **Fence writes.** Every task description begins with `file_scope:` listing",
		"exact writable paths. Concurrent siblings must have disjoint scopes. Put shared",
		"files in one contract owner or integration task. Favor a contract layer, a wide",
		"implementation/test layer, and narrow joins; emit as many truthful leaves as",
		"needed, not a size-limited plan.",
		"",
		"3. **Copy interface signatures verbatim.** If architecture.md has Go/Rust/Python",
		"   code blocks for a module's interface, paste those lines into that task's",
		"   `## Interface Contracts` description block byte-for-byte.",
		"",
		"4. **Use unique taskKey strings.** Sibling references in `deps[].from_task` must",
		"   point at another task's taskKey within the same DAG. Don't use plandb IDs",
		"   (you don't see them — the applier resolves taskKeys to IDs).",
		"",
		"5. **Tags:** every task gets `agent:fixer` and one of `scope:tiny|small|medium|large`.",
		"   Add `risk:high` only if architecture.md calls the module critical.",
		"   Add `tests:required` only if architecture.md's acceptance lists behavioral tests.",
		"",
		"## Workflow",
		"",
		"  1. read architecture.md",
		"  2. read product.md",
		"  3. write the dag.json file via the `write` tool",
		"  4. end your turn",
		"",
		"Do not narrate the plan in prose. The plan IS the JSON file. Anything else is",
		"noise that ends your turn early.",
		"",
		"## Per-turn discipline (HARD RULE)",
		"",
		"If the DAG is large (>30 tasks or >20KB JSON), DO NOT try to write it all in",
		"one go — that long sustained generation stalls. Instead, split:",
		"  - write dag.json with the first N tasks (covering the foundational layer)",
		"  - read dag.json (confirms what's there)",
		"  - write dag.json with previous content + next N tasks (next layer up)",
		"  - repeat until all tasks + edges are encoded.",
		"Each individual write stays under ~3KB. The final write has the full DAG.",
		"",
		"If the DAG is small (<30 tasks), one write is fine — but keep description",
		"fields terse: bullet points and verbatim interface signatures from",
		"architecture.md, not narrative paragraphs.",
	}
	if frontierContext != nil && *frontierContext != "" {
		tick := "0"
		if frontierTick != nil {
			tick = jscompat.FormatNumber(*frontierTick)
		}
		lines = append(lines,
			"",
			"<system-reminder>",
			"FRONTIER MODE is indicated in your reminder. Emit only the next tranche",
			"of tasks you can shape confidently from the residual and evidence below.",
			"Do not repeat completed work or recreate existing tasks. Include a single",
			"contexts entry with kind=\"residual\" on the root when work remains.",
			"Frontier tick: "+tick,
			"",
			*frontierContext,
			"</system-reminder>",
		)
	}
	return strings.Join(lines, "\n")
}

// DispatchPlannerTranslate invokes the baked planner through agentjson.
func DispatchPlannerTranslate(
	ctx context.Context,
	input DispatchPlannerTranslateInput,
	deps agentjson.Dependencies,
) (agentjson.Result[DAGData], error) {
	outputPath := filepath.Join(input.Workspace, ".codeaf", "plan", "dag.json")
	if input.OutputPath != nil {
		outputPath = *input.OutputPath
	}
	maxRetries := 1
	timeout := int64(30 * 60_000)
	label := "planner-translate"
	fallback := cloneFallback()
	return agentjson.DispatchJSON(ctx, agentjson.Input[DAGData]{
		Agent:             "planner-translate",
		ParentSessionID:   input.ParentSessionID,
		Workspace:         input.Workspace,
		TaskPrompt:        BuildPrompt(outputPath, input.FrontierContext, input.FrontierTick),
		OutputPath:        outputPath,
		Schema:            DAGSchema{},
		Fallback:          &fallback,
		MaxRetries:        &maxRetries,
		TimeoutMS:         &timeout,
		Label:             &label,
		ParseFixBudget:    6,
		SchemaFixBudget:   4,
		PreserveOnSuccess: true,
	}, deps)
}

func cloneFallback() DAGData {
	fallback := DAGFallback
	fallback.Tasks = []DAGTask{}
	return fallback
}
