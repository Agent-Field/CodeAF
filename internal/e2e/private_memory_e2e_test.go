//go:build e2e

package e2e

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/gitidentity"
	"github.com/Agent-Field/codeaf/internal/store"
)

// privateMemoryPlan is loaded only by an explicit private acceptance run.
// Personal prompts, expected answers, identities and transcripts never belong
// in source fixtures or ordinary test output.
type privateMemoryPlan struct {
	SourceDB      string                  `json:"source_db"`
	ExpectedOwner string                  `json:"expected_owner"`
	EvidenceDir   string                  `json:"evidence_dir"`
	Scenarios     []privateMemoryScenario `json:"scenarios"`
}
type privateMemoryScenario struct {
	MemoryID      string   `json:"memory_id"`
	Prompt        string   `json:"prompt"`
	ExpectedAny   []string `json:"expected_any"`
	ExpectedAll   []string `json:"expected_all"`
	Forbidden     []string `json:"forbidden"`
	Rubric        string   `json:"rubric"`
	ImmediateNext bool     `json:"immediate_next"`
}
type privateMemoryOutcome struct {
	Scenario            int                         `json:"scenario"`
	Prompt              string                      `json:"prompt"`
	Answer              string                      `json:"answer"`
	Screen              string                      `json:"screen"`
	MemoryID            string                      `json:"memory_id"`
	Admission           store.MemoryAdmission       `json:"preview_admission"`
	Selected            bool                        `json:"preview_selected"`
	Tools               []string                    `json:"tools"`
	Passed              bool                        `json:"passed"`
	Reason              string                      `json:"reason"`
	SourceText          string                      `json:"source_text"`
	ActualPromptPresent *bool                       `json:"actual_prompt_present"`
	ActualMemoryBlock   string                      `json:"actual_memory_block"`
	ActualPromptTrace   string                      `json:"actual_prompt_trace"`
	ActualMemoryLines   []privateMemoryAdmittedLine `json:"actual_memory_lines"`
}

// TestPrivateMemoryAcrossOrdinaryChats uses a complete, explicitly supplied
// private database copy. Its scenarios are never filtered by admission: a
// missing useful line is a product failure, not a reason to choose another one.
func TestPrivateMemoryAcrossOrdinaryChats(t *testing.T) {
	path := os.Getenv("CODEAF_PRIVATE_MEMORY_SCENARIOS")
	if path == "" {
		t.Skip("private memory acceptance is opt-in")
	}
	requireTmuxAndKey(t)
	const model = "deepseek/deepseek-v4.1-flash"
	if e2eModel != model {
		t.Fatal("private acceptance requires the explicitly requested model for every role")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("could not read private acceptance plan")
	}
	var plan privateMemoryPlan
	if json.Unmarshal(raw, &plan) != nil || len(plan.Scenarios) < 6 || plan.SourceDB == "" || plan.EvidenceDir == "" || plan.ExpectedOwner == "" {
		t.Fatal("private acceptance plan requires a whole store, owner, evidence directory and at least six scenarios")
	}
	if !filepath.IsAbs(plan.EvidenceDir) {
		t.Fatal("private evidence destination must be absolute")
	}
	rel, err := filepath.Rel(repoRoot(t), plan.EvidenceDir)
	if err != nil || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		t.Fatal("private evidence must stay outside the source checkout")
	}
	if err := os.MkdirAll(plan.EvidenceDir, 0o700); err != nil {
		t.Fatal("could not create private evidence directory")
	}
	generated := newHome(t, map[string]any{"memory.enabled": true})
	home := filepath.Join(plan.EvidenceDir, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal("private acceptance needs a fresh evidence destination")
	}
	privateMemoryCopy(t, filepath.Join(generated, "config.json"), filepath.Join(home, "config.json"))
	privateMemoryCopy(t, plan.SourceDB, filepath.Join(home, "graph.db"))
	ws := workspaceAt(t, filepath.Join(plan.EvidenceDir, "workspace"), false)
	key, err := gitidentity.ProjectKey(ws)
	if err != nil {
		t.Fatal("could not identify isolated workspace")
	}
	owner := store.OwnerProject(key)
	count := privateMemoryReroot(t, filepath.Join(home, "graph.db"), plan.ExpectedOwner, owner)
	privateMemoryWrite(t, filepath.Join(plan.EvidenceDir, "identity-mapping.json"), map[string]any{"source_owner": plan.ExpectedOwner, "fixture_owner": owner, "total_rows_preserved": count})
	st, err := store.Open(filepath.Join(home, "graph.db"))
	if err != nil {
		t.Fatal("private source copy could not open")
	}
	defer st.Close()
	r := startDefault(t, "private_memory", home, ws, 160, 50, "chat", "--one-model", "--debug")
	// Suppress the generic rig's failure dump: evidence stays in the explicitly
	// private directory, including when assertions fail after all scenarios run.
	defer func() { r.kill(); r.home = "" }()
	outcomes := make([]privateMemoryOutcome, 0, len(plan.Scenarios))
	passed := 0
	for index, scenario := range plan.Scenarios {
		if index > 0 {
			r.lit("/new")
			r.keys("Enter")
			if !plan.Scenarios[index-1].ImmediateNext {
				time.Sleep(time.Second)
			}
		}
		block, err := st.AssembleMemoryBlock(store.MemoryContext{Owners: []string{store.OwnerUser, store.OwnerMachine, owner}, Project: key, Keywords: strings.Fields(scenario.Prompt)})
		if err != nil {
			t.Fatal("could not inspect private admission")
		}
		outcome := privateMemoryOutcome{Scenario: index + 1, Prompt: scenario.Prompt, MemoryID: scenario.MemoryID, Admission: block.Decisions[scenario.MemoryID]}
		outcome.Selected = outcome.Admission.Riding
		if source, found, err := st.MemoryRecord(scenario.MemoryID); err == nil && found {
			outcome.SourceText = source.Text
		}
		visible, err := st.MemoriesFor([]string{store.OwnerUser, store.OwnerMachine, owner})
		if err != nil {
			t.Fatal("could not snapshot private pre-turn source labels")
		}
		r.lit(scenario.Prompt + " ")
		r.keys("Enter")
		outcome.Answer, outcome.Tools = privateMemoryAwaitAnswer(home, scenario.Prompt, 3*time.Minute)
		outcome.Screen = r.capture()
		outcome.ActualPromptPresent, outcome.ActualMemoryBlock, outcome.ActualPromptTrace = privateMemoryPromptGroundTruth(home, scenario.Prompt, outcome.SourceText)
		outcome.ActualMemoryLines = privateMemoryActualLines(visible, outcome.ActualMemoryBlock)
		outcome.Passed, outcome.Reason = privateMemoryGrade(scenario, outcome.Answer, outcome.Tools)
		if outcome.Passed {
			passed++
		}
		outcomes = append(outcomes, outcome)
		privateMemoryWrite(t, filepath.Join(plan.EvidenceDir, "outcomes.json"), outcomes)
		if source, found, err := st.MemoryRecord(scenario.MemoryID); err == nil && found {
			privateMemoryWrite(t, filepath.Join(plan.EvidenceDir, fmt.Sprintf("source-state-%02d.json", index+1)), source)
		}
		t.Logf("private ordinary-chat scenario %d of %d finished; accepted=%t", index+1, len(plan.Scenarios), outcome.Passed)
	}
	models := callModels(t, home)
	modelsOK := len(models) > 0
	for _, actual := range models {
		if actual != model {
			modelsOK = false
		}
	}
	privateMemoryWrite(t, filepath.Join(plan.EvidenceDir, "model-receipts.json"), models)
	privateMemoryWrite(t, filepath.Join(plan.EvidenceDir, "summary.json"), map[string]any{"passed": passed, "total": len(outcomes), "recorded_calls": len(models), "exact_model_every_role": modelsOK, "all_source_rows_preserved": count})
	for _, outcome := range outcomes {
		if outcome.ActualPromptPresent == nil {
			t.Error("private acceptance has no inspectable first-turn request trace")
			break
		}
	}
	if !modelsOK {
		t.Error("private acceptance model receipts do not all match the requested model")
	}
	if passed != len(outcomes) {
		t.Errorf("private ordinary-chat acceptance: %d of %d scenarios passed; private evidence contains failure details", passed, len(outcomes))
	}
}

func privateMemoryCopy(t *testing.T, source, dest string) {
	t.Helper()
	in, err := os.Open(source)
	if err != nil {
		t.Fatal("could not read private fixture")
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal("could not create private fixture copy")
	}
	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err != nil || closeErr != nil {
		t.Fatal("could not complete private fixture copy")
	}
}
func privateMemoryWrite(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil || os.WriteFile(path, raw, 0o600) != nil {
		t.Fatal("could not preserve private acceptance evidence")
	}
}
func privateMemoryReroot(t *testing.T, path, source, target string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal("could not open private fixture for identity mapping")
	}
	defer db.Close()
	var before, after int
	if db.QueryRow("SELECT COUNT(*) FROM memories").Scan(&before) != nil {
		t.Fatal("could not count private source rows")
	}
	if _, err = db.Exec("UPDATE memories SET owner=? WHERE owner=?", target, source); err != nil {
		t.Fatal("could not map isolated project identity")
	}
	if db.QueryRow("SELECT COUNT(*) FROM memories").Scan(&after) != nil || before != after {
		t.Fatal("private fixture identity mapping changed memory row count")
	}
	return after
}
func privateMemoryAwaitAnswer(home, prompt string, within time.Duration) (string, []string) {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		answer, tools := privateMemoryJournalAnswer(home, prompt)
		if answer != "" {
			return answer, tools
		}
		time.Sleep(pollEvery)
	}
	return "", nil
}
func privateMemoryJournalAnswer(home, prompt string) (string, []string) {
	var answer string
	var tools []string
	_ = filepath.WalkDir(home, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		matched := false
		for _, line := range strings.Split(string(raw), "\n") {
			var row struct {
				journalLine
				Note bool `json:"note"`
			}
			if json.Unmarshal([]byte(line), &row) != nil {
				continue
			}
			if row.Role == "user" && !row.Note {
				matched = canonicalPrivatePrompt(row.Content) == canonicalPrivatePrompt(prompt)
			}
			if !matched || row.Role != "assistant" {
				continue
			}
			for _, call := range row.ToolCalls {
				tools = append(tools, call.Function.Name)
			}
			if len(row.ToolCalls) == 0 && strings.TrimSpace(row.Content) != "" {
				answer = row.Content
			}
		}
		return nil
	})
	return answer, tools
}
func privateMemoryGrade(s privateMemoryScenario, answer string, tools []string) (bool, string) {
	if answer == "" {
		return false, "no completed assistant answer"
	}

	lower := strings.ToLower(answer)
	matched := len(s.ExpectedAny) == 0
	for _, word := range s.ExpectedAny {
		matched = matched || strings.Contains(lower, strings.ToLower(word))
	}
	if !matched {
		return false, "answer omitted the required existing-memory detail"
	}
	for _, word := range s.ExpectedAll {
		if !strings.Contains(lower, strings.ToLower(word)) {
			return false, "answer violated a required existing-memory detail"
		}
	}
	for _, word := range s.Forbidden {
		if strings.Contains(lower, strings.ToLower(word)) {
			return false, "answer contradicted the existing-memory expectation"
		}
	}
	if privateMemoryLookupUsed(tools) {
		return false, "source detail recalled with a memory lookup, not anticipated"
	}
	return true, "source detail applied without a memory lookup"
}

// canonicalPrivatePrompt tolerates punctuation folded by terminal input while
// keeping the full words. Authored notes never end a person's reply boundary.
func canonicalPrivatePrompt(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, text)
}
func privateMemoryLookupUsed(tools []string) bool {
	for _, tool := range tools {
		switch tool {
		case "search_memories", "search_conversations", "recall":
			return true
		}
	}
	return false
}

type privateMemoryAdmittedLine struct{ ID, Text, SemanticType string }

func privateMemoryActualLines(memories []store.Memory, block string) []privateMemoryAdmittedLine {
	var lines []privateMemoryAdmittedLine
	for _, memory := range memories {
		if strings.Contains(block, store.MemoryQuote(memory.Text)) {
			lines = append(lines, privateMemoryAdmittedLine{memory.ID, memory.Text, memory.SemanticType})
		}
	}
	return lines
}
