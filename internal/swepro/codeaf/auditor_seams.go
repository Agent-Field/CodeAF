package codeaf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/agentjson"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/auditorgate"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/lowjudge"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/specclauses"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/tia"
)

type liveClauseJudger struct{ runner *pipeline }

type clauseValue struct {
	Clauses []string `json:"clauses"`
}

type matrixValue struct {
	Cells []string `json:"cells"`
}

type clauseSchema struct{}

func (clauseSchema) SafeParse(value any) lowjudge.SchemaParseResult[clauseValue] {
	var parsed clauseValue
	if !decodeGenerated(value, &parsed) || parsed.Clauses == nil || len(parsed.Clauses) > 40 {
		return lowjudge.SchemaParseResult[clauseValue]{ErrorMessage: "invalid clauses array"}
	}
	for _, clause := range parsed.Clauses {
		length := len(utf16.Encode([]rune(clause)))
		if length < 4 || length > 300 {
			return lowjudge.SchemaParseResult[clauseValue]{ErrorMessage: "clause length out of range"}
		}
	}
	return lowjudge.SchemaParseResult[clauseValue]{Success: true, Data: parsed}
}

type matrixSchema struct{}

func (matrixSchema) SafeParse(value any) lowjudge.SchemaParseResult[matrixValue] {
	var parsed matrixValue
	if !decodeGenerated(value, &parsed) || parsed.Cells == nil || len(parsed.Cells) > 400 {
		return lowjudge.SchemaParseResult[matrixValue]{ErrorMessage: "invalid cells array"}
	}
	for _, cell := range parsed.Cells {
		if cell == "" {
			return lowjudge.SchemaParseResult[matrixValue]{ErrorMessage: "empty matrix cell"}
		}
	}
	return lowjudge.SchemaParseResult[matrixValue]{Success: true, Data: parsed}
}

func decodeGenerated(value any, target any) bool {
	body, err := json.Marshal(value)
	return err == nil && json.Unmarshal(body, target) == nil
}

type clauseMemoEntry struct {
	Clauses []string  `json:"clauses"`
	Count   float64   `json:"count"`
	Source  string    `json:"source"`
	Matrix  *[]string `json:"matrix,omitempty"`
}

const clauseJudgePrompt = "Extract the distinct behavioral clauses from the software task spec below.\n" +
	"A clause is one independently testable demand: a behavior, an option value's\n" +
	"effect, an operator's semantics, an edge rule, a format constraint, or a\n" +
	"stated interaction between two of these. Split bundled sentences into their\n" +
	"separate demands. Do NOT include background prose, motivations, repository\n" +
	"instructions (branching/committing), or restatements. Phrase each clause as\n" +
	"one short line. If the spec is a simple single-goal task (one bug, one\n" +
	"behavior), return just that one clause. Return JSON: {\"clauses\": [...]}.\n\n" +
	"--- SPEC ---\n"

const matrixJudgePrompt = "You are given a software task spec and the behavioral clauses already extracted from it.\n" +
	"Most acceptance failures are SECOND-ORDER: a clause is implemented correctly\n" +
	"on its own but breaks when it must hold INSIDE another construct. Derive the\n" +
	"INTERACTION MATRIX — the structural contexts and feature combinations, drawn\n" +
	"from THIS SPEC'S OWN DOMAIN, under which each clause must still hold. For every\n" +
	"clause consider only the contexts the spec actually admits:\n" +
	"  - embedding / nesting: the behavior occurring inside another construct;\n" +
	"  - negation / inversion: the behavior under a negated, disabled, or inverted form;\n" +
	"  - repetition: the behavior applied repeatedly or to repeated input;\n" +
	"  - pairwise combination: the clause co-occurring with each OTHER listed clause.\n" +
	"Emit ONE cell per interaction the spec genuinely implies, written as\n" +
	"\"clause ⊗ context\" — a short clause fragment, the ⊗ symbol, then the context.\n" +
	"Do NOT emit a mechanical cross product: skip any pairing the spec does not\n" +
	"imply is possible or meaningful in its domain. Keep each cell to one short line.\n" +
	"Return JSON: {\"cells\": [\"clause ⊗ context\", ...]}. Emit at most 60 cells.\n\n" +
	"--- CLAUSES ---\n"

func (judge liveClauseJudger) JudgeSpecClauses(
	_ context.Context, spec, workspace string,
) (specclauses.SpecClauseJudgment, error) {
	if judge.runner == nil || judge.runner.runtime == nil {
		return fallbackClauseJudgment(spec), nil
	}
	hash := sha256.Sum256([]byte(spec))
	key := hex.EncodeToString(hash[:])[:16]
	memoPath := filepath.Join(workspace, ".codeaf", "spec-clauses.json")
	memo := map[string]clauseMemoEntry{}
	if body, err := os.ReadFile(memoPath); err == nil {
		_ = json.Unmarshal(body, &memo)
	}
	hit, cacheHit := memo[key]
	cacheHit = cacheHit && hit.Clauses != nil

	modelRef := firstModel(judge.runner.pool.low)
	var language any
	var generator lowjudge.ObjectGenerator
	if modelRef != "" {
		model := agentjsonModel(modelRef)
		language = lowjudge.ModelReference{ModelID: model.ModelID, ID: modelRef}
		generator = runtimeObjectGenerator{
			runtime: judge.runner.runtime, workspace: workspace, model: model,
		}
	}
	logLine := func(line string) { judge.runner.note(line + "\n") }
	clippedSpec := prefixUTF16(spec, 20_000)
	judgment := fallbackClauseJudgment(spec)
	if cacheHit {
		judgment = specclauses.SpecClauseJudgment{
			Clauses: hit.Clauses, Count: hit.Count, Source: "cache", Matrix: hit.Matrix,
		}
	} else {
		clausesResult := lowjudge.LowJudge(lowjudge.LowJudgeInput[clauseValue]{
			Prompt: clauseJudgePrompt + clippedSpec, Schema: clauseSchema{},
			Language: language, Generate: generator, Fallback: func() clauseValue {
				return clauseValue{Clauses: []string{}}
			}, Log: logLine,
		})
		if clausesResult.Source == "llm" {
			judgment = specclauses.SpecClauseJudgment{
				Clauses: clausesResult.Value.Clauses,
				Count:   float64(len(clausesResult.Value.Clauses)), Source: "llm",
			}
		}
	}
	freshMatrix := false
	if len(judgment.Clauses) >= specclauses.MatrixMinClauses && judgment.Matrix == nil {
		lines := make([]string, 0, len(judgment.Clauses))
		for index, clause := range judgment.Clauses {
			lines = append(lines, itoa(index+1)+". "+clause)
		}
		matrixResult := lowjudge.LowJudge(lowjudge.LowJudgeInput[matrixValue]{
			Prompt: matrixJudgePrompt + strings.Join(lines, "\n") +
				"\n\n--- SPEC ---\n" + clippedSpec,
			Schema: matrixSchema{}, Language: language, Generate: generator,
			Fallback: func() matrixValue { return matrixValue{Cells: []string{}} },
			Log:      logLine,
		})
		if matrixResult.Source == "llm" {
			cells := make([]any, len(matrixResult.Value.Cells))
			for index, cell := range matrixResult.Value.Cells {
				cells[index] = cell
			}
			matrix := specclauses.ClampMatrixCells(cells)
			judgment.Matrix = &matrix
			freshMatrix = true
		}
	}
	if judgment.Source == "llm" || freshMatrix {
		persistedSource := judgment.Source
		if persistedSource == "cache" {
			persistedSource = hit.Source
			if persistedSource == "" {
				persistedSource = "llm"
			}
		}
		memo[key] = clauseMemoEntry{
			Clauses: judgment.Clauses, Count: judgment.Count,
			Source: persistedSource, Matrix: judgment.Matrix,
		}
		if body, err := json.Marshal(memo); err == nil &&
			os.MkdirAll(filepath.Dir(memoPath), 0o755) == nil {
			_ = os.WriteFile(memoPath, body, 0o666)
		}
	}
	return judgment, nil
}

func fallbackClauseJudgment(spec string) specclauses.SpecClauseJudgment {
	return specclauses.SpecClauseJudgment{
		Clauses: []string{}, Count: specclauses.CountSpecClauses(spec), Source: "fallback",
	}
}

type runtimeObjectGenerator struct {
	runtime   *runtimeAdapter
	workspace string
	model     agentjson.Model
}

func (generator runtimeObjectGenerator) Generate(
	params lowjudge.GenerateParams,
) (lowjudge.GeneratedObject, error) {
	if generator.runtime == nil || generator.runtime.backend == nil {
		return lowjudge.GeneratedObject{}, errors.New("low judge backend unavailable")
	}
	ctx := context.Background()
	if params.AbortSignal != nil && params.AbortSignal.Context != nil {
		ctx = params.AbortSignal.Context
	}
	result, err := generator.runtime.backend.Run(ctx, turn{
		SessionID: generator.runtime.nextID("session"), Agent: "low-judge",
		Workspace: generator.workspace, ProviderID: generator.model.ProviderID,
		ModelID: generator.model.ModelID, Prompt: params.Prompt,
		LowModels: generator.runtime.pool.values("low"),
	})
	generator.runtime.observeTurn("low-judge", result)
	generator.runtime.addCost(result.CostUSD)
	if err != nil {
		return lowjudge.GeneratedObject{}, err
	}
	raw := strings.TrimSpace(turnText(result))
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return lowjudge.GeneratedObject{}, errors.New("low judge returned no JSON object")
	}
	decoder := json.NewDecoder(bytes.NewBufferString(raw[start : end+1]))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return lowjudge.GeneratedObject{}, err
	}
	return lowjudge.GeneratedObject{Object: value}, nil
}

func (runner *pipeline) auditorDependencies() auditorgate.GateDependencies {
	start := runner.runtime.auditCount()
	return auditorgate.GateDependencies{
		AgentJSON: runner.agentJSON(),
		Clauses:   liveClauseJudger{runner: runner},
		Observer:  runner.observerTracker(),
		Adjudicator: auditorgate.AgentJSONAdjudicator{
			Dependencies: runner.agentJSON(),
		},
		ImpactedTests: func(workspace string, changedFiles []string) []string {
			result := tia.ComputeImpactedTestsForWorkspace(
				tia.ComputeImpactedTestsForWorkspaceOptions{
					Workspace: workspace, ChangedFiles: changedFiles,
				},
			)
			if result == nil || result.Confidence != tia.ConfidenceExact ||
				len(result.Impacted) < 1 || len(result.Impacted) > 20 {
				return nil
			}
			return result.Impacted
		},
		ExecutedCommands: func(attempt int) []string {
			return runner.runtime.auditObservation(start + attempt - 1).executed
		},
		EvidenceBlocks: func(attempt int, _ auditorgate.AuditorVerdict) []string {
			return runner.runtime.auditObservation(start + attempt - 1).evidence
		},
	}
}
