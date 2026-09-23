package codexauth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestE4TheTransportPutsNoServerNameOnACodexAnswer(t *testing.T) {
	// E4 (#1391): a Codex call row names `Codex` because the SERVICE declares it
	// (modelsource's ServedAs), never because an answer carries it. A `provider`
	// field on an answer is a lane internal/provider rates, strikes and clears a
	// router account for, and a machine the live screen draws beside the model;
	// none of that is true of a service that is its own one machine. So no chunk
	// of a mapped stream — opening, delta, terminal, length-cut — no whole
	// completion and no mapped failure may carry one.
	for _, testCase := range []struct {
		name     string
		stream   bool
		terminal string
	}{
		{name: "stream completed", stream: true, terminal: `{"type":"response.completed","response":{"id":"r-1","model":"gpt-5.5","usage":{"input_tokens":4,"output_tokens":2}}}`},
		{name: "stream cut at length", stream: true, terminal: `{"type":"response.incomplete","response":{"id":"r-1","model":"gpt-5.5","incomplete_details":{"reason":"max_output_tokens"}}}`},
		{name: "whole completion", stream: false, terminal: `{"type":"response.completed","response":{"id":"r-1","model":"gpt-5.5","usage":{"input_tokens":4,"output_tokens":2}}}`},
		{name: "failure", stream: true, terminal: `{"type":"response.failed","response":{"error":{"message":"the backend fell over"}}}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			now := time.Now()
			dir := t.TempDir()
			if err := Save(dir, validTokens(now)); err != nil {
				t.Fatal(err)
			}
			backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "text/event-stream")
				for _, event := range []string{
					`{"type":"response.created","response":{"id":"r-1","model":"gpt-5.5","created_at":1800000000}}`,
					`{"type":"response.reasoning_summary_text.delta","delta":"thinking"}`,
					`{"type":"response.output_text.delta","delta":"answer"}`,
					`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call-1","name":"read"}}`,
					`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{}"}`,
					testCase.terminal,
				} {
					fmt.Fprintf(writer, "data: %s\n\n", event)
				}
			}))
			defer backend.Close()
			client := ClientWithOptions(dir, Options{Backend: backend.URL, HTTPClient: backend.Client(), Now: func() time.Time { return now }})
			_, body := doTurnResponse(t, client, backend.URL+"/chat/completions", map[string]any{
				"model": "gpt-5.5", "stream": testCase.stream,
				"messages": []any{map[string]any{"role": "user", "content": "hello"}},
			})
			var answers []map[string]any
			if testCase.stream {
				for _, line := range strings.Split(body, "\n") {
					data, found := strings.CutPrefix(strings.TrimSpace(line), "data:")
					if !found || strings.TrimSpace(data) == "[DONE]" {
						continue
					}
					var chunk map[string]any
					if err := json.Unmarshal([]byte(data), &chunk); err != nil {
						t.Fatalf("chunk %q: %v", data, err)
					}
					answers = append(answers, chunk)
				}
			} else {
				var whole map[string]any
				if err := json.NewDecoder(io.NopCloser(strings.NewReader(body))).Decode(&whole); err != nil {
					t.Fatalf("whole completion %q: %v", body, err)
				}
				answers = append(answers, whole)
			}
			if len(answers) == 0 {
				t.Fatalf("no answers in %q", body)
			}
			sawTerminal := false
			for _, answer := range answers {
				if named, carried := answer["provider"]; carried {
					t.Fatalf("an answer carried a server name %v, which the provider client would learn as a lane: %v", named, answer)
				}
				if _, failed := answer["error"]; failed {
					sawTerminal = true
					continue
				}
				choices, _ := answer["choices"].([]any)
				if len(choices) == 1 {
					choice, _ := choices[0].(map[string]any)
					if choice["finish_reason"] != nil {
						sawTerminal = true
					}
				}
			}
			if !sawTerminal {
				t.Fatalf("no terminal answer among %d: %q", len(answers), body)
			}
		})
	}
}
