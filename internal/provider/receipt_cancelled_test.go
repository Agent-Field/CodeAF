package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCancelledSendWithNoTextOrUsageKeepsAnUnknownCharge(t *testing.T) {
	for _, direct := range []bool{false, true} {
		t.Run(map[bool]string{false: "routed", true: "direct"}[direct], func(t *testing.T) {
			ready := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.(http.Flusher).Flush()
				close(ready)
				<-r.Context().Done()
			}))
			defer server.Close()
			client, err := NewClient(Config{APIKey: "fixture", BaseURL: server.URL, Model: "sim/model", HTTPClient: server.Client(), Direct: direct})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			results := make(chan Reconciled, 2)
			ctx = WithReconcile(WithStreamObserver(ctx, func(StreamEvent) {}), func(r Reconciled) { results <- r })
			done := make(chan error, 1)
			go func() { _, err := client.CompleteWithMessages(ctx, userMessages("hold")); done <- err }()
			select {
			case <-ready:
			case <-time.After(5 * time.Second):
				t.Fatal("request did not arrive")
			}
			cancel()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("cancelled call succeeded")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("cancelled call did not end")
			}
			select {
			case result := <-results:
				if result.Found || result.Cost != 0 || result.Model != "sim/model" {
					t.Fatalf("invented a price or lost the model: %+v", result)
				}
			case <-time.After(100 * time.Millisecond):
				t.Fatal("cancelled send disappeared instead of reporting unknown cost")
			}
			select {
			case <-results:
				t.Fatal("one send settled twice")
			default:
			}
		})
	}
}
