package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/lane/lanestub"
)

// ── A HEDGE LOSER THAT IS PRICED BY ITS OWN LATE RECEIPT ────────────────────
//
// TestObservedHedgeGivesEachArmItsOwnToken proves the loser is never read as
// free, and that it stays unknown because the stub serves no receipt route and
// the cut arm never named its generation. This file proves the other half: a
// loser that DID name its generation before it was cancelled is priced by the
// exact receipt, after the winning call has already returned, on the loser's
// own token and nowhere else.
//
// THE FIXTURE IS A PROXY IN FRONT OF THE OFFICIAL LANE STUB, not a second fake
// router. Every request still reaches [lanestub.Server] (lane choice, TTFT, the
// held first token, the winner's usage frame, the cancel count); the proxy adds
// only the two things the stub cannot say: one metadata frame carrying the
// loser's generation id BEFORE its first token, and a /generation route whose
// answer is held until the test lets it go.

// loserFixture is the proxy plus what it saw.
type loserFixture struct {
	url string

	releaseReceipt chan struct{}
	releaseOnce    sync.Once

	completions atomic.Int32

	mu       sync.Mutex
	askedFor []string
}

func (f *loserFixture) release() { f.releaseOnce.Do(func() { close(f.releaseReceipt) }) }

func (f *loserFixture) asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.askedFor...)
}

// newLoserFixture identifies the primary from its lane preference or the
// declared stub default, rather than assuming HTTP arrivals preserve arm order.
func newLoserFixture(t *testing.T, stub *lanestub.Server, loserRef string, receipt string) *loserFixture {
	t.Helper()
	upstream := strings.TrimSuffix(stub.URL(), "/api/v1")
	fixture := &loserFixture{releaseReceipt: make(chan struct{})}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/generation") {
			ref := r.URL.Query().Get("id")
			fixture.mu.Lock()
			fixture.askedFor = append(fixture.askedFor, ref)
			fixture.mu.Unlock()
			if ref != loserRef {
				http.NotFound(w, r)
				return
			}
			// HELD UNTIL AFTER THE WINNING CALL HAS RETURNED.
			select {
			case <-fixture.releaseReceipt:
			case <-r.Context().Done():
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, receipt)
			return
		}
		body, _ := io.ReadAll(r.Body)
		inject := false
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			fixture.completions.Add(1)
			var request struct {
				Provider struct {
					Only  []string `json:"only"`
					Order []string `json:"order"`
				} `json:"provider"`
			}
			if err := json.Unmarshal(body, &request); err != nil {
				http.Error(w, "invalid fixture request", http.StatusBadRequest)
				return
			}
			choices := request.Provider.Only
			if len(choices) == 0 {
				choices = request.Provider.Order
			}
			// The stub's unqualified primary goes to its first declared lane A;
			// a rescue names its demanded lane explicitly. Neither depends on
			// which concurrent HTTP request happened to arrive first.
			inject = len(choices) == 0 || choices[0] == "A"
		}
		forward, err := http.NewRequestWithContext(r.Context(), r.Method, upstream+r.URL.RequestURI(), strings.NewReader(string(body)))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		forward.Header = r.Header.Clone()
		response, err := http.DefaultClient.Do(forward)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		for key, values := range response.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		flush := http.NewResponseController(w)
		_ = flush.Flush()
		if inject {
			// A harmless metadata frame: the generation id, no choices, no token.
			fmt.Fprintf(w, "data: {\"id\":%q,\"object\":\"chat.completion.chunk\",\"choices\":[]}\n\n", loserRef)
			_ = flush.Flush()
		}
		buf := make([]byte, 512)
		for {
			n, err := response.Body.Read(buf)
			if n > 0 {
				if _, werr := w.Write(buf[:n]); werr != nil {
					return
				}
				_ = flush.Flush()
			}
			if err != nil {
				return
			}
		}
	}))
	t.Cleanup(func() {
		fixture.release()
		proxy.Close()
	})
	fixture.url = proxy.URL + "/api/v1"
	return fixture
}

func lastFin(token *recToken) (FinancialObservation, bool) {
	_, fins := token.seen()
	if len(fins) == 0 {
		return FinancialObservation{}, false
	}
	return fins[len(fins)-1], true
}

func TestObservedHedgeLoserIsPricedByItsLateReceiptAfterTheWinnerReturned(t *testing.T) {
	const (
		loserRef  = "gen-loser-early-id"
		loserCost = 0.0375
	)
	receipt := fmt.Sprintf(`{"data":{"total_cost":%v,"tokens_prompt":41,"tokens_completion":9}}`, loserCost)

	firstToken := make(chan struct{})
	t.Cleanup(func() { close(firstToken) })
	rig := newLaneRig(t, "late/first-token-priced-loser",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 300 * time.Millisecond, Rate: 2000, Tokens: 24, FirstTokenUntil: firstToken}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{PriceIn: 1e-6, PriceOut: 2e-6, TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)
	fixture := newLoserFixture(t, rig.server, loserRef, receipt)
	// The client talks to the proxy; the lane sheet and takeover state the rig
	// armed for the stub's base are armed for the proxy's too.
	rig.client.config.BaseURL = fixture.url
	lanes.HeardPrefsCarried(fixture.url)
	if err := rig.client.RequireActualAttemptCoverage(); err != nil {
		t.Fatalf("the rig's client must be countable: %v", err)
	}

	var winnerBanks, loserBanks atomic.Int32
	var loserReconciled atomic.Pointer[Reconciled]
	report := &HedgeReport{}
	obs := &recObserver{}
	owner := WorkOwner{Kind: "turn", ID: "priced-loser"}
	ctx := WithHedgeReport(observed(t, talking(), obs, owner), report)
	ctx = WithBilling(ctx, func(Billed) { winnerBanks.Add(1) })
	ctx = WithReconcile(ctx, func(r Reconciled) {
		loserBanks.Add(1)
		loserReconciled.Store(&r)
	})
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 12*time.Millisecond))

	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	// THE WINNING CALL HAS RETURNED, and the loser's receipt is still held.
	if !report.Hedged() {
		t.Fatalf("the scenario did not hedge: A=%d B=%d completions=%d", rig.server.Requests("A"), rig.server.Requests("B"), fixture.completions.Load())
	}
	waitFor(t, func() bool { return rig.server.Cancels("A") == 1 })
	if rig.server.Requests("A") != 1 || rig.server.Requests("B") != 1 || fixture.completions.Load() != 2 {
		t.Fatalf("A saw %d, B saw %d, proxy saw %d completions; want one send per arm", rig.server.Requests("A"), rig.server.Requests("B"), fixture.completions.Load())
	}
	if obs.count() != 2 {
		t.Fatalf("observer admitted %d sends, want exactly the two actual Do's", obs.count())
	}
	// Admission goroutines and HTTP arrivals may be scheduled independently.
	// Identify the two outcomes by their owned facts, never by append order.
	waitFor(t, func() bool {
		for i := 0; i < 2; i++ {
			_, token := obs.at(i)
			ends, fins := token.seen()
			if len(ends) != 1 || len(fins) == 0 {
				return false
			}
		}
		return true
	})
	var loserInfo, winnerInfo ActualAttemptInfo
	var loserTok, winnerTok *recToken
	for i := 0; i < 2; i++ {
		info, token := obs.at(i)
		fin, _ := lastFin(token)
		switch fin.State {
		case FinancialPending:
			loserInfo, loserTok = info, token
		case FinancialKnown:
			winnerInfo, winnerTok = info, token
		default:
			t.Fatalf("arm %d has unexpected pre-receipt state %+v", i, fin)
		}
	}
	if loserTok == nil || winnerTok == nil {
		t.Fatal("expected one pending loser and one priced winner")
	}
	if loserInfo.ID == "" || winnerInfo.ID == "" || loserInfo.ID == winnerInfo.ID {
		t.Fatalf("attempt IDs %q / %q must be set and distinct", loserInfo.ID, winnerInfo.ID)
	}
	if loserInfo.Owner != owner || winnerInfo.Owner != owner {
		t.Fatalf("owners = %+v / %+v, want %+v on both", loserInfo.Owner, winnerInfo.Owner, owner)
	}

	// The loser is owed a receipt: Pending, never Known, never a zero.
	waitFor(t, func() bool { fin, ok := lastFin(loserTok); return ok && fin == PendingCost() })
	waitFor(t, func() bool { ends, _ := winnerTok.seen(); _, ok := lastFin(winnerTok); return len(ends) == 1 && ok })
	_, loserFins := loserTok.seen()
	for _, fin := range loserFins {
		if fin.State == FinancialKnown {
			t.Fatalf("the loser was priced %+v before any receipt arrived", fin)
		}
	}
	// Do already returned the loser's headers (and its early id), so its transport
	// ended as responded; the hedge cancel cut the BODY, which is the money fact.
	wantEnds(t, "loser", loserTok, TransportResponded)
	wantEnds(t, "winner", winnerTok, TransportResponded)
	winnerFin, _ := lastFin(winnerTok)
	if winnerFin.State != FinancialKnown || winnerFin.CostUSD <= 0 || winnerFin.CostUSD == loserCost {
		t.Fatalf("winner = %+v, want the stub's positive usage-frame cost", winnerFin)
	}
	wantFins(t, "winner", winnerTok, winnerFin)
	if loserBanks.Load() != 0 {
		t.Fatalf("legacy reconcile sink fired %d times before the receipt existed", loserBanks.Load())
	}
	// Pending is published before the asynchronous receipt request starts.
	waitFor(t, func() bool { return len(fixture.asked()) > 0 })
	if asked := fixture.asked(); asked[0] != loserRef {
		t.Fatalf("receipt route was asked for %v, want the loser's early id %q", asked, loserRef)
	}

	// NOW THE RECEIPT ARRIVES, after the winner returned.
	fixture.release()
	waitFor(t, func() bool { fin, _ := lastFin(loserTok); return fin.State == FinancialKnown })
	wantFins(t, "loser after its receipt", loserTok, PendingCost(), KnownCost(loserCost))
	wantEnds(t, "loser after its receipt", loserTok, TransportResponded)

	// Nothing leaked across the two tokens and nothing banked twice.
	wantFins(t, "winner untouched by the loser's receipt", winnerTok, winnerFin)
	wantEnds(t, "winner untouched by the loser's receipt", winnerTok, TransportResponded)
	waitFor(t, func() bool { return loserBanks.Load() == 1 })
	if winnerBanks.Load() != 1 {
		t.Fatalf("winner banked %d times through the usage door, want 1", winnerBanks.Load())
	}
	got := loserReconciled.Load()
	if got == nil || got.Ref != loserRef || !got.Found || got.Cost != loserCost || got.Reason != receiptTornReason {
		t.Fatalf("legacy loser receipt = %+v, want found %v for %q as a torn stream", got, loserCost, loserRef)
	}
	if obs.count() != 2 {
		t.Fatalf("a late receipt must not admit anything: %d admissions", obs.count())
	}
	total := 0.0
	for i := 0; i < 2; i++ {
		_, tok := obs.at(i)
		_, fins := tok.seen()
		known := 0
		for _, fin := range fins {
			if fin.State == FinancialKnown {
				known++
				total += fin.CostUSD
			}
		}
		if known != 1 {
			t.Fatalf("token %d delivered %d known costs, want exactly 1: %+v", i, known, fins)
		}
	}
	if want := winnerFin.CostUSD + loserCost; total != want {
		t.Fatalf("observer money = %v, want winner + loser = %v", total, want)
	}
}
