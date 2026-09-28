//go:build !windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/delegate/builtin"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
)

// seniorDevModel is senior-dev's side of one scripted conversation, played by
// codeaf's funnel instead of a model: write the feature, write the checklist,
// submit, and say it is done — the conversation internal/seniordev's own test
// plays against a server that imitates the model API. Here the API is the
// real one, and every call through it is billed.
type seniorDevModel struct {
	mu    sync.Mutex
	calls int
	keys  []string
}

func (m *seniorDevModel) completerFor(string) modelapi.Completer { return m }

func (m *seniorDevModel) CompleteWithMessages(ctx context.Context, _ []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	m.mu.Lock()
	m.calls++
	call := m.calls
	m.keys = append(m.keys, provider.CacheKeyFrom(ctx))
	m.mu.Unlock()
	if sink := provider.BillingSinkFrom(ctx); sink != nil {
		sink(provider.Billed{Model: request.Model, PromptTokens: 300, CompletionTokens: 20, Cost: 0.002})
	}
	tool := func(name string, arguments map[string]any) (*ai.Response, error) {
		encoded, _ := json.Marshal(arguments)
		return &ai.Response{Model: request.Model, Choices: []ai.Choice{{
			Message: ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{
				// ONE ID PER CALL, as a model gives them: senior-dev reports a
				// finished call once per id, so two writes under one id read as
				// one step.
				ID: fmt.Sprintf("call-%s-%d", name, call), Type: "function", Function: ai.ToolCallFunction{Name: name, Arguments: string(encoded)},
			}}},
			FinishReason: "tool_calls",
		}}}, nil
	}
	switch call {
	case 1:
		return tool("write", map[string]any{"filePath": "feature.txt", "content": "implemented\n"})
	case 2:
		return tool("write", map[string]any{"filePath": ".senior-dev/checklist.md", "content": "- [x] the feature is implemented\n"})
	case 3:
		return tool("submit", map[string]any{
			"reason": "feature.txt now holds the feature", "evidence": "make test exits 0", "checklist_satisfied": true,
		})
	}
	return &ai.Response{Model: request.Model, Choices: []ai.Choice{{
		Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "Done."}}}, FinishReason: "stop",
	}}}, nil
}

// SENIOR-DEV ITSELF, THROUGH THE WHOLE ROAD: a person's shell run of the
// program this build carries serves it the real model API, starts it as a real
// child of this executable, and senior-dev — speaking its own OpenRouter
// dialect over a real socket, streaming — works a scripted task in a real
// repository to a passing ending. Every call is metered onto this machine's
// ledger once, the conversation is kept, and the work is in the tree.
func TestSeniorDevWorksATaskThroughTheShellHostsModelAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("drives the real senior-dev engine")
	}
	program, carried := builtin.Find("senior-dev")
	if !carried {
		t.Skip("this build carries no senior-dev")
	}
	workspace := seniorDevWorkspace(t)
	t.Setenv(carriedChildEnv, "real")
	t.Setenv("DO_NOT_TRACK", "1")
	t.Setenv("CODEAF_NO_UPDATE_CHECK", "1")
	model := &seniorDevModel{}
	previousRoad, previousOut, previousGrace := carriedModels, carriedStdout, carriedGrace
	carriedModels = func() (carriedRoad, error) { return carriedRoad{completerFor: model.completerFor}, nil }
	printed := &lockedBuffer{}
	carriedStdout = printed
	carriedGrace = 5 * time.Second
	t.Cleanup(func() { carriedModels, carriedStdout, carriedGrace = previousRoad, previousOut, previousGrace })

	err := runCarried(program, []string{"--high", "openrouter/fixture/vendor-model", "--dir", workspace, "--", "Add", "the", "feature."})
	out := printed.String()
	if code := exitCodeOf(err); code != 0 {
		stderr := ""
		if matches, _ := filepath.Glob(filepath.Join(newestSeniorDevRecord(t), carriedStderrName)); len(matches) > 0 {
			data, _ := os.ReadFile(matches[0])
			stderr = string(data)
		}
		t.Fatalf("senior-dev's shell run left with %d:\n%s\nits stderr:\n%s", code, out, stderr)
	}
	for _, want := range []string{"senior-dev · working in " + workspace, "senior-dev finished", "senior-dev's model said: feature.txt now holds the feature", " · 300 in · 20 out · $0.0020"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the shell run never printed %q:\n%s", want, out)
		}
	}
	if content, err := os.ReadFile(filepath.Join(workspace, "feature.txt")); err != nil || string(content) != "implemented\n" {
		t.Fatalf("the work is not in the tree: %q %v", content, err)
	}
	model.mu.Lock()
	calls, keys := model.calls, append([]string(nil), model.keys...)
	model.mu.Unlock()
	if calls < 4 {
		t.Fatalf("senior-dev made %d calls through the API, want the scripted four", calls)
	}
	for _, key := range keys {
		if key == "" {
			t.Fatalf("a call lost senior-dev's own prompt_cache_key: %q", keys)
		}
	}
	if rows := ledgerRowsFor(t, workspace); len(rows) != calls {
		t.Fatalf("%d ledger rows for %d calls, want exactly one each", len(rows), calls)
	}
	turns, err := delegate.ReadTurns(newestSeniorDevRecord(t), 0)
	if err != nil || len(turns) != calls {
		t.Fatalf("the kept conversation holds %d turns (%v), want one per call", len(turns), err)
	}
	var submitted bool
	for _, turn := range turns {
		for _, use := range turn.Calls {
			submitted = submitted || use.Name == "submit"
		}
	}
	if !submitted || turns[0].Thread == delegate.MainThread {
		t.Fatalf("the conversation lacks the submit or senior-dev's own thread: %+v", turns)
	}
}

// seniorDevWorkspace is the hermetic world senior-dev's own test runs in
// (internal/seniordev's hermeticRun): nothing of the machine's configuration,
// its model catalog on disk and no fetch, and a git repository whose build and
// tests pass.
func seniorDevWorkspace(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("SENIOR_DEV_CONFIG_DIR", t.TempDir())
	t.Setenv("SENIOR_DEV_CONFIG", "")
	t.Setenv("SENIOR_DEV_CONFIG_CONTENT", "")
	t.Setenv("SENIOR_DEV_PERMISSION", "")
	t.Setenv("SENIOR_DEV_NET", "allow")
	t.Setenv("SENIOR_DEV_SCRATCH_ROOT", t.TempDir())
	t.Setenv("SENIOR_DEV_DISABLE_MODELS_FETCH", "1")
	catalog, err := filepath.Abs(filepath.Join("..", "..", "internal", "seniordev", "modelsdev", "testdata", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(catalog); err != nil {
		t.Skipf("senior-dev's fixture catalog is not where its test keeps it: %v", err)
	}
	t.Setenv("SENIOR_DEV_MODELS_PATH", catalog)
	workspace := t.TempDir()
	for name, content := range map[string]string{"README.md": "base\n", "Makefile": "build:\n\t@true\n\ntest:\n\t@true\n"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"add", "README.md", "Makefile"},
		{"-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "base"},
	} {
		command := exec.Command("git", args...)
		command.Dir = workspace
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return workspace
}

// newestSeniorDevRecord is the most recent shell run's record folder for
// senior-dev.
func newestSeniorDevRecord(t *testing.T) string {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(carriedRecordRoot("senior-dev"), "*"))
	newest := ""
	for _, match := range matches {
		if match > newest {
			newest = match
		}
	}
	return newest
}
