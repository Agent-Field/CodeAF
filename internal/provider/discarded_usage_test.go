package provider

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestDiscardedUsagePrecedesRetryAndPreservesItsContract(t *testing.T) {
	for _, ending := range []string{"answer", "error", "cancel"} {
		t.Run(ending, func(t *testing.T) {
			model := "discarded/" + ending
			quirksAt(t, model)
			recorded := &capture{}
			client, err := NewClient(Config{APIKey: "test-key", BaseURL: "http://provider.test", Model: model, SupportsParameter: func(string, string) (bool, bool) { return true, true }, HTTPClient: handlerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				recorded.record(r)
				w.Header().Set("Content-Type", "application/json")
				if recorded.count() == 1 {
					fmt.Fprintf(w, `{"model":%q,"choices":[{"finish_reason":"length","message":{"role":"assistant","content":""}}],"usage":{"prompt_tokens":718,"completion_tokens":320,"total_tokens":1038,"prompt_tokens_details":{"cached_tokens":100},"cache_creation_input_tokens":20,"completion_tokens_details":{"reasoning_tokens":320},"cost":0.0005994}}`, model)
					return
				}
				if ending == "error" {
					w.WriteHeader(401)
					fmt.Fprint(w, `{"error":{"message":"test unauthorized"}}`)
					return
				}
				fmt.Fprintf(w, `{"model":%q,"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"the answer"}}],"usage":{"prompt_tokens":718,"completion_tokens":789,"total_tokens":1507,"cache_read_input_tokens":640,"cost":0.00097404}}`, model)
			}))})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx = WithConfiguredReasoningEffort(WithCallTag(ctx, "worker"), EffortOff)
			var billed []Billed
			ctx = WithBilling(ctx, func(b Billed) { billed = append(billed, b) })
			var order []int
			ctx = WithDiscardedUsage(ctx, func(gotModel, tag string, response *ai.Response) {
				order = append(order, 1)
				if gotModel != model || tag != "worker" || response.Text() != "" || response.Usage.CompletionTokens != 320 || response.Usage.CacheReadTokens() != 100 || response.Usage.CacheCreationTokens() != 20 {
					t.Errorf("discarded model=%q tag=%q response=%+v", gotModel, tag, response)
				}
				if recorded.count() != 1 || len(billed) != 1 {
					t.Errorf("notice arrived after retry or before billing: requests%d billed%d", recorded.count(), len(billed))
				}
			})
			ctx = WithDiscardedUsage(ctx, func(_ string, _ string, _ *ai.Response) {
				order = append(order, 2)
				if ending == "cancel" {
					cancel()
				}
			})
			response, err := client.CompleteWithMessages(ctx, userMessages("summarize"), ai.WithMaxTokens(320))
			if fmt.Sprint(order) != "[1 2]" {
				t.Fatalf("observers=%v", order)
			}
			wantCost := 0.0005994
			wantCalls := 1
			if ending == "answer" {
				wantCost += 0.00097404
				wantCalls++
				if err != nil || response.Text() != "the answer" || response.Usage.TotalTokens != 1507 || response.Usage.PromptTokens != 718 || response.Usage.CacheReadTokens() != 640 || math.Abs(*response.Usage.Cost-0.00097404) > 1e-12 {
					t.Fatalf("changed final answer: %+v %v", response, err)
				}
			} else if err == nil || response != nil {
				t.Fatalf("lost error contract: %+v %v", response, err)
			}
			var cost float64
			for _, b := range billed {
				cost += b.Cost
			}
			if len(billed) != wantCalls || math.Abs(cost-wantCost) > 1e-12 {
				t.Fatalf("full billing doubled/lost attempts: %d $%.8f", len(billed), cost)
			}
			if ending == "error" && !strings.Contains(err.Error(), "unauthorized") {
				t.Fatalf("error changed: %v", err)
			}
			if recorded.count() > 2 {
				t.Fatalf("retry limit changed: %d", recorded.count())
			}
		})
	}
}
