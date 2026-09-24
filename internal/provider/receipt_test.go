package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// receiptTestClient uses the supplied transport and removes the retry sleep.
// Every receipt test therefore exercises the real HTTP request without either
// a live key or time passing for the reconciliation schedule.
func receiptTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	client, err := NewClient(Config{
		APIKey: "receipt-key", BaseURL: server.URL, Model: "sim/model",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.velocity = newVelocityLedger()
	client.wait = func(context.Context, time.Duration) error { return nil }
	return client
}

func receiptResult(t *testing.T, results <-chan Reconciled) Reconciled {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("the receipt worker did not report what became of the call")
		return Reconciled{}
	}
}

// writeReceiptCut writes one identified answer fragment and then leaves the
// stream open. The shortened silence guard closes it, reproducing the ending
// whose absent usage block makes receipt reconciliation relevant.
func writeReceiptCut(w http.ResponseWriter, r *http.Request, id string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `data: {"id":%q,"choices":[{"index":0,"delta":{"content":"begun"}}]}`+"\n\n", id)
	w.(http.Flusher).Flush()
	<-r.Context().Done()
}

// TestContracts1And3ADirectCutStopsAtItsCompletion is validation contract
// items 1 and 3: after a direct stream ends without usage, either kind of
// direct billing door receives only the completion and no missing-price result
// reaches the sink that would book an unbilled marker.
func TestContracts1And3ADirectCutStopsAtItsCompletion(t *testing.T) {
	defer shortenStallBounds(t, 30*time.Millisecond, 30*time.Millisecond)()
	for _, door := range []string{"coding plan", "pay-as-you-go"} {
		t.Run(door, func(t *testing.T) {
			var mu sync.Mutex
			releaseUnexpected := make(chan struct{})
			var releaseOnce sync.Once
			var requests []struct {
				method string
				path   string
				header http.Header
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				requests = append(requests, struct {
					method string
					path   string
					header http.Header
				}{method: r.Method, path: r.URL.Path, header: r.Header.Clone()})
				mu.Unlock()
				if r.URL.Path != "/chat/completions" {
					<-releaseUnexpected
					http.NotFound(w, r)
					return
				}
				writeReceiptCut(w, r, "direct-cut")
			}))
			t.Cleanup(func() {
				releaseOnce.Do(func() { close(releaseUnexpected) })
				server.Close()
			})
			client, err := NewClient(Config{
				APIKey: "direct-key", BaseURL: server.URL, Model: "direct/model",
				Direct: true, BillingDoor: door, HTTPClient: server.Client(),
			})
			if err != nil {
				t.Fatal(err)
			}
			var reconciled atomic.Int64
			ctx := WithStreamObserver(t.Context(), func(StreamEvent) {})
			ctx = WithReconcile(ctx, func(Reconciled) { reconciled.Add(1) })
			if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err == nil {
				t.Fatal("the cut direct stream returned no error")
			}
			mu.Lock()
			got := append([]struct {
				method string
				path   string
				header http.Header
			}(nil), requests...)
			mu.Unlock()
			if len(got) != 1 || got[0].method != http.MethodPost || got[0].path != "/chat/completions" {
				t.Fatalf("direct service requests = %+v, want only POST /chat/completions", got)
			}
			if agent := got[0].header.Get("User-Agent"); agent != DirectUserAgent {
				t.Fatalf("direct completion User-Agent = %q, want %q", agent, DirectUserAgent)
			}
			for _, name := range []string{"HTTP-Referer", "X-Title", "X-OpenRouter-Title", "X-OpenRouter-Categories"} {
				if value := got[0].header.Get(name); value != "" {
					t.Fatalf("direct completion %s = %q, want no OpenRouter attribution", name, value)
				}
			}
			client.receiptMu.Lock()
			receiptWorkers := client.receiptRunning
			client.receiptMu.Unlock()
			if receiptWorkers != 0 {
				t.Fatalf("the direct cut started %d receipt workers, want none", receiptWorkers)
			}
			if reconciled.Load() != 0 {
				t.Fatalf("the direct cut entered receipt reconciliation %d times, want none", reconciled.Load())
			}
			releaseOnce.Do(func() { close(releaseUnexpected) })
		})
	}
}

// TestContracts2And4AnOpenRouterCutChasesAnIdentifiedReceipt is validation
// contract items 2 and 4: the real default-service client still asks for the
// missing receipt on a 404, and both that GET and its completion identify as
// codeaf while the GET keeps OpenRouter's attribution.
func TestContracts2And4AnOpenRouterCutChasesAnIdentifiedReceipt(t *testing.T) {
	defer shortenStallBounds(t, 30*time.Millisecond, 30*time.Millisecond)()
	var requests atomic.Int64
	completionHeaders := make(chan http.Header, 1)
	receiptRequests := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/chat/completions":
			completionHeaders <- r.Header.Clone()
			writeReceiptCut(w, r, "openrouter-cut")
		case "/generation":
			receiptRequests <- r.Clone(r.Context())
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(Config{
		APIKey: "router-key", BaseURL: server.URL, Model: "openrouter/test-model",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.wait = func(context.Context, time.Duration) error { return nil }
	results := make(chan Reconciled, 1)
	ctx := WithStreamObserver(t.Context(), func(StreamEvent) {})
	ctx = WithReconcile(ctx, func(result Reconciled) { results <- result })
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err == nil {
		t.Fatal("the cut OpenRouter stream returned no error")
	}
	result := receiptResult(t, results)
	if result.Found || result.Ref != "openrouter-cut" || !result.Billed.Empty() {
		t.Fatalf("404 receipt result = %+v, want the named call reported without invented figures", result)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("OpenRouter requests = %d, want the completion and one receipt GET", got)
	}
	completion := <-completionHeaders
	if got := completion.Get("User-Agent"); got != DirectUserAgent {
		t.Fatalf("completion User-Agent = %q, want %q", got, DirectUserAgent)
	}
	receipt := <-receiptRequests
	if receipt.Method != http.MethodGet || receipt.URL.Path != "/generation" || receipt.URL.Query().Get("id") != "openrouter-cut" {
		t.Fatalf("receipt request = %s %s, want GET /generation?id=openrouter-cut", receipt.Method, receipt.URL.String())
	}
	if got := receipt.Header.Get("User-Agent"); got != DirectUserAgent {
		t.Fatalf("receipt User-Agent = %q, want %q", got, DirectUserAgent)
	}
	if got := receipt.Header.Get("Authorization"); got != "Bearer router-key" {
		t.Fatalf("receipt Authorization = %q", got)
	}
	assertAttributed(t, receipt.Header, "receipt GET")
}

// TestACutStreamIsBilledFromTheProvidersReceipt is C1: the stream names its
// generation twice and then stalls, and only the provider's later figures are
// delivered. Native counts are deliberately different so the normalised pair
// is proved to be the ledger pair.
func TestACutStreamIsBilledFromTheProvidersReceipt(t *testing.T) {
	defer shortenStallBounds(t, time.Second, 30*time.Millisecond)()
	receiptAsked := make(chan struct{})
	releaseReceipt := make(chan struct{})
	var askedOnce, releaseOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher := w.(http.Flusher)
			fmt.Fprint(w, `data: {"id":"generation-c1","provider":"gusher","choices":[{"index":0,"delta":{"content":"the answer "}}]}`+"\n\n")
			flusher.Flush()
			fmt.Fprint(w, `data: {"id":"generation-c1","choices":[{"index":0,"delta":{"content":"begins"}}]}`+"\n\n")
			flusher.Flush()
			<-r.Context().Done()
		case "/generation":
			if r.URL.Query().Get("id") != "generation-c1" {
				t.Errorf("receipt id = %q, want generation-c1", r.URL.Query().Get("id"))
			}
			if r.Header.Get("Authorization") != "Bearer receipt-key" {
				t.Errorf("authorization = %q", r.Header.Get("Authorization"))
			}
			askedOnce.Do(func() { close(receiptAsked) })
			<-releaseReceipt
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":{"total_cost":0.37,"tokens_prompt":91,"tokens_completion":17,"native_tokens_prompt":190,"native_tokens_completion":71}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseReceipt) })
		server.Close()
	})
	client := receiptTestClient(t, server)
	results := make(chan Reconciled, 1)
	ctx := WithReconcile(WithStreamObserver(t.Context(), func(StreamEvent) {}), func(result Reconciled) {
		results <- result
	})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err == nil {
		t.Fatal("the stream did not reach its cut")
	}
	select {
	case <-receiptAsked:
	case <-time.After(5 * time.Second):
		t.Fatal("the cut call never asked for its receipt")
	}
	select {
	case result := <-results:
		t.Fatalf("the call was reported before its receipt arrived: %+v", result)
	default:
	}
	releaseOnce.Do(func() { close(releaseReceipt) })
	result := receiptResult(t, results)
	if !result.Found || result.Ref != "generation-c1" || result.Cost != 0.37 {
		t.Fatalf("reconciliation = %+v, want the generation's $0.37 receipt", result)
	}
	if result.PromptTokens != 91 || result.CompletionTokens != 17 {
		t.Fatalf("tokens = %d/%d, want the normalised 91/17", result.PromptTokens, result.CompletionTokens)
	}
}

// TestAReceiptFollowsTheGrowingSchedule pins D7 at the wait seam: an empty
// response means "not ready", every growing pause is recorded, and the fourth
// answer's own figures are the only ones delivered.
func TestAReceiptFollowsTheGrowingSchedule(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) < int64(receiptAttempts) {
			fmt.Fprint(w, `{"data":{}}`)
			return
		}
		fmt.Fprint(w, `{"data":{"total_cost":0.12,"tokens_prompt":30,"tokens_completion":4}}`)
	}))
	t.Cleanup(server.Close)
	client := receiptTestClient(t, server)
	var waits []time.Duration
	client.wait = func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	}
	results := make(chan Reconciled, 1)
	client.settle(WithReconcile(t.Context(), func(result Reconciled) { results <- result }),
		"sim/model", &ai.Response{ID: "scheduled-receipt"}, "stalled", 0)
	result := receiptResult(t, results)
	if !result.Found || result.Cost != 0.12 {
		t.Fatalf("scheduled receipt = %+v", result)
	}
	if requests.Load() != int64(receiptAttempts) {
		t.Fatalf("receipt requests = %d, want %d", requests.Load(), receiptAttempts)
	}
	if len(waits) != len(receiptRetrySchedule) {
		t.Fatalf("receipt waits = %v, want %d pauses", waits, len(receiptRetrySchedule))
	}
	for index, delay := range waits {
		if delay != receiptRetrySchedule[index] {
			t.Fatalf("receipt wait %d = %s, want %s", index+1, delay, receiptRetrySchedule[index])
		}
	}
}

// TestReceiptWorkersDoNotDrainTheWholeQueueSerially pins the small pool: four
// late requests may all reach the provider while the first is still waiting.
// The fixed width matters because a burst may make progress, but may not turn
// late bookkeeping into an unbounded burst of its own.
func TestReceiptWorkersDoNotDrainTheWholeQueueSerially(t *testing.T) {
	arrived := make(chan struct{}, receiptWorkerCount)
	release := make(chan struct{})
	var releaseOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		arrived <- struct{}{}
		<-release
		fmt.Fprint(w, `{"data":{"total_cost":0.01,"tokens_prompt":1,"tokens_completion":1}}`)
	}))
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		server.Close()
	})
	client := receiptTestClient(t, server)
	results := make(chan Reconciled, receiptWorkerCount)
	ctx := WithReconcile(t.Context(), func(result Reconciled) { results <- result })
	for index := range receiptWorkerCount {
		client.settle(ctx, "sim/model", &ai.Response{ID: fmt.Sprintf("pooled-%d", index)}, "stalled", 0)
	}
	for index := range receiptWorkerCount {
		select {
		case <-arrived:
		case <-time.After(time.Second):
			t.Fatalf("only %d of %d receipt workers reached the provider together", index, receiptWorkerCount)
		}
	}
	releaseOnce.Do(func() { close(release) })
	for range receiptWorkerCount {
		if result := receiptResult(t, results); !result.Found || result.Cost != 0.01 {
			t.Fatalf("pooled receipt = %+v", result)
		}
	}
}

// TestAnUnknownGenerationDoesNotRetireTheReceiptRoute keeps C2 and C4
// separate: a structured 404 is the route answering about one id, not evidence
// that the base lacks the route for every call that follows.
func TestAnUnknownGenerationDoesNotRetireTheReceiptRoute(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) <= int64(receiptAttempts) {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":{"message":"Unknown generation","code":404}}`)
			return
		}
		fmt.Fprint(w, `{"data":{"total_cost":0.09,"tokens_prompt":20,"tokens_completion":3}}`)
	}))
	t.Cleanup(server.Close)
	client := receiptTestClient(t, server)
	results := make(chan Reconciled, 2)
	ctx := WithReconcile(t.Context(), func(result Reconciled) { results <- result })
	client.settle(ctx, "sim/model", &ai.Response{ID: "unknown-generation"}, "stalled", 0)
	if first := receiptResult(t, results); first.Found {
		t.Fatalf("an unknown generation produced a receipt: %+v", first)
	}
	client.settle(ctx, "sim/model", &ai.Response{ID: "known-generation"}, "stalled", 0)
	if second := receiptResult(t, results); !second.Found || second.Cost != 0.09 {
		t.Fatalf("the id-specific 404 retired the whole route: %+v", second)
	}
	if requests.Load() != int64(receiptAttempts+1) {
		t.Fatalf("generation route requests = %d, want %d", requests.Load(), receiptAttempts+1)
	}
}

// TestAReceiptUsesNativeCountsOnlyWhenNormalisedCountsAreAbsent is D8's token
// dialect rule. A provider may carry both pairs, and a zero normalised pair is
// still present rather than permission to substitute different native counts.
func TestAReceiptUsesNativeCountsOnlyWhenNormalisedCountsAreAbsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("id") {
		case "native":
			fmt.Fprint(w, `{"data":{"total_cost":0.07,"native_tokens_prompt":44,"native_tokens_completion":6}}`)
		case "normalised-zero":
			fmt.Fprint(w, `{"data":{"total_cost":0.07,"tokens_prompt":0,"tokens_completion":0,"native_tokens_prompt":44,"native_tokens_completion":6}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := receiptTestClient(t, server)
	native, found, noRoute := client.fetchReceipt(t.Context(), "native")
	if !found || noRoute || native.PromptTokens != 44 || native.CompletionTokens != 6 {
		t.Fatalf("native-only receipt = %+v, found %t, no route %t", native, found, noRoute)
	}
	normalised, found, noRoute := client.fetchReceipt(t.Context(), "normalised-zero")
	if !found || noRoute || normalised.PromptTokens != 0 || normalised.CompletionTokens != 0 || normalised.Cost != 0.07 {
		t.Fatalf("normalised zero receipt = %+v, found %t, no route %t", normalised, found, noRoute)
	}
}

// TestAReceiptRouteThatCannotBeHadIsReportedMissing is the provider half of
// C2: a definite missing route produces one absent result and no invented
// figures for the session sink to bank.
func TestAReceiptRouteThatCannotBeHadIsReportedMissing(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	client := receiptTestClient(t, server)
	results := make(chan Reconciled, 1)
	client.settle(WithReconcile(t.Context(), func(result Reconciled) { results <- result }),
		"sim/model", &ai.Response{ID: "missing-c2"}, "stalled", 0)
	result := receiptResult(t, results)
	if result.Found || !result.Billed.Empty() {
		t.Fatalf("a missing route invented a receipt: %+v", result)
	}
	if requests.Load() != 1 {
		t.Fatalf("the missing route was asked %d times, want once", requests.Load())
	}
}

// TestAnAnonymousCutCountsOnlyAfterItProducedText is amended C3: text proves
// the provider got far enough to charge even when it never supplied an id, but
// a silent call with neither fact leaves no money claim behind. Neither case
// makes an unanswerable receipt request.
func TestAnAnonymousCutCountsOnlyAfterItProducedText(t *testing.T) {
	defer shortenStallBounds(t, 30*time.Millisecond, 30*time.Millisecond)()
	for _, testCase := range []struct {
		name         string
		frame        string
		wantReported bool
	}{
		{name: "text without an id is unpriced", frame: `data: {"choices":[{"index":0,"delta":{"content":"begun"}}]}` + "\n\n", wantReported: true},
		{name: "neither text nor an id is nothing", wantReported: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var receiptRequests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/generation" {
					receiptRequests.Add(1)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				if testCase.frame != "" {
					fmt.Fprint(w, testCase.frame)
				}
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			t.Cleanup(server.Close)
			client := receiptTestClient(t, server)
			results := make(chan Reconciled, 1)
			var unpriced atomic.Int64
			ctx := WithReconcile(WithStreamObserver(t.Context(), func(StreamEvent) {}), func(result Reconciled) {
				if !result.Found {
					unpriced.Add(1)
				}
				results <- result
			})
			if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err == nil {
				t.Fatal("the anonymous stream did not reach its cut")
			}
			if testCase.wantReported {
				result := receiptResult(t, results)
				if result.Found || result.Ref != "" {
					t.Fatalf("anonymous cut = %+v, want one missing result with no ref", result)
				}
			} else {
				select {
				case result := <-results:
					t.Fatalf("a call with neither id nor text was reported as money: %+v", result)
				default:
				}
			}
			if receiptRequests.Load() != 0 {
				t.Fatalf("an anonymous cut made %d receipt requests, want none", receiptRequests.Load())
			}
			wantUnpriced := int64(0)
			if testCase.wantReported {
				wantUnpriced = 1
			}
			if got := unpriced.Load(); got != wantUnpriced {
				t.Fatalf("the anonymous cut moved the missing-price count by %d, want %d", got, wantUnpriced)
			}
		})
	}
}

// TestABaseThatHasNoReceiptRouteIsNotAskedTwice is C4: the first 404 decides
// the capability for the memo's bounded life, so the next unpriced call at the
// same base is answered from that fact.
func TestABaseThatHasNoReceiptRouteIsNotAskedTwice(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	client := receiptTestClient(t, server)
	results := make(chan Reconciled, 2)
	ctx := WithReconcile(t.Context(), func(result Reconciled) { results <- result })
	client.settle(ctx, "sim/model", &ai.Response{ID: "first-c4"}, "stalled", 0)
	_ = receiptResult(t, results)
	client.settle(ctx, "sim/model", &ai.Response{ID: "second-c4"}, "stalled", 0)
	_ = receiptResult(t, results)
	if requests.Load() != 1 {
		t.Fatalf("a base without the route was asked %d times, want once", requests.Load())
	}
}

// TestReconcilingNeverWaitsOnTheTurn is C5: a full bounded queue reports the
// overflow immediately instead of waiting for the worker or touching HTTP.
func TestReconcilingNeverWaitsOnTheTurn(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	t.Cleanup(server.Close)
	client := receiptTestClient(t, server)
	// Reserve all worker slots without starting them, so the queue remains
	// deterministically full for this turn-path assertion.
	client.receiptRunning = receiptWorkerCount
	for range receiptQueueDepth {
		client.receipts <- receiptWork{}
	}
	results := make(chan Reconciled, 1)
	var unpriced atomic.Int64
	returned := make(chan struct{})
	go func() {
		client.settle(WithReconcile(t.Context(), func(result Reconciled) {
			if !result.Found {
				unpriced.Add(1)
			}
			results <- result
		}),
			"sim/model", &ai.Response{ID: "overflow-c5"}, "torn", 0)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("a full receipt queue made settle wait")
	}
	if result := receiptResult(t, results); result.Found || result.Ref != "overflow-c5" {
		t.Fatalf("overflow result = %+v, want the named call reported missing", result)
	}
	if requests.Load() != 0 {
		t.Fatalf("an overflowing hand-off made %d provider requests, want none", requests.Load())
	}
	if unpriced.Load() != 1 {
		t.Fatalf("the overflowing hand-off moved the missing-price count by %d, want one", unpriced.Load())
	}
}

// TestAHedgeLoserIsBilledFromItsOwnReceipt is the provider half of C6: a
// cancelled rescue arm keeps the generation id it saw and marks the later
// receipt as hedged.
func TestAHedgeLoserIsBilledFromItsOwnReceipt(t *testing.T) {
	seen := make(chan struct{})
	var seenOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/generation" {
			fmt.Fprint(w, `{"data":{"total_cost":0.19,"tokens_prompt":43,"tokens_completion":9}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `data: {"id":"hedge-c6","provider":"rescue-lane","choices":[{"index":0,"delta":{"content":"losing"}}]}`+"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	client := receiptTestClient(t, server)
	results := make(chan Reconciled, 1)
	callCtx, cancel := context.WithCancel(t.Context())
	callCtx = withHedgeLane(callCtx, "rescue-lane")
	callCtx = WithReconcile(callCtx, func(result Reconciled) { results <- result })
	callCtx = WithStreamObserver(callCtx, func(event StreamEvent) {
		if event.Kind == StreamDelta {
			seenOnce.Do(func() { close(seen) })
		}
	})
	callDone := make(chan error, 1)
	go func() {
		_, err := client.CompleteWithMessages(callCtx, userMessages("hello"))
		callDone <- err
	}()
	select {
	case <-seen:
	case <-time.After(5 * time.Second):
		t.Fatal("the rescue arm never delivered its first frame")
	}
	cancel()
	if err := <-callDone; err == nil {
		t.Fatal("the cancelled rescue arm returned no error")
	}
	result := receiptResult(t, results)
	if !result.Found || !result.Hedged || result.Ref != "hedge-c6" || result.Cost != 0.19 {
		t.Fatalf("hedge reconciliation = %+v, want its own marked receipt", result)
	}
}

// TestAPricedStreamIsBilledOnceAndAsksForNoReceipt is C8: the terminal usage
// frame stays entirely on the old billing door even when both sinks are armed.
func TestAPricedStreamIsBilledOnceAndAsksForNoReceipt(t *testing.T) {
	var receiptRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/generation" {
			receiptRequests.Add(1)
			http.Error(w, "unexpected", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `data: {"id":"priced-c8","choices":[{"index":0,"delta":{"content":"done"}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"id":"priced-c8","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":3,"cost":0.08}}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	client := receiptTestClient(t, server)
	billed := make(chan Billed, 2)
	reconciled := make(chan Reconciled, 1)
	ctx := WithStreamObserver(t.Context(), func(StreamEvent) {})
	ctx = WithBilling(ctx, func(row Billed) { billed <- row })
	ctx = WithReconcile(ctx, func(row Reconciled) { reconciled <- row })
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	row := <-billed
	if row.Cost != 0.08 || row.PromptTokens != 12 || row.CompletionTokens != 3 {
		t.Fatalf("ordinary bill = %+v", row)
	}
	select {
	case duplicate := <-billed:
		t.Fatalf("the priced stream was billed twice: %+v", duplicate)
	default:
	}
	select {
	case late := <-reconciled:
		t.Fatalf("the priced stream entered the receipt door: %+v", late)
	default:
	}
	if receiptRequests.Load() != 0 {
		t.Fatalf("a priced stream asked for %d receipts, want none", receiptRequests.Load())
	}
}

// A role client may be short lived. Its final receipt must release its worker
// rather than keep the client, transport and account credentials alive forever.
func TestReceiptWorkersRetireAfterTheirQueueDrains(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data":{"total_cost":0.01}}`)
	}))
	defer server.Close()
	client := receiptTestClient(t, server)
	for round := range 2 {
		results := make(chan Reconciled, 1)
		client.receiptRunning = 1
		client.receipts <- receiptWork{result: Reconciled{Ref: fmt.Sprint(round)}, sink: func(r Reconciled) { results <- r }}
		done := make(chan struct{})
		go func() { client.runReceipts(); close(done) }()
		if result := receiptResult(t, results); !result.Found {
			t.Fatal("receipt was lost")
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("the empty receipt queue retained its worker")
		}
		if client.receiptRunning != 0 {
			t.Fatalf("%d workers still registered", client.receiptRunning)
		}
	}
	results := make(chan Reconciled, 1)
	client.settle(WithReconcile(t.Context(), func(r Reconciled) { results <- r }),
		"sim/model", &ai.Response{ID: "after-retirement"}, "torn", 0)
	if result := receiptResult(t, results); !result.Found {
		t.Fatal("a receipt arriving after retirement never restarted its worker")
	}
}

// TestAQueuedReceiptIsOwedUntilItsSinkHasBankedIt pins the pending door that
// lets a run wait for the price of the call it was cut in the middle of: the
// work is told a receipt is owed before the fetch begins, and told it was
// answered only after the sink has had the money — never the other way round,
// or a caller could read its total in the gap. A call that queues no receipt
// (a usage block, or no id and no text) owes nothing.
func TestAQueuedReceiptIsOwedUntilItsSinkHasBankedIt(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		fmt.Fprint(w, `{"data":{"total_cost":0.058,"tokens_prompt":52139,"tokens_completion":4895}}`)
	}))
	t.Cleanup(server.Close)
	client := receiptTestClient(t, server)
	var mu sync.Mutex
	var owed int
	var order []string
	pending := func() func() {
		mu.Lock()
		owed++
		mu.Unlock()
		return func() {
			mu.Lock()
			owed--
			order = append(order, "answered")
			mu.Unlock()
		}
	}
	results := make(chan Reconciled, 1)
	ctx := WithReceiptPending(WithReconcile(t.Context(), func(result Reconciled) {
		mu.Lock()
		order = append(order, "banked")
		mu.Unlock()
		results <- result
	}), pending)

	// Neither of these queues a receipt, so neither is owed.
	cost := 0.01
	client.settle(ctx, "sim/model", &ai.Response{Usage: &ai.Usage{PromptTokens: 1, Cost: &cost}}, "stalled", 0)
	client.settle(ctx, "sim/model", &ai.Response{}, "stalled", 0)
	mu.Lock()
	if owed != 0 {
		mu.Unlock()
		t.Fatalf("owed = %d after two calls that queued no receipt", owed)
	}
	mu.Unlock()

	client.settle(ctx, "sim/model", &ai.Response{ID: "cut-in-flight"}, "stopped", 12)
	mu.Lock()
	if owed != 1 {
		mu.Unlock()
		t.Fatalf("owed = %d while the receipt is being fetched, want 1", owed)
	}
	mu.Unlock()
	close(release)
	if result := receiptResult(t, results); !result.Found || result.Cost != 0.058 {
		t.Fatalf("receipt = %+v", result)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		settled := owed == 0 && len(order) == 2
		got := append([]string(nil), order...)
		mu.Unlock()
		if settled {
			if got[0] != "banked" || got[1] != "answered" {
				t.Fatalf("order = %v, want the money banked before the receipt is marked answered", got)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the receipt was never marked answered: order %v", got)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
