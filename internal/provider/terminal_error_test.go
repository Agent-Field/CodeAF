package provider

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// A terminal error can arrive without an error object. Both HTTP body shapes
// must reject it before it warms a cache or teaches a successful generation.
func TestTerminalErrorDoesNotKeepProviderAffinity(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			var requests atomic.Int64
			client, recorded := pinningClient(t, StaticRouting(RoutingLatency), 0, 0, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if requests.Add(1) > 1 {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, answerFrom("healthy", 100, 0, 0.001))
					return
				}
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"provider\":\"broken\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning\":\"Thinking about the request.\"}}]}\n\n")
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"error\"}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20,\"cost\":0.01}}\n\ndata: [DONE]\n\n")
				} else {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"provider":"broken","choices":[{"index":0,"message":{"role":"assistant","content":"","reasoning":"Thinking about the request."},"finish_reason":"error"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"cost":0.01}}`)
				}
			}))
			client.velocity = newVelocityLedger()
			ledger := primed(t, client.config.Model)
			client.pins.hold("terminal-error", client.config.Model, "broken")
			var bills []Billed
			ctx := WithBilling(lineage("terminal-error"), func(b Billed) { bills = append(bills, b) })
			var finished atomic.Int64
			var answerBytes atomic.Int64
			ctx = WithProseAnswer(ctx)
			ctx = WithStreamObserver(ctx, func(event StreamEvent) {
				if event.Kind == StreamFinished {
					finished.Add(1)
				}
				if event.Kind == StreamDelta {
					answerBytes.Add(int64(len(event.Delta)))
				}
			})
			response, err := client.CompleteWithMessages(ctx, userMessages("hello"))
			if err == nil || response != nil {
				t.Errorf("terminal provider error became a successful response: response=%v error=%v", response, err)
			}
			if refusal, ok := RefusalFrom(err); !ok || refusal.Status != http.StatusBadGateway || refusal.Provider != "broken" {
				t.Errorf("terminal error lost upstream recovery evidence: %v", err)
			}
			if pinned := client.pins.endpoint("terminal-error", client.config.Model); pinned != "" {
				t.Errorf("failed endpoint retained cache affinity: %q", pinned)
			}
			if sightings := ledger.sightings(); len(sightings) != 0 {
				t.Errorf("failed generation taught speed: %+v", sightings)
			}
			if finished.Load() != 0 {
				t.Error("failed generation announced a finished stream")
			}
			if answerBytes.Load() != 0 {
				t.Error("failed generation promoted reasoning into answer text")
			}
			ledger.mu.Lock()
			if len(ledger.outcomes) != 1 || ledger.outcomes[0].Accepted || ledger.outcomes[0].ID.Lane != "broken" {
				t.Errorf("failed generation lost its quality evidence: %+v", ledger.outcomes)
			}
			ledger.mu.Unlock()
			if len(bills) != 1 || bills[0].Cost != 0.01 || bills[0].CompletionTokens != 20 {
				t.Errorf("failed generation lost or duplicated billing: %+v", bills)
			}
			if len(recorded.bodies) != 1 {
				t.Errorf("terminal response unexpectedly resent: %d requests", len(recorded.bodies))
			}
			if response, err := client.CompleteWithMessages(ctx, userMessages("try again")); err != nil || responseText(response) != "ok" {
				t.Fatalf("next request failed: %v, %v", response, err)
			}
			if order := orderOn(t, recorded, 1); namesEndpoint(order, "broken") {
				t.Errorf("next request preferred the failed endpoint: %v", order)
			}
			if ignored := words(prefsOn(t, recorded, 1)["ignore"]); !namesEndpoint(ignored, "broken") {
				t.Errorf("next request did not route around the failed endpoint: %v", ignored)
			}
		})
	}
}

// Tool arguments and partial prose do not override an explicit failure. Other
// finish markers retain their existing contract, including empty length-capped
// reasoning whose recovery is owned by learnFromAnswer and its caller.
func TestTerminalErrorKeepsHealthyFinishContracts(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, finish := range []string{"error", "stop", "tool_calls", "length", ""} {
			for _, kind := range []string{"text", "tools", "reasoning"} {
				t.Run(fmt.Sprintf("stream=%v/finish=%s/%s", stream, finish, kind), func(t *testing.T) {
					parts := map[string]string{
						"text":      `"content":"A partial or complete answer."`,
						"tools":     `"tool_calls":[{"index":0,"id":"call-a","type":"function","function":{"name":"read","arguments":"{}"}}]`,
						"reasoning": `"reasoning":"Thinking about the request."`,
					}
					client, _ := pinningClient(t, StaticRouting(RoutingLatency), 0, 0, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if stream {
							w.Header().Set("Content-Type", "text/event-stream")
							fmt.Fprintf(w, "data: {\"provider\":\"endpoint\",\"choices\":[{\"index\":0,\"delta\":{%s}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", parts[kind], finish)
						} else {
							w.Header().Set("Content-Type", "application/json")
							fmt.Fprintf(w, `{"provider":"endpoint","choices":[{"index":0,"message":{"role":"assistant",%s},"finish_reason":%q}]}`, parts[kind], finish)
						}
					}))
					response, err := client.CompleteWithMessages(lineage("finish-contract"), userMessages("hello"))
					if finish == "error" {
						if err == nil || response != nil || !strings.Contains(err.Error(), "finish_reason=error") {
							t.Fatalf("partial reply overrode failure: %v, %v", response, err)
						}
						return
					}
					if err != nil || response == nil {
						t.Fatalf("non-error finish changed: %v, %v", response, err)
					}
					if kind == "tools" && !answeredWithToolCalls(response) {
						t.Fatal("valid tool-only answer lost its call")
					}
				})
			}
		}
	}
}
