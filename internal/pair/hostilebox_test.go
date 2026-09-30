package pair

// The relay as an enemy. A hostileBox stands between a device and a real
// mailbox and does to the messages whatever a test scripted: swallows one,
// says it twice, holds it back, changes a byte, hands an old answer out again,
// or refuses to be reached. It also keeps a copy of everything either side ever
// wrote, which is the strongest way to ask "what could the relay read": the
// recording is a superset of what any relay could have known.
//
// SCRIPTS ARE KEYED BY THE NUMBER OF THE MESSAGE IN THE INTRODUCTION, not by a
// side and an index, because that is how the contract talks: message 3 is the
// joining device's first Noise message wherever it is stored.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pairbox"
)

// messageNumbers is which introduction message each write of a side is, in the
// order the two devices meant to send them. The joining device's third write is
// its word that it read the answer.
var messageNumbers = map[pairbox.Side][]int{
	pairbox.SideB: {1, 3, 5},
	pairbox.SideA: {2, 4},
}

// fate is what a relay does to one write. It answers what the writer is told.
type fate func(h *hostileBox, w write) (int, error)

// write is one Post as the relay saw it.
type write struct {
	ctx   context.Context
	plate string
	side  pairbox.Side
	key   pairbox.Key
	msg   []byte
	// index is how many earlier writes this side made to this mailbox.
	index int
}

type hostileBox struct {
	mu    sync.Mutex
	inner pairbox.Box
	fates map[int]fate
	// sent counts the writes each side of each mailbox made, stored or not.
	sent map[string]int
	// wrote is every message each side wrote, whatever became of it.
	wrote map[pairbox.Side][][]byte
	calls map[string]int
	// keyOf remembers the key that made each mailbox, so a restart can end them.
	keyOf map[string]pairbox.Key
	// limitsErr and createErr, when set, are what those calls answer.
	limitsErr, createErr error
	// replays holds, per side, the first batch a poll returned and whether it is
	// to be handed out again.
	replays map[pairbox.Side]*replay
	// held is a write kept back until the same side writes again.
	held map[string]write
	// reached is closed once the message of that number has been handled.
	reached map[int]chan struct{}
}

type replay struct {
	batch *pairbox.Batch
	used  bool
}

func newHostile(inner pairbox.Box) *hostileBox {
	return &hostileBox{inner: inner, fates: map[int]fate{}, sent: map[string]int{},
		wrote: map[pairbox.Side][][]byte{}, calls: map[string]int{}, keyOf: map[string]pairbox.Key{},
		replays: map[pairbox.Side]*replay{}, held: map[string]write{}, reached: map[int]chan struct{}{}}
}

var _ pairbox.Box = (*hostileBox)(nil)

// ── the scripts ─────────────────────────────────────────────────────────────

// on scripts what happens to message n.
func (h *hostileBox) on(n int, f fate) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fates[n] = f
}

// swallow says message n was stored and stores nothing.
func swallow(*hostileBox, write) (int, error) { return 0, nil }

// twice stores message n two times.
func twice(h *hostileBox, w write) (int, error) {
	if _, err := h.store(w); err != nil {
		return 0, err
	}
	return h.store(w)
}

// flipped stores message n with one byte changed.
func flipped(h *hostileBox, w write) (int, error) {
	w.msg = append([]byte(nil), w.msg...)
	w.msg[len(w.msg)/2] ^= 0x40
	return h.store(w)
}

// holdBack keeps message n until the same side writes again, and then stores
// the newer message first.
func holdBack(h *hostileBox, w write) (int, error) {
	h.mu.Lock()
	h.held[w.plate+string(w.side)] = w
	h.mu.Unlock()
	return 0, nil
}

// replayBatches makes the next poll of a side, after it has once returned
// messages, hand that same batch out again.
func (h *hostileBox) replayBatches(side pairbox.Side) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.replays[side] = &replay{}
}

// failLimits makes Limits answer err.
func (h *hostileBox) failLimits(err error) { h.mu.Lock(); h.limitsErr = err; h.mu.Unlock() }

// failCreate makes Create answer err.
func (h *hostileBox) failCreate(err error) { h.mu.Lock(); h.createErr = err; h.mu.Unlock() }

// restart ends every live mailbox, waking the polls on it, and then serves from
// a service that has never heard of any of them: a relay that was restarted.
func (h *hostileBox) restart(fresh pairbox.Box) {
	h.mu.Lock()
	old, keys := h.inner, h.keyOf
	h.inner, h.keyOf = fresh, map[string]pairbox.Key{}
	h.mu.Unlock()
	for plate, key := range keys {
		_ = old.Delete(context.Background(), plate, key)
	}
}

// ── what it saw ─────────────────────────────────────────────────────────────

// reachedMessage is closed once message n has been written, or swallowed.
func (h *hostileBox) reachedMessage(n int) <-chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.gate(n)
}

func (h *hostileBox) gate(n int) chan struct{} {
	if h.reached[n] == nil {
		h.reached[n] = make(chan struct{})
	}
	return h.reached[n]
}

// wroteBy is every message a side wrote, in order.
func (h *hostileBox) wroteBy(side pairbox.Side) [][]byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([][]byte(nil), h.wrote[side]...)
}

// everything is every byte either side wrote.
func (h *hostileBox) everything() []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	var all bytes.Buffer
	for _, side := range []pairbox.Side{pairbox.SideA, pairbox.SideB} {
		for _, m := range h.wrote[side] {
			all.Write(m)
		}
	}
	return all.Bytes()
}

// calledTimes is how often a method was reached, whatever it answered.
func (h *hostileBox) calledTimes(method string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls[method]
}

// requests is every call of every method.
func (h *hostileBox) requests() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	total := 0
	for _, n := range h.calls {
		total += n
	}
	return total
}

func (h *hostileBox) enter(method string) pairbox.Box {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls[method]++
	return h.inner
}

// ── the Box it pretends to be ───────────────────────────────────────────────

func (h *hostileBox) Limits(ctx context.Context) (pairbox.Limits, error) {
	inner := h.enter("Limits")
	h.mu.Lock()
	err := h.limitsErr
	h.mu.Unlock()
	if err != nil {
		return pairbox.Limits{}, err
	}
	return inner.Limits(ctx)
}

func (h *hostileBox) Create(ctx context.Context, key pairbox.Key) (pairbox.Created, error) {
	inner := h.enter("Create")
	h.mu.Lock()
	err := h.createErr
	h.mu.Unlock()
	if err != nil {
		return pairbox.Created{}, err
	}
	made, err := inner.Create(ctx, key)
	if err == nil {
		h.mu.Lock()
		h.keyOf[made.Nameplate] = key
		h.mu.Unlock()
	}
	return made, err
}

func (h *hostileBox) Delete(ctx context.Context, plate string, key pairbox.Key) error {
	return h.enter("Delete").Delete(ctx, plate, key)
}

func (h *hostileBox) Post(ctx context.Context, plate string, side pairbox.Side, key pairbox.Key, msg []byte) (int, error) {
	h.enter("Post")
	w, n, f := h.note(write{ctx: ctx, plate: plate, side: side, key: key, msg: append([]byte(nil), msg...)})
	defer h.signal(n)
	if f == nil {
		f = (*hostileBox).storeAndRelease
	}
	return f(h, w)
}

// note counts and records a write, and finds what is to be done to it.
func (h *hostileBox) note(w write) (write, int, fate) {
	h.mu.Lock()
	defer h.mu.Unlock()
	k := w.plate + string(w.side)
	w.index = h.sent[k]
	h.sent[k]++
	h.wrote[w.side] = append(h.wrote[w.side], w.msg)
	n := 0
	if numbers := messageNumbers[w.side]; w.index < len(numbers) {
		n = numbers[w.index]
	}
	return w, n, h.fates[n]
}

// signal marks message n as handled.
func (h *hostileBox) signal(n int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	select {
	case <-h.gate(n):
	default:
		close(h.gate(n))
	}
}

// store hands a write to the real mailbox.
func (h *hostileBox) store(w write) (int, error) {
	h.mu.Lock()
	inner := h.inner
	h.mu.Unlock()
	return inner.Post(w.ctx, w.plate, w.side, w.key, w.msg)
}

// storeAndRelease is the honest path, which also lets go of a write that was
// held back for this side.
func (h *hostileBox) storeAndRelease(w write) (int, error) {
	n, err := h.store(w)
	h.mu.Lock()
	late, ok := h.held[w.plate+string(w.side)]
	delete(h.held, w.plate+string(w.side))
	h.mu.Unlock()
	if ok && err == nil {
		_, err = h.store(late)
	}
	return n, err
}

func (h *hostileBox) Poll(ctx context.Context, plate string, side pairbox.Side, after int, wait time.Duration) (pairbox.Batch, error) {
	h.enter("Poll")
	if again, ok := h.replayed(side); ok {
		return again, nil
	}
	got, err := h.current().Poll(ctx, plate, side, after, wait)
	if err == nil && len(got.Msgs) > 0 {
		h.remember(side, got)
	}
	return got, err
}

func (h *hostileBox) current() pairbox.Box {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.inner
}

func (h *hostileBox) remember(side pairbox.Side, b pairbox.Batch) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r := h.replays[side]; r != nil && r.batch == nil {
		r.batch = &b
	}
}

// replayed is the recorded batch of a side that is set to replay, once.
func (h *hostileBox) replayed(side pairbox.Side) (pairbox.Batch, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.replays[side]
	if r == nil || r.batch == nil || r.used {
		return pairbox.Batch{}, false
	}
	r.used = true
	return *r.batch, true
}

// ── errors a network makes ──────────────────────────────────────────────────

// unreachable is what an HTTP client answers when nothing is listening: a
// *url.Error around a net error.
func unreachable() error {
	return &url.Error{Op: "Get", URL: "https://relay.test/v1/pair/limits",
		Err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}}
}

// ── what must never be readable ─────────────────────────────────────────────

// secretsOf is every byte string that would give an identity away: the whole
// document, each secret as hex text and each as raw bytes. They are compared
// against and never printed.
func secretsOf(t *testing.T, id identity.Identity) [][]byte {
	t.Helper()
	doc, err := id.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(doc, &fields); err != nil {
		t.Fatal(err)
	}
	out := [][]byte{doc}
	for _, v := range fields {
		text, ok := v.(string)
		if !ok {
			continue
		}
		raw, err := hex.DecodeString(text)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, []byte(text), raw)
	}
	return out
}

// leaks reports how many of the secrets appear in what was recorded.
func leaks(recorded []byte, secrets [][]byte) int {
	n := 0
	for _, s := range secrets {
		if bytes.Contains(recorded, s) {
			n++
		}
	}
	return n
}
