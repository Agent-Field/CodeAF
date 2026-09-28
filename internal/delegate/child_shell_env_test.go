//go:build !windows

package delegate_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/tool"
)

func TestChildShellDropsHostTmuxAndModelKeys(t *testing.T) {
	if os.Getenv("FC_CHILD_BASH") == "1" {
		workspace := os.Getenv("FC_WORKSPACE")
		input, _ := json.Marshal(map[string]any{"command": "env"})
		result, err := tool.New(workspace).Execute(context.Background(), steploop.ToolCall{
			ID: "environment", Name: "bash", Input: input,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv("FC_RESULT"), []byte(result.Output), 0o600); err != nil {
			t.Fatal(err)
		}
		os.Stdout.WriteString("{\"type\":\"hello\",\"protocol\":2,\"delegate\":\"fake\"}\n")
		os.Stdout.WriteString("{\"type\":\"terminal\",\"status\":\"pass\"}\n")
		return
	}
	profile := t.TempDir()
	if err := config.WriteSources(profile, []config.PersistedSource{{ID: "custom", Written: "custom", KeyEnv: "MY_SERVICE_SECRET"}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("MY_SERVICE_SECRET", "custom-secret")
	t.Setenv("TMUX", "/tmp/host-tmux/default,1,0")
	t.Setenv("TMUX_PANE", "%7")
	t.Setenv("OPENROUTER_API_KEY", "router-secret")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-secret")
	t.Setenv("AFORGE_API_KEY", "legacy-secret") // legacy-name
	t.Setenv("GOPATH", filepath.Join(t.TempDir(), "go"))
	t.Setenv("FC_CHILD_BASH", "1")
	workspace := t.TempDir()
	resultPath := filepath.Join(t.TempDir(), "environment")
	t.Setenv("FC_WORKSPACE", workspace)
	t.Setenv("FC_RESULT", resultPath)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	_, err = delegate.Run(context.Background(), delegate.Launch{
		Name: "fake", Bin: self, Args: []string{"-test.run=^TestChildShellDropsHostTmuxAndModelKeys$"},
		Env: delegate.ChildEnv(delegate.ModelAPI{BaseURL: "http://127.0.0.1:9/v1", Token: "loopback-secret"}),
		Dir: workspace,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, entry := range strings.Split(string(raw), "\n") {
		name, value, ok := strings.Cut(entry, "=")
		if ok {
			got[name] = value
		}
	}
	for _, name := range []string{"TMUX", "TMUX_PANE", "OPENROUTER_API_KEY", "ANTHROPIC_API_KEY", "AFORGE_API_KEY", "MY_SERVICE_SECRET", delegate.EnvModelToken} { // legacy-name
		if value, ok := got[name]; ok {
			t.Errorf("%s reached model bash: %q", name, value)
		}
	}
	for _, name := range []string{"PATH", "HOME", "GOPATH"} {
		if got[name] == "" {
			t.Errorf("ordinary variable %s missing", name)
		}
	}
	if got["TMUX_TMPDIR"] == "" {
		t.Error("model bash did not get a private tmux socket directory")
	}
}
