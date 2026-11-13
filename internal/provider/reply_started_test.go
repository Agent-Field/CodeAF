package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReplyStartedWaitsForTextThoughtOrToolToken(t *testing.T) {
	for name, delta := range map[string]string{
		"text":    `{"content":"<think>"}`,
		"thought": `{"reasoning":"thinking"}`,
		"tool":    `{"tool_calls":[{"index":0,"id":"call","type":"function","function":{"name":"read","arguments":"{}"}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			headers, token, finish := make(chan struct{}), make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n")
				w.(http.Flusher).Flush()
				close(headers)
				select {
				case <-token:
				case <-r.Context().Done():
					return
				}
				fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":%s}]}\n\n", delta)
				w.(http.Flusher).Flush()
				select {
				case <-finish:
				case <-r.Context().Done():
					return
				}
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}],\"usage\":{\"cost\":0}}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			client, err := NewClient(Config{APIKey: "fixture", BaseURL: server.URL, Model: "test/model", HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			started := make(chan struct{}, 2)
			ctx = WithReplyStarted(WithStreamObserver(ctx, func(StreamEvent) {}), func() { started <- struct{}{} })
			done := make(chan error, 1)
			go func() { _, err := client.CompleteWithMessages(ctx, userMessages("work")); done <- err }()
			select {
			case <-headers:
			case <-ctx.Done():
				t.Fatal("no headers")
			}
			select {
			case <-started:
				t.Fatal("headers counted as a reply token")
			default:
			}
			close(token)
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("reply token was hidden by display buffering")
			}
			close(finish)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
				t.Fatal("first-token callback repeated")
			default:
			}
		})
	}
}
