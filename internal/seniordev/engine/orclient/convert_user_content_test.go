//go:build !windows

package orclient

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
)

// A user message whose content is anything but a string or a []any of content
// parts must be REFUSED at the wire, not sent as `"content": []`.
func TestConvertUserMessageRefusesUnconvertibleContent(t *testing.T) {
	cases := map[string]any{
		"typed text slice": []msgmodel.TextContent{{Type: "text", Text: "hello"}},
		"empty part list":  []any{},
		"nil":              nil,
		"empty string":     "",
		"number":           42,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ConvertToOpenRouterChatMessages([]msgmodel.ModelMessage{
				{Role: "user", Content: content},
			})
			if err == nil {
				t.Fatalf("content %#v was converted instead of refused", content)
			}
		})
	}
}

func TestConvertUserMessageAcceptsStringAndCanonicalParts(t *testing.T) {
	const text = "<conversation>\nHELLO TRANSCRIPT\n</conversation>"
	for name, msg := range map[string]msgmodel.ModelMessage{
		"bare string": {Role: "user", Content: text},
		"UserText":    msgmodel.UserText(text),
	} {
		t.Run(name, func(t *testing.T) {
			body, err := BuildRequestBody(RequestParams{ModelID: "m", Prompt: []msgmodel.ModelMessage{msg}})
			if err != nil {
				t.Fatal(err)
			}
			want := `{"role":"user","content":"<conversation>\nHELLO TRANSCRIPT\n</conversation>"}`
			if !strings.Contains(string(body), want) {
				t.Fatalf("wire body = %s\nwant message %s", body, want)
			}
		})
	}
}

// An assistant message with bare string content (the max-steps prompt is
// built that way) must reach the wire as text, not as an empty message.
func TestConvertAssistantMessageAcceptsStringContent(t *testing.T) {
	body, err := BuildRequestBody(RequestParams{ModelID: "m", Prompt: []msgmodel.ModelMessage{
		{Role: "assistant", Content: "You have reached the step limit."},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"content":"You have reached the step limit."`) {
		t.Fatalf("assistant text lost on the wire: %s", body)
	}
	if _, err := ConvertToOpenRouterChatMessages([]msgmodel.ModelMessage{
		{Role: "assistant", Content: []msgmodel.TextContent{{Type: "text", Text: "x"}}},
	}); err == nil {
		t.Fatal("typed assistant content was converted instead of refused")
	}
}

// The whole request body, the way DoStream builds it: the transcript handed
// to the summarizer must be present verbatim in the bytes that leave.
func TestBuildRequestBodyCarriesTheSummaryTranscript(t *testing.T) {
	const transcript = "[User]: Fix src/a.go\n[Assistant]: Reading the file first."
	maximum := float64(1024)
	body, err := BuildRequestBody(RequestParams{
		ModelID:         "vendor/model",
		MaxOutputTokens: &maximum,
		Prompt: []msgmodel.ModelMessage{
			{Role: "system", Content: "You are a context serializer."},
			msgmodel.UserText("<conversation>\n" + transcript + "\n</conversation>\n\n## Working State"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("body is not the expected shape: %v\n%s", err, body)
	}
	if len(decoded.Messages) != 2 || decoded.Messages[1].Role != "user" {
		t.Fatalf("wire messages = %#v", decoded.Messages)
	}
	var content string
	if err := json.Unmarshal(decoded.Messages[1].Content, &content); err != nil {
		t.Fatalf("user content is not a bare string: %s", decoded.Messages[1].Content)
	}
	if !strings.Contains(content, transcript) {
		t.Fatalf("summary request lost its transcript on the wire:\n%s", body)
	}
}
