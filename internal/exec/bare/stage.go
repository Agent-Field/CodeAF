package bare

import (
	"context"
	"encoding/json"
	"sync"
)

// ── a call that may start before the message carrying it is whole ───────────
//
// A tool that HANDS WORK OUT does two different things in one call. It gets the
// work ready to go — reads its arguments, takes a slot, puts a card in front of
// the person and starts the card's clock — and then it lets the work go. The
// first half can be taken back without anybody being the worse for it; the
// second cannot. Most tools have no first half worth the name, and for them
// Execute is the whole call and nothing here applies.
//
// For the ones that do, the split is the tool's SHAPE, never a flag it sets
// about itself. [StagedTool] builds a tool out of its first half alone, and the
// second half is reachable only through the [Staged] value that first half
// returns — so a tool cannot claim to be safe to start early without actually
// being cut in two at the point where it stops being safe. A flag would be a
// promise the caller has no way to check, and the caller that relies on it is
// the one that runs calls nobody has finished asking for yet.
//
// THE CALLER THAT STARTS SUCH A CALL EARLY HOLDS IT AT THE SEAM. The session's
// turn loop sees a call's arguments close while the rest of the message is
// still streaming, and it may start a staged call right then, on a context
// carrying a [Hold]. The first half runs at once; the second waits at
// [RunStaged] until the loop knows whether the message arrived whole, and the
// loop then releases the hold (the message is recorded, the call goes ahead) or
// withdraws it (the stream was cut and will be asked again, the reply was
// refused, the turn ended) — and a withdrawn call takes back everything its
// first half did. A caller that did not start the call early holds nothing, and
// the two halves run back to back exactly as one Execute always did.

// Staged is one call of a staged tool, taken as far as it goes without letting
// anything go.
//
// Exactly one of the two methods is ever called, and at most once: a call either
// goes ahead or is taken back, never both and never twice. [RunStaged] is the
// only caller of either, which is what keeps that true.
type Staged interface {
	// Commit finishes the call — waits for whatever the work still needs, lets
	// it go — and returns what Execute returns.
	Commit(ctx context.Context) (text string, isError bool, err error)
	// Withdraw takes back everything the first half did: a slot it took, a card
	// it showed, a clock it started. Nothing may be left behind that says the
	// call happened, because as far as the conversation will ever record, it
	// did not.
	Withdraw()
}

// Settled is a [Staged] whose answer is already known: a refusal the first half
// reached before it did anything that would need taking back. Committing it
// hands the refusal over; withdrawing it has nothing to undo.
func Settled(text string, isError bool) Staged { return settled{text: text, isError: isError} }

type settled struct {
	text    string
	isError bool
}

func (s settled) Commit(context.Context) (string, bool, error) { return s.text, s.isError, nil }

func (settled) Withdraw() {}

// StagedTool is the one way to build a tool that may start before the message
// carrying its call is whole. Execute is derived from stage and is the two
// halves run back to back through [RunStaged]; nothing outside this package can
// build a tool whose [Tool.Stages] answers yes.
func StagedTool(name, description string, schema json.RawMessage, stage func(ctx context.Context, args json.RawMessage) Staged) Tool {
	return Tool{
		Name:        name,
		Description: description,
		Schema:      schema,
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			return RunStaged(ctx, stage(ctx, args))
		},
		staged: true,
	}
}

// Stages reports whether this tool was built by [StagedTool], which is the
// property an early start keys on: a call to such a tool may begin while its
// message is still arriving, because everything it does before [RunStaged]'s
// hold can be taken back.
func (t Tool) Stages() bool { return t.staged }

// withdrawnBeforeItWent is what a withdrawn call returns. Nobody reads it in
// the ordinary course — the loop that withdrew the call has already thrown its
// result away — but a call is never allowed to return nothing, and if a turn
// ever does record it, it must say what happened in the model's own terms.
const withdrawnBeforeItWent = "this call was taken back before it went ahead: the turn ended, or the reply that carried it was cut off"

// RunStaged is the seam between the two halves: it waits at the [Hold] the
// context carries, if any, then commits the call or withdraws it.
//
// A context with no hold is released already, which is every call that was not
// started early and every caller outside a turn: the halves run back to back.
func RunStaged(ctx context.Context, staged Staged) (string, bool, error) {
	if !holdFrom(ctx).wait(ctx) {
		staged.Withdraw()
		return withdrawnBeforeItWent, true, nil
	}
	return staged.Commit(ctx)
}

// Hold is where an early start keeps a staged call between its two halves,
// until it knows whether the message carrying the call arrived whole.
//
// IT IS DECIDED ONCE. The first of [Hold.Release] and [Hold.Withdraw] wins and
// every later one is a no-op, so the loop may withdraw on every road out of an
// attempt — a retry, a steer, a refused reply, the end of the turn — without
// asking whether some other road already released the call. Both methods are
// safe on a nil Hold, which is the hold of a call that was never held.
type Hold struct {
	once    sync.Once
	decided chan struct{}
	release bool
}

// NewHold is a hold nobody has decided yet.
func NewHold() *Hold { return &Hold{decided: make(chan struct{})} }

// Release lets the held call go ahead: its message arrived whole and is in the
// transcript.
func (h *Hold) Release() { h.decide(true) }

// Withdraw takes the held call back: its message did not arrive whole, or the
// turn will not run it.
func (h *Hold) Withdraw() { h.decide(false) }

func (h *Hold) decide(release bool) {
	if h == nil {
		return
	}
	h.once.Do(func() {
		h.release = release
		close(h.decided)
	})
}

// wait blocks until the hold is decided and reports whether it was released.
// The context ending first is a withdrawal: the turn that would have released
// the call is gone. A decision already made is read before the context, so a
// call released a moment before its turn was stopped still goes ahead, exactly
// as it would have had it been run in the batch.
func (h *Hold) wait(ctx context.Context) bool {
	if h == nil {
		return true
	}
	select {
	case <-h.decided:
		return h.release
	default:
	}
	select {
	case <-h.decided:
		return h.release
	case <-ctx.Done():
		return false
	}
}

type holdKey struct{}

// WithHold returns a context that holds a staged call run on it at
// [RunStaged] until h is decided.
func WithHold(ctx context.Context, h *Hold) context.Context {
	return context.WithValue(ctx, holdKey{}, h)
}

func holdFrom(ctx context.Context) *Hold {
	h, _ := ctx.Value(holdKey{}).(*Hold)
	return h
}
