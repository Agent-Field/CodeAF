package codeaf

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/adaptiveflag"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/frontierplanning"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/specidentifiers"
)

// runConventionScout ports W12b: path identifiers seed a one-shot sibling
// convention read, and only high-confidence predictions reach the audit gate.
func (runner *pipeline) runConventionScout(ctx context.Context, goal string) {
	if os.Getenv("CODEAF_GLOSSARY") == "0" || !adaptiveflag.AdaptiveCutsEnabled() ||
		!runner.frontierPlanningLedger().TryTake(frontierplanning.RoleGlossary) {
		return
	}
	explicit := specidentifiers.EnforceableIdentifiers(specidentifiers.ExtractSpecIdentifiers(goal))
	frontierExplicit := make([]frontierplanning.SpecIdentifier, 0, len(explicit))
	pathIDs := make([]string, 0, 4)
	for _, identifier := range explicit {
		frontierExplicit = append(frontierExplicit, frontierplanning.SpecIdentifier{
			Value: identifier.Value, Kind: frontierplanning.SpecIdentifierKind(identifier.Kind),
			Source: identifier.Source,
		})
		if identifier.Kind == specidentifiers.KindPath && len(pathIDs) < 4 {
			pathIDs = append(pathIDs, identifier.Value)
		}
	}
	siblingContext := gatherSiblingConventionContext(runner.workspace, pathIDs)
	if siblingContext == "" {
		runner.note("[codeaf] W12b glossary: no spec path identifiers to seed sibling conventions — skipped\n")
		return
	}
	runner.note("[codeaf] W12b convention-scout: dispatching (frontier one-shot)\n")
	modelID := firstModel(runner.pool.frontier)
	if modelID == "" {
		modelID = firstModel(runner.pool.high)
	}
	model := agentjsonModel(modelID)
	sessionID, err := runner.runtime.Create(ctx, runner.sessionID, "convention-scout")
	if err != nil {
		return
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	response, err := runner.runtime.Prompt(timeoutCtx, oneShotPromptRequest{
		MessageID: runner.runtime.nextID("message"), SessionID: sessionID,
		Model: oneShotPromptModel{ModelID: model.ModelID, ProviderID: model.ProviderID},
		Agent: "convention-scout", Tools: &oneShotToolSettings{}, Workspace: runner.workspace,
		Parts: []any{oneShotTextPart{Type: "text", Text: frontierplanning.BuildGlossaryPrompt(struct {
			TaskText       string                            `json:"taskText"`
			Identifiers    []frontierplanning.SpecIdentifier `json:"identifiers"`
			SiblingContext string                            `json:"siblingContext"`
		}{TaskText: goal, Identifiers: frontierExplicit, SiblingContext: siblingContext})}},
	})
	if err != nil {
		return
	}
	result, ok := response.(turnResult)
	if !ok {
		return
	}
	predictions := frontierplanning.ParseGlossary(turnText(result))
	enforced := frontierplanning.EnforceableGlossaryIdentifiers(predictions, frontierExplicit)
	runner.predictedIdentifiers = make([]specidentifiers.SpecIdentifier, 0, len(enforced))
	for _, identifier := range enforced {
		context := specidentifiers.ContextRequirement
		runner.predictedIdentifiers = append(runner.predictedIdentifiers, specidentifiers.SpecIdentifier{
			Value: identifier.Value, Kind: specidentifiers.KindCode,
			Source: identifier.Source, Context: &context,
		})
	}
	runner.note("[codeaf] W12b glossary: " + itoa(len(predictions)) + " prediction(s), " +
		itoa(len(runner.predictedIdentifiers)) + " machine-enforced\n")
}

func gatherSiblingConventionContext(workspace string, pathIDs []string) string {
	chunks := []string{}
	for _, pathID := range pathIDs {
		relDir := filepath.Dir(pathID)
		directory := filepath.Join(workspace, relDir)
		extension := filepath.Ext(pathID)
		entries, err := os.ReadDir(directory)
		if err != nil {
			continue
		}
		if len(entries) > 40 {
			entries = entries[:40]
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		chunks = append(chunks, "### dir "+filepath.ToSlash(relDir)+"\n"+strings.Join(names, "\n"))
		excerpts := 0
		for _, name := range names {
			if extension == "" || !strings.HasSuffix(name, extension) || excerpts == 3 {
				continue
			}
			body, err := os.ReadFile(filepath.Join(directory, name))
			if err != nil {
				continue
			}
			lines := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
			if len(lines) > 80 {
				lines = lines[:80]
			}
			chunks = append(chunks, "### excerpt "+filepath.ToSlash(filepath.Join(relDir, name))+"\n"+strings.Join(lines, "\n"))
			excerpts++
		}
	}
	return truncateUTF16(strings.Join(chunks, "\n\n"), 24_000)
}

func truncateUTF16(value string, limit int) string {
	units := utf16.Encode([]rune(value))
	if len(units) <= limit {
		return value
	}
	return string(utf16.Decode(units[:limit]))
}
