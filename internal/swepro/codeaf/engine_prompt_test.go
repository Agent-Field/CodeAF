package codeaf

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/assets"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/baked"
	configpkg "github.com/Agent-Field/aforge-v2/internal/swepro/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/project"
)

func systemTextFromRequest(t *testing.T, raw []byte) string {
	t.Helper()
	var body struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Messages) == 0 || body.Messages[0].Role != "system" {
		t.Fatalf("request has no leading system message: %s", raw)
	}
	var content []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body.Messages[0].Content, &content); err != nil || len(content) != 1 {
		t.Fatalf("invalid system content: %s", body.Messages[0].Content)
	}
	return content[0].Text
}

func TestCoderRequestSystemPromptOrderAndEnvironment(t *testing.T) {
	// Validation contracts 1-3: a coder request strips frontmatter, preserves
	// the TS system sequence, and carries every environment field.
	t.Setenv("AGENTFIELD_COMMIT_ATTRIBUTION", "0")
	workspace := t.TempDir()
	active := filepath.Join(workspace, "leaf")
	if err := gitRun(workspace, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(active, "placeholder"), "x\n"); err != nil {
		t.Fatal(err)
	}
	rawAgent, ok := baked.GetBakedAgentMarkdown("coder")
	if !ok {
		t.Fatal("missing coder")
	}

	var requestBody []byte
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var err error
		requestBody, err = readRequestBody(request)
		if err != nil {
			return nil, err
		}
		return recordedResponse(request, http.StatusOK, "text/event-stream", chatReply("done", 10)), nil
	})}
	backend := &openRouterBackend{apiKey: "test", client: client}
	vcs := "git"
	ctx := project.WithContext(context.Background(), project.InstanceContext{
		Directory: active, Worktree: workspace,
		Project: project.Info{Worktree: workspace, VCS: &vcs},
	})
	_, err := backend.Run(ctx, turn{
		Agent: "coder", AgentMarkdown: rawAgent,
		ProviderID: "openrouter", ModelID: "openai/gpt-6.1-codex",
		Workspace: active, Prompt: "implement it",
		SystemInstructions: []string{"ROOT INSTRUCTION"}, Reminder: "LEAF REMINDER",
	})
	if err != nil {
		t.Fatal(err)
	}
	system := systemTextFromRequest(t, requestBody)
	for _, forbidden := range []string{"---\nmode: subagent", "permission:\n", "model: inherit"} {
		if strings.Contains(system, forbidden) {
			t.Fatalf("frontmatter fragment %q reached request:\n%s", forbidden, system)
		}
	}
	ordered := []string{
		"<Role>",
		"You are powered by the model named openai/gpt-6.1-codex.",
		"ROOT INSTRUCTION",
		"LEAF REMINDER",
	}
	position := -1
	for _, fragment := range ordered {
		next := strings.Index(system, fragment)
		if next <= position {
			t.Fatalf("system sequence missing or reordered at %q:\n%s", fragment, system)
		}
		position = next
	}
	// llm.ts:186 picks the agent prompt *or* the family prompt, never both.
	// codex.txt opens with this line; a coder agent must not also receive it.
	if strings.Contains(system, "You are Codeaf, the best coding agent on the planet.") {
		t.Fatalf("codex family prompt was appended to an agent prompt:\n%s", system)
	}
	for _, field := range []string{
		"The exact model ID is openrouter/openai/gpt-6.1-codex",
		"  Working directory: " + active,
		"  Workspace root folder: " + workspace,
		"  Is directory a git repo: yes",
		"  Platform: " + runtime.GOOS,
		"  Today's date: ",
	} {
		if !strings.Contains(system, field) {
			t.Errorf("environment missing %q:\n%s", field, system)
		}
	}
}

func TestProviderPromptFallbackSelectsModelFamily(t *testing.T) {
	// Validation contract 4: the no-agent fallback selects GPT-family prompt
	// assets without leaking that family prompt to a DeepSeek model.
	t.Setenv("AGENTFIELD_COMMIT_ATTRIBUTION", "0")
	gptPrompt, _ := assets.Get("src/session/prompt/gpt.txt")
	defaultPrompt, _ := assets.Get("src/session/prompt/default.txt")
	gpt := composeTurnSystem(
		context.Background(), turn{Workspace: t.TempDir()}, "openrouter", "gpt-5", nil,
	)
	deepseek := composeTurnSystem(
		context.Background(), turn{Workspace: t.TempDir()}, "openrouter", "deepseek/deepseek-v3", nil,
	)
	if !strings.HasPrefix(gpt, gptPrompt) {
		t.Fatal("GPT-family prompt was not selected")
	}
	if strings.HasPrefix(deepseek, gptPrompt) || !strings.HasPrefix(deepseek, defaultPrompt) {
		t.Fatal("DeepSeek received the wrong provider prompt")
	}
	agent := "<Role>specialist</Role>"
	deepseekAgent := composeTurnSystem(
		context.Background(), turn{Workspace: t.TempDir(), AgentMarkdown: agent},
		"openrouter", "deepseek/deepseek-v3", nil,
	)
	if !strings.HasPrefix(deepseekAgent, agent+"\nYou are powered") ||
		strings.Contains(deepseekAgent, defaultPrompt) {
		t.Fatal("DeepSeek agent received a model-family base prompt")
	}
}

func TestConfiguredAgentPromptIsPassedVerbatim(t *testing.T) {
	// Validation contract: TS hands a configured `agent.prompt` straight to the
	// model (llm.ts:186). Only baked agent documents carry YAML frontmatter, so
	// a config string that merely opens with a Markdown rule must survive whole.
	t.Setenv("AGENTFIELD_COMMIT_ATTRIBUTION", "0")
	config := &codeafConfig{info: configpkg.Info{
		"agent": map[string]any{
			"coder": map[string]any{"prompt": "---\nHouse rules\n---\nAlways run the linter."},
		},
	}}
	configured, err := config.configureTurn(turn{Agent: "coder", Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	system := composeTurnSystem(
		context.Background(), configured, "openrouter", "deepseek/deepseek-v3", nil,
	)
	for _, fragment := range []string{"House rules", "Always run the linter."} {
		if !strings.Contains(system, fragment) {
			t.Fatalf("configured prompt lost %q:\n%s", fragment, system)
		}
	}

	// An unterminated leading rule must not empty the prompt and fall back to
	// the provider asset.
	defaultPrompt, _ := assets.Get("src/session/prompt/default.txt")
	config.info = configpkg.Info{"agent": map[string]any{
		"coder": map[string]any{"prompt": "---\nOnly one rule: be careful."},
	}}
	configured, err = config.configureTurn(turn{Agent: "coder", Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	system = composeTurnSystem(
		context.Background(), configured, "openrouter", "deepseek/deepseek-v3", nil,
	)
	if !strings.Contains(system, "Only one rule: be careful.") {
		t.Fatalf("unterminated rule emptied the configured prompt:\n%s", system)
	}
	if strings.Contains(system, defaultPrompt) {
		t.Fatalf("configured prompt fell back to the provider asset:\n%s", system)
	}
}

func readRequestBody(request *http.Request) ([]byte, error) {
	defer request.Body.Close()
	return io.ReadAll(request.Body)
}
