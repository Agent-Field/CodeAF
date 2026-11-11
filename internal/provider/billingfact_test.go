package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/modelsource/sourcestub"
)

// pricedServer answers every completion — whole or streamed — with a usage
// block carrying a provider cost, which is the one shape a billing fact rides
// on through [Client.bill].
func pricedServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope struct {
			Stream bool `json:"stream"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &envelope)
		usage := `"usage":{"prompt_tokens":11,"completion_tokens":5,"total_tokens":16,"cost":0.02}`
		if envelope.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"id\":\"g-1\",\"model\":\"m/x\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"done\"},\"finish_reason\":\"stop\"}],%s}\n\ndata: [DONE]\n\n", usage)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"g-1","model":"m/x","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"done"}}],%s}`, usage)
	}))
	t.Cleanup(server.Close)
	return server
}

// billingProbe arms the two doors a billing fact reaches a banker through: the
// caller's served slot and a billing sink.
func billingProbe(ctx context.Context) (context.Context, *ServedEndpoint, func() []Billed) {
	slot := &ServedEndpoint{}
	var mu sync.Mutex
	var billed []Billed
	ctx = WithBilling(WithServedEndpoint(ctx, slot), func(b Billed) {
		mu.Lock()
		defer mu.Unlock()
		billed = append(billed, b)
	})
	return ctx, slot, func() []Billed {
		mu.Lock()
		defer mu.Unlock()
		return append([]Billed(nil), billed...)
	}
}

// TestABoundDoorBillsItsAnswerAsItsOwnRoad: a plan door's own statement rides
// its answer to both doors, exactly once — a second take of the slot is empty.
func TestABoundDoorBillsItsAnswerAsItsOwnRoad(t *testing.T) {
	server := pricedServer(t)
	plan := IncludedDoorFact("z-ai", "coding-plan")
	client, err := NewClient(Config{
		APIKey: "k", BaseURL: server.URL, Model: "m/x", Direct: true,
		HTTPClient: server.Client(), Billing: plan,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, slot, billed := billingProbe(t.Context())
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if got := billed(); len(got) != 1 || got[0].Billing != plan || got[0].Cost != 0.02 {
		t.Fatalf("billed = %+v, want one $0.02 answer on %+v", got, plan)
	}
	if got := slot.TakeBilling(); got != plan {
		t.Fatalf("slot billing = %+v, want %+v", got, plan)
	}
	if got := slot.TakeBilling(); got != (BillingFact{}) {
		t.Fatalf("second take = %+v, want nothing: a take forgets", got)
	}
}

// TestAnOverflowAnswerIsMeteredWhateverTheDoorSays: the plan door refuses with
// its pause, the person's opted-in overflow answers, and the answer is billed
// as the metered overflow road — never as the plan the client was built for.
func TestAnOverflowAnswerIsMeteredWhateverTheDoorSays(t *testing.T) {
	oldLimiter := sharedLimiter
	sharedLimiter = newAdaptiveLimiter()
	defer func() { sharedLimiter = oldLimiter }()

	plan := sourcestub.New()
	defer plan.Close()
	plan.RefuseCompletion(http.StatusTooManyRequests, `{"code":"1316","message":"Insufficient balance for extra usage"}`)
	metered := pricedServer(t)
	client, err := NewClient(Config{
		APIKey: "k", BaseURL: plan.URL(), Model: "glm-5.3-flash", Direct: true,
		BillingDoor: "coding plan", Billing: IncludedDoorFact("z-ai", "coding-plan"),
		PlanOverflow: metered.URL, PlanOverflowDoor: "pay-as-you-go", OverflowOnPlanPause: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, slot, billed := billingProbe(t.Context())
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	want := BillingFact{Kind: BillingMetered, Basis: "overflow:pay-as-you-go"}
	if got := billed(); len(got) != 1 || got[0].Billing != want {
		t.Fatalf("billed = %+v, want one answer on %+v", got, want)
	}
	if got := slot.TakeBilling(); got != want {
		t.Fatalf("slot billing = %+v, want %+v", got, want)
	}
}

// TestALateReceiptKeepsTheRoadItsAttemptWentOutOn: a cut stream's receipt is
// owed under the attempt's road, and a client whose statement changes before
// the receipt lands does not relabel money already spent.
func TestALateReceiptKeepsTheRoadItsAttemptWentOutOn(t *testing.T) {
	defer shortenStallBounds(t, time.Second, 30*time.Millisecond)()
	releaseReceipt := make(chan struct{})
	var releaseOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `data: {"id":"generation-b1","choices":[{"index":0,"delta":{"content":"begun"}}]}`+"\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case "/generation":
			<-releaseReceipt
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":{"total_cost":0.41,"tokens_prompt":30,"tokens_completion":9}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseReceipt) })
		server.Close()
	})
	client := receiptTestClient(t, server)
	road := MeteredDoorFact("openrouter", "credits")
	client.config.Billing = road
	results := make(chan Reconciled, 1)
	ctx := WithReconcile(WithStreamObserver(t.Context(), func(StreamEvent) {}), func(result Reconciled) {
		results <- result
	})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err == nil {
		t.Fatal("the stream did not reach its cut")
	}
	// The statement moves while the receipt is still owed.
	client.config.Billing = IncludedDoorFact("openrouter", "plan")
	releaseOnce.Do(func() { close(releaseReceipt) })
	result := receiptResult(t, results)
	if !result.Found || result.Cost != 0.41 || result.Billing != road {
		t.Fatalf("reconciled = %+v, want the $0.41 receipt on %+v", result, road)
	}
}

// TestAnUnstatedRoadIsNeverIncluded pins the vocabulary's floor: nothing that
// was not stated in so many words reads as included, and folding answers
// together can only err toward a charge.
func TestAnUnstatedRoadIsNeverIncluded(t *testing.T) {
	for name, fact := range map[string]BillingFact{
		"zero":               {},
		"door with no id":    IncludedDoorFact("z-ai", " "),
		"included, no basis": {Kind: BillingIncluded},
		"foreign kind":       {Kind: "free", Basis: "door:x/y"},
	} {
		if got := fact.normalized(); got.Included() || got.Kind != BillingUnknown {
			t.Errorf("%s normalized = %+v, want unknown", name, got)
		}
	}
	plan := IncludedDoorFact("z-ai", "coding-plan")
	meter := MeteredDoorFact("z-ai", "metered")
	unknown := BillingFact{Kind: BillingUnknown}
	for _, fold := range []struct{ a, b, want BillingFact }{
		{plan, plan, plan},
		{plan, meter, meter},
		{meter, plan, meter},
		{plan, unknown, unknown},
		{unknown, plan, unknown},
	} {
		if got := foldBilling(fold.a, fold.b); got != fold.want {
			t.Errorf("fold(%+v, %+v) = %+v, want %+v", fold.a, fold.b, got, fold.want)
		}
	}
	// A client that may switch to a metered overflow cannot say, without the
	// attempt's own trace, which road answered.
	client := &Client{config: Config{Billing: plan, PlanOverflow: "http://meter", OverflowOnPlanPause: true}}
	if got := client.answeredBilling(); got.Kind != BillingUnknown {
		t.Fatalf("untraced answer on an overflow-armed plan = %+v, want unknown", got)
	}
	if got := client.answeredBilling(&callTrace{attemptBilled: true, attemptBilling: plan}); got != plan {
		t.Fatalf("traced answer = %+v, want %+v", got, plan)
	}
}
