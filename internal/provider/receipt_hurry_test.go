package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// TestAHurriedReceiptAsksOnceMoreAndAnswersWithoutItsPauses: a receipt sitting
// in a long pause of its schedule is hurried by a quitting process, asks once
// more, and is answered as unpriced at once instead of waiting the schedule out.
func TestAHurriedReceiptAsksOnceMoreAndAnswersWithoutItsPauses(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"data":{}}`)
	}))
	t.Cleanup(server.Close)
	client := receiptTestClient(t, server)
	// Every pause would last until the schedule's ceiling: only the hurry can
	// end it early.
	pausing := make(chan struct{}, 8)
	client.wait = func(ctx context.Context, _ time.Duration) error {
		pausing <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	results := make(chan Reconciled, 1)
	client.settle(WithReconcile(t.Context(), func(result Reconciled) { results <- result }),
		"sim/model", &ai.Response{ID: "gen-hurried"}, "cut", 0)
	select {
	case <-pausing:
	case <-time.After(10 * time.Second):
		t.Fatal("the receipt never reached its first pause")
	}
	began := time.Now()
	calm := HurryReceipts()
	t.Cleanup(calm)
	result := receiptResult(t, results)
	if took := time.Since(began); took > receiptHurryAsk+time.Second {
		t.Fatalf("a hurried receipt took %v to answer, want within about %v", took, receiptHurryAsk)
	}
	if result.Found {
		t.Fatalf("a receipt the provider never priced was found: %+v", result)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("a hurried receipt asked %d times, want its first ask and one last one", got)
	}
}

// TestAReceiptOwedAfterTheHurryAsksOnceAndNeverPauses: once the process is
// leaving, a newly owed receipt asks one time and is answered.
func TestAReceiptOwedAfterTheHurryAsksOnceAndNeverPauses(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"data":{}}`)
	}))
	t.Cleanup(server.Close)
	client := receiptTestClient(t, server)
	var paused atomic.Int64
	client.wait = func(context.Context, time.Duration) error {
		paused.Add(1)
		return nil
	}
	calm := HurryReceipts()
	t.Cleanup(calm)
	results := make(chan Reconciled, 1)
	client.settle(WithReconcile(t.Context(), func(result Reconciled) { results <- result }),
		"sim/model", &ai.Response{ID: "gen-after-hurry"}, "cut", 0)
	if result := receiptResult(t, results); result.Found {
		t.Fatalf("a receipt the provider never priced was found: %+v", result)
	}
	if requests.Load() != 1 || paused.Load() != 0 {
		t.Fatalf("after the hurry a receipt asked %d times and paused %d times, want 1 and 0", requests.Load(), paused.Load())
	}
}

// TestAHurriedAskInFlightIsCutWithinItsBound: an ask the router is slow to
// answer when the hurry comes is given [receiptHurryAsk] more, not the rest of
// the schedule's ceiling.
func TestAHurriedAskInFlightIsCutWithinItsBound(t *testing.T) {
	inFlight := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case inFlight <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
		case <-release:
		}
		fmt.Fprint(w, `{"data":{}}`)
	}))
	t.Cleanup(func() { close(release); server.Close() })
	client := receiptTestClient(t, server)
	client.wait = func(context.Context, time.Duration) error { return nil }
	results := make(chan Reconciled, 1)
	client.settle(WithReconcile(t.Context(), func(result Reconciled) { results <- result }),
		"sim/model", &ai.Response{ID: "gen-slow-router"}, "cut", 0)
	select {
	case <-inFlight:
	case <-time.After(10 * time.Second):
		t.Fatal("the receipt was never asked for")
	}
	began := time.Now()
	calm := HurryReceipts()
	t.Cleanup(calm)
	if result := receiptResult(t, results); result.Found {
		t.Fatalf("an unanswered receipt was found: %+v", result)
	}
	// The ask in flight gets its bound and is the last ask: never the
	// seventy-second ceiling.
	if took := time.Since(began); took > receiptHurryAsk+time.Second {
		t.Fatalf("a hurried receipt with a slow router took %v, want within %v", took, receiptHurryAsk)
	}
}

// TestCalmingTheHurryLeavesTheNextReceiptItsSchedule: the hurry is the
// process's, and its answer gives the schedule back to whatever runs next.
func TestCalmingTheHurryLeavesTheNextReceiptItsSchedule(t *testing.T) {
	HurryReceipts()()
	if err := receiptHurried().Err(); err != nil {
		t.Fatalf("a calmed hurry still reads %v", err)
	}
}
