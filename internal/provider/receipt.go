package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE RECEIPT: MONEY A CUT STREAM LEFT OFF THE WIRE.
//
// A provider can accept a request, begin a streamed answer and charge for the
// work, then lose the connection or be cut before the terminal usage block.
// When the stream named its generation id, the provider's generation route is
// the only honest source for the missing figures. Text without an id is still
// charged work but cannot name a receipt; neither text nor an id is nothing to
// count. This file reports only those facts and never estimates money.

const (
	// receiptQueueDepth bounds how many ended calls may wait behind one receipt
	// fetch. A full queue loses a price rather than making a person's reply wait.
	receiptQueueDepth = 64
	// receiptWorkerCount is deliberately small. These are late bookkeeping
	// requests: several cut calls must make progress together, but their burst
	// must not become a second burst at the provider.
	receiptWorkerCount = 4
	// receiptAttempts is the fixed number of times a receipt that is not ready
	// yet is asked for before its price is reported missing. It is derived from
	// the pauses: one first request, then one request after every pause.
	receiptAttempts = 1 + len(receiptRetrySchedule)
	// The growing pauses give a generation receipt time to appear after its call
	// ends. It is the only honest source of this money, and this bounded wait is
	// entirely in the background, so generosity here costs the person nothing.
	receiptFirstRetryDelay  = time.Second
	receiptSecondRetryDelay = 4 * time.Second
	receiptThirdRetryDelay  = 15 * time.Second
	// receiptRequestAllowance leaves each attempt room to complete in addition
	// to the pauses. The ceiling is derived from every part of that schedule so
	// widening one cannot silently leave the background context too short.
	receiptRequestAllowance = 5 * time.Second
	// receiptScheduleSlack leaves the derived ceiling comfortably beyond both
	// the growing pauses and every request's allowance.
	receiptScheduleSlack = 5 * time.Second
	receiptFetchTimeout  = receiptFirstRetryDelay + receiptSecondRetryDelay + receiptThirdRetryDelay +
		time.Duration(receiptAttempts)*receiptRequestAllowance + receiptScheduleSlack
	// receiptRouteTTL is how long a base's answer that it has no generation
	// route is trusted before the capability may be asked about again.
	receiptRouteTTL = 5 * time.Minute
	// maxReceiptBytes is generous beside the five scalar fields one receipt
	// carries and still prevents an upstream body becoming an unbounded read.
	maxReceiptBytes = 1 << 20

	// These two words name endings that have no [CutReason] of their own. They
	// live here so every such ending and every receipt row spell them alike.
	receiptTornReason    = "torn"
	receiptRefusalReason = "refusal"
)

var receiptRetrySchedule = [...]time.Duration{
	receiptFirstRetryDelay,
	receiptSecondRetryDelay,
	receiptThirdRetryDelay,
}

// receiptWork is all the worker may retain from a call whose own context is
// usually cancelled. The sink and attribution are values; no request context
// crosses the hand-off because its cancellation is why this work exists.
type receiptWork struct {
	result Reconciled
	sink   ReconcileSink
}

// receiptRouteMemo remembers only the one definite capability answer: a base
// that answered that no generation route exists. Transient failures and an id
// not ready yet teach it nothing and are tried on the fixed schedule instead.
type receiptRouteMemo struct {
	mu      sync.Mutex
	refused map[string]time.Time
}

var receiptRoutes = receiptRouteMemo{refused: map[string]time.Time{}}

// askable reports whether a base may be asked for a receipt now. An old refusal
// expires so a proxy or local gateway upgraded in place can gain the capability
// without this process having to restart.
func (m *receiptRouteMemo) askable(base string, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	refusedAt, refused := m.refused[base]
	return !refused || now.Sub(refusedAt) >= receiptRouteTTL
}

// heard records the base's definite answer that the route does not exist.
func (m *receiptRouteMemo) heard(base string, now time.Time) {
	m.mu.Lock()
	m.refused[base] = now
	m.mu.Unlock()
}

// settle closes one of the four streamed endings that may still hold provider
// money. A usage block stays on the ordinary billing door. Without one, a
// generation id proves the provider got far enough for its receipt to be
// asked for, while text already written without an id is reported unpriced
// without making an unanswerable request. A call with neither is not reported
// at all: the measured first-frame in-band refusal is an upstream breaking
// before it produced a generation, not money the provider charged. Those
// mutually exclusive doors keep one call from ever being counted twice.
func (c *Client) settle(ctx context.Context, model string, response *ai.Response, reason string, answerBytes int) {
	if response != nil && response.Usage != nil {
		c.bill(ctx, model, response)
		return
	}
	sink := reconcileFrom(ctx)
	if sink == nil {
		return
	}
	if served := CallFrom(ctx).Model(); served != "" {
		model = served
	}
	result := Reconciled{
		Billed: Billed{Node: callNode(ctx), Model: model},
		Reason: strings.TrimSpace(reason),
		Hedged: hedgeLaneFrom(ctx) != "",
	}
	if response != nil {
		result.Ref = strings.TrimSpace(response.ID)
	}
	// NO ID MEANS NO REQUEST. Text still proves that the call got somewhere and
	// may be charged, so its missing price is said; without text there is no
	// generation and nothing honest to count.
	if result.Ref == "" {
		if answerBytes > 0 {
			sink(result)
		}
		return
	}
	work := receiptWork{result: result, sink: sink}
	c.receiptOnce.Do(func() {
		for range receiptWorkerCount {
			guard.Go("provider.receipts", c.runReceipts)
		}
	})
	select {
	case c.receipts <- work:
	default:
		// THE TURN NEVER WAITS FOR ACCOUNTING. Once the bounded queue is full,
		// this call is reported unpriced on the spot instead of blocking behind
		// a provider or a connection that may not answer.
		sink(result)
	}
}

// runReceipts is one member of the small fixed pool draining this client's
// bounded queue. A pool keeps one slow receipt from holding every later call,
// while its fixed size keeps late bookkeeping from bursting at the provider.
func (c *Client) runReceipts() {
	for work := range c.receipts {
		c.reconcile(work)
	}
}

// reconcile follows the fixed growing schedule and delivers exactly one answer.
// It starts from a fresh context because the call's own context has commonly
// been cancelled already, then puts one ceiling around the entire schedule.
func (c *Client) reconcile(work receiptWork) {
	result := work.result
	base := strings.TrimRight(strings.TrimSpace(c.config.BaseURL), "/")
	if !receiptRoutes.askable(base, time.Now()) {
		work.sink(result)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), receiptFetchTimeout)
	defer cancel()
	for attempt := 0; attempt < receiptAttempts; attempt++ {
		billed, found, noRoute := c.fetchReceipt(ctx, result.Ref)
		if noRoute {
			receiptRoutes.heard(base, time.Now())
			work.sink(result)
			return
		}
		if found {
			billed.Node = result.Node
			billed.Model = result.Model
			result.Billed = billed
			result.Found = true
			work.sink(result)
			return
		}
		if attempt < len(receiptRetrySchedule) {
			if err := c.wait(ctx, receiptRetrySchedule[attempt]); err != nil {
				break
			}
		}
	}
	work.sink(result)
}

// receiptWire is the provider's generation response. Pointers preserve the
// difference between an absent normalised token pair and a real pair of zeroes,
// which is the only condition under which the native counts may stand in.
type receiptWire struct {
	Data struct {
		TotalCost              *float64 `json:"total_cost"`
		TokensPrompt           *int     `json:"tokens_prompt"`
		TokensCompletion       *int     `json:"tokens_completion"`
		NativeTokensPrompt     *int     `json:"native_tokens_prompt"`
		NativeTokensCompletion *int     `json:"native_tokens_completion"`
	} `json:"data"`
}

// fetchReceipt asks once for one generation id. The booleans separate a real
// receipt from the one durable capability answer; every other failure simply
// leaves both false so the caller may follow its bounded retry schedule.
func (c *Client) fetchReceipt(ctx context.Context, ref string) (Billed, bool, bool) {
	endpoint, err := url.Parse(strings.TrimSpace(c.config.BaseURL))
	if err != nil {
		return Billed{}, false, false
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/generation"
	endpoint.RawPath = ""
	query := endpoint.Query()
	query.Set("id", ref)
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return Billed{}, false, false
	}
	key, err := c.apiKeyNow()
	if err != nil {
		return Billed{}, false, false
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+key)
	ApplyAttribution(request.Header)
	response, err := c.http.Do(request)
	if err != nil {
		return Billed{}, false, false
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, maxReceiptBytes))
		return Billed{}, false, receiptRouteMissing(payload)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxReceiptBytes))
		return Billed{}, false, false
	}
	var wire receiptWire
	if err := json.NewDecoder(io.LimitReader(response.Body, maxReceiptBytes)).Decode(&wire); err != nil {
		return Billed{}, false, false
	}
	billed := Billed{}
	if wire.Data.TotalCost != nil {
		billed.Cost = *wire.Data.TotalCost
	}
	if wire.Data.TokensPrompt != nil || wire.Data.TokensCompletion != nil {
		billed.PromptTokens = valueOrZero(wire.Data.TokensPrompt)
		billed.CompletionTokens = valueOrZero(wire.Data.TokensCompletion)
	} else {
		billed.PromptTokens = valueOrZero(wire.Data.NativeTokensPrompt)
		billed.CompletionTokens = valueOrZero(wire.Data.NativeTokensCompletion)
	}
	if billed.Empty() {
		return Billed{}, false, false
	}
	return billed, true, false
}

// receiptRouteMissing reads the same two meanings of a 404 as the lane sheet:
// a provider error envelope is the route answering about this generation, while
// a bare page is the base answering that no such route exists. Only the latter
// is safe to memoize for every later call at the base.
func receiptRouteMissing(payload []byte) bool {
	refusal, ok := RefusalFrom(apiError(http.StatusNotFound, payload))
	return !ok || strings.TrimSpace(refusal.Message) == ""
}

// valueOrZero turns an absent optional count into the zero its enclosing
// receipt carries, after the normalised-versus-native choice has been made.
func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
