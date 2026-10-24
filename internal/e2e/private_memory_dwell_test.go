//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/gitidentity"
	"github.com/Agent-Field/codeaf/internal/store"
)

// TestPrivateMemoryDwellInRealChat separates adjacent-turn stability from the
// independent-question acceptance batch. All source data stays opt-in/private.
func TestPrivateMemoryDwellInRealChat(t *testing.T) {
	path := os.Getenv("CODEAF_PRIVATE_MEMORY_DWELL_PLAN")
	if path == "" {
		t.Skip("private adjacent-turn acceptance is opt-in")
	}
	requireTmuxAndKey(t)
	if e2eModel != "deepseek/deepseek-v4.1-flash" {
		t.Fatal("private dwell acceptance requires the requested model")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("could not read private dwell plan")
	}
	var plan privateMemoryPlan
	if json.Unmarshal(raw, &plan) != nil || len(plan.Scenarios) == 0 {
		t.Fatal("private dwell plan requires a source question")
	}
	if !filepath.IsAbs(plan.EvidenceDir) {
		t.Fatal("private evidence destination must be absolute")
	}
	rel, err := filepath.Rel(repoRoot(t), plan.EvidenceDir)
	if err != nil || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		t.Fatal("private evidence must stay outside the source checkout")
	}
	if err := os.MkdirAll(plan.EvidenceDir, 0o700); err != nil {
		t.Fatal("could not prepare private dwell evidence")
	}
	generated := newHome(t, map[string]any{"memory.enabled": true, "vision_model": e2eModel, "model.image": e2eModel, "model.speech": e2eModel, "model.music": e2eModel, "model.video": e2eModel, "model.voice": e2eModel})
	home := filepath.Join(plan.EvidenceDir, "home")
	if os.Mkdir(home, 0o700) != nil {
		t.Fatal("private dwell destination must be fresh")
	}
	privateMemoryCopy(t, filepath.Join(generated, "config.json"), filepath.Join(home, "config.json"))
	privateMemoryCopy(t, plan.SourceDB, filepath.Join(home, "graph.db"))
	ws := workspaceAt(t, filepath.Join(plan.EvidenceDir, "workspace"), false)
	key, err := gitidentity.ProjectKey(ws)
	if err != nil {
		t.Fatal("could not identify private dwell workspace")
	}
	privateMemoryReroot(t, filepath.Join(home, "graph.db"), plan.ExpectedOwner, store.OwnerProject(key))
	st, err := store.Open(filepath.Join(home, "graph.db"))
	if err != nil {
		t.Fatal("could not open private dwell store")
	}
	defer st.Close()
	source, found, err := st.MemoryRecord(plan.Scenarios[0].MemoryID)
	if err != nil || !found {
		t.Fatal("could not inspect private dwell source")
	}
	r := startPrivateMemory(t, "private_dwell", home, ws, 160, 50, "chat", "--one-model", "--debug")
	defer func() { r.kill(); r.home = "" }()
	outcomes := []privateMemoryOutcome{}
	for i, prompt := range []string{plan.Scenarios[0].Prompt, "Can you give an example?", "Why?"} {
		r.lit(prompt + " ")
		r.keys("Enter")
		answer, tools := privateMemoryAwaitAnswer(home, prompt, 3*time.Minute)
		present, block, trace := privateMemoryPromptGroundTruth(home, prompt, source.Text)
		outcomes = append(outcomes, privateMemoryOutcome{Scenario: i + 1, Prompt: prompt, Answer: answer, Tools: tools, ActualPromptPresent: present, ActualMemoryBlock: block, ActualPromptTrace: trace})
		privateMemoryWrite(t, filepath.Join(plan.EvidenceDir, "outcomes.json"), outcomes)
		if answer == "" || present == nil {
			t.Fatal("private dwell observation lacks a completed reply or first-request trace")
		}
	}
	same := outcomes[0].ActualMemoryBlock == outcomes[1].ActualMemoryBlock && outcomes[1].ActualMemoryBlock == outcomes[2].ActualMemoryBlock
	models := callModels(t, home)
	modelsOK := len(models) > 0
	for _, model := range models {
		if model != e2eModel {
			modelsOK = false
		}
	}
	privateMemoryWrite(t, filepath.Join(plan.EvidenceDir, "summary.json"), map[string]any{"same_memory_block_all_turns": same, "exact_model_every_role": modelsOK, "calls": len(models), "turns": len(outcomes)})
	if !modelsOK {
		t.Fatal("private dwell receipt used a different model")
	}
	if !same {
		t.Fatal("ordinary followups changed the admitted memory block; private evidence retained")
	}
	t.Logf("Observed three real same-conversation turns; memory bytes stable=%t", same)
}
