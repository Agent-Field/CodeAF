package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The SDK constructs its own defaults before applying options. Exercise that
// real request builder so clearing its defaults cannot erase a caller's choice.
func TestSDKLoopOmitsDefaultsAndPreservesExplicitGenerationOptions(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		name := "provider defaults"
		if explicit {
			name = "explicit zero temperature and output limit"
		}
		t.Run(name, func(t *testing.T) {
			recorded := &capture{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				recorded.record(r)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"model":"sim/model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`))
			}))
			defer server.Close()
			client, err := NewClient(Config{APIKey: "stub-key", BaseURL: server.URL, Model: "sim/model"})
			if err != nil {
				t.Fatal(err)
			}
			if client.shippedRouterHint() {
				t.Fatal("fixture must exercise the SDK request builder")
			}
			var options []ai.Option
			if explicit {
				options = []ai.Option{ai.WithTemperature(0), ai.WithMaxTokens(256)}
			}
			response, _, err := client.ExecuteToolCallLoop(context.Background(), userMessages("hello"), nil,
				ai.ToolCallConfig{MaxTurns: 2, MaxToolCalls: 2},
				func(context.Context, string, map[string]interface{}) (map[string]interface{}, error) {
					t.Error("the fixture returned no tool call")
					return nil, nil
				}, options...)
			if err != nil {
				t.Fatal(err)
			}
			if response.Text() != "ok" {
				t.Fatalf("response = %q", response.Text())
			}
			body := recorded.body(0)
			if explicit {
				if body["temperature"] != float64(0) || body["max_tokens"] != float64(256) {
					t.Fatalf("explicit generation choices changed: %#v", body)
				}
			} else {
				for _, key := range []string{"temperature", "max_tokens", "max_completion_tokens", "reasoning", "top_p"} {
					if _, present := body[key]; present {
						t.Errorf("SDK injected %s without a caller choice", key)
					}
				}
			}
		})
	}
}
