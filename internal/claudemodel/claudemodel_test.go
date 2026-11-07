package claudemodel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/claudeagent"
)

func fake(reply string, seen *claudeagent.Ask) *http.Client {
	return &http.Client{Transport: transport{ask: func(_ context.Context, ask claudeagent.Ask) (claudeagent.Answer, error) {
		*seen = ask
		return claudeagent.Answer{Text: reply, InputTokens: 12, OutputTokens: 3}, nil
	}}}
}

func TestATitleCallIsAnsweredByClaudeCodeAsChatCompletions(t *testing.T) {
	var seen claudeagent.Ask
	body := `{"model":"claude-code/haiku","messages":[{"role":"system","content":"You write titles."},{"role":"user","content":[{"type":"text","text":"fix the flaky test"}]}]}`
	resp, err := fake("Fix flaky test", &seen).Post(Address+"/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Choices[0].Message.Content != "Fix flaky test" || out.Usage.PromptTokens != 12 {
		t.Fatalf("answer = %+v", out)
	}
	if seen.Model != "haiku" || seen.System != "You write titles." || seen.Prompt != "fix the flaky test" {
		t.Fatalf("asked = %+v", seen)
	}
}

func TestAStreamedCallEndsLikeAnyStream(t *testing.T) {
	var seen claudeagent.Ask
	body := `{"model":"sonnet","stream":true,"messages":[{"role":"user","content":"a"},{"role":"assistant","content":"b"},{"role":"user","content":"c"}]}`
	resp, _ := fake("d", &seen).Post(Address+"/chat/completions", "application/json", strings.NewReader(body))
	raw, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(raw), `"content":"d"`) || !strings.HasSuffix(string(raw), "data: [DONE]\n\n") {
		t.Fatalf("stream = %s", raw)
	}
	if !strings.Contains(seen.Prompt, "User: a\n\nAssistant: b\n\nUser: c") {
		t.Fatalf("prompt = %q", seen.Prompt)
	}
}

func TestEveryRowTakesTools(t *testing.T) {
	// A row without tools would have the provider strip the belt, and the turn
	// would be sent as a one-shot call that cannot work in the folder.
	for _, m := range Models() {
		found := false
		for _, p := range m.Parameters {
			found = found || p == "tools"
		}
		if !found {
			t.Fatalf("%s does not list tools", m.ID)
		}
	}
}

func TestACatalogSpellingOfClaudeIsReadAsItsFamily(t *testing.T) {
	for id, want := range map[string]string{"anthropic/claude-opus-4.6": "opus", "claude-code/sonnet": "sonnet", "haiku": "haiku"} {
		if got := model(id); got != want {
			t.Errorf("model(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestATurnBackOnClaudeCodeCarriesWhatAnotherModelSaid(t *testing.T) {
	msg := func(role, text string) struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} {
		raw, _ := json.Marshal(text)
		return struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}{role, raw}
	}
	chat := chatRequest{}
	chat.Messages = append(chat.Messages, msg("system", "rules"), msg("user", "one"), msg("assistant", "claude answered"), msg("user", "two"), msg("assistant", "another model answered"), msg("user", "three"))
	if got := turnPrompt(chat, 3); !strings.Contains(got, "User: two") || !strings.Contains(got, "Assistant: another model answered") || strings.Contains(got, "claude answered") || !strings.HasSuffix(got, "three") {
		t.Fatalf("back on Claude Code: %q", got)
	}
	if got := turnPrompt(chat, len(chat.Messages)); got != "three" {
		t.Fatalf("an adopted turn is given only the correction: %q", got)
	}
	if got := turnPrompt(chat, 0); strings.Contains(got, "rules") || !strings.Contains(got, "User: one") {
		t.Fatalf("a fresh session: %q", got)
	}
}
