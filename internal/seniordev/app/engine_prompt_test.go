//go:build !windows

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/baked"
	configpkg "github.com/Agent-Field/codeaf/internal/seniordev/config"
	"github.com/Agent-Field/codeaf/internal/seniordev/project"
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
	// A coder request strips frontmatter, keeps the system prompt in order
	// (role, model line, root instructions), and carries every environment
	// field.
	t.Setenv("AGENTFIELD_COMMIT_ATTRIBUTION", "0")
	workspace := t.TempDir()
	active := filepath.Join(workspace, "nested")
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
		SystemInstructions: []string{"ROOT INSTRUCTION"},
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
	}
	position := -1
	for _, fragment := range ordered {
		next := strings.Index(system, fragment)
		if next <= position {
			t.Fatalf("system sequence missing or reordered at %q:\n%s", fragment, system)
		}
		position = next
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

func TestComposeTurnSystemRequiresAnAgentPrompt(t *testing.T) {
	// There is no model-family base prompt behind the agent prompt: a turn
	// without one is refused instead of being sent with an empty role.
	t.Setenv("AGENTFIELD_COMMIT_ATTRIBUTION", "0")
	if _, err := composeTurnSystem(
		context.Background(), turn{Agent: "coder", Workspace: t.TempDir()},
		"openrouter", "deepseek/deepseek-v3", nil,
	); err == nil || !strings.Contains(err.Error(), "no system prompt") {
		t.Fatalf("empty agent prompt composed a system prompt: err=%v", err)
	}
	agent := "<Role>specialist</Role>"
	system, err := composeTurnSystem(
		context.Background(), turn{Workspace: t.TempDir(), AgentMarkdown: agent},
		"openrouter", "deepseek/deepseek-v3", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(system, agent+"\nYou are powered") {
		t.Fatalf("agent prompt must open the system prompt directly:\n%s", system)
	}
}

func TestConfiguredAgentPromptIsPassedVerbatim(t *testing.T) {
	// A configured `agent.prompt` reaches the model verbatim. Only baked agent
	// documents carry YAML frontmatter, so a config string that merely opens
	// with a Markdown rule must survive whole.
	t.Setenv("AGENTFIELD_COMMIT_ATTRIBUTION", "0")
	config := &seniorDevConfig{info: configpkg.Info{
		"agent": map[string]any{
			"coder": map[string]any{"prompt": "---\nHouse rules\n---\nAlways run the linter."},
		},
	}}
	configured, err := config.configureTurn(turn{Agent: "coder", Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	system, err := composeTurnSystem(
		context.Background(), configured, "openrouter", "deepseek/deepseek-v3", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"House rules", "Always run the linter."} {
		if !strings.Contains(system, fragment) {
			t.Fatalf("configured prompt lost %q:\n%s", fragment, system)
		}
	}

	// An unterminated leading rule must not empty the prompt.
	config.info = configpkg.Info{"agent": map[string]any{
		"coder": map[string]any{"prompt": "---\nOnly one rule: be careful."},
	}}
	configured, err = config.configureTurn(turn{Agent: "coder", Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	system, err = composeTurnSystem(
		context.Background(), configured, "openrouter", "deepseek/deepseek-v3", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(system, "Only one rule: be careful.") {
		t.Fatalf("unterminated rule emptied the configured prompt:\n%s", system)
	}
}

func TestBakedRuntimeControlsKeepExplicitPoolModel(t *testing.T) {
	// coder.md declares `model: inherit` and no step cap: the pool model the
	// caller chose stays, and no baked cap is invented.
	cfg := &seniorDevConfig{}
	configured, err := cfg.configureTurn(turn{
		Agent: "coder", ProviderID: "openrouter",
		ModelID: "deepseek/deepseek-v4-flash-0731",
	})
	if err != nil {
		t.Fatal(err)
	}
	if configured.ProviderID != "openrouter" ||
		configured.ModelID != "deepseek/deepseek-v4-flash-0731" {
		t.Fatalf("baked model overrode the explicit pool model: %+v", configured)
	}
	if configured.MaxSteps != nil {
		t.Fatalf("coder max steps = %v, want none from the baked document", *configured.MaxSteps)
	}
}

func TestConfiguredTurnEmitsEffectiveRuntimeProvenance(t *testing.T) {
	var output bytes.Buffer
	runtime := &runtimeAdapter{
		config: &seniorDevConfig{}, events: newEventWriter(&output),
	}
	if _, err := runtime.configureTurn(turn{
		Agent: "coder", SessionID: "ses-coder",
		AgentMarkdown: "coder prompt", ProviderID: "openrouter",
		ModelID: "deepseek/model",
	}); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`"stage":"agent-runtime"`, `"agent":"coder"`,
		`"session_id":"ses-coder"`, `"model_id":"deepseek/model"`,
		`"prompt_sha256"`,
	} {
		if !strings.Contains(output.String(), fragment) {
			t.Fatalf("runtime provenance missing %s: %s", fragment, output.String())
		}
	}
}

func readRequestBody(request *http.Request) ([]byte, error) {
	defer request.Body.Close()
	return io.ReadAll(request.Body)
}
