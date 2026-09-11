package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// These are real adapter calls: both response decoders must teach the next
// request, including reasoning that cannot be read while a tool is waiting.
func TestCompletedWireAnswersTeachWorkloadOnBothTransports(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			client, _ := pinningClient(t, StaticRouting(RoutingLatency), 0, 0, false,
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					usage := `"usage":{"prompt_tokens":10,"completion_tokens":120,"total_tokens":130,"completion_tokens_details":{"reasoning_tokens":80}}`
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(w, "data: {\"provider\":\"test-endpoint\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"An answer with useful words.\"}}]}\n\n")
						fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],%s}\n\ndata: [DONE]\n\n", usage)
					} else {
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprintf(w, `{"provider":"test-endpoint","choices":[{"index":0,"message":{"role":"assistant","content":"An answer with useful words."},"finish_reason":"stop"}],%s}`, usage)
					}
				}))
			ctx := context.Background()
			if stream {
				ctx = WithStreamObserver(ctx, func(StreamEvent) {})
			}
			if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
				t.Fatal(err)
			}
			visible, hidden := client.workloadFor(client.config.Model, knobsFrom(ctx), &ai.Request{})
			if visible != 40 || hidden != 80 {
				t.Fatalf("next request learned %d readable / %d waiting tokens, want 40/80", visible, hidden)
			}
		})
	}
}

func TestToolWorkForecastValuesCompletionSpeedAndHonorsOutputCap(t *testing.T) {
	client, _, model := stubbedRouter(t)
	primed(t, model,
		laneBelief(model, "early", 400, 20, 0.2),
		laneBelief(model, "productive", 700, 200, 0.2),
	)
	text := strings.Repeat("word", 1000)
	prose := &ai.Request{Messages: []ai.Message{{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}}}}}
	tool := &ai.Request{Messages: []ai.Message{{Role: "assistant", ToolCalls: []ai.ToolCall{{Type: "function", Function: ai.ToolCallFunction{Name: "write", Arguments: text}}}}}}
	choose := func(request *ai.Request) string {
		ask := client.laneRequest(model, callKnobs{}, request, lanes.AttentionValue)
		ask.Typical = true
		return lanes.HeadOf(lanes.Default().Chooser().Choose(ask))
	}
	if got := choose(prose); got != "early" {
		t.Fatalf("readable prose chose %q instead of the quicker first words", got)
	}
	if got := choose(tool); got != "productive" {
		t.Fatalf("tool arguments chose %q instead of the faster completed operation", got)
	}
	cap := 100
	tool.MaxTokens = &cap
	visible, hidden := client.workloadFor(model, callKnobs{}, tool)
	if visible != 0 || hidden != cap {
		t.Fatalf("forecast escaped the request cap: %d/%d", visible, hidden)
	}
}

func TestPartialAndUnusableAnswersCannotTeachShortWork(t *testing.T) {
	client, _ := pinningClient(t, StaticRouting(RoutingLatency), 0, 0, false, answering(ok(answerFrom("test", 10, 0, 0))))
	for _, finish := range []string{"length", "content_filter", "", "error"} {
		response := &ai.Response{Usage: &ai.Usage{CompletionTokens: 100}, Choices: []ai.Choice{{
			FinishReason: finish, Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "partial"}}},
		}}}
		client.noteWorkload(&ai.Request{}, callKnobs{}, response, 0)
	}
	history := lanes.Default().Ledger().(lanes.Workloads)
	if _, _, known := history.Workload(client.config.Model, client.workloadClass(client.config.Model, callKnobs{}, &ai.Request{}), time.Now()); known {
		t.Fatal("an incomplete answer became a completed-work forecast")
	}
}

func TestCachePreferenceAndStallWatchNameTheSameEndpoint(t *testing.T) {
	client, _, model := stubbedRouter(t)
	primed(t, model, laneBelief(model, "quicksilver", 400, 70, 0.2), laneBelief(model, "brass", 900, 60, 0.3))
	client.pins = newEndpointPins()
	client.pins.hold("warm", model, "brass")
	request := &ai.Request{Messages: userMessages("continue")}
	ctx := client.withLaneChoice(lineage("warm"), request)
	choice, made := laneChoiceFromContext(ctx)
	prefs := client.wirePreferences(model, knobsFrom(ctx), request)
	if !made || lanes.HeadOf(choice) != "brass" || prefs == nil || len(prefs.Order) == 0 || prefs.Order[0] != "brass" {
		t.Fatalf("watch and wire disagree on cached endpoint: choice=%+v, prefs=%+v", choice, prefs)
	}
	plan := client.planFor(ctx, choice, model, 0)
	if plan.Lane != "brass" {
		t.Fatalf("watch still times %q", plan.Lane)
	}
	for _, alt := range plan.Alts {
		if alt.Lane == "brass" {
			t.Fatal("rescue would duplicate the primary endpoint")
		}
	}
}
