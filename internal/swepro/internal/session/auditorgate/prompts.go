// This file ports the byte-visible full/light auditor briefs from
// src/session/auditor-gate.ts:1315-1559.
package auditorgate

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/artifactregistry"
)

type AuditPromptArgs struct {
	UserPrompt      string   `json:"userPrompt"`
	Diff            string   `json:"diff"`
	Workspace       string   `json:"workspace"`
	CarryForward    *string  `json:"carryForward,omitempty"`
	ImpactedTests   []string `json:"impactedTests,omitempty"`
	ClauseInventory []string `json:"clauseInventory,omitempty"`
	ClauseMatrix    []string `json:"clauseMatrix,omitempty"`
	BaseSHA         *string  `json:"baseSha,omitempty"`
}

type LightAuditPromptArgs struct {
	UserPrompt      string   `json:"userPrompt"`
	Diff            string   `json:"diff"`
	Workspace       string   `json:"workspace"`
	TurnCap         float64  `json:"turnCap"`
	CarryForward    *string  `json:"carryForward,omitempty"`
	ImpactedTests   []string `json:"impactedTests,omitempty"`
	ClauseInventory []string `json:"clauseInventory,omitempty"`
	ClauseMatrix    []string `json:"clauseMatrix,omitempty"`
	BaseSHA         *string  `json:"baseSha,omitempty"`
}

func impactedTestsBlock(impacted []string) []string {
	lines := []string{
		"",
		"# Impacted tests (precomputed reverse-import reachability from the diff)",
	}
	for _, test := range impacted {
		lines = append(lines, "  - "+test)
	}
	return append(lines,
		"Run these FIRST — any failure here is an automatic fail with no further",
		"test runs needed. Only after they pass, run the project's standard test",
		"entrypoint once as final confirmation.",
		"NOTE: these regression checks were mostly authored by the worker and share",
		"its blind spots — passing them is necessary but NOT sufficient. Your Step 2d",
		"spec-derived probes are still required for a pass verdict.",
	)
}

func diffSection(diff, workspace string, baseSHA *string) []string {
	base := "HEAD"
	baseDescription := "HEAD (no base recorded)"
	if baseSHA != nil {
		base, baseDescription = *baseSHA, *baseSHA
	}
	header := []string{
		"# Worker's diff (staged tree vs " + base + ")",
		"Base SHA: " + baseDescription,
	}
	body := diff
	if body == "" {
		body = "(no diff captured)"
	}
	if !artifactregistry.ArtifactRefsEnabled() {
		return append(header, "```diff", body, "```")
	}
	head, tail := 6000.0, 2000.0
	return append(header, artifactregistry.EmbedArtifact(
		workspace,
		"diff",
		body,
		&artifactregistry.RenderRefOptions{
			ExcerptHead: &head,
			ExcerptTail: &tail,
			Note:        "excerpt only — read the artifact file for omitted hunks",
		},
	))
}

func specSection(header, prompt, workspace string) []string {
	if !artifactregistry.ArtifactRefsEnabled() || utf16Len(prompt) <= 4000 {
		return []string{header, prompt}
	}
	head, tail := 4000.0, 2000.0
	return []string{
		header,
		artifactregistry.EmbedArtifact(
			workspace,
			"spec",
			prompt,
			&artifactregistry.RenderRefOptions{
				ExcerptHead: &head,
				ExcerptTail: &tail,
				Note:        "excerpt only — full spec in the artifact file",
			},
		),
	}
}

func clauseInventoryBlock(clauses, matrix []string) []string {
	if len(clauses) == 0 {
		return []string{}
	}
	lines := []string{
		"",
		"# Spec clause inventory (low-tier judgment — verify, don't rebuild)",
		"Every line below is a distinct behavioral demand extracted from the spec.",
		"Probe each per Step 2d (independent inputs, incl. pairwise interactions)",
		"and record the evidence in `clause_coverage`. Extend the list if the spec",
		"demands something it missed; never shrink it.",
	}
	for i, clause := range clauses {
		lines = append(lines, formatInt(i+1)+". "+clause)
	}
	if len(matrix) > 0 {
		lines = append(lines,
			"",
			"# Interaction matrix (the behavior must hold in each)",
			"First-order clause coverage is NOT sufficient: the most common miss is a",
			"clause that works at top level but breaks INSIDE another construct",
			"(nesting, negation, repetition, combination with a sibling feature). Each",
			"cell below (\"clause ⊗ context\") is a context this spec's domain admits",
			"under which the named clause must STILL hold. Your Step 2d probes must",
			"cover these cells; pooled probes that exercise several cells at once are",
			"encouraged. Record each in `clause_coverage` — either with probe evidence",
			"or, when the spec makes a cell genuinely not-applicable, an explicit",
			"not-applicable justification naming the cell. A pass that leaves a cell",
			"neither probed nor justified is inadmissible.",
		)
		for i, cell := range matrix {
			lines = append(lines, formatInt(i+1)+". "+cell)
		}
	}
	return lines
}

func BuildLightAuditPrompt(args LightAuditPromptArgs) string {
	lines := []string{
		"You are auditing a task that ran on the root-cut fast path — a single",
		"strong-model leaf, no decomposition, with a short Definition of Done.",
		"Run a SINGLE-PASS verification, NOT the full four-step adversarial loop.",
		"Default verdict = fail; you must be convinced by fresh evidence to pass.",
		"",
	}
	lines = append(lines, specSection(
		"# Original task spec (includes the Definition of done)",
		args.UserPrompt,
		args.Workspace,
	)...)
	lines = append(lines, "")
	lines = append(lines, diffSection(args.Diff, args.Workspace, args.BaseSHA)...)
	lines = append(lines, "", "# Worktree", args.Workspace)
	if args.CarryForward != nil && *args.CarryForward != "" {
		lines = append(lines, "", *args.CarryForward)
	}
	if len(args.ImpactedTests) > 0 {
		lines = append(lines, impactedTestsBlock(args.ImpactedTests)...)
	}
	lines = append(lines, clauseInventoryBlock(args.ClauseInventory, args.ClauseMatrix)...)
	lines = append(lines,
		"",
		"Do these once, in a single pass, then emit the verdict and stop:",
		"  1. Read the diff against the Definition of done above.",
		"  2. Detect the project's build/test entrypoint (manifest/CI) and RUN it",
		"     in a FRESH subprocess. Record the exact command + exit code in",
		"     step2_signal.commands. A broken build or a failing test is an",
		"     automatic fail.",
		"  3. If the spec names an explicit verification command or expected",
		"     output, RUN it and byte-compare against the spec's claim.",
		"  4. Write the verdict JSON.",
		"",
		BlockerSeverityInstruction,
		"",
		NamingConventionInstruction,
		"",
		"This is REAL verification, not a rubber stamp: verdict=pass REQUIRES at",
		"least one real command with its exit code in step2_signal.commands. A",
		"pass with no fresh command evidence is forbidden (default-pass is the one",
		"banned outcome — its result poisons downstream calibration).",
		"",
		"Hard budget: "+formatNumber(args.TurnCap)+" turns. Do NOT re-derive a full mental model,",
		"grep every caller/sibling, or do a separate cold structural pass — the",
		"root-cut path is reserved for tasks a strong model one-shots, so a focused",
		"build + test + DoD check is sufficient and expected.",
		"",
		"Write your verdict to "+args.Workspace+"/.codeaf/auditor-verdict.json AND include the same JSON in your final assistant message.",
		"",
		"UPDATE THAT FILE INCREMENTALLY: after EVERY probe batch, rewrite it folding",
		"in the evidence gathered so far (commands + exit codes, clause_coverage",
		"entries). Never leave it as a skeleton while you continue probing — the",
		"session can end at any moment (context compaction, turn cap), and whatever",
		"is on disk at that moment IS your verdict; a skeleton is rejected as",
		"evidence-free and the entire session's work is discarded.",
		"",
		"If your turn budget is exhausted and you cannot confidently pass, return fail with reason 'insufficient evidence' — default-pass is forbidden.",
	)
	return strings.Join(lines, "\n")
}

func BuildAuditPrompt(args AuditPromptArgs) string {
	lines := []string{
		"You are auditing the completion of this task. Default verdict = fail; you must be convinced by independent evidence to pass.",
		"",
	}
	lines = append(lines, specSection("# Original task spec", args.UserPrompt, args.Workspace)...)
	lines = append(lines, "")
	lines = append(lines, diffSection(args.Diff, args.Workspace, args.BaseSHA)...)
	lines = append(lines, "", "# Worktree", args.Workspace)
	if args.CarryForward != nil && *args.CarryForward != "" {
		lines = append(lines, "", *args.CarryForward)
	}
	if len(args.ImpactedTests) > 0 {
		lines = append(lines, impactedTestsBlock(args.ImpactedTests)...)
	}
	lines = append(lines, clauseInventoryBlock(args.ClauseInventory, args.ClauseMatrix)...)
	lines = append(lines,
		"",
		"Execute the four-step adversarial procedure from your role:",
		"  1. Goal re-extraction (before re-reading the diff)",
		"  2. Signal reproduction (fresh subprocess for any tests/builds the spec implies)",
		"  3. Scope adequacy (grep callers/siblings/regressions for every changed symbol)",
		"  4. Cold structural read (does the diff match the simplest fix shape?)",
		"",
		BlockerSeverityInstruction,
		"",
		NamingConventionInstruction,
		"",
		"Write your verdict to "+args.Workspace+"/.codeaf/auditor-verdict.json AND include the same JSON in your final assistant message.",
		"",
		"UPDATE THAT FILE INCREMENTALLY: after EVERY probe batch, rewrite it folding",
		"in the evidence gathered so far (commands + exit codes, clause_coverage",
		"entries). Never leave it as a skeleton while you continue probing — the",
		"session can end at any moment (context compaction, turn cap), and whatever",
		"is on disk at that moment IS your verdict; a skeleton is rejected as",
		"evidence-free and the entire session's work is discarded.",
		"",
		"If your evidence budget is exhausted and you cannot confidently pass, return fail with reason 'insufficient evidence' — default-pass is forbidden.",
	)
	return strings.Join(lines, "\n")
}

func formatInt(value int) string        { return formatNumber(float64(value)) }
func formatNumber(value float64) string { return jscompat.FormatNumber(value) }
