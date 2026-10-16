package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/lane/lanestub"
	"github.com/Agent-Field/codeaf/internal/taxonomy"
)

// Every test below drives the real Client over a real loopback HTTP server: the
// observer is installed on a context and the assertions are about what the
// transport actually sent, not about a hook called in isolation.

// recToken records what one send's token was told.
type recToken struct {
	mu   sync.Mutex
	ends []TransportEnd
	fins []FinancialObservation
}

func (r *recToken) EndTransport(e TransportEnd) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ends = append(r.ends, e)
}

func (r *recToken) ObserveFinancial(o FinancialObservation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fins = append(r.fins, o)
}

func (r *recToken) seen() ([]TransportEnd, []FinancialObservation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]TransportEnd(nil), r.ends...), append([]FinancialObservation(nil), r.fins...)
}

// recObserver is a pointer type so it is comparable, like a real authority.
type recObserver struct {
	mu     sync.Mutex
	calls  int // every BeforeAttempt, denied or not
	infos  []ActualAttemptInfo
	tokens []*recToken
	// deny, when set, answers the nth admission (1-based). A nil token with a
	// nil error is the "forgot the token" refusal.
	deny     func(n int) (refuse bool, err error)
	rollback bool
	rolled   []*recToken
}

func (o *recObserver) BeforeAttempt(_ context.Context, info ActualAttemptInfo) (ActualAttemptToken, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls++
	token := &recToken{}
	if o.deny != nil {
		if refuse, err := o.deny(o.calls); refuse {
			if o.rollback {
				// A careless observer: hands back a token AND an error.
				o.rolled = append(o.rolled, token)
				return token, err
			}
			if err == nil {
				return nil, nil
			}
			return nil, err
		}
	}
	o.infos = append(o.infos, info)
	o.tokens = append(o.tokens, token)
	return token, nil
}

func (o *recObserver) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.infos)
}

func (o *recObserver) at(i int) (ActualAttemptInfo, *recToken) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.infos[i], o.tokens[i]
}

func observed(t *testing.T, ctx context.Context, o ActualAttemptObserver, owner WorkOwner) context.Context {
	t.Helper()
	ctx, err := WithActualAttemptObserver(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	if owner.set() {
		if ctx, err = WithWorkOwner(ctx, owner); err != nil {
			t.Fatal(err)
		}
	}
	return ctx
}

// observedClient is the receipt-test client plus the declaration a custom
// HTTP client needs to be countable. The server's own client is a plain
// *http.Transport.
func observedClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	client := receiptTestClient(t, server)
	client.config.SingleSendTransport = true
	return client
}

func wholeAnswer(w http.ResponseWriter, usage string) {
	w.Header().Set("Content-Type", "application/json")
	if usage != "" {
		usage = "," + usage
	}
	fmt.Fprintf(w, `{"id":"g-1","model":"sim/model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"done"}}]%s}`, usage)
}

func wantFins(t *testing.T, what string, token *recToken, want ...FinancialObservation) {
	t.Helper()
	_, got := token.seen()
	if len(got) != len(want) {
		t.Fatalf("%s: financial = %+v, want %+v", what, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: financial = %+v, want %+v", what, got, want)
		}
	}
}

func wantEnds(t *testing.T, what string, token *recToken, want ...TransportEnd) {
	t.Helper()
	got, _ := token.seen()
	if len(got) != len(want) {
		t.Fatalf("%s: transport ends = %v, want %v", what, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: transport ends = %v, want %v", what, got, want)
		}
	}
}

// A retried send is a separate paid send: the 503 and the 200 each have their
// own ID and token, the owner is copied to both, and the refusal is unknown
// while the priced answer is known — with no billing sink armed anywhere.
func TestObservedRetryHasItsOwnTokenPerDo(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"error":{"message":"overloaded","code":503}}`)
			return
		}
		wholeAnswer(w, `"usage":{"prompt_tokens":11,"completion_tokens":5,"total_tokens":16,"cost":0.02}`)
	}))
	t.Cleanup(server.Close)
	client := observedClient(t, server)
	obs := &recObserver{}
	owner := WorkOwner{Kind: "task", ID: "t-1"}
	if _, err := client.CompleteWithMessages(observed(t, t.Context(), obs, owner), userMessages("hi")); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 || obs.count() != 2 {
		t.Fatalf("server saw %d sends, observer admitted %d; want 2 and 2", hits.Load(), obs.count())
	}
	first, firstTok := obs.at(0)
	second, secondTok := obs.at(1)
	if first.ID == "" || first.ID == second.ID {
		t.Fatalf("attempt IDs %q and %q must be set and distinct", first.ID, second.ID)
	}
	if first.Owner != owner || second.Owner != owner || first.SelectedModel != "sim/model" {
		t.Fatalf("infos = %+v / %+v", first, second)
	}
	wantEnds(t, "refused send", firstTok, TransportResponded)
	wantFins(t, "refused send", firstTok, UnknownCost())
	wantEnds(t, "answered send", secondTok, TransportResponded)
	wantFins(t, "answered send", secondTok, KnownCost(0.02))
}

// A repaired body is a send of its own too. The refused one is unknown (it was
// superseded without a price), the repair has no usage block and is unknown.
func TestObservedRepairedBodyHasItsOwnToken(t *testing.T) {
	quirksAt(t, "strict/always-reasons")
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		hits.Add(1)
		if strings.Contains(string(body), `"enabled":false`) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"message":"Reasoning is mandatory for this endpoint and cannot be disabled.","code":400}}`)
			return
		}
		wholeAnswer(w, "")
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(Config{
		APIKey: "k", BaseURL: server.URL, Model: "strict/always-reasons",
		HTTPClient: server.Client(), SingleSendTransport: true,
		SupportsParameter: func(string, string) (bool, bool) { return true, true },
	})
	if err != nil {
		t.Fatal(err)
	}
	obs := &recObserver{}
	ctx := WithConfiguredReasoningEffort(observed(t, t.Context(), obs, WorkOwner{}), EffortOff)
	if _, err := client.CompleteWithMessages(ctx, userMessages("plan")); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 || obs.count() != 2 {
		t.Fatalf("server saw %d sends, observer admitted %d; want 2 and 2", hits.Load(), obs.count())
	}
	a, aTok := obs.at(0)
	b, bTok := obs.at(1)
	if a.ID == b.ID {
		t.Fatalf("a repaired send reused attempt ID %q", a.ID)
	}
	wantEnds(t, "refused body", aTok, TransportResponded)
	wantFins(t, "refused body", aTok, UnknownCost())
	wantEnds(t, "repaired body", bTok, TransportResponded)
	wantFins(t, "repaired body", bTok, UnknownCost())
}

// A real hedge: two arms, two sends, two tokens; the cancelled loser never
// reads as free, and every token's transport ends exactly once.
func TestObservedHedgeGivesEachArmItsOwnToken(t *testing.T) {
	firstToken := make(chan struct{})
	t.Cleanup(func() { close(firstToken) })
	rig := newLaneRig(t, "late/first-token",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 300 * time.Millisecond, Rate: 2000, Tokens: 24, FirstTokenUntil: firstToken}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{PriceIn: 1e-6, PriceOut: 2e-6, TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)
	report := &HedgeReport{}
	obs := &recObserver{}
	ctx := WithHedgeReport(observed(t, talking(), obs, WorkOwner{Kind: "turn", ID: "h"}), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 12*time.Millisecond))
	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if !report.Hedged() {
		t.Fatal("the scenario did not hedge")
	}
	waitFor(t, func() bool { return rig.server.Cancels("A") == 1 })
	if obs.count() != 2 || rig.server.Requests("A") != 1 || rig.server.Requests("B") != 1 {
		t.Fatalf("admitted %d; A saw %d, B saw %d; want one send per arm", obs.count(), rig.server.Requests("A"), rig.server.Requests("B"))
	}
	a, _ := obs.at(0)
	b, _ := obs.at(1)
	if a.ID == b.ID || a.Owner != b.Owner {
		t.Fatalf("arms must have distinct IDs and one owner: %+v %+v", a, b)
	}
	// EVERY ARM MUST COME TO A FINANCIAL FACT, not merely avoid a wrong one.
	waitFor(t, func() bool {
		for i := 0; i < 2; i++ {
			_, tok := obs.at(i)
			ends, fins := tok.seen()
			if len(ends) != 1 || len(fins) == 0 {
				return false
			}
		}
		return true
	})
	known := 0
	for i := 0; i < 2; i++ {
		_, tok := obs.at(i)
		ends, fins := tok.seen()
		last := fins[len(fins)-1]
		switch ends[0] {
		case TransportCancelled:
			// The cancelled loser was never priced: unknown, never a zero.
			if last != UnknownCost() {
				t.Fatalf("arm %d was cancelled and ended as %+v, want unknown", i, last)
			}
		case TransportResponded:
			if last.State == FinancialKnown {
				known++
				if last.CostUSD <= 0 {
					t.Fatalf("arm %d priced %+v; the lane's usage frame states a positive cost", i, last)
				}
			} else if last != UnknownCost() {
				t.Fatalf("arm %d ended as %+v, want known or unknown", i, last)
			}
		default:
			t.Fatalf("arm %d ended %v", i, ends)
		}
	}
	// The winner's exact usage frame prices it. The loser's receipt route is not
	// served by lanestub, so a PRICED loser is not proven here (it is unknown).
	if known != 1 {
		t.Fatalf("%d arms priced known, want exactly the winner", known)
	}
}

// Cancelling a send after its request is out is unknown, never zero.
func TestObservedCancellationAfterDoIsUnknown(t *testing.T) {
	arrived := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body) // the server notices a hang-up only once the body is read
		once.Do(func() { close(arrived) })
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	client := observedClient(t, server)
	obs := &recObserver{}
	ctx, cancel := context.WithCancel(observed(t, t.Context(), obs, WorkOwner{}))
	done := make(chan error, 1)
	go func() {
		_, err := client.CompleteWithMessages(ctx, userMessages("hi"))
		done <- err
	}()
	<-arrived
	cancel()
	if err := <-done; err == nil {
		t.Fatal("a cancelled call returned no error")
	}
	if obs.count() != 1 {
		t.Fatalf("admitted %d sends, want 1", obs.count())
	}
	_, tok := obs.at(0)
	// The call may return the moment its context ends, before the abandoned
	// send has unwound; the transport end follows it.
	waitFor(t, func() bool { ends, fins := tok.seen(); return len(ends) == 1 && len(fins) == 1 })
	wantEnds(t, "cancelled send", tok, TransportCancelled)
	wantFins(t, "cancelled send", tok, UnknownCost())
}

// A late receipt reports to the send that was cut, with that send's owner, even
// after the same observer has admitted a later send for other work.
func TestObservedLateReceiptKeepsItsOwnTokenAfterALaterCall(t *testing.T) {
	defer shortenStallBounds(t, time.Second, 30*time.Millisecond)()
	releaseReceipt := make(chan struct{})
	var releaseOnce sync.Once
	var completions atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			if completions.Add(1) == 1 {
				writeReceiptCut(w, r, "generation-late")
				return
			}
			wholeAnswer(w, `"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5,"cost":0.01}`)
		case "/generation":
			<-releaseReceipt
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":{"total_cost":0.37,"tokens_prompt":91,"tokens_completion":17}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseReceipt) })
		server.Close()
	})
	client := observedClient(t, server)
	obs := &recObserver{}
	ownerA, ownerB := WorkOwner{Kind: "task", ID: "A"}, WorkOwner{Kind: "task", ID: "B"}

	cut := WithStreamObserver(observed(t, t.Context(), obs, ownerA), func(StreamEvent) {})
	if _, err := client.CompleteWithMessages(cut, userMessages("first")); err == nil {
		t.Fatal("the stream did not reach its cut")
	}
	// A later call, other work, same client and observer, while A's receipt is owed.
	if _, err := client.CompleteWithMessages(observed(t, t.Context(), obs, ownerB), userMessages("second")); err != nil {
		t.Fatal(err)
	}
	if obs.count() != 2 {
		t.Fatalf("admitted %d sends, want 2", obs.count())
	}
	first, firstTok := obs.at(0)
	second, secondTok := obs.at(1)
	if first.Owner != ownerA || second.Owner != ownerB {
		t.Fatalf("owners = %+v / %+v", first.Owner, second.Owner)
	}
	// Pending is visible while the receipt is still owed, and the later call's
	// money went to the later token only.
	wantFins(t, "cut send before its receipt", firstTok, PendingCost())
	wantFins(t, "later send", secondTok, KnownCost(0.01))
	releaseOnce.Do(func() { close(releaseReceipt) })
	waitFor(t, func() bool { _, fins := firstTok.seen(); return len(fins) == 2 })
	wantFins(t, "cut send after its receipt", firstTok, PendingCost(), KnownCost(0.37))
	wantEnds(t, "cut send", firstTok, TransportResponded)
	wantFins(t, "later send, untouched by the old receipt", secondTok, KnownCost(0.01))
}

// Exact zero is a price; missing, negative and non-finite are not. Proved on
// the whole-answer usage block and on the late receipt, neither of which can
// carry the distinction through Billed.Empty.
func TestObservedExplicitZeroVersusMissingNegativeAndNonFinite(t *testing.T) {
	t.Run("usage block", func(t *testing.T) {
		for name, usage := range map[string]struct {
			block string
			want  FinancialObservation
		}{
			"explicit zero": {`"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0,"cost":0}`, KnownCost(0)},
			"missing cost":  {`"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}`, UnknownCost()},
			"negative":      {`"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6,"cost":-0.5}`, UnknownCost()},
			"stated":        {`"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6,"cost":0.125}`, KnownCost(0.125)},
		} {
			t.Run(name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { wholeAnswer(w, usage.block) }))
				t.Cleanup(server.Close)
				client := observedClient(t, server)
				obs := &recObserver{}
				if _, err := client.CompleteWithMessages(observed(t, t.Context(), obs, WorkOwner{}), userMessages("hi")); err != nil {
					t.Fatal(err)
				}
				_, tok := obs.at(0)
				wantFins(t, name, tok, usage.want)
			})
		}
	})
	t.Run("late receipt", func(t *testing.T) {
		defer shortenStallBounds(t, time.Second, 30*time.Millisecond)()
		for name, receipt := range map[string]struct {
			body string
			want FinancialObservation
		}{
			"explicit zero":     {`{"data":{"total_cost":0,"tokens_prompt":9,"tokens_completion":2}}`, KnownCost(0)},
			"empty placeholder": {`{"data":{"total_cost":0,"tokens_prompt":0,"tokens_completion":0}}`, UnknownCost()},
			"missing cost":      {`{"data":{"tokens_prompt":9,"tokens_completion":2}}`, UnknownCost()},
			"negative":          {`{"data":{"total_cost":-1,"tokens_prompt":9,"tokens_completion":2}}`, UnknownCost()},
			"stated":            {`{"data":{"total_cost":0.5,"tokens_prompt":9,"tokens_completion":2}}`, KnownCost(0.5)},
		} {
			t.Run(name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/generation" {
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprint(w, receipt.body)
						return
					}
					writeReceiptCut(w, r, "generation-z")
				}))
				t.Cleanup(server.Close)
				client := observedClient(t, server)
				obs := &recObserver{}
				ctx := WithStreamObserver(observed(t, t.Context(), obs, WorkOwner{}), func(StreamEvent) {})
				if _, err := client.CompleteWithMessages(ctx, userMessages("hi")); err == nil {
					t.Fatal("the stream did not reach its cut")
				}
				_, tok := obs.at(0)
				waitFor(t, func() bool { _, fins := tok.seen(); return len(fins) == 2 })
				wantFins(t, name, tok, PendingCost(), receipt.want)
			})
		}
	})
	t.Run("non-finite constructors", func(t *testing.T) {
		for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -0.0001} {
			if got := KnownCost(bad); got != UnknownCost() {
				t.Fatalf("KnownCost(%v) = %+v, want unknown", bad, got)
			}
		}
		if got := KnownCost(0); got.State != FinancialKnown || got.CostUSD != 0 {
			t.Fatalf("KnownCost(0) = %+v, want a known zero", got)
		}
	})
}

// A known cost is final and a repeated statement is not delivered twice.
func TestAttemptHandleNeverDoublesMoney(t *testing.T) {
	tok := &recToken{}
	h := &attemptHandle{tok: tok}
	h.unknown()
	h.unknown()
	h.financial(PendingCost())
	h.financial(KnownCost(0.4))
	h.financial(KnownCost(0.9))
	h.unknown()
	h.supersede()
	h.end(TransportResponded)
	h.end(TransportFailed)
	wantFins(t, "handle", tok, UnknownCost(), PendingCost(), KnownCost(0.4))
	wantEnds(t, "handle", tok, TransportResponded)
	var none *attemptHandle
	none.unknown()
	none.end(TransportNotSent)
}

// A refusal sends no HTTP: the server's counter does not move, no token exists,
// and the typed error survives the dispatcher's wrapping.
func TestObservedDenialSendsNoHTTP(t *testing.T) {
	for name, deny := range map[string]func(int) (bool, error){
		"refused":   func(int) (bool, error) { return true, errors.New("cap reached") },
		"nil token": func(int) (bool, error) { return true, nil },
	} {
		t.Run(name, func(t *testing.T) {
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				wholeAnswer(w, "")
			}))
			t.Cleanup(server.Close)
			client := observedClient(t, server)
			obs := &recObserver{deny: deny}
			_, err := client.CompleteWithMessages(observed(t, t.Context(), obs, WorkOwner{}), userMessages("hi"))
			var denied *AttemptDeniedError
			if !errors.As(err, &denied) {
				t.Fatalf("err = %v, want an AttemptDeniedError", err)
			}
			if name == "nil token" && !errors.Is(err, ErrActualAttemptNoToken) {
				t.Fatalf("err = %v, want ErrActualAttemptNoToken", err)
			}
			if hits.Load() != 0 || obs.count() != 0 {
				t.Fatalf("server saw %d sends and %d tokens exist; a denial must send nothing", hits.Load(), obs.count())
			}
		})
	}
}

// With no observer installed the same traffic behaves as before: same sends,
// same answer, and no handle is ever attached to a trace.
func TestUnobservedBehaviourIsUnchanged(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"error":{"message":"overloaded","code":503}}`)
			return
		}
		wholeAnswer(w, `"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"cost":0.01}`)
	}))
	t.Cleanup(server.Close)
	// No SingleSendTransport declaration: unobserved, a custom client is fine.
	client := receiptTestClient(t, server)
	var billed []Billed
	ctx := WithBilling(t.Context(), func(b Billed) { billed = append(billed, b) })
	if _, err := client.CompleteWithMessages(ctx, userMessages("hi")); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 || len(billed) != 1 || billed[0].Cost != 0.01 {
		t.Fatalf("hits=%d billed=%+v; want the retry and one banked answer", hits.Load(), billed)
	}
	if actualObserverFrom(ctx) != nil {
		t.Fatal("an observer appeared from nowhere")
	}
	if err := refuseObserved(ctx, SurfaceMedia); err != nil {
		t.Fatalf("unobserved media must not be refused: %v", err)
	}
}

// The observed send never follows a redirect (a followed one is a second paid
// send no single Do can see); the unobserved send keeps its old behaviour.
func TestObservedSendRefusesRedirects(t *testing.T) {
	var landed atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/landed" {
			landed.Add(1)
			wholeAnswer(w, "")
			return
		}
		http.Redirect(w, r, "/landed", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(server.Close)
	client := observedClient(t, server)
	obs := &recObserver{}
	redirectCtx := WithStreamObserver(observed(t, t.Context(), obs, WorkOwner{}), func(StreamEvent) {})
	_, err := client.CompleteWithMessages(redirectCtx, userMessages("hi"))
	var unsupported *UnsupportedAttemptSurfaceError
	if !errors.As(err, &unsupported) {
		t.Fatalf("observed redirect = %v; want typed coverage refusal", err)
	}
	if client.unstreamable.Load() {
		t.Fatal("a refused observed redirect changed streaming for later unobserved calls")
	}
	if landed.Load() != 0 || obs.count() != 1 {
		t.Fatalf("landed=%d admitted=%d; an observed send must not follow a redirect", landed.Load(), obs.count())
	}
	_, token := obs.at(0)
	wantEnds(t, "refused redirect", token, TransportResponded)
	wantFins(t, "refused redirect", token, UnknownCost())
	if _, err := client.CompleteWithMessages(t.Context(), userMessages("hi")); err != nil {
		t.Fatalf("unobserved redirect behaviour changed: %v", err)
	}
	if landed.Load() != 1 {
		t.Fatalf("unobserved send landed %d times, want its old redirect-following behaviour", landed.Load())
	}
}

// A second, different observer or owner is an explicit error and never shadows
// the first.
func TestDuplicateObserverCannotShadowTheCapObserver(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		wholeAnswer(w, "")
	}))
	t.Cleanup(server.Close)
	client := observedClient(t, server)
	capObserver, shadow := &recObserver{}, &recObserver{}
	ctx := observed(t, t.Context(), capObserver, WorkOwner{Kind: "task", ID: "1"})
	if same, err := WithActualAttemptObserver(ctx, capObserver); err != nil || same != ctx {
		t.Fatalf("installing the same observer again = (%v, %v), want a no-op", same == ctx, err)
	}
	if kept, err := WithActualAttemptObserver(ctx, shadow); !errors.Is(err, ErrActualAttemptObserverInstalled) || kept != ctx {
		t.Fatalf("a second observer = (%v), want ErrActualAttemptObserverInstalled and the context unchanged", err)
	}
	if _, err := WithWorkOwner(ctx, WorkOwner{Kind: "task", ID: "2"}); !errors.Is(err, ErrActualAttemptObserverInstalled) {
		t.Fatalf("a second owner = %v, want ErrActualAttemptObserverInstalled", err)
	}
	if _, err := client.CompleteWithMessages(ctx, userMessages("hi")); err != nil {
		t.Fatal(err)
	}
	if capObserver.count() != 1 || shadow.count() != 0 {
		t.Fatalf("cap observer saw %d sends, shadow saw %d; want 1 and 0", capObserver.count(), shadow.count())
	}
}

// Everything this foundation does not observe is refused before the network,
// and a custom transport that has not declared itself single-send is refused
// for the completion door itself.
func TestObservedRequiresCoverageBeforeAnyNetwork(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		wholeAnswer(w, "")
	}))
	t.Cleanup(server.Close)
	obs := &recObserver{}
	ctx := observed(t, t.Context(), obs, WorkOwner{})
	var unsupported *UnsupportedAttemptSurfaceError

	client := receiptTestClient(t, server) // custom HTTP client, no declaration
	if _, err := client.CompleteWithMessages(ctx, userMessages("hi")); !errors.As(err, &unsupported) {
		t.Fatalf("undeclared custom transport = %v, want UnsupportedAttemptSurfaceError", err)
	}
	if err := client.RequireActualAttemptCoverage(); err == nil {
		t.Fatal("the preflight accepted an undeclared custom transport")
	}
	client.config.SingleSendTransport = true
	if err := client.RequireActualAttemptCoverage(); err != nil {
		t.Fatalf("the preflight refused a declared transport: %v", err)
	}

	media, err := NewMediaClient(Config{APIKey: "k", BaseURL: server.URL, Model: "m", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, imageErr := media.GenerateImage(ctx, ImageRequest{Model: "m", Prompt: "x"})
	_, speechErr := media.Speak(ctx, SpeechRequest{Model: "m", Input: "x"})
	_, videoErr := media.GenerateVideo(ctx, VideoRequest{Model: "m", Prompt: "x"})
	_, documentErr := client.ParseDocument(ctx, DocumentRequest{})
	_, probeErr := client.probeLane(ctx, "m", "lane")
	_, _, loopErr := client.ExecuteToolCallLoop(ctx, userMessages("hi"), nil, ai.ToolCallConfig{MaxTurns: 1}, nil)
	for name, err := range map[string]error{"image": imageErr, "speech": speechErr, "video": videoErr,
		"document": documentErr, "probe": probeErr, "tool loop": loopErr} {
		if !errors.As(err, &unsupported) {
			t.Fatalf("%s under an observer = %v, want UnsupportedAttemptSurfaceError", name, err)
		}
	}
	client.ProbeLanes(ctx, "m") // returns without buying anything
	if hits.Load() != 0 || obs.count() != 0 {
		t.Fatalf("server saw %d requests and %d tokens exist; refusals must precede the network", hits.Load(), obs.count())
	}
	if RequireActualAttemptCoverage(SurfaceCompletion) != nil || RequireActualAttemptCoverage(SurfaceMedia) == nil {
		t.Fatal("the package-level preflight disagrees with the coverage statement")
	}
}

// Every admitted Do reaches a financial fact at the call door, after the parser:
// a malformed body, an error envelope, an answer with no usage block and a raw
// stream the caller drains all end unknown — one transport end each, and no
// token left silent.
func TestObservedEveryAdmittedDoReachesAFinancialFact(t *testing.T) {
	for name, serve := range map[string]func(http.ResponseWriter){
		"malformed json": func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"choices":[{"mess`)
		},
		"error envelope": func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"message":"bad request shape","code":400}}`)
		},
		"no usage block": func(w http.ResponseWriter) { wholeAnswer(w, "") },
	} {
		t.Run(name, func(t *testing.T) {
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				serve(w)
			}))
			t.Cleanup(server.Close)
			client := observedClient(t, server)
			obs := &recObserver{}
			_, _ = client.CompleteWithMessages(observed(t, t.Context(), obs, WorkOwner{}), userMessages("hi"))
			if obs.count() == 0 || int32(obs.count()) != hits.Load() {
				t.Fatalf("admitted %d, server saw %d; want one token per Do", obs.count(), hits.Load())
			}
			for i := 0; i < obs.count(); i++ {
				_, tok := obs.at(i)
				wantEnds(t, name, tok, TransportResponded)
				wantFins(t, name, tok, UnknownCost())
			}
		})
	}
	t.Run("raw stream", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, `data: {"id":"g","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
		}))
		t.Cleanup(server.Close)
		client := observedClient(t, server)
		obs := &recObserver{}
		chunks, errs := client.StreamComplete(observed(t, t.Context(), obs, WorkOwner{}), "hello")
		for range chunks {
		}
		for err := range errs {
			t.Fatal(err)
		}
		if obs.count() != 1 {
			t.Fatalf("admitted %d, want 1", obs.count())
		}
		_, tok := obs.at(0)
		waitFor(t, func() bool { _, fins := tok.seen(); return len(fins) == 1 })
		wantEnds(t, "raw stream", tok, TransportResponded)
		wantFins(t, "raw stream", tok, UnknownCost())
	})
	t.Run("a pending receipt survives the door", func(t *testing.T) {
		// The door's exit must not turn an owed receipt into unknown: the late
		// receipt test above already proves pending then known; here the exit has
		// run first (the call returned) and the fins are still exactly those.
		tok := &recToken{}
		h := &attemptHandle{tok: tok}
		trace := &callTrace{attempt: h}
		h.financial(PendingCost())
		trace.finishAttempt()
		h.financial(KnownCost(0.3))
		trace.finishAttempt()
		wantFins(t, "door exit", tok, PendingCost(), KnownCost(0.3))
	})
}

// A token handed back with an error is rolled back as not sent and free, and no
// HTTP goes out.
func TestObservedTokenWithErrorIsRolledBack(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); wholeAnswer(w, "") }))
	t.Cleanup(server.Close)
	client := observedClient(t, server)
	obs := &recObserver{rollback: true, deny: func(int) (bool, error) { return true, errors.New("cap") }}
	_, err := client.CompleteWithMessages(observed(t, t.Context(), obs, WorkOwner{}), userMessages("hi"))
	var denied *AttemptDeniedError
	if !errors.As(err, &denied) || hits.Load() != 0 || len(obs.rolled) == 0 {
		t.Fatalf("err=%v hits=%d rolled=%d; want a denial, no HTTP and a rolled-back token", err, hits.Load(), len(obs.rolled))
	}
	for _, tok := range obs.rolled {
		wantEnds(t, "rolled back", tok, TransportNotSent)
		wantFins(t, "rolled back", tok, KnownCost(0))
	}
}

// A dial failure after admission is NOT inferred as not-sent: the transport
// cannot prove no dispatch, so the token ends failed and unknown, never free.
func TestObservedDialFailureIsNotInferredAsFree(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()
	client, err := NewClient(Config{APIKey: "k", BaseURL: url, Model: "sim/model", HTTPClient: &http.Client{}, SingleSendTransport: true})
	if err != nil {
		t.Fatal(err)
	}
	client.wait = func(context.Context, time.Duration) error { return nil }
	obs := &recObserver{}
	ctx, cancel := context.WithTimeout(observed(t, t.Context(), obs, WorkOwner{}), 300*time.Millisecond)
	defer cancel()
	_, _ = client.CompleteWithMessages(ctx, userMessages("hi"))
	if obs.count() == 0 {
		t.Fatal("no send was admitted")
	}
	for i := 0; i < obs.count(); i++ {
		_, tok := obs.at(i)
		waitFor(t, func() bool { ends, fins := tok.seen(); return len(ends) == 1 && len(fins) >= 1 })
		ends, fins := tok.seen()
		if ends[0] == TransportNotSent || fins[len(fins)-1].State == FinancialKnown {
			t.Fatalf("send %d inferred free after Do: ends=%v fins=%+v", i, ends, fins)
		}
	}
}

// A denial is terminal through the REAL hedge walk: the observer refuses the
// first send only and would allow every later one, a valid next lane exists,
// and still no second send is requested and nothing reaches either lane.
func TestObservedDenialDoesNotWalkToTheNextLane(t *testing.T) {
	firstToken := make(chan struct{})
	t.Cleanup(func() { close(firstToken) })
	rig := newLaneRig(t, "late/first-token",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 300 * time.Millisecond, Rate: 2000, Tokens: 24, FirstTokenUntil: firstToken}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)
	cause := errors.New("daily cap reached")
	obs := &recObserver{deny: func(n int) (bool, error) { return n == 1, cause }}
	ctx := WithLaneChoice(observed(t, talking(), obs, WorkOwner{Kind: "turn", ID: "d"}), choiceFor(rig.model, 12*time.Millisecond))
	_, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	var denied *AttemptDeniedError
	if !errors.As(err, &denied) || !errors.Is(err, cause) {
		t.Fatalf("err = %v, want the typed denial with its cause preserved", err)
	}
	time.Sleep(100 * time.Millisecond) // a wrongly started next arm would have asked by now
	obs.mu.Lock()
	calls := obs.calls
	obs.mu.Unlock()
	if calls != 1 || rig.server.Requests("A")+rig.server.Requests("B") != 0 {
		t.Fatalf("observer asked %d times; lanes saw A=%d B=%d; a denial must neither walk nor send", calls, rig.server.Requests("A"), rig.server.Requests("B"))
	}
}

// The classification boundary: an admission refusal is generic and terminal
// whatever its cause's prose sounds like, and unsupported-surface refusals are
// the same fact. No hop, no retry, and not the credential reason.
func TestAdmissionRefusalIsTerminalWhateverTheCauseSoundsLike(t *testing.T) {
	for _, prose := range []string{
		"context deadline exceeded while waiting", "maximum context length exceeded: prompt is too long",
		"request timed out", "401 unauthorized", "rate limit exceeded, retry later",
	} {
		for name, err := range map[string]error{
			"denied":      fmt.Errorf("wrapped: %w", &AttemptDeniedError{Cause: errors.New(prose)}),
			"unsupported": fmt.Errorf("wrapped: %w", &UnsupportedAttemptSurfaceError{Surface: SurfaceMedia, Reason: prose}),
		} {
			evidence := Evidence(err)
			if !evidence.AdmissionRefused || evidence.Overflow || evidence.Timeout || evidence.TerminalTransport {
				t.Fatalf("%s/%q evidence = %+v, want only AdmissionRefused", name, prose, evidence)
			}
			evidence.FallbackAvailable = true
			verdict := taxonomy.Classify(evidence, taxonomy.Limits{})
			if verdict.Action != taxonomy.ActionReport || verdict.Reason != taxonomy.ReasonAdmissionRefused || verdict.Reason == taxonomy.ReasonUnauthorized {
				t.Fatalf("%s/%q verdict = %+v, want a report with the generic admission reason and no hop", name, prose, verdict)
			}
		}
	}
	if Evidence(errors.New("plain")).AdmissionRefused {
		t.Fatal("an ordinary error read as an admission refusal")
	}
}

type nilAttemptTokenObserver struct{}

func (*nilAttemptTokenObserver) BeforeAttempt(context.Context, ActualAttemptInfo) (ActualAttemptToken, error) {
	return (*recToken)(nil), nil
}
func TestObservedTypedNilTokenRefusesBeforeHTTP(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); wholeAnswer(w, `"usage":{"cost":0.1}`) }))
	defer server.Close()
	client := observedClient(t, server)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = observed(t, ctx, &nilAttemptTokenObserver{}, WorkOwner{})
	_, err := client.CompleteWithMessages(ctx, userMessages("hi"))
	var denied *AttemptDeniedError
	if !errors.As(err, &denied) || !errors.Is(err, ErrActualAttemptNoToken) || hits.Load() != 0 {
		t.Fatalf("typed nil token: err=%v HTTP=%d; want typed denial before HTTP", err, hits.Load())
	}
}

func TestObservedCutUsageNeedsItsFinalReceipt(t *testing.T) {
	defer shortenStallBounds(t, time.Second, 30*time.Millisecond)()
	receiptGate := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/generation" {
			select {
			case <-receiptGate:
			case <-r.Context().Done():
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":{"total_cost":0.41,"tokens_prompt":11,"tokens_completion":5}}`)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"id":"generation-partial","choices":[{"index":0,"delta":{"content":"begun"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"cost":0}}`+"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer func() { once.Do(func() { close(receiptGate) }); server.Close() }()
	client := observedClient(t, server)
	obs := &recObserver{}
	ctx := WithStreamObserver(observed(t, t.Context(), obs, WorkOwner{Kind: "turn", ID: "cut"}), func(StreamEvent) {})
	var banked, reconciled, leases atomic.Int32
	ctx = WithBilling(ctx, func(Billed) { banked.Add(1) })
	ctx = WithReconcile(ctx, func(Reconciled) { reconciled.Add(1) })
	ctx = WithReceiptPending(ctx, func() func() { leases.Add(1); return func() {} })
	if _, err := client.CompleteWithMessages(ctx, userMessages("hi")); err == nil {
		t.Fatal("expected interrupted stream")
	}
	_, tok := obs.at(0)
	wantFins(t, "intermediate usage cannot finish a cut send", tok, PendingCost())
	once.Do(func() { close(receiptGate) })
	waitFor(t, func() bool { _, f := tok.seen(); return len(f) == 2 })
	wantFins(t, "final receipt owns the complete price", tok, PendingCost(), KnownCost(0.41))
	if banked.Load() != 1 || reconciled.Load() != 0 || leases.Load() != 0 {
		t.Fatalf("legacy consumers changed: billed=%d reconciled=%d leases=%d", banked.Load(), reconciled.Load(), leases.Load())
	}
}

func TestTypedNilObserverCannotBeInstalled(t *testing.T) {
	var nilObserver *recObserver
	ctx, err := WithActualAttemptObserver(t.Context(), nilObserver)
	if !errors.Is(err, ErrActualAttemptNilObserver) || ActualAttemptObserverFrom(ctx) != nil {
		t.Fatalf("typed nil observer: %v, observer %v", err, ActualAttemptObserverFrom(ctx))
	}
}

func TestObservedCutInterimUsageWithoutIDStaysUnknown(t *testing.T) {
	defer shortenStallBounds(t, time.Second, 30*time.Millisecond)()
	var receiptHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/generation" {
			receiptHits.Add(1)
			http.NotFound(w, r)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"content":"begun"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"cost":0}}`+"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	client := observedClient(t, server)
	obs := &recObserver{}
	ctx := WithStreamObserver(observed(t, t.Context(), obs, WorkOwner{}), func(StreamEvent) {})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hi")); err == nil {
		t.Fatal("expected stream cut")
	}
	_, tok := obs.at(0)
	wantFins(t, "unidentified interrupted send", tok, UnknownCost())
	if receiptHits.Load() != 0 {
		t.Fatal("unidentified send cannot request a generation receipt")
	}
}

func TestObservedAllMediaAdaptersRefuseAtSharedHTTPDoor(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); wholeAnswer(w, "") }))
	defer server.Close()
	media, err := NewMediaClient(Config{APIKey: "fixture", BaseURL: server.URL, Model: "m", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := observed(t, t.Context(), &recObserver{}, WorkOwner{})
	_, musicErr := media.GenerateMusic(ctx, MusicRequest{Model: "m", Prompt: "one note"})
	_, transcribeErr := media.Transcribe(ctx, TranscriptionRequest{Model: "m", Data: []byte("fixture audio"), MIME: "audio/wav"})
	for name, e := range map[string]error{"music": musicErr, "transcription": transcribeErr} {
		var unsupported *UnsupportedAttemptSurfaceError
		if !errors.As(e, &unsupported) {
			t.Errorf("%s: %v; want unsupported before HTTP", name, e)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("unsupported media sent %d HTTP requests", hits.Load())
	}
}
