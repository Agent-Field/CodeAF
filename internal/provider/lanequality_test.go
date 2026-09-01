package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
)

// ── THE QUALITY LOOP CLOSES ─────────────────────────────────────────────────
//
// The belief has carried a quality axis since the routing wave landed — a Beta
// posterior, decayed toward the lane's prior over QualityHalfLife, read by the
// frontier gate against the role's own QualityNeed — and until this wave NOTHING
// IN THE SHIPPED PATH EVER WROTE TO IT. Ledger.NoteOutcome had one caller in the
// whole tree and it was a simulator.
//
// So the measured failure of 2026-08-31 was not a missing mechanism. An endpoint
// served a reply that had stopped being language, the stream was cut for it, the
// old strike ledger refused the lane for five minutes — and the belief, which is
// what the chooser actually ranks on, learned nothing at all. These pin the two
// wires that were missing.

// qualityClient builds a client that talks to `base` as though it were the
// router, with a recording ledger under it. The model id carries the router's
// prefix, which is what makes the attribution seams fire.
func qualityClient(t *testing.T, base string) (*Client, *recordingLedger, string) {
	t.Helper()
	const model = "openrouter/quality-model"
	ledger := primed(t, lanes.BareModel(model),
		laneBelief(lanes.BareModel(model), "gusher", 400, 200, 0.25),
		laneBelief(lanes.BareModel(model), "steady", 500, 180, 0.25),
	)
	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: base, Model: model,
		Routing: StaticRouting(RoutingLatency),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.velocity = newVelocityLedger()
	return client, ledger, model
}

// soupServer streams a degenerate reply, naming the lane that served it.
func soupServer(t *testing.T, soup string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		fmt.Fprint(w, `data: {"id":"one","provider":"gusher","choices":`+
			`[{"index":0,"delta":{"content":"here goes "}}]}`+"\n\n")
		w.(http.Flusher).Flush()
		for at := 0; at < len(soup); at += 200 {
			end := at + 200
			if end > len(soup) {
				end = len(soup)
			}
			fmt.Fprint(w, "data: "+deltaChunk(soup[at:end])+"\n\n")
			w.(http.Flusher).Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	return server
}

// TestAReplyThatStoppedBeingLanguageTellsTheBeliefTheLaneDidNotServe is the
// acceptance's first clause, at the seam where it was broken.
func TestAReplyThatStoppedBeingLanguageTellsTheBeliefTheLaneDidNotServe(t *testing.T) {
	soup := loadCorpus(t, "babble-repetition-loop.txt")
	client, ledger, _ := qualityClient(t, soupServer(t, soup).URL)

	ctx := WithStreamObserver(context.Background(), func(StreamEvent) {})
	_, err := client.CompleteWithMessages(ctx, userMessages("hello"))
	cut, ok := CutFrom(err)
	if !ok || cut.Reason != CutBabble {
		t.Fatalf("err = %v, want a degeneration cut", err)
	}

	outcomes := ledger.judged()
	if len(outcomes) == 0 {
		t.Fatal("the lane's belief was told nothing about a reply that had stopped being language")
	}
	last := outcomes[len(outcomes)-1]
	if last.Accepted {
		t.Fatalf("the belief was told the answer was usable: %+v", last)
	}
	if last.ID.Lane != "gusher" {
		t.Fatalf("the outcome was charged to %q, want the endpoint that served it", last.ID.Lane)
	}
	if last.Reason != "babble" {
		t.Fatalf("reason = %q, want the cut's own word", last.Reason)
	}
	if last.At.IsZero() {
		t.Fatal("the outcome carries no moment, so nothing can ever age it back")
	}
}

// TestAGoodAnswerTellsTheBeliefTheLaneServed is the recovery half, and it is what
// keeps the axis from being a penalty box: a lane walks its own standing back up
// on the answers it gets right, without waiting on any clock.
func TestAGoodAnswerTellsTheBeliefTheLaneServed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		fmt.Fprint(w, `data: {"id":"one","provider":"gusher","choices":`+
			`[{"index":0,"delta":{"content":"a real answer, in words"}}]}`+"\n\n")
		w.(http.Flusher).Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	client, ledger, _ := qualityClient(t, server.URL)

	ctx := WithStreamObserver(context.Background(), func(StreamEvent) {})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatalf("a healthy answer failed: %v", err)
	}

	outcomes := ledger.judged()
	if len(outcomes) == 0 {
		t.Fatal("a lane that served was told nothing, so nothing it does can ever lift a demotion")
	}
	last := outcomes[len(outcomes)-1]
	if !last.Accepted {
		t.Fatalf("a usable answer was recorded as a failure: %+v", last)
	}
	if last.ID.Lane != "gusher" || last.Reason != "served" {
		t.Fatalf("outcome = %+v, want the serving lane credited", last)
	}
}

// TestAnAnswerWithNothingInItIsNotService pins the perverse incentive shut. An
// endpoint that returns 200 and nothing else passes every stream bound, and the
// turn loop has always read it as the failure it is; counting it as service here
// would let the fastest way to keep a good standing be to answer nothing.
func TestAnAnswerWithNothingInItIsNotService(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		fmt.Fprint(w, `data: {"id":"one","provider":"gusher","choices":`+
			`[{"index":0,"delta":{"content":""}}]}`+"\n\n")
		w.(http.Flusher).Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	client, ledger, _ := qualityClient(t, server.URL)

	ctx := WithStreamObserver(context.Background(), func(StreamEvent) {})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatalf("an empty answer should still come back as an answer: %v", err)
	}
	outcomes := ledger.judged()
	if len(outcomes) == 0 {
		t.Fatal("an empty answer taught the belief nothing at all")
	}
	last := outcomes[len(outcomes)-1]
	if last.Accepted {
		t.Fatalf("an answer with nothing in it was counted as service: %+v", last)
	}
	if last.Reason != "empty" {
		t.Fatalf("reason = %q, want the word for an answer that was not one", last.Reason)
	}
}

// TestAnUnnamedStreamTeachesTheBeliefNothing is the attribution law, which this
// wave must not weaken: crediting an anonymous answer to some lane is how a
// belief learns a fact about a machine that was never asked.
func TestAnUnnamedStreamTeachesTheBeliefNothing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		fmt.Fprint(w, "data: "+deltaChunk("an answer from nobody in particular")+"\n\n")
		w.(http.Flusher).Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	client, ledger, _ := qualityClient(t, server.URL)

	ctx := WithStreamObserver(context.Background(), func(StreamEvent) {})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatalf("the answer failed: %v", err)
	}
	if outcomes := ledger.judged(); len(outcomes) != 0 {
		t.Fatalf("an unattributed answer was charged to %d lanes: %+v", len(outcomes), outcomes)
	}
}

// TestRoutingOffTeachesTheBeliefNothing pins the operator's instruction. Somebody
// who asked for no steering asked for no demotions either, which is the same law
// the strike ledger keeps about its own writes.
func TestRoutingOffTeachesTheBeliefNothing(t *testing.T) {
	soup := loadCorpus(t, "babble-repetition-loop.txt")
	client, ledger, _ := qualityClient(t, soupServer(t, soup).URL)
	client.config.Routing = StaticRouting(RoutingOff)

	ctx := WithStreamObserver(context.Background(), func(StreamEvent) {})
	_, _ = client.CompleteWithMessages(ctx, userMessages("hello"))
	if outcomes := ledger.judged(); len(outcomes) != 0 {
		t.Fatalf("a session with routing off wrote %d outcomes: %+v", len(outcomes), outcomes)
	}
}
