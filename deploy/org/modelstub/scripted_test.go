package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func msg(role, text string) message {
	raw, _ := json.Marshal(text)
	return message{Role: role, Content: raw}
}

// toolCallsOf reads the streamed answer back the way a client does: the name and
// the decoded arguments of every tool call, and the last finish reason.
func toolCallsOf(t *testing.T, body string) (calls []map[string]string, names []string, finish string) {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		payload := strings.TrimPrefix(line, "data: ")
		if payload == line || payload == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					ToolCalls []struct {
						Function struct{ Name, Arguments string }
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			t.Fatalf("a chunk is not JSON: %v: %s", err, payload)
		}
		for _, c := range chunk.Choices {
			for _, call := range c.Delta.ToolCalls {
				var args map[string]string
				if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
					t.Fatalf("arguments are not JSON: %v", err)
				}
				calls, names = append(calls, args), append(names, call.Function.Name)
			}
			if c.FinishReason != nil {
				finish = *c.FinishReason
			}
		}
	}
	return calls, names, finish
}

// A `bash:` message is one bash tool call that carries the command.
func TestScriptedBashMakesOneToolCall(t *testing.T) {
	w := httptest.NewRecorder()
	if !scriptedBash(w, []message{msg("system", "be brief"), msg("user", "bash: echo hi > f && git status --short")}, 7) {
		t.Fatal("a bash: message was not answered")
	}
	calls, names, finish := toolCallsOf(t, w.Body.String())
	if len(calls) != 1 || names[0] != "bash" || calls[0]["command"] != "echo hi > f && git status --short" {
		t.Fatalf("calls = %v %v", names, calls)
	}
	if finish != "tool_calls" {
		t.Fatalf("the turn ends on %q, not on the tool call", finish)
	}
}

// The tool's answer to a scripted command is quoted, so it shows in the transcript.
func TestScriptedBashQuotesTheToolOutput(t *testing.T) {
	w := httptest.NewRecorder()
	messages := []message{msg("user", "bash: echo hi"), msg("assistant", ""), msg("tool", "hi\n?? f")}
	if !scriptedBash(w, messages, 8) {
		t.Fatal("the tool answer was not quoted")
	}
	if body := w.Body.String(); !strings.Contains(body, "STUB ran: hi") || !strings.Contains(body, "?? f") {
		t.Fatalf("the output is not quoted: %s", body)
	}
}

// Everything else is left to the existing behaviour.
func TestScriptedBashLeavesOtherTurnsAlone(t *testing.T) {
	for _, messages := range [][]message{
		nil,
		{msg("user", "hello there")},
		{msg("user", "Required JSON Schema: {}"), msg("tool", "ok")},
	} {
		if scriptedBash(httptest.NewRecorder(), messages, 1) {
			t.Fatalf("answered %+v", messages)
		}
	}
}
