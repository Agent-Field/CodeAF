package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/control"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type progressRecorder struct {
	control.Controller
	visible *atomic.Int64
}

func (r progressRecorder) Note(reading control.Reading) control.Act {
	r.visible.Add(int64(reading.Visible))
	return r.Controller.Note(reading)
}

func TestWireProgressIsIndependentOfTextBatching(t *testing.T) {
	text := "A complete paragraph that arrives in a batch still represents all its words."
	for _, width := range []int{1, len(text)} {
		t.Run(fmt.Sprintf("batch=%d", width), func(t *testing.T) {
			client, _ := pinningClient(t, StaticRouting(RoutingLatency), 0, 0, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				for offset := 0; offset < len(text); offset += width {
					part, _ := json.Marshal(text[offset:min(offset+width, len(text))])
					fmt.Fprintf(w, "data: {\"provider\":\"test-endpoint\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%s}}]}\n\n", part)
				}
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			var visible atomic.Int64
			before := lanes.SetController(func(plan control.Plan) control.Controller {
				return progressRecorder{Controller: control.New(plan), visible: &visible}
			})
			t.Cleanup(func() { lanes.SetController(before) })
			ctx := WithStreamObserver(talking(), func(StreamEvent) {})
			response, err := client.CompleteWithMessages(ctx, userMessages("hello"))
			if err != nil || responseText(response) != text {
				t.Fatalf("wire answer changed: %v, %v", response, err)
			}
			if got, want := visible.Load(), int64((len(text)+charsPerToken-1)/charsPerToken); got != want {
				t.Fatalf("controller saw %d tokens, want %d regardless of batching", got, want)
			}
		})
	}
}

func TestToolOnlyStreamTeachesFirstTokenAndGenerationSeparately(t *testing.T) {
	clock := newClock()
	client, _ := pinningClient(t, StaticRouting(RoutingLatency), 0, 0, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"provider\":\"test-endpoint\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-a\",\"type\":\"function\",\"function\":{\"name\":\"read\",\"arguments\":\"{}\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":40,\"total_tokens\":50}}\n\ndata: [DONE]\n\n")
	}))
	ledger := primed(t, client.config.Model)
	client.velocity = newVelocityLedger()
	client.now = func() time.Time {
		clock.advance(10 * time.Millisecond)
		return clock.now()
	}
	ctx := WithStreamObserver(context.Background(), func(StreamEvent) {})
	if _, err := client.CompleteWithMessages(ctx, userMessages("read")); err != nil {
		t.Fatal(err)
	}
	for _, seen := range ledger.sightings() {
		if seen.ID.Lane == "test-endpoint" && seen.TTFT > 0 && seen.Gen > 0 && seen.Tokens == 40 {
			return
		}
	}
	t.Fatalf("tool generation was counted as first-token waiting: %+v", ledger.sightings())
}

func TestWatchedFaultUsesFundedAlternativeBeforeRepeatingTheFailedRequest(t *testing.T) {
	var calls, waits atomic.Int64
	client, recorded := pinningClient(t, StaticRouting(RoutingLatency), 0, 0, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct{ Provider struct{ Only []string } }
		copy, err := r.GetBody()
		if err == nil {
			_ = json.NewDecoder(copy).Decode(&body)
			copy.Close()
		}
		if len(body.Provider.Only) == 0 || body.Provider.Only[0] != "B" {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"error":{"message":"upstream unavailable","metadata":{"provider_name":"A"}}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"provider\":\"B\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"The replacement answered normally.\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	client.wait = func(context.Context, time.Duration) error { waits.Add(1); return nil }
	SetHedgeBudget(lanes.NewBudget(1, 0))
	t.Cleanup(func() { SetHedgeBudget(nil) })
	// This request overrides the client's default model. A refusal for that
	// default must not take away an alternative serving the requested model.
	const model = "vendor/override"
	lanes.RefuseServing(client.config.Model, "B")
	t.Cleanup(lanes.ForgetRefusals)
	ctx := WithLaneChoice(WithStreamObserver(talking(), func(StreamEvent) {}), choiceFor(model, time.Second))
	response, err := client.CompleteWithMessages(ctx, userMessages("hello"), ai.WithModel(model))
	if err != nil || !strings.Contains(responseText(response), "replacement") {
		t.Fatalf("alternative did not answer: %v, %v", response, err)
	}
	if calls.Load() != 2 || waits.Load() != 0 {
		t.Fatalf("recovery spent %d calls and %d backoffs; requests=%v", calls.Load(), waits.Load(), recorded.body(0))
	}
}

func TestStrictPinDoesNotWalkToAnotherEndpointAfterTransportFailure(t *testing.T) {
	client, recorded := pinningClient(t, StaticRouting(RoutingLatency), 0, 0, false,
		answering(reply{status: 503, body: `{"error":{"message":"temporarily unavailable","metadata":{"provider_name":"A"}}}`}))
	client.wait = func(context.Context, time.Duration) error { return nil }
	SetHedgeBudget(lanes.NewBudget(1, 0))
	t.Cleanup(func() { SetHedgeBudget(nil) })
	choice := choiceFor(client.config.Model, time.Second)
	choice.Only, choice.Order = []string{"A"}, nil
	ctx := WithLaneChoice(WithStreamObserver(talking(), func(StreamEvent) {}), choice)
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err == nil {
		t.Fatal("the only permitted endpoint failed but the request succeeded")
	}
	for _, body := range recorded.bodies {
		prefs, _ := body["provider"].(map[string]any)
		only := words(prefs["only"])
		if len(only) != 1 || only[0] != "A" {
			t.Fatalf("a strict pin escaped through recovery: %v", prefs)
		}
	}
}

func TestBorrowableUserPreferenceLeadsBothWarmAndDifferentCacheEndpoints(t *testing.T) {
	for _, cached := range []string{"brass", "quicksilver"} {
		t.Run(cached, func(t *testing.T) {
			client, _, model := stubbedRouter(t)
			primed(t, model, laneBelief(model, "quicksilver", 400, 70, 0.2), laneBelief(model, "brass", 900, 60, 0.3))
			pinned(t, LanePin{Lane: "brass", Borrow: true})
			client.pins = newEndpointPins()
			client.pins.hold("warm", model, cached)
			request := &ai.Request{Messages: userMessages("continue")}
			ctx := client.withLaneChoice(lineage("warm"), request)
			choice, _ := laneChoiceFromContext(ctx)
			prefs := client.wirePreferences(model, knobsFrom(ctx), request)
			if lanes.HeadOf(choice) != "brass" || prefs == nil || len(prefs.Order) == 0 || prefs.Order[0] != "brass" {
				t.Fatalf("cache displaced the person's choice: choice=%+v prefs=%+v", choice, prefs)
			}
		})
	}
}
