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
	msg := func(role, text string) chatMessage {
		raw, _ := json.Marshal(text)
		return chatMessage{Role: role, Content: raw}
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

func TestThePictureOnTheNewestMessageGoesToClaudeCode(t *testing.T) {
	var chat chatRequest
	_ = json.Unmarshal([]byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"what is this"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}},{"type":"image_url","image_url":{"url":"https://example.com/x.png"}}]}]}`), &chat)
	images := newestImages(chat)
	if len(images) != 1 || images[0].MediaType != "image/png" || images[0].Data != "AAAA" {
		t.Fatalf("images = %+v", images)
	}
}

func TestCodeafToolsClaudeCodeHasItsOwnOfAreNotServedTwice(t *testing.T) {
	belt := beltOf(json.RawMessage(`[{"type":"function","function":{"name":"bash","parameters":{"type":"object"}}},{"type":"function","function":{"name":"propose_task","description":"d","parameters":{"type":"object"}}},{"type":"function","function":{"name":"manual"}}]`))
	if len(belt) != 2 || belt[0].Name != "propose_task" || belt[1].Name != "manual" || string(belt[1].InputSchema) == "" {
		t.Fatalf("belt = %+v", belt)
	}
}

func TestClaudeCodesOwnStepsAreSpelledAsCodeafsCalls(t *testing.T) {
	edit, ok := stepFor("t1", startedStep{name: "Edit", input: json.RawMessage(`{"file_path":"a.go","old_string":"x","new_string":"y"}`)}, "ok", false)
	if !ok || edit.ID != "claude_code_t1" || edit.Name != "edit" || string(edit.Arguments) != `{"edits":[{"newText":"y","oldText":"x"}],"path":"a.go"}` {
		t.Fatalf("edit = %+v %s", edit, edit.Arguments)
	}
	bash, ok := stepFor("t2", startedStep{name: "Bash", input: json.RawMessage(`{"command":"go test ./...","description":"run tests"}`)}, "FAIL", true)
	if !ok || bash.Name != "bash" || string(bash.Arguments) != `{"command":"go test ./..."}` || !bash.IsError {
		t.Fatalf("bash = %+v %s", bash, bash.Arguments)
	}
	if _, ok := stepFor("t3", startedStep{name: "TodoWrite", input: json.RawMessage(`{}`)}, "", false); ok {
		t.Fatal("a step codeaf has no tool for was spelled as one")
	}
	long, _ := stepFor("t4", startedStep{name: "Read", input: json.RawMessage(`{"file_path":"big"}`)}, strings.Repeat("x", 40_000), false)
	if len(long.Output) > stepOutputBudget+100 || !strings.Contains(long.Output, "bytes not kept") {
		t.Fatalf("a long output was kept at %d bytes", len(long.Output))
	}
}

func TestClaudeCodeIsNotToldItsOwnStepsAsSomethingItMissed(t *testing.T) {
	text := func(s string) json.RawMessage { raw, _ := json.Marshal(s); return raw }
	step := chatMessage{Role: "assistant", Content: text("")}
	step.ToolCalls = append(step.ToolCalls, struct {
		ID string `json:"id"`
	}{ID: "claude_code_t1"})
	chat := chatRequest{Messages: []chatMessage{
		{Role: "system", Content: text("be codeaf")},
		{Role: "user", Content: text("make notes")},
		step,
		{Role: "tool", ToolCallID: "claude_code_t1", Content: text("SECRET-STEP-OUTPUT")},
		{Role: "assistant", Content: text("done")},
		{Role: "user", Content: text("what changed?")},
	}}
	// The session saw the system prompt, the request and its own answer.
	prompt := turnPrompt(chat, 3)
	if prompt != "what changed?" {
		t.Fatalf("Claude Code was told %q", prompt)
	}
}
