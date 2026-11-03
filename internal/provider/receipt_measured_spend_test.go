package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The public completion and reconciliation doors must retain paid partial work.
func TestTokenOnlyReceiptKeepsMeasuredSpend(t *testing.T) {
	for _, tc := range []struct {
		name                                                  string
		usage, noID, noReceipt, direct, final, tokenOnly, raw bool
	}{
		{name: "token-only-receipt", usage: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read := loggingTo(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					if tc.noReceipt {
						w.WriteHeader(404)
						return
					}
					fmt.Fprint(w, `{"data":{"tokens_prompt":11,"tokens_completion":3}}`)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				u := ""
				if tc.usage {
					u = `,"usage":{"prompt_tokens":11,"completion_tokens":3,"cost":0.07}`
				}
				if tc.tokenOnly {
					u = `,"usage":{"prompt_tokens":11,"completion_tokens":3}`
				}
				id := "cancel-generation"
				if tc.noID {
					id = ""
				}
				finish := ""
				if tc.raw {
					finish = `,"finish_reason":"error"`
				}
				fmt.Fprintf(w, "data: {\"id\":%q,\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}%s}]%s}\n\n", id, finish, u)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer server.Close()
			client := receiptTestClient(t, server)
			client.config.Direct = tc.direct
			results := make(chan Reconciled, 3)
			if tc.final {
				ctx = WithFinalReceipts(ctx)
			}
			ctx = WithCallTag(WithReconcile(ctx, func(r Reconciled) { results <- r }), "caption")
			ctx = WithStreamObserver(ctx, func(e StreamEvent) {
				if e.Kind == StreamDelta {
					cancel()
				}
			})
			var err error
			if tc.raw {
				chunks, errs := client.StreamComplete(ctx, "hello")
				for range chunks {
					cancel()
				}
				for failure := range errs {
					if failure != nil {
						err = failure
					}
				}
			} else {
				_, err = client.CompleteWithMessages(ctx, []ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "hello"}}}})
			}
			if err == nil {
				t.Fatal("cancelled completion succeeded")
			}
			select {
			case r := <-results:
				wantFound := tc.usage || !tc.noReceipt
				wantCost := 0.07
				if !wantFound || tc.tokenOnly {
					wantCost = 0
				}
				if r.Found != wantFound || r.Cost != wantCost {
					t.Errorf("measured spend lost: got $%.2f, want $%.2f; settlement=%+v", r.Cost, wantCost, r)
				}
				data, _ := json.Marshal(r)
				var settlement map[string]any
				json.Unmarshal(data, &settlement)
				if tc.tokenOnly && settlement["PriceMissing"] != true {
					t.Errorf("token-only charge must stay unknown: %+v", r)
				}
				if tc.usage && (tc.noReceipt || tc.noID || tc.direct) && settlement["Interim"] != true {
					t.Errorf("interim usage not identified: %+v", r)
				}
			case <-time.After(time.Second):
				t.Error("paid cancellation has no settlement")
			}
			for _, row := range ended(read()) {
				data, _ := json.Marshal(row)
				var fields map[string]any
				json.Unmarshal(data, &fields)
				if !tc.noID && fields["generation_id"] != "cancel-generation" {
					t.Errorf("helper generation id missing: %s", data)
				}
			}
			select {
			case extra := <-results:
				t.Errorf("double booking: %+v", extra)
			default:
			}
		})
	}
}
