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

func TestAffordable402RetriesOnceAndRingsForEveryRefusal(t *testing.T) {
	for _, tc := range []struct {
		name     string
		refusals int
		sentence string
		want     int
	}{
		{"reply lands", 1, "You can only afford 641 tokens. Add credits.", 2},
		{"second refusal ends", 2, "You can only afford 641 tokens. Add credits.", 2},
		{"no figure does not retry", 1, "Add credits.", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ceilings []int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					MaxTokens int `json:"max_tokens"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode request: %v", err)
				}
				ceilings = append(ceilings, body.MaxTokens)
				w.Header().Set("Content-Type", "application/json")
				if len(ceilings) <= tc.refusals {
					w.WriteHeader(http.StatusPaymentRequired)
					fmt.Fprintf(w, `{"error":{"message":%q,"code":402}}`, tc.sentence)
					return
				}
				fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"landed"},"finish_reason":"stop"}]}`)
			}))
			defer server.Close()
			rings := make(chan string, 4)
			stop := SetPaymentRequiredHook(func(base string) { rings <- base })
			defer stop()
			client, err := NewClient(Config{APIKey: "key", BaseURL: server.URL, Model: "sim/model", Direct: true, HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.CompleteWithMessages(context.Background(), userMessages("hello"), ai.WithMaxTokens(1000))
			if len(ceilings) != tc.want {
				t.Fatalf("requests = %d, want %d", len(ceilings), tc.want)
			}
			if tc.want == 2 && ceilings[1] != 641 {
				t.Fatalf("retry max_tokens = %d, want 641", ceilings[1])
			}
			if tc.refusals == 1 && tc.want == 2 && (err != nil || response == nil) {
				t.Fatalf("reply did not land: %v, %v", response, err)
			}
			if tc.refusals == 2 && err == nil {
				t.Fatal("second 402 became success")
			}
			for i := 0; i < tc.refusals; i++ {
				select {
				case base := <-rings:
					if base != server.URL {
						t.Errorf("hook base = %q", base)
					}
				case <-time.After(time.Second):
					t.Fatal("402 hook did not fire")
				}
			}
		})
	}
}
