package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAStreamNeedsACompletionSignalOrFinishReason(t *testing.T) {
	const contentFrame = `data: {"id":"one","provider":"test-node","choices":[{"index":0,"delta":{"content":"partial answer"}}]}` + "\n\n"
	tests := []struct {
		name             string
		body             string
		wantCut          bool
		wantFinishReason string
	}{
		{
			name:    "connection ends without either signal",
			body:    contentFrame,
			wantCut: true,
		},
		{
			name:             "finish reason without done marker",
			body:             contentFrame + `data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n",
			wantFinishReason: "stop",
		},
		{
			name: "done marker without finish reason",
			body: contentFrame + "data: [DONE]\n\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()

			client, err := NewClient(Config{APIKey: "k", BaseURL: server.URL, Model: "sim/model"})
			if err != nil {
				t.Fatal(err)
			}
			var events []StreamEvent
			ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
				events = append(events, event)
			})
			response, err := client.CompleteWithMessages(ctx, userMessages("hello"))

			if test.wantCut {
				cut, ok := CutFrom(err)
				if !ok || cut.Reason != CutTruncated {
					t.Fatalf("err = %v, want a truncated stream cut", err)
				}
				if response != nil {
					t.Fatalf("truncated stream returned a response: %+v", response)
				}
				var delta, failed, finished bool
				for _, event := range events {
					delta = delta || event.Kind == StreamDelta && event.Delta == "partial answer"
					failed = failed || event.Kind == StreamFailed
					finished = finished || event.Kind == StreamFinished
				}
				if !delta || !failed || finished {
					t.Fatalf("truncated stream events = %+v, want visible partial text and failure without completion", events)
				}
				return
			}

			if err != nil {
				t.Fatalf("complete stream: %v", err)
			}
			if response == nil || len(response.Choices) != 1 {
				t.Fatalf("response = %+v, want one completed choice", response)
			}
			if response.Choices[0].FinishReason != test.wantFinishReason {
				t.Errorf("finish reason = %q, want %q", response.Choices[0].FinishReason, test.wantFinishReason)
			}
			if len(response.Choices[0].Message.Content) != 1 || response.Choices[0].Message.Content[0].Text != "partial answer" {
				t.Errorf("response content = %+v, want the streamed answer", response.Choices[0].Message.Content)
			}
		})
	}
}
