package provider

// A CALL CANCELLED BEFORE ITS FIRST STREAMED FRAME IS STILL A CALL SOMEBODY PAID
// FOR (#2566).
//
// The first SSE frame is what names the provider's generation, and the receipt
// that prices a call whose usage block never arrived is asked for by that id. A
// person's Esc that cut the socket before the frame landed threw the id away, so
// the charge reached neither a priced row nor the unbilled marker. These tests
// are the contract the fix was written against:
//
//	C1  cancelled after the request reached the provider and before its first
//	    frame, and the frame arrives within the hold: the call's receipt is
//	    booked — the reconcile sink is told, never left silent.
//	C2  cancelled before it is sent: it returns at once with the caller's error
//	    and nothing reaches the provider.
//	C3  cancelled on a silent wire: the hold is bounded.
//	C4  a call nobody reconciles (or a direct service, which has no receipt to
//	    ask for) is cut at once, with no added wait.
//	C5  an abandoned race keeps the caller's pending-spend token until its cut
//	    arm has settled, and gives it back within [abandonGrace] even if an arm
//	    never reports.
//
// TWO DOORS. Every shipped build installs the waiting controller at internal/
// lane's init, so a call is raced: the caller is answered by
// [hedgeRace.abandon] the moment it lets go and only the arm holds its socket.
// A build with no controller runs the bare stream loop, where the caller itself
// waits out the hold. C1 to C4 run through both, and the latencies they log are
// the added Esc wait on each.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	lanes "github.com/Agent-Field/codeaf/internal/lane"
)

// cancelLatencyCeiling is the bounded-wait assertion C3 makes: the hold plus a
// generous slack for a shared box. A hold that waited for anything but its own
// timer would blow straight through it.
const cancelLatencyCeiling = 500 * time.Millisecond

// cancelCallCeiling is a hang detector for a step that should already have
// happened; a passing test never reaches it.
const cancelCallCeiling = 10 * time.Second

// cancelDoors are the two roads a streamed call can take to the wire.
var cancelDoors = []struct {
	name  string
	raced bool
}{
	{name: "raced", raced: true},
	{name: "unraced", raced: false},
}

// onDoor puts the test on one of [cancelDoors]. The raced door is the shipped
// controller and is only checked for; the unraced door takes it away and puts
// back exactly what it found.
func onDoor(t *testing.T, raced bool) {
	t.Helper()
	if raced {
		if lanes.Controller() == nil {
			t.Fatal("no waiting controller is installed, so no call here is raced")
		}
		return
	}
	shipped := lanes.SetController(nil)
	t.Cleanup(func() { lanes.SetController(shipped) })
}

// calledBack is what one streamed call handed back, and when.
type calledBack struct {
	err error
	at  time.Time
}

// streamInBackground starts one streamed call on ctx and answers the channel its
// outcome arrives on, stamped the instant the call returned.
func streamInBackground(ctx context.Context, client *Client) <-chan calledBack {
	done := make(chan calledBack, 1)
	go func() {
		_, err := client.CompleteWithMessages(ctx, userMessages("hello"))
		done <- calledBack{err: err, at: time.Now()}
	}()
	return done
}

// awaitCall waits for a call that has been cancelled to come back, and checks
// it came back with the caller's own cancellation.
func awaitCall(t *testing.T, done <-chan calledBack) calledBack {
	t.Helper()
	select {
	case back := <-done:
		if !errors.Is(back.err, context.Canceled) {
			t.Fatalf("the cancelled call answered %v, want the caller's own cancellation", back.err)
		}
		return back
	case <-time.After(cancelCallCeiling):
		t.Fatal("the cancelled call never returned")
		return calledBack{}
	}
}

// awaitSignal waits for one step the test sequences on.
func awaitSignal(t *testing.T, signal <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(cancelCallCeiling):
		t.Fatalf("%s never happened", what)
	}
}

// openedObserver is a stream observer that says once when the call's headers
// have landed and its decode loop is about to wait for the first frame.
func openedObserver(opened chan<- struct{}) StreamObserver {
	var once sync.Once
	return func(event StreamEvent) {
		if event.Kind == StreamStarted {
			once.Do(func() { close(opened) })
		}
	}
}

// readWholeRequest consumes the request body. A handler that has read its body
// to the end is one net/http watches the connection for, so its request context
// ends the moment the client cuts the call — which is how these tests see a
// cut from the provider's side of the wire.
func readWholeRequest(r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
}

// TestC1ACallCancelledBeforeItsFirstFrameIsStillBooked is C1. The provider is
// held until the caller has cancelled, and only then sends the frame that names
// the generation — exactly the Esc of #2566. The call must still reach the
// reconcile sink with that id: booked here, because the receipt route prices
// it. A call that reached neither the booked row nor the unbilled marker is the
// defect.
func TestC1ACallCancelledBeforeItsFirstFrameIsStillBooked(t *testing.T) {
	for _, door := range cancelDoors {
		for _, row := range []struct {
			name string
			// headersFirst sends the response headers before the cancel, so the
			// cancel lands while the decode loop waits for the first frame;
			// without it the cancel lands while the request still waits for
			// headers.
			headersFirst bool
		}{
			{name: "cancelled while waiting for headers"},
			{name: "cancelled after headers, before the first frame", headersFirst: true},
		} {
			t.Run(door.name+"/"+row.name, func(t *testing.T) {
				onDoor(t, door.raced)
				id := fmt.Sprintf("gen-before-first-frame-%t-%t", door.raced, row.headersFirst)
				arrived := make(chan struct{})
				sendFirst := make(chan struct{})
				var arrivedOnce, sendOnce sync.Once
				var receiptAsks atomic.Int64
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/chat/completions":
						readWholeRequest(r)
						flusher := w.(http.Flusher)
						if row.headersFirst {
							w.Header().Set("Content-Type", "text/event-stream")
							w.WriteHeader(http.StatusOK)
							flusher.Flush()
						}
						arrivedOnce.Do(func() { close(arrived) })
						select {
						case <-sendFirst:
						case <-r.Context().Done():
							return
						}
						if !row.headersFirst {
							w.Header().Set("Content-Type", "text/event-stream")
							w.WriteHeader(http.StatusOK)
						}
						fmt.Fprintf(w, `data: {"id":%q,"choices":[{"index":0,"delta":{"content":"begun"}}]}`+"\n\n", id)
						flusher.Flush()
						<-r.Context().Done()
					case "/generation":
						receiptAsks.Add(1)
						if got := r.URL.Query().Get("id"); got != id {
							t.Errorf("receipt asked for %q, want %q", got, id)
						}
						fmt.Fprint(w, `{"data":{"total_cost":0.21,"tokens_prompt":64,"tokens_completion":5}}`)
					default:
						http.NotFound(w, r)
					}
				}))
				t.Cleanup(func() {
					sendOnce.Do(func() { close(sendFirst) })
					server.Close()
				})
				client := receiptTestClient(t, server)

				results := make(chan Reconciled, 2)
				opened := make(chan struct{})
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				ctx = WithReconcile(WithStreamObserver(ctx, openedObserver(opened)), func(result Reconciled) { results <- result })

				done := streamInBackground(ctx, client)
				awaitSignal(t, arrived, "the request reaching the provider")
				if row.headersFirst {
					awaitSignal(t, opened, "the stream opening on the caller's side")
				}
				cancel()
				// The frame is released only after the cancel, so the caller can
				// not have read the id before letting go; it arrives well inside
				// the hold.
				sendOnce.Do(func() { close(sendFirst) })
				awaitCall(t, done)

				// BOOKED OR UNBILLED, NEVER NEITHER — and booked, because the id
				// was had and the route priced it.
				result := receiptResult(t, results)
				if !result.Found || result.Ref != id || result.Cost != 0.21 || result.PromptTokens != 64 || result.CompletionTokens != 5 {
					t.Fatalf("reconciliation = %+v, want the generation %s booked at $0.21", result, id)
				}
				select {
				case duplicate := <-results:
					t.Fatalf("the one call was reconciled twice: %+v", duplicate)
				default:
				}
				if got := receiptAsks.Load(); got != 1 {
					t.Fatalf("the receipt was asked for %d times, want once", got)
				}
			})
		}
	}
}

// TestC2ACallCancelledBeforeItIsSentNeverReachesTheProvider is C2: a caller who
// has already let go is answered with their own error at once, and no request —
// so no charge — is made on their behalf.
func TestC2ACallCancelledBeforeItIsSentNeverReachesTheProvider(t *testing.T) {
	for _, door := range cancelDoors {
		t.Run(door.name, func(t *testing.T) {
			onDoor(t, door.raced)
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				http.NotFound(w, r)
			}))
			t.Cleanup(server.Close)
			client := receiptTestClient(t, server)

			owed := newOwedCount()
			ctx, cancel := context.WithCancel(t.Context())
			ctx = WithReconcile(WithStreamObserver(ctx, func(StreamEvent) {}), func(result Reconciled) {
				t.Errorf("a call that was never sent was reconciled: %+v", result)
			})
			ctx = WithReceiptPending(ctx, owed.owe)
			cancel()
			began := time.Now()
			back := awaitCall(t, streamInBackground(ctx, client))
			took := back.at.Sub(began)
			t.Logf("%s: pre-cancelled call returned in %s", door.name, took)
			if took > cancelLatencyCeiling {
				t.Fatalf("the pre-cancelled call took %s to return, want under %s", took, cancelLatencyCeiling)
			}
			// A raced call's arm may run on behind the caller. When the race
			// answered by abandoning, its pending token is still held here and
			// comes back once the drain has heard the arm — the moment it is safe
			// to say the arm sent nothing. When the arm's own report reached the
			// race first, nothing is owed and the arm has already finished.
			if owing, _ := owed.now(); owing > 0 {
				owed.awaitOwed(t, 0, cancelCallCeiling)
			}
			if got := requests.Load(); got != 0 {
				t.Fatalf("the provider saw %d requests from a call cancelled before it was sent, want none", got)
			}
		})
	}
}

// silentWire is a provider that accepts the request and never writes a frame.
// With headers it opens the stream and stops; without them it never answers at
// all. It says when the request has reached it, and when the client cut it.
func silentWire(t *testing.T, headers bool) (server *httptest.Server, arrived <-chan struct{}, cut <-chan time.Time) {
	t.Helper()
	arrivals := make(chan struct{}, 1)
	cuts := make(chan time.Time, 1)
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		readWholeRequest(r)
		if headers {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
		}
		arrivals <- struct{}{}
		<-r.Context().Done()
		cuts <- time.Now()
	}))
	t.Cleanup(server.Close)
	return server, arrivals, cuts
}

// wireLatency is how long one cancel took to reach the caller and the wire.
type wireLatency struct {
	returned time.Duration
	cut      time.Duration
}

// cancelOnTheWire runs one streamed call against a silent wire, cancels it once
// the request is there (and, with headers, once the stream has opened), and
// answers how long the caller waited and how long the socket stayed open after
// the cancel.
func cancelOnTheWire(t *testing.T, client *Client, arrived <-chan struct{}, cut <-chan time.Time, headers bool, arm func(context.Context) context.Context) wireLatency {
	t.Helper()
	opened := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx = arm(WithStreamObserver(ctx, openedObserver(opened)))
	done := streamInBackground(ctx, client)
	awaitSignal(t, arrived, "the request reaching the provider")
	if headers {
		awaitSignal(t, opened, "the stream opening on the caller's side")
	}
	cancelled := time.Now()
	cancel()
	back := awaitCall(t, done)
	select {
	case at := <-cut:
		return wireLatency{returned: back.at.Sub(cancelled), cut: at.Sub(cancelled)}
	case <-time.After(cancelCallCeiling):
		t.Fatal("the cancelled call never cut its stream")
		return wireLatency{}
	}
}

// TestC3TheHoldOnASilentWireIsBounded is C3: a call that reconciles receipts
// waits for its generation id, but a provider that never sends one cannot hold
// the socket — or, on the unraced door, the caller — past [receiptStreamIDWait]
// and a little slack.
func TestC3TheHoldOnASilentWireIsBounded(t *testing.T) {
	for _, door := range cancelDoors {
		for _, headers := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/headers sent %t", door.name, headers), func(t *testing.T) {
				onDoor(t, door.raced)
				server, arrived, cut := silentWire(t, headers)
				client := receiptTestClient(t, server)
				took := cancelOnTheWire(t, client, arrived, cut, headers, func(ctx context.Context) context.Context {
					return WithReconcile(ctx, func(Reconciled) {})
				})
				t.Logf("%s, reconciled, silent wire (headers %t): cancel to return %s, cancel to wire cut %s (hold %s)",
					door.name, headers, took.returned, took.cut, receiptStreamIDWait)
				if took.cut > cancelLatencyCeiling {
					t.Fatalf("the stream stayed open %s after the cancel on a silent wire, want under %s", took.cut, cancelLatencyCeiling)
				}
				if took.returned > cancelLatencyCeiling {
					t.Fatalf("the caller waited %s after the cancel on a silent wire, want under %s", took.returned, cancelLatencyCeiling)
				}
			})
		}
	}
}

// TestC4ACallWithNothingToReconcileIsCutAtOnce is C4: the hold is only for a
// call whose receipt can be asked for. A call with no reconcile sink, and a
// direct service — which has no receipt route — are cut the moment the caller
// lets go.
//
// THE PROOF IS A CUT INSIDE THE HOLD. A held stream can never be cut before
// [receiptStreamIDWait] has elapsed, because the hold is a timer; so one cut
// faster than that proves there was no hold, and a slow box is allowed a few
// tries to show it without ever letting a held stream pass.
func TestC4ACallWithNothingToReconcileIsCutAtOnce(t *testing.T) {
	for _, door := range cancelDoors {
		for _, row := range []struct {
			name   string
			direct bool
			sink   bool
		}{
			{name: "no reconcile sink"},
			{name: "a direct service with a reconcile sink", direct: true, sink: true},
		} {
			t.Run(door.name+"/"+row.name, func(t *testing.T) {
				onDoor(t, door.raced)
				arm := func(ctx context.Context) context.Context {
					if row.sink {
						return WithReconcile(ctx, func(Reconciled) {})
					}
					return ctx
				}
				const tries = 5
				var fastest time.Duration
				for try := range tries {
					// A wire of its own per try, so nothing from an earlier try
					// can be read as this one's arrival or cut.
					server, arrived, cut := silentWire(t, true)
					client := receiptTestClient(t, server)
					if row.direct {
						direct, err := NewClient(Config{
							APIKey: "direct-key", BaseURL: server.URL, Model: "direct/model",
							Direct: true, BillingDoor: "pay-as-you-go", HTTPClient: server.Client(),
						})
						if err != nil {
							t.Fatal(err)
						}
						direct.velocity = newVelocityLedger()
						client = direct
					}
					took := cancelOnTheWire(t, client, arrived, cut, true, arm)
					t.Logf("%s, %s: cancel to return %s, cancel to wire cut %s (try %d)", door.name, row.name, took.returned, took.cut, try+1)
					if try == 0 || took.cut < fastest {
						fastest = took.cut
					}
					if took.cut < receiptStreamIDWait {
						return
					}
				}
				t.Fatalf("%s: the fastest cut came %s after the cancel over %d tries, never inside the %s hold — the stream was held",
					row.name, fastest, tries, receiptStreamIDWait)
			})
		}
	}
}

// owedCount is a [ReceiptPending] that counts what is owed and keeps the facts
// C5 asserts: whether the count ever came back to zero, and each value it moved
// to, in order.
type owedCount struct {
	mu      sync.Mutex
	n       int
	zeroes  int
	changed chan int
}

func newOwedCount() *owedCount { return &owedCount{changed: make(chan int, 16)} }

func (o *owedCount) owe() func() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.n++
	o.changed <- o.n
	var once sync.Once
	return func() { once.Do(o.answered) }
}

func (o *owedCount) answered() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.n--
	if o.n == 0 {
		o.zeroes++
	}
	o.changed <- o.n
}

func (o *owedCount) now() (owed, zeroes int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.n, o.zeroes
}

// awaitOwed waits for the count's next move to land on want, and answers when
// it did. Every move is read in order, so a count that passed through want on
// the way somewhere else is not mistaken for one that stopped there.
func (o *owedCount) awaitOwed(t *testing.T, want int, ceiling time.Duration) time.Time {
	t.Helper()
	deadline := time.After(ceiling)
	for {
		select {
		case n := <-o.changed:
			if n == want {
				return time.Now()
			}
		case <-deadline:
			owed, _ := o.now()
			t.Fatalf("the pending count stayed at %d, want it to reach %d within %s", owed, want, ceiling)
			return time.Time{}
		}
	}
}

// TestC5ACancelledRaceKeepsItsPendingSpendUntilTheArmsReceiptIsOwed is C5's
// first half, end to end on the shipped raced door. The caller lets go before
// the first frame and is answered at once by [hedgeRace.abandon]; the arm holds
// its socket, reads the frame that names the generation, and only then reaches
// [Client.settle] and queues its receipt. A waiter on the work's pending spend
// must never see zero owed across that gap, and the race's own token must come
// back as soon as the arm has reported — not at the end of [abandonGrace].
func TestC5ACancelledRaceKeepsItsPendingSpendUntilTheArmsReceiptIsOwed(t *testing.T) {
	onDoor(t, true)
	const id = "gen-abandoned-arm"
	arrived := make(chan struct{})
	sendFirst := make(chan struct{})
	receiptAsked := make(chan struct{})
	releaseReceipt := make(chan struct{})
	var arrivedOnce, sendOnce, askedOnce, releaseOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			readWholeRequest(r)
			arrivedOnce.Do(func() { close(arrived) })
			select {
			case <-sendFirst:
			case <-r.Context().Done():
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `data: {"id":%q,"choices":[{"index":0,"delta":{"content":"begun"}}]}`+"\n\n", id)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case "/generation":
			askedOnce.Do(func() { close(receiptAsked) })
			<-releaseReceipt
			fmt.Fprint(w, `{"data":{"total_cost":0.04,"tokens_prompt":20,"tokens_completion":2}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		sendOnce.Do(func() { close(sendFirst) })
		releaseOnce.Do(func() { close(releaseReceipt) })
		server.Close()
	})
	client := receiptTestClient(t, server)

	owed := newOwedCount()
	var banked atomic.Bool
	results := make(chan Reconciled, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx = WithReconcile(WithStreamObserver(ctx, func(StreamEvent) {}), func(result Reconciled) {
		banked.Store(true)
		results <- result
	})
	ctx = WithReceiptPending(ctx, owed.owe)

	done := streamInBackground(ctx, client)
	awaitSignal(t, arrived, "the request reaching the provider")
	cancelled := time.Now()
	cancel()
	awaitCall(t, done)

	// THE SEAM. The caller has its answer and the arm has not yet seen a frame,
	// so nothing but the race's own token can be owed — and something must be.
	if n, zeroes := owed.now(); n != 1 || zeroes != 0 {
		t.Fatalf("pending spend = %d (back to zero %d times) once the cancelled call returned, want the race's one token held", n, zeroes)
	}
	owed.awaitOwed(t, 1, cancelCallCeiling)
	sendOnce.Do(func() { close(sendFirst) })

	// The arm settles: its receipt is owed beside the race's token, and asked for.
	owed.awaitOwed(t, 2, cancelCallCeiling)
	awaitSignal(t, receiptAsked, "the cut arm asking for its receipt")
	// The race's token comes back once the drain has heard the arm, while the
	// arm's receipt is still owed.
	gaveBack := owed.awaitOwed(t, 1, cancelCallCeiling)
	if held := gaveBack.Sub(cancelled); held >= abandonGrace {
		t.Fatalf("the race held its token %s after the cancel, want it given back when the arm reported rather than at the end of the %s grace", held, abandonGrace)
	}
	if _, zeroes := owed.now(); zeroes != 0 {
		t.Fatalf("pending spend reached zero %d times while the cut arm's receipt was still owed", zeroes)
	}

	releaseOnce.Do(func() { close(releaseReceipt) })
	result := receiptResult(t, results)
	if !result.Found || result.Hedged || result.Ref != id || result.Cost != 0.04 {
		t.Fatalf("the abandoned arm's receipt = %+v, want its own $0.04 booked", result)
	}
	owed.awaitOwed(t, 0, cancelCallCeiling)
	if !banked.Load() {
		t.Fatal("pending spend reached zero before the receipt was banked")
	}
	if _, zeroes := owed.now(); zeroes != 1 {
		t.Fatalf("pending spend reached zero %d times, want once, after the receipt was banked", zeroes)
	}
}

// TestC5AnArmThatNeverReportsReleasesThePendingSpendWithinTheGrace is C5's
// second half: the race's token is a bridge, not a leak. An arm that never
// reports holds it for the drain's whole grace and not a moment past it.
//
// The lower bound is exact rather than a guess: the grace is a timer started
// inside abandon, so the token can not come back before it has run. The upper
// bound is the bounded-wait assertion this file allows itself.
func TestC5AnArmThatNeverReportsReleasesThePendingSpendWithinTheGrace(t *testing.T) {
	owed := newOwedCount()
	ctx, cancel := context.WithCancel(WithReceiptPending(t.Context(), owed.owe))
	cancel()

	// A bare race of one arm whose result never arrives, which is all
	// [hedgeRace.abandon] reads.
	race := &hedgeRace{results: make(chan armResult, 1), arms: []*hedgeArm{{index: 0}}, winner: -1, speaker: -1}
	began := time.Now()
	if _, _, err := race.abandon(ctx, map[int]armResult{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("abandon answered %v, want the caller's own cancellation", err)
	}
	answeredIn := time.Since(began)
	if n, _ := owed.now(); n != 1 {
		t.Fatalf("pending spend = %d once abandon returned, want the race's one token held", n)
	}
	owed.awaitOwed(t, 1, cancelCallCeiling)
	gaveBack := owed.awaitOwed(t, 0, abandonGrace+cancelLatencyCeiling+time.Second)
	held := gaveBack.Sub(began)
	t.Logf("abandon answered its caller in %s; the token was held %s for an arm that never reported (grace %s)", answeredIn, held, abandonGrace)
	if held < abandonGrace {
		t.Fatalf("the token came back after %s, before the %s grace an arm is given to report", held, abandonGrace)
	}
	if held > abandonGrace+cancelLatencyCeiling {
		t.Fatalf("the token was held %s for a silent arm, want it given back within the %s grace and %s slack", held, abandonGrace, cancelLatencyCeiling)
	}
}
