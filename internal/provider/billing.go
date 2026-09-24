package provider

import (
	"context"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A BILLED RESPONSE IS BANKED WHEN IT ARRIVES, NOT WHEN THE WORK LANDS.
//
// THE DEFECT THIS ANSWERS. A leaf's spend used to reach the journal exactly
// once, from the executor's in-memory total, on the way out of the run
// (resident.Runner.recordSpend). Every ending that returns no outcome therefore
// returned no money either: the ink run of 2026-08-29 made a hundred and nine
// billed calls over seventeen minutes, was given up on by its watchdog, and
// left a store whose usage table held one row — the planner's — and a cost.json
// reading $0.000228 for a run that had written a twenty-six kilobyte patch.
// The transcript had been hardened against exactly these endings a wave
// earlier, with a flush on each side of the abandonment; the money had no
// equivalent, so a $0.85 leaf and a free one were the same row.
//
// The fix is not another flush on another ending — there is always one more
// ending. The fact that was missing is that THE PROVIDER ALREADY KNOWS WHAT
// EACH CALL COST at the moment it decodes the answer, and this adapter is the
// one door every outbound call in the process passes through (calllog.go says
// so, and writes its own per-call row here for the same reason). So the row is
// written where the fact is known, and a turn roll-up becomes a second kind of
// record rather than the only one.
//
// It is reported through the context exactly as the transcript sink and the
// liveness span already are (exec.WithTranscript, exec.WithLiveness), because
// the adapter underneath is shared by every agent in the process and the call
// is the only thing that knows whose call it is. Nobody listening is the
// ordinary case — a unit test, a bench harness, a client built for one probe —
// and costs one type assertion.
//
// A SECOND DOOR reports calls the wire never priced. [WithReconcile] is kept
// apart from [WithBilling] because the ordinary response and the provider's
// later receipt are mutually exclusive facts: one sink fires for a usage block
// and the other fires only when that block never arrived. Keeping those doors
// separate is what makes it impossible for one call to be banked through both.

// Billed is one model response the provider charged for, as the adapter read it
// off the wire. It carries the node the call belongs to so a listener does not
// have to re-derive it: the same [WithCallNode] the call log is keyed by names
// the work, and a call made without one is spend nobody can file.
type Billed struct {
	// Node is what [WithCallNode] named, empty when nothing did.
	Node string
	// Model is who actually served the call — the rung a panel picked or an
	// escalation moved to — rather than the name the caller asked for.
	Model string
	// The four figures the usage table holds. CachedTokens is the share of
	// PromptTokens the provider billed at the cached rate.
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	Cost             float64
}

// Empty reports a response the adapter cannot bill: the provider sent no usage
// block, or sent one that says nothing was spent. Neither is written down —
// guessing at a number is worse than a gap, and the gap is visible as a call
// the ledger did not see rather than as money the ledger invented.
func (b Billed) Empty() bool {
	return b.PromptTokens == 0 && b.CompletionTokens == 0 && b.Cost == 0
}

// BillingSink is told what one response cost, as soon as it is decoded. An
// implementation must be safe for concurrent use: a leaf's batch of parallel
// tools can each be waiting on a call of their own.
type BillingSink func(Billed)

// Reconciled is what became of one call the wire never priced. Found says the
// provider's own receipt supplied the embedded figures; when it is false every
// figure is zero and nothing may be banked.
type Reconciled struct {
	Billed
	// Ref is the provider's generation id, and is empty when the stream never
	// named the call well enough for a receipt to be requested.
	Ref string
	// Reason is the cut's own word, or the ending that left the stream without
	// a usage block.
	Reason string
	// Hedged says this was a rescue arm, so any money on its receipt is waste.
	Hedged bool
	// Found says the provider supplied a receipt. False means the figures above
	// stay empty and the missing price is counted instead.
	Found bool
}

// ReconcileSink is told exactly once what became of an unpriced call. Like a
// billing sink it must be safe for concurrent use, because separate calls can
// finish without their usage blocks at the same instant.
type ReconcileSink func(Reconciled)

// ReceiptPending is told the moment a receipt is queued for a call whose
// stream ended without its usage block, and answers the function to call once
// that receipt's one answer has reached the [ReconcileSink]. The answer is
// called exactly once, found or not, so a count kept with it always comes back
// to zero.
//
// IT EXISTS FOR WORK WHOSE BOOKS CLOSE. A receipt is fetched in the background
// on a schedule that runs for seconds after the call returned, and a caller
// that reads its total and closes its books the moment its last call ends
// reads a total without that money — the stopped senior-dev runs of
// 2026-09-23 lost their in-flight call exactly so, about twenty seconds before
// its receipt arrived. With this armed, such a caller can wait (bounded by
// [ReceiptWait]) for what it is still owed before it reads the total.
type ReceiptPending func() (done func())

type billingContextKey struct{}
type reconcileContextKey struct{}
type receiptPendingContextKey struct{}

// WithBilling arms one piece of work's banking. Like the transcript sink it
// belongs to the work rather than to the client, because one client serves
// every node in the process.
func WithBilling(ctx context.Context, sink BillingSink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, billingContextKey{}, sink)
}

// WithReconcile arms one piece of work for a receipt that arrives after its
// call has already ended. It is separate from [WithBilling] so an ordinary
// usage block and a late receipt can never both bank the same call.
func WithReconcile(ctx context.Context, sink ReconcileSink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, reconcileContextKey{}, sink)
}

// WithReceiptPending arms one piece of work to be told about every receipt
// queued on its behalf and when each was answered ([ReceiptPending]). It
// changes nothing about how a receipt is fetched or banked: the money still
// reaches the work through [WithReconcile] alone.
func WithReceiptPending(ctx context.Context, pending ReceiptPending) context.Context {
	if pending == nil {
		return ctx
	}
	return context.WithValue(ctx, receiptPendingContextKey{}, pending)
}

// receiptPendingFrom reads back what [WithReceiptPending] armed, or nil.
func receiptPendingFrom(ctx context.Context) ReceiptPending {
	if ctx == nil {
		return nil
	}
	pending, _ := ctx.Value(receiptPendingContextKey{}).(ReceiptPending)
	return pending
}

// billingFrom reads back the sink WithBilling armed, or nil.
func billingFrom(ctx context.Context) BillingSink {
	if ctx == nil {
		return nil
	}
	sink, _ := ctx.Value(billingContextKey{}).(BillingSink)
	return sink
}

// reconcileFrom reads back the sink [WithReconcile] armed, or nil.
func reconcileFrom(ctx context.Context) ReconcileSink {
	if ctx == nil {
		return nil
	}
	sink, _ := ctx.Value(reconcileContextKey{}).(ReconcileSink)
	return sink
}

// bill reports one decoded response to whoever is banking this work.
//
// It is called from the two places a billed answer is decoded — the whole-body
// completion and the end of a stream — and from nowhere else, so a call that is
// retried, repaired or relaxed banks once per answer the provider actually
// returned, which is once per answer it actually charged for.
//
// The model named is whoever ANSWERED, read the way the pool's own billing
// reads it: the slot's model unless the routing lane recorded a served rung,
// because a row naming the model somebody asked for is a row that cannot be
// summed per model after an escalation.
func (c *Client) bill(ctx context.Context, model string, response *ai.Response) {
	sink := billingFrom(ctx)
	if sink == nil || response == nil || response.Usage == nil {
		return
	}
	if served := CallFrom(ctx).Model(); served != "" {
		model = served
	}
	billed := Billed{
		Node:             callNode(ctx),
		Model:            model,
		PromptTokens:     response.Usage.PromptTokens,
		CompletionTokens: response.Usage.CompletionTokens,
		CachedTokens:     response.Usage.CacheReadTokens(),
	}
	if response.Usage.Cost != nil {
		billed.Cost = *response.Usage.Cost
	}
	if billed.Empty() {
		return
	}
	sink(billed)
}

// BillingSinkFrom and CallNodeFrom read back what a leaf's context was armed
// with. They exist for the surfaces that arm it and the tests that check they
// did: arming billing is one line at three call sites, and a call site that
// silently armed nothing is exactly the shape of the defect this file answers.
func BillingSinkFrom(ctx context.Context) BillingSink { return billingFrom(ctx) }

// ReconcileSinkFrom reads back the receipt sink [WithReconcile] armed, or nil.
func ReconcileSinkFrom(ctx context.Context) ReconcileSink { return reconcileFrom(ctx) }

// ReceiptPendingFrom reads back what [WithReceiptPending] armed, or nil — for a
// scripted funnel that owes a receipt the way the provider's own does.
func ReceiptPendingFrom(ctx context.Context) ReceiptPending { return receiptPendingFrom(ctx) }

// CallNodeFrom is the node WithCallNode named, empty when nothing did.
func CallNodeFrom(ctx context.Context) string { return callNode(ctx) }
