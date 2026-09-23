//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/permission"
)

func TestProjectConfigChangesLiveRuntimePermissionsAndInstructions(t *testing.T) {
	workspace := t.TempDir()
	global := t.TempDir()
	t.Setenv("SENIOR_DEV_CONFIG_DIR", global)
	t.Setenv("SENIOR_DEV_CONFIG", "")
	t.Setenv("SENIOR_DEV_CONFIG_CONTENT", "")
	t.Setenv("SENIOR_DEV_PERMISSION", "")
	shell := filepath.Join(global, "configured-shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nprintf 'custom-config-dir-shell\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	shellJSON, _ := json.Marshal(shell)
	if err := os.WriteFile(filepath.Join(global, "senior-dev.json"), []byte(`{
  "shell": `+string(shellJSON)+`,
  "tools": {"read": false}
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "EXTRA.md"), []byte("PROJECT CONFIG INSTRUCTION"), 0o644); err != nil {
		t.Fatal(err)
	}
	configText := `{
  "instructions": ["EXTRA.md"],
  "permission": {"edit": "deny", "read": "allow"},
  "agent": {
    "coder": {
      "prompt": "configured coder prompt",
      "model": "openrouter/vendor/configured-model",
      "temperature": 0.25,
      "tools": {"bash": false},
      "options": {"agent_option": true}
    }
  },
  "provider": {
    "openrouter": {
      "options": {
        "apiKey": "configured-key",
        "baseURL": "https://router.example/api/v1",
        "timeout": false,
        "chunkTimeout": 45000,
        "headers": {"X-Config": "provider", "X-Provider": "yes"}
      },
      "models": {
        "vendor/configured-model": {
          "limit": {"context": 64000, "output": 4096},
          "headers": {"X-Config": "model"},
          "options": {"model_option": "configured"}
        }
      }
    }
  }
}`
	if err := os.WriteFile(filepath.Join(workspace, "senior-dev.json"), []byte(configText), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadSeniorDevConfig(workspace)
	if err != nil {
		t.Fatal(err)
	}
	runtime := newConfiguredRuntime(workspace, &capturingBackend{}, cfg)
	defer runtime.Close()

	// Project instructions reach the live system prompt service.
	instructions := strings.Join(runtime.registry.SystemInstructions(context.Background()), "\n")
	if !strings.Contains(instructions, "PROJECT CONFIG INSTRUCTION") {
		t.Fatalf("configured instructions missing from live registry: %q", instructions)
	}
	// The registry consumes the exact loader configured by SENIOR_DEV_CONFIG_DIR,
	// including shell/formatter settings.
	bashInput, _ := json.Marshal(map[string]any{"command": "ignored"})
	bashResult, err := runtime.registry.Execute(context.Background(), steploop.ToolCall{
		Name: "bash", Input: bashInput, SessionID: "ses_config",
	})
	if err != nil || !strings.Contains(bashResult.Output, "custom-config-dir-shell") {
		t.Fatalf("custom config shell result = %#v, %v", bashResult, err)
	}
	// The tools block is normalized after config merging, so the explicit
	// project permission remains authoritative.
	if rule := permission.Evaluate("read", "anything", cfg.global); rule.Action != permission.ActionAllow {
		t.Fatalf("merged tools/permission rule = %+v, want allow", rule)
	}

	// A project deny policy blocks the live tool executor with a permission.DeniedError.
	input, _ := json.Marshal(map[string]any{
		"filePath": filepath.Join(workspace, "blocked.txt"), "content": "blocked",
	})
	_, err = runtime.registry.Execute(context.Background(), steploop.ToolCall{
		Name: "write", Input: input, Agent: "coder",
	})
	var denied permission.DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("configured write error = %T %v", err, err)
	}

	configured, err := cfg.configureTurn(turn{
		Agent: "coder", AgentMarkdown: "baked", ProviderID: "openrouter", ModelID: "old",
	})
	if err != nil {
		t.Fatal(err)
	}
	if configured.AgentMarkdown != "configured coder prompt" ||
		configured.ProviderID != "openrouter" || configured.ModelID != "vendor/configured-model" {
		t.Fatalf("configured turn = %+v", configured)
	}
	definitions := runtime.definitionsFor(configured.ProviderID, configured.ModelID, "coder", nil)
	for _, definition := range definitions {
		if definition.Provider.Name == "bash" || definition.Provider.Name == "apply_patch" ||
			definition.Provider.Name == "edit" || definition.Provider.Name == "write" {
			t.Fatalf("denied tool %q remained advertised", definition.Provider.Name)
		}
	}

	backend := &openRouterBackend{}
	cfg.applyBackend(backend)
	model, err := (seniorDevModels{
		backend: backend, sessionID: "ses", agent: "coder",
	}).GetModel(context.Background(), "openrouter", "vendor/configured-model")
	if err != nil {
		t.Fatal(err)
	}
	options, _ := model.Params.OpenRouterOptions.MarshalJSON()
	if backend.apiKey != "configured-key" || backend.baseURL() != "https://router.example/api/v1" ||
		backend.totalTimeoutMS != -1 || backend.chunkTimeoutMS != 45000 ||
		!strings.Contains(string(options), `"model_option":"configured"`) ||
		!strings.Contains(string(options), `"agent_option":true`) ||
		model.Params.MaxOutputTokens == nil || *model.Params.MaxOutputTokens != 4096 {
		t.Fatalf("provider/model config not consumed: backend=%+v options=%s model=%+v", backend, options, model)
	}
	headers := seniorDevOpenRouterHeadersWithConfig(
		backend.apiKey, "ses", cfg.headers("openrouter", "vendor/configured-model"),
	)
	headerText, _ := json.Marshal(headers)
	if !strings.Contains(string(headerText), `"name":"x-config","value":"model"`) ||
		!strings.Contains(string(headerText), `"name":"x-provider","value":"yes"`) {
		t.Fatalf("configured headers not consumed: %s", headerText)
	}
}

func TestSeniorDevPermissionEnvironmentPreservesLastMatchOrder(t *testing.T) {
	// SENIOR_DEV_PERMISSION object order survives config loading because
	// last-match-wins evaluation is observable behavior.
	workspace := t.TempDir()
	t.Setenv("SENIOR_DEV_CONFIG_DIR", t.TempDir())
	t.Setenv("SENIOR_DEV_CONFIG", "")
	t.Setenv("SENIOR_DEV_CONFIG_CONTENT", "")
	t.Setenv("SENIOR_DEV_PERMISSION", `{"read":"allow","*":"deny"}`)
	cfg, err := loadSeniorDevConfig(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if rule := permission.Evaluate("read", "README.md", cfg.global); rule.Action != permission.ActionDeny {
		t.Fatalf("ordered env permission = %+v, want trailing wildcard deny", rule)
	}
}

func TestSeniorDevConfigDirFeedsRegistryFormatterContract(t *testing.T) {
	// Formatter lookup shares the pipeline's SENIOR_DEV_CONFIG_DIR-aware loader
	// instead of constructing a default loader.
	workspace := t.TempDir()
	global := t.TempDir()
	t.Setenv("SENIOR_DEV_CONFIG_DIR", global)
	t.Setenv("SENIOR_DEV_CONFIG", "")
	t.Setenv("SENIOR_DEV_CONFIG_CONTENT", "")
	t.Setenv("SENIOR_DEV_PERMISSION", "")
	formatter := filepath.Join(global, "formatter")
	if err := os.WriteFile(formatter, []byte("#!/bin/sh\nprintf 'formatted-by-custom-dir\\n' > \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	formatterJSON, _ := json.Marshal(formatter)
	configText := `{"formatter":{"custom":{"extensions":[".fmtx"],"command":[` +
		string(formatterJSON) + `,"$FILE"]}}}`
	if err := os.WriteFile(filepath.Join(global, "senior-dev.json"), []byte(configText), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadSeniorDevConfig(workspace)
	if err != nil {
		t.Fatal(err)
	}
	runtime := newConfiguredRuntime(workspace, &capturingBackend{}, cfg)
	defer runtime.Close()
	target := filepath.Join(workspace, "sample.fmtx")
	input, _ := json.Marshal(map[string]any{"filePath": target, "content": "unformatted\n"})
	if _, err := runtime.registry.Execute(context.Background(), steploop.ToolCall{
		Name: "write", Input: input, SessionID: "ses_formatter",
	}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(target)
	if err != nil || string(body) != "formatted-by-custom-dir\n" {
		t.Fatalf("custom-dir formatter output = %q, %v", body, err)
	}
}
