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

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// THE RECEIPT: MONEY A CUT STREAM LEFT OFF THE WIRE.
//
// A routed provider can accept a request, begin a streamed answer and charge
// for the work, then lose the connection or be cut before the terminal usage block.
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
	//
	// THE FOURTH PAUSE IS THE MEASURED ONE. The receipts of the three stopped
	// senior-dev runs of 2026-09-23 — each for the call in flight when the run
	// was cut — landed 20.5, 20.6 and 20.8 seconds after the cut: on the fourth
	// and then last request, with nothing to spare. A cancelled generation takes
	// the router about that long to price, so a little more lag on its side
	// turned a real charge into an unpriced marker. One more request twenty
	// seconds later gives that ending a second chance.
	receiptFirstRetryDelay  = time.Second
	receiptSecondRetryDelay = 4 * time.Second
	receiptThirdRetryDelay  = 15 * time.Second
	receiptFourthRetryDelay = 20 * time.Second
	// receiptRequestAllowance leaves each attempt room to complete in addition
	// to the pauses. The ceiling is derived from every part of that schedule so
	// widening one cannot silently leave the background context too short.
	receiptRequestAllowance = 5 * time.Second
	// receiptScheduleSlack leaves the derived ceiling comfortably beyond both
	// the growing pauses and every request's allowance.
	receiptScheduleSlack = 5 * time.Second
	receiptFetchTimeout  = receiptFirstRetryDelay + receiptSecondRetryDelay + receiptThirdRetryDelay + receiptFourthRetryDelay +
		time.Duration(receiptAttempts)*receiptRequestAllowance + receiptScheduleSlack
	// ReceiptWait is the longest one receipt can take to be answered once it is
	// queued: the whole schedule's ceiling, counted from the queue however long
	// the receipt waited there for a worker ([receiptWork.deadline]), so a
	// waiter that starts after every receipt it is owed was queued sees each one
	// answered within it. It is exported for work that waits
	// for the receipts it is owed before it closes its books
	// ([WithReceiptPending]), so that wait and this schedule are one figure and
	// widening the schedule widens the wait with it.
	ReceiptWait = receiptFetchTimeout
	// receiptRouteTTL is how long a base's answer that it has no generation
	// route is trusted before the capability may be asked about again.
	receiptRouteTTL = 5 * time.Minute
	// maxReceiptBytes is generous beside the five scalar fields one receipt
	// carries and still prevents an upstream body becoming an unbounded read.
	maxReceiptBytes = 1 << 20

	// These words name endings that have no [CutReason] of their own. They
	// live here so every such ending and every receipt row spell them alike.
	// unmetered is an answer that arrived whole with no usage block
	// ([WithUnmeteredReceipts]).
	receiptTornReason      = "torn"
	receiptRefusalReason   = "refusal"
	receiptUnmeteredReason = "unmetered"
)

var receiptRetrySchedule = [...]time.Duration{
	receiptFirstRetryDelay,
	receiptSecondRetryDelay,
	receiptThirdRetryDelay,
	receiptFourthRetryDelay,
}

// receiptWork is all the worker may retain from a call whose own context is
// usually cancelled. The sink and attribution are values; no request context
// crosses the hand-off because its cancellation is why this work exists.
// queued is the instant the receipt was owed, which its ceiling is counted from.
type receiptWork struct {
	result Reconciled
	sink   ReconcileSink
	queued time.Time
}

// deadline is the latest a receipt may be answered: [receiptFetchTimeout] after
// it was queued.
//
// IT IS COUNTED FROM THE QUEUE AND NOT FROM THE WORKER, because the queue is
// what a waiter sees. A client drains its receipts with [receiptWorkerCount]
// workers, so a fifth receipt owed behind four slow ones started its whole
// schedule some forty seconds late and was answered about eighty seconds after
// it was queued — past [ReceiptWait], so a run that waited that long for its
// books closed them without that call's price. A receipt that waited in the
// queue loses none of its chance by this: the provider was pricing its
// generation the whole time it waited, and the worker's first request for it
// is made that much later.
func (w receiptWork) deadline() time.Time {
	queued := w.queued
	if queued.IsZero() {
		queued = time.Now()
	}
	return queued.Add(receiptFetchTimeout)
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
	defer m.mu.Unlock()
	m.refused[base] = now
}

// settle closes one of the four streamed endings that may still hold provider
// money. A usage block stays on the ordinary billing door. A direct service
// stops there because it has no routed receipt. Otherwise a generation id
// proves the provider got far enough for its receipt to be asked for, while
// text already written without an id is reported unpriced without making an
// unanswerable request. A call with neither is not reported at all: the
// measured first-frame in-band refusal is an upstream breaking before it
// produced a generation, not money the provider charged. Those mutually
// exclusive doors keep one call from ever being counted twice.
func (c *Client) settle(ctx context.Context, model string, response *ai.Response, reason string, answerBytes int) {
	if response != nil && response.Usage != nil {
		c.bill(ctx, model, response)
		return
	}
	// A DIRECT SERVICE HAS NO OPENROUTER GENERATION ROUTE TO ASK. Its missing
	// usage block supplies no measured figures to record, and calling the
	// router's route on the vendor would send that vendor a request and bearer
	// for a fact this client already knows cannot be there.
	if c.config.Direct {
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
	// THE WORK IS TOLD A RECEIPT IS OWED BEFORE IT IS QUEUED, and told it was
	// answered only after the sink has banked it, so a caller waiting for its
	// receipts cannot see zero owed while money is between the two
	// ([ReceiptPending]).
	if pending := receiptPendingFrom(ctx); pending != nil {
		done := pending()
		work.sink = func(answer Reconciled) {
			defer done()
			sink(answer)
		}
	}
	// THE RECEIPT'S BOUND STARTS HERE, when it is owed, and not when a worker
	// gets to it ([receiptWork.deadline]).
	work.queued = time.Now()
	if !c.queueReceipt(work) {
		// A full queue reports the missing price without holding up the turn.
		work.sink(result)
	}
}

// runReceipts is one member of the small fixed pool draining this client's
// bounded queue. A pool keeps one slow receipt from holding every later call,
// while its fixed size keeps late bookkeeping from bursting at the provider.
func (c *Client) runReceipts() {
	for {
		work, ok := c.nextReceipt()
		if !ok {
			return
		}
		c.reconcile(work)
	}
}

// queueReceipt starts only enough workers for the queued work. Admission and
// retirement share a lock, so work cannot arrive behind the last retiring worker.
func (c *Client) queueReceipt(work receiptWork) bool {
	c.receiptMu.Lock()
	defer c.receiptMu.Unlock()
	select {
	case c.receipts <- work:
		if c.receiptRunning < receiptWorkerCount {
			c.receiptRunning++
			guard.Go("provider.receipts", c.runReceipts)
		}
		return true
	default:
		return false
	}
}

// nextReceipt retires an idle worker immediately. No client close hook is
// needed, and a transient role client can be collected after its receipts finish.
func (c *Client) nextReceipt() (receiptWork, bool) {
	c.receiptMu.Lock()
	defer c.receiptMu.Unlock()
	select {
	case work := <-c.receipts:
		return work, true
	default:
		c.receiptRunning--
		return receiptWork{}, false
	}
}

// reconcile follows the fixed growing schedule and delivers exactly one answer.
// It starts from a fresh context because the call's own context has commonly
// been cancelled already, then puts one ceiling around the entire schedule,
// counted from when the receipt was queued ([receiptWork.deadline]).
func (c *Client) reconcile(work receiptWork) {
	result := work.result
	base := strings.TrimRight(strings.TrimSpace(c.config.BaseURL), "/")
	if !receiptRoutes.askable(base, time.Now()) {
		work.sink(result)
		return
	}
	ctx, cancel := context.WithDeadline(context.Background(), work.deadline())
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
	c.applyRequestIdentity(request)
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
