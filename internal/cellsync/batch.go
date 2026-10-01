package cellsync

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
)

const (
	// DefaultInterval is how often a flush publishes when the caller sets none.
	DefaultInterval = 5 * time.Second
	// MaxBackoff caps the wait between flushes after failures.
	MaxBackoff = 60 * time.Second
)

// Publishing wraps the Stage 0 store: seal exactly as before, then note the
// turn. A seal never waits on the network.
type Publishing struct {
	Inner   cellstore.Store
	Batcher *Batcher
}

// Batcher publishes the newest sealed turn on an interval and keeps the lease
// alive while nothing is being published, each on its own loop so a slow upload
// cannot starve a heartbeat.
type Batcher struct {
	Publisher    *Publisher
	Brancher     Brancher
	Driving      *Driving
	Interval     time.Duration // default DefaultInterval
	Info         func() PublishInfo
	OnFlush      func(Flush)      // telemetry hook
	OnSuperseded func(Superseded) // the chat becomes a viewer (L7)
	OnError      func(error)      // a refusal (see RefusalOf) the person should hear of, once per episode
	Now          func() time.Time // default time.Now; tests inject a fake clock
	// Liveness, when set, is a watch socket that can vouch for the lease, so
	// the heartbeat is sent only while it cannot. Nil beats every tick.
	Liveness Liveness
	// Sleep waits d or until ctx ends; tests inject a fake. Default is real time.
	Sleep func(ctx context.Context, d time.Duration) error

	mu        sync.Mutex // guards Driving, noted, stale, published, refusedSince and told; never held over the network
	noted     []string   // heads of sealed turns not yet durable, oldest first
	stale     bool       // the lease was lost and the orphans are not branched yet
	published bool       // a publish renewed the lease since the last heartbeat tick
	beatSent  uint32     // the pending count the directory last heard, from a beat or a publish

	refusedSince time.Time // when the present run of refusals began; zero while the relay is answering
	told         string    // the kind of refusal the person has heard of in this run, so each is said once

	work  wakeup // tells the flush loop a turn was noted, so a closed window can open at once
	idle  wakeup // asks the flush loop to upload now: the agent stopped to wait for the person
	freed wakeup // ends a halt: something the relay refused for has changed

	flushMu sync.Mutex // one flush, branch or close at a time
	backoff time.Duration
	failing bool // the last flush failed, so Idle must wait out the backoff
}

// Superseded says another device continued the chat.
type Superseded struct {
	// Branch is the new cell that holds this device's orphaned turns; empty
	// when there were none and the chat simply becomes a viewer.
	Branch string
	// By is the device that took the chat, from the directory's lease holder;
	// empty when the directory could not be asked.
	By string
}

// Flush is what one publish sent.
type Flush struct {
	Head    string
	Turns   int
	Frames  int
	Objects int
	Bytes   int64
}

// Seal implements cellstore.Store.
func (p *Publishing) Seal(ctx context.Context, c cell.Cell, info cellstore.TurnInfo) (cellstore.Sealed, error) {
	s, err := p.Inner.Seal(ctx, c, info)
	if err == nil {
		p.Batcher.Note(s.Turn)
	}
	return s, err
}

// Note records a sealed turn and wakes the flush loop. It only takes a lock and
// never waits, so it is safe on the seal path. It sends nothing itself: the turn
// goes up with the next publish, which is also what tells the directory it is no
// longer pending.
func (b *Batcher) Note(t cellstore.Turn) {
	b.mu.Lock()
	if n := len(b.noted); n == 0 || b.noted[n-1] != t.ID {
		b.noted = append(b.noted, t.ID)
	}
	b.mu.Unlock()
	b.work.fire()
}

// Idle says the agent has stopped and is waiting for the person, so the turns
// sealed since the last upload go up at once instead of at the end of the
// window: nothing more is coming to share the request with, and this is the
// moment the person is likeliest to pick the chat up on another machine. It
// never waits, so it is safe to call from the turn's own goroutine.
func (b *Batcher) Idle() { b.idle.fire() }

// Freed says something a refusal waited on has changed (space was freed on the
// relay, say), so a halted chat tries again now instead of waiting for a
// reopen. It never waits, so it is safe to call from any goroutine.
func (b *Batcher) Freed() { b.freed.fire() }

// wakeup carries at most one pending request from a caller to a loop. The zero
// value is ready to use.
type wakeup struct {
	once sync.Once
	c    chan struct{}
}

func (w *wakeup) ch() chan struct{} {
	w.once.Do(func() { w.c = make(chan struct{}, 1) })
	return w.c
}

// fire leaves a request for the loop and never blocks; a request already
// waiting will be answered with the state this one would have seen.
func (w *wakeup) fire() {
	select {
	case w.ch() <- struct{}{}:
	default:
	}
}

// Fence is the lease fence this device now holds, 0 before the cell has a record.
func (b *Batcher) Fence() uint64 {
	d, _, _ := b.view()
	return d.Fence
}

// Pending is the number of noted turns that are not durable yet.
func (b *Batcher) Pending() uint32 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return uint32(len(b.noted))
}

// Run uploads noted turns and heartbeats each directory.HeartbeatEvery until
// ctx ends. An upload opens a window of one Interval; turns sealed while it is
// open wait for it to close and then go up together, so a busy agent costs two
// publishes a window at most and a lone tool call is durable at once. Sealing
// is never held back: only the upload waits.
func (b *Batcher) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	wg.Go(func() { b.beats(ctx) })
	b.flushes(ctx)
	wg.Wait()
	return ctx.Err()
}

// flushes is the whole state machine: wait until a turn is noted, upload it,
// which opens a window, and rest until the window closes. A turn noted while
// the window is open is still noted when it closes, so it goes straight up and
// opens the next window; a turn noted in a quiet period goes up at once. There
// is no separate rule for the first turn of a burst.
func (b *Batcher) flushes(ctx context.Context) {
	for {
		if b.awaitWork(ctx) != nil {
			return
		}
		if err := b.rest(ctx, b.flush(ctx)); err != nil {
			return
		}
	}
}

// awaitWork returns once a turn is noted. An Idle with nothing noted has
// nothing to send, so it is dropped here and never shortens a later window.
func (b *Batcher) awaitWork(ctx context.Context) error {
	for b.Pending() == 0 {
		select {
		case <-b.work.ch():
		case <-b.idle.ch():
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// rest waits out the window d, which is zero when the flush sent nothing. After
// a failure it waits the whole backoff whatever Idle says, and forgets an Idle
// that arrived meanwhile: when the relay is failing, a client that retried after
// every finished turn would be thousands of clients hammering it at once. The
// flush that ends the backoff sends what Idle wanted sent.
func (b *Batcher) rest(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	if !b.failing {
		return b.nap(ctx, d, &b.idle)
	}
	err := b.nap(ctx, d, &b.freed)
	select {
	case <-b.idle.ch():
	default:
	}
	return err
}

// beats renews the lease once every HeartbeatEvery in which nothing was
// published. A publish is proof of life and renews the lease itself, so a busy
// chat costs the relay no heartbeat at all. An idle one costs one per tick,
// unless a watch socket is vouching for the lease (Liveness): the socket's own
// pings are then the proof, and a beat is sent only when the count of pending
// turns has moved, which the directory shows, and at once when the socket stops
// vouching, so a dead socket is covered before the lease would have lapsed.
func (b *Batcher) beats(ctx context.Context) {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	ticks, handled := b.ticker(ctx)
	var held holding
	defer held.release()
	for {
		held.follow(b.Liveness, b.view)
		select {
		case <-ticks:
			b.tick(ctx, &held)
			handled <- struct{}{}
		case <-held.changes():
			b.socketMoved(ctx, &held)
		case <-ctx.Done():
			return
		}
	}
}

// ticker delivers one tick per HeartbeatEvery until ctx ends, and counts the
// next interval only once the caller says the tick was handled on handled, so
// a slow beat does not make two ticks run together.
func (b *Batcher) ticker(ctx context.Context) (ticks <-chan struct{}, handled chan<- struct{}) {
	out, ack := make(chan struct{}), make(chan struct{}, 1)
	go func() {
		for b.sleeper()(ctx, directory.HeartbeatEvery) == nil {
			select {
			case out <- struct{}{}:
			case <-ctx.Done():
				return
			}
			select {
			case <-ack:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, ack
}

// tick is one heartbeat interval gone by. The lease needs a beat unless a
// publish renewed it or the socket vouches and the directory already knows the
// pending count.
func (b *Batcher) tick(ctx context.Context, held *holding) {
	if b.takePublished() {
		return
	}
	if !held.covers(b.view, b.toldPending) {
		b.beat(ctx)
	}
}

// socketMoved is the socket's state changing. When it no longer vouches the
// lease is on its stored expiry alone, so the beat goes now, not at the tick.
func (b *Batcher) socketMoved(ctx context.Context, held *holding) {
	if !held.covers(b.view, b.toldPending) {
		b.beat(ctx)
	}
}

// toldPending says whether n is what the directory last heard as pending.
func (b *Batcher) toldPending(n uint32) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.beatSent == n
}

func (b *Batcher) tell(n uint32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.beatSent = n
}

// takePublished says whether a publish renewed the lease since the last call,
// and forgets it. A publish just after one tick is skipped at the next tick and
// covered by the tick after, at most two ticks, which the TTL allows.
func (b *Batcher) takePublished() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	was := b.published
	b.published = false
	return was
}

// nap waits d, or until w is fired. The sleep is the injected one, so a fake
// clock governs it. It answers only ctx's own error: being woken is not one.
func (b *Batcher) nap(ctx context.Context, d time.Duration, w *wakeup) error {
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	over := make(chan struct{})
	woke := make(chan struct{})
	go func() {
		defer close(woke)
		select {
		case <-w.ch():
			cancel()
		case <-over:
		}
	}()
	_ = b.sleeper()(sctx, d)
	close(over)
	<-woke
	return ctx.Err()
}

func (b *Batcher) sleeper() func(context.Context, time.Duration) error {
	if b.Sleep != nil {
		return b.Sleep
	}
	return realSleep
}

func realSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close is the quit and lid-close path: flush, publish, release.
func (b *Batcher) Close(ctx context.Context) error {
	b.flushMu.Lock()
	defer b.flushMu.Unlock()
	err := b.drain(ctx)
	return errors.Join(err, b.release(ctx))
}

// release gives the lease up so another device can take over at once. A lease
// already lost has nothing to release.
func (b *Batcher) release(ctx context.Context) error {
	d, _, stale := b.view()
	if d.Fence == 0 || stale {
		return nil
	}
	err := b.Publisher.Dir.Release(ctx, d.ID(), d.Fence)
	if errors.Is(err, directory.ErrFenceStale) {
		return nil
	}
	return err
}

// flush publishes the newest noted turn and says how long the window it opened
// lasts: the interval, or a doubled backoff after a failure. A flush that had
// nothing to send opens no window.
func (b *Batcher) flush(ctx context.Context) time.Duration {
	b.flushMu.Lock()
	defer b.flushMu.Unlock()
	_, sending := b.newest()
	err := b.drain(ctx)
	window := b.after(err)
	if sending == 0 && err == nil {
		return 0
	}
	return window
}

// after turns a flush's outcome into the next wait. A failure that is not a
// refusal is the zero Refusal, which backs off and says nothing.
func (b *Batcher) after(err error) time.Duration {
	interval := b.interval()
	b.failing = err != nil
	if err == nil {
		b.backoff = interval
		return b.backoff
	}
	r, _ := b.surface(err)
	b.backoff = r.next(b.backoff, interval, err)
	return b.backoff
}

func (b *Batcher) interval() time.Duration {
	if b.Interval > 0 {
		return b.Interval
	}
	return DefaultInterval
}

// drain publishes what is noted, or branches it when the lease is already lost.
// The caller holds flushMu.
func (b *Batcher) drain(ctx context.Context) error {
	head, n := b.newest()
	if n == 0 {
		return nil
	}
	if _, _, stale := b.view(); stale {
		return b.resolve(ctx)
	}
	return b.publish(ctx, head)
}

func (b *Batcher) publish(ctx context.Context, head string) error {
	d, _, _ := b.view()
	ex, err := b.Publisher.publish(ctx, &d, head, b.info())
	if errors.Is(err, ErrSuperseded) {
		b.setStale()
		return b.resolve(ctx)
	}
	if err != nil {
		return err
	}
	b.adopt(d)
	b.setFlag(&b.published, true)
	b.tell(0) // a publish always carries the newest turn, so nothing is pending
	b.recovered()
	turns := b.durable(head)
	b.emit(Flush{Head: head, Turns: turns, Frames: len(ex.Frames), Objects: ex.Objects, Bytes: ex.Bytes})
	return nil
}

// beat renews the lease and reports how many turns are not durable yet.
func (b *Batcher) beat(ctx context.Context) {
	d, pending, stale := b.view()
	if d.Fence == 0 || stale {
		return
	}
	_, err := b.Publisher.Dir.Heartbeat(ctx, d.ID(), directory.Beat{Fence: d.Fence, Pending: pending})
	switch {
	case err == nil:
		b.tell(pending)
		b.recovered()
	case errors.Is(err, directory.ErrFenceStale):
		b.lost(ctx, d)
	default:
		b.surface(err)
	}
}

// lost handles a heartbeat that found the lease of seen gone. It waits for any
// flush in flight, and does nothing if that flush already branched. When the
// branch fails the state stays stale and the next flush retries it.
func (b *Batcher) lost(ctx context.Context, seen Driving) {
	b.flushMu.Lock()
	defer b.flushMu.Unlock()
	if now, _, _ := b.view(); now.ID() != seen.ID() || now.Fence != seen.Fence {
		return
	}
	b.setStale()
	_ = b.resolve(ctx)
}

// resolve turns the orphaned turns into a branch, or, when there are none,
// just tells the caller the chat is now a viewer. The caller holds flushMu.
func (b *Batcher) resolve(ctx context.Context) error {
	head, n := b.newest()
	by := b.holder(ctx)
	if n == 0 {
		b.superseded(Superseded{By: by})
		return nil
	}
	id, err := b.branch(ctx, head, n)
	if err != nil {
		return err
	}
	b.durable(head)
	b.clearStale()
	b.superseded(Superseded{Branch: id, By: by})
	return nil
}

// holder names the device that now holds the lease, or "" when the directory
// cannot say; the answer only decorates a message, so it is never an error.
func (b *Batcher) holder(ctx context.Context) string {
	d, _, _ := b.view()
	v, err := b.Publisher.Dir.Cell(ctx, d.ID())
	if err != nil {
		return ""
	}
	return v.Cell.Lease.Device
}

// branch creates the cell that holds the orphaned turns; the Brancher uploads
// their objects first.
func (b *Batcher) branch(ctx context.Context, head string, turns int) (string, error) {
	d, _, _ := b.view()
	id, err := b.Brancher.Branch(ctx, &d, head, uint32(turns), b.info())
	if err != nil {
		return "", err
	}
	b.adopt(d)
	return id, nil
}

// info is what the caller says about the cell now, with nothing pending: a
// publish always carries the newest turn.
func (b *Batcher) info() PublishInfo {
	var in PublishInfo
	if b.Info != nil {
		in = b.Info()
	}
	in.Pending = 0
	return in
}

// newest is the head of the newest noted turn and how many are noted.
func (b *Batcher) newest() (string, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.noted) == 0 {
		return "", 0
	}
	return b.noted[len(b.noted)-1], len(b.noted)
}

// durable forgets the noted turns up to and including head, answers how many.
func (b *Batcher) durable(head string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, id := range b.noted {
		if id == head {
			b.noted = b.noted[i+1:]
			return i + 1
		}
	}
	return 0
}

// view is a copy of the driving state, how many turns are pending, and whether
// the lease is known lost.
func (b *Batcher) view() (Driving, uint32, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return *b.Driving, uint32(len(b.noted)), b.stale
}

func (b *Batcher) adopt(d Driving) {
	b.mu.Lock()
	defer b.mu.Unlock()
	*b.Driving = d
}

func (b *Batcher) setStale()   { b.setFlag(&b.stale, true) }
func (b *Batcher) clearStale() { b.setFlag(&b.stale, false) }

func (b *Batcher) setFlag(flag *bool, v bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	*flag = v
}

// surface tells the person of a refusal when the run of refusals is news, and
// answers which refusal err was.
func (b *Batcher) surface(err error) (Refusal, bool) {
	r, ok := RefusalOf(err)
	if ok && b.news(r) && b.OnError != nil {
		b.OnError(err)
	}
	return r, ok
}

// news says whether the person should hear r now: it has lasted as long as its
// row asks to stay quiet, and this sentence has not been said in this run. It
// marks the sentence as said, so asking twice answers true once.
func (b *Batcher) news(r Refusal) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if b.refusedSince.IsZero() {
		b.refusedSince = now
	}
	if b.told == r.Is.Error() || now.Sub(b.refusedSince) < r.Quiet {
		return false
	}
	b.told = r.Is.Error()
	return true
}

func (b *Batcher) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// recovered notes that the relay answered, so the next refusal is news again.
func (b *Batcher) recovered() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refusedSince, b.told = time.Time{}, ""
}

func (b *Batcher) emit(f Flush) {
	if b.OnFlush != nil {
		b.OnFlush(f)
	}
}

func (b *Batcher) superseded(s Superseded) {
	if b.OnSuperseded != nil {
		b.OnSuperseded(s)
	}
}
