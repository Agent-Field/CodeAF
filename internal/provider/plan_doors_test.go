package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/modelsource/sourcestub"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestAnExhaustedPlanWaitsAndSpendsNothing(t *testing.T) {
	oldLimiter := sharedLimiter
	sharedLimiter = newAdaptiveLimiter()
	defer func() { sharedLimiter = oldLimiter }()

	plan, metered := sourcestub.New(), sourcestub.New()
	defer plan.Close()
	defer metered.Close()
	plan.RefuseCompletion(http.StatusTooManyRequests, `{"error":{"code":"1316","message":"Usage limit reached for the past 5 hours. Insufficient balance for extra usage. Your limit will reset at 18:30 UTC"}}`)

	told := listen(t)
	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: plan.URL(), Model: "glm-5.3-flash", Direct: true,
		BillingDoor: "coding plan", PlanOverflow: metered.URL(), PlanOverflowDoor: "pay-as-you-go",
	})
	if err != nil {
		t.Fatal(err)
	}
	client.wait = func(context.Context, time.Duration) error { return nil }
	response, err := client.CompleteWithMessages(context.Background(), userMessages("hello"))
	if err == nil || response != nil {
		t.Fatalf("an exhausted window became an answer: response=%v error=%v", response, err)
	}
	if len(metered.Requests()) != 0 {
		t.Fatalf("wait mode sent %d requests to the metered door", len(metered.Requests()))
	}
	if len(plan.Requests()) != 1 {
		t.Fatalf("wait mode retried the paused plan door %d times; want one request", len(plan.Requests()))
	}
	refusal, ok := RefusalFrom(err)
	if !ok || refusal.AccountCannotPay() {
		t.Fatalf("window refusal = %+v, terminal=%t", refusal, ok && refusal.AccountCannotPay())
	}
	ending, ok := PlanPauseFrom(err)
	if !ok || ending.Error() != "plan paused · resets at 18:30 UTC · /connect can switch to pay-as-you-go" {
		t.Fatalf("plan-pause ending = %+v, found=%t", ending, ok)
	}
	if evidence := Evidence(err); !evidence.PlanPaused || evidence.Unserved {
		t.Fatalf("plan-pause evidence = %+v, want a distinct temporary pause", evidence)
	}
	paused, ok := told.find(PhasePlanPaused)
	if !ok || paused.Detail != "resets at 18:30 UTC · /connect can switch to pay-as-you-go" {
		t.Fatalf("paused phase = %+v, found %t", paused, ok)
	}
}

func TestADirectToolLoopUsesTheDoorAwareAttributedTransport(t *testing.T) {
	oldLimiter := sharedLimiter
	sharedLimiter = newAdaptiveLimiter()
	defer func() { sharedLimiter = oldLimiter }()

	plan, metered := sourcestub.New(), sourcestub.New()
	defer plan.Close()
	defer metered.Close()
	plan.RefuseCompletion(http.StatusTooManyRequests, `{"code":"1316","message":"plan window exhausted"}`)
	client, err := NewClient(Config{
		APIKey: "tool-loop-test-key", BaseURL: plan.URL(), Model: "glm-5.3-flash", Direct: true,
		BillingDoor: "coding plan", PlanOverflow: metered.URL(), PlanOverflowDoor: "pay-as-you-go",
		OverflowOnPlanPause: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	response, _, err := client.ExecuteToolCallLoop(context.Background(), userMessages("hello"), nil,
		ai.ToolCallConfig{MaxTurns: 1, MaxToolCalls: 1}, nil)
	if err != nil || response == nil || response.Text() != "done" {
		t.Fatalf("door-aware tool loop = %v, %v", response, err)
	}
	if len(plan.Requests()) != 1 || len(metered.Requests()) != 1 {
		t.Fatalf("tool-loop requests: plan=%d metered=%d", len(plan.Requests()), len(metered.Requests()))
	}
	for _, request := range append(plan.Requests(), metered.Requests()...) {
		if request.Agent != DirectUserAgent || request.Bearer != "Bearer tool-loop-test-key" {
			t.Fatalf("direct tool-loop request omitted its identity or bearer: %+v", request)
		}
	}
}

func TestOverflowGoesToTheMeteredDoorAndSaysSo(t *testing.T) {
	oldLimiter := sharedLimiter
	sharedLimiter = newAdaptiveLimiter()
	defer func() { sharedLimiter = oldLimiter }()

	plan, metered := sourcestub.New(), sourcestub.New()
	defer plan.Close()
	defer metered.Close()
	plan.RefuseCompletion(http.StatusTooManyRequests, `{"code":"1316","message":"Insufficient balance for extra usage"}`)

	told := listen(t)
	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: plan.URL(), Model: "glm-5.3-flash", Direct: true,
		BillingDoor: "coding plan", PlanOverflow: metered.URL(), PlanOverflowDoor: "pay-as-you-go",
		OverflowOnPlanPause: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.CompleteWithMessages(context.Background(), userMessages("hello"))
	if err != nil || response == nil {
		t.Fatalf("metered overflow = %v, %v", response, err)
	}
	if len(plan.Requests()) != 1 || len(metered.Requests()) != 1 ||
		plan.Requests()[0].Agent != DirectUserAgent || metered.Requests()[0].Agent != DirectUserAgent {
		t.Fatalf("requests: plan=%d metered=%d", len(plan.Requests()), len(metered.Requests()))
	}
	writing, ok := told.find(PhaseWriting)
	if !ok || writing.Door != "pay-as-you-go" {
		t.Fatalf("writing phase = %+v, found %t", writing, ok)
	}
	for _, news := range told.all() {
		if news.Phase == PhaseRetrying || news.Phase == PhaseConnectionLost {
			t.Fatalf("billing-door switch was narrated as recovery: %+v", news)
		}
	}
}

func TestOverflowCanNameASecondDoorOnTheSameHost(t *testing.T) {
	oldLimiter := sharedLimiter
	sharedLimiter = newAdaptiveLimiter()
	defer func() { sharedLimiter = oldLimiter }()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(writer, `{"code":"1316","message":"plan window exhausted"}`)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	base := server.URL + "/v1"
	told := listen(t)
	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: base, Model: "sim/model", Direct: true,
		BillingDoor: "fixed plan", PlanOverflow: base, PlanOverflowDoor: "pay-as-you-go",
		OverflowOnPlanPause: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.CompleteWithMessages(context.Background(), userMessages("hello"))
	if err != nil || response == nil || requests.Load() != 2 {
		t.Fatalf("same-host overflow: response=%v error=%v requests=%d", response, err, requests.Load())
	}
	writing, ok := told.find(PhaseWriting)
	if !ok || writing.Door != "pay-as-you-go" {
		t.Fatalf("same-host writing phase = %+v, found %t", writing, ok)
	}
}

func TestPlanOverflowHappensAtMostOnce(t *testing.T) {
	oldLimiter := sharedLimiter
	sharedLimiter = newAdaptiveLimiter()
	defer func() { sharedLimiter = oldLimiter }()

	plan, metered := sourcestub.New(), sourcestub.New()
	defer plan.Close()
	defer metered.Close()
	plan.RefuseCompletion(http.StatusTooManyRequests, `{"code":"1316","message":"plan window exhausted"}`)
	metered.RefuseCompletion(http.StatusTooManyRequests, `{"code":"1316","message":"plan window exhausted"}`)
	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: plan.URL(), Model: "glm-5.3-flash", Direct: true,
		BillingDoor: "coding plan", PlanOverflow: metered.URL(), PlanOverflowDoor: "pay-as-you-go",
		OverflowOnPlanPause: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.CompleteWithMessages(context.Background(), userMessages("hello"))
	if response != nil {
		t.Fatalf("twice-paused request became an answer: %+v", response)
	}
	if _, ok := PlanPauseFrom(err); !ok {
		t.Fatalf("twice-paused request ended as %T %v, want PlanPauseError", err, err)
	}
	if len(plan.Requests()) != 1 || len(metered.Requests()) != 1 {
		t.Fatalf("overflow was not one-shot: plan=%d metered=%d", len(plan.Requests()), len(metered.Requests()))
	}
}

func TestPlanOverflowCannotRepeatAcrossARepair(t *testing.T) {
	oldLimiter := sharedLimiter
	sharedLimiter = newAdaptiveLimiter()
	defer func() { sharedLimiter = oldLimiter }()

	var planCalls, meteredCalls atomic.Int32
	plan := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		planCalls.Add(1)
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(writer, `{"code":"1316","message":"plan window exhausted"}`)
	}))
	defer plan.Close()
	metered := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		meteredCalls.Add(1)
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(writer, `{"error":{"message":"Reasoning is mandatory for this endpoint; do not send effort: none","code":400}}`)
	}))
	defer metered.Close()

	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: plan.URL, Model: "plan-repair-overflow/model", Direct: true,
		BillingDoor: "coding plan", PlanOverflow: metered.URL, PlanOverflowDoor: "pay-as-you-go",
		OverflowOnPlanPause: true,
		SupportsParameter:   func(string, string) (bool, bool) { return true, true },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithRequiredReasoningEffort(context.Background(), EffortOff)
	response, err := client.CompleteWithMessages(ctx, userMessages("hello"), ai.WithMaxTokens(1_000))
	if response != nil {
		t.Fatalf("a second overflow became an answer: %+v", response)
	}
	if _, ok := PlanPauseFrom(err); !ok {
		t.Fatalf("repair re-entry ended as %T %v, want PlanPauseError", err, err)
	}
	if planCalls.Load() != 2 || meteredCalls.Load() != 1 {
		t.Fatalf("repair repeated overflow: plan=%d metered=%d, want plan=2 metered=1", planCalls.Load(), meteredCalls.Load())
	}
}
