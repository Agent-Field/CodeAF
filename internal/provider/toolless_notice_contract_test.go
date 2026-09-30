package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestNoToolsModelAnnouncesOnceWithEmptyAndFullBelt(t *testing.T) {
	for _, withTools := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "full"}[withTools], func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				if _, has := body["tools"]; has {
					t.Error("tool-less model received tools")
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
			}))
			defer server.Close()
			client, err := NewClient(Config{APIKey: "fixture", BaseURL: server.URL, Model: "review/no-tools", Direct: true,
				SupportsParameter: func(_, p string) (bool, bool) { return p != "tools", true }})
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var news []string
			ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
				if event.Kind == StreamRowNews {
					mu.Lock()
					news = append(news, event.Delta)
					mu.Unlock()
				}
			})
			var options []ai.Option
			if withTools {
				options = append(options, ai.WithTools([]ai.ToolDefinition{{Type: "function", Function: ai.ToolFunction{Name: "read"}}}))
			}
			for i := 0; i < 2; i++ {
				if _, err := client.CompleteWithMessages(ctx, userMessages("hello"), options...); err != nil {
					t.Fatal(err)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if len(news) != 1 || news[0] != toollessNotice("review/no-tools") || calls.Load() != 2 {
				t.Fatalf("news=%q calls=%d, want one line and two calls", news, calls.Load())
			}
		})
	}
}

func TestLearnedToollessModelAnnouncesWithEmptyBelt(t *testing.T) {
	var news []string
	client, _ := NewClient(Config{BaseURL: "http://fixture.test", Model: "review/learned", Direct: true})
	client.toolless.learn("review/learned")
	ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
		if event.Kind == StreamRowNews {
			news = append(news, event.Delta)
		}
	})
	for i := 0; i < 2; i++ {
		client.leaveOffTools(ctx, "review/learned", callKnobs{}, false)
	}
	if len(news) != 1 || news[0] != toollessNotice("review/learned") {
		t.Fatalf("news=%q, want one learned-model notice", news)
	}
}
