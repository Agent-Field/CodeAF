package cellsync

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/wireauth"
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
// alive, each on its own loop so a slow upload cannot starve a heartbeat.
type Batcher struct {
	Publisher    *Publisher
	Brancher     Brancher
	Driving      *Driving
	Interval     time.Duration // default DefaultInterval
	Info         func() PublishInfo
	OnFlush      func(Flush)      // telemetry hook
	OnSuperseded func(Superseded) // the chat becomes a viewer (L7)
	OnError      func(error)      // wireauth.ErrSkew and other person-facing refusals
	// Sleep waits d or until ctx ends; tests inject a fake. Default is real time.
	Sleep func(ctx context.Context, d time.Duration) error

	mu        sync.Mutex // guards Driving, noted, stale and skewShown; never held over the network
	noted     []string   // heads of sealed turns not yet durable, oldest first
	stale     bool       // the lease was lost and the orphans are not branched yet
	skewShown bool

	flushMu sync.Mutex // one flush, branch or close at a time
	backoff time.Duration
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

// Note records a sealed turn. It only takes a lock, so it is safe on the seal path.
func (b *Batcher) Note(t cellstore.Turn) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n := len(b.noted); n == 0 || b.noted[n-1] != t.ID {
		b.noted = append(b.noted, t.ID)
	}
}

// Pending is the number of noted turns that are not durable yet.
func (b *Batcher) Pending() uint32 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return uint32(len(b.noted))
}

// Run flushes each Interval and heartbeats each directory.HeartbeatEvery until
// ctx ends.
func (b *Batcher) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	wg.Go(func() {
		b.loop(ctx, func() time.Duration { b.beat(ctx); return directory.HeartbeatEvery })
	})
	b.loop(ctx, func() time.Duration { return b.flush(ctx) })
	wg.Wait()
	return ctx.Err()
}

// loop does a step, then waits as long as the step asks.
func (b *Batcher) loop(ctx context.Context, step func() time.Duration) {
	sleep := b.Sleep
	if sleep == nil {
		sleep = realSleep
	}
	for sleep(ctx, step()) == nil {
	}
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

// flush publishes the newest noted turn and says how long to wait before the
// next flush: the interval, or a doubled backoff after a failure.
func (b *Batcher) flush(ctx context.Context) time.Duration {
	b.flushMu.Lock()
	defer b.flushMu.Unlock()
	return b.after(b.drain(ctx))
}

// after turns a flush's outcome into the next wait.
func (b *Batcher) after(err error) time.Duration {
	interval := b.interval()
	switch {
	case err == nil:
		b.backoff = interval
	case b.surface(err):
		b.backoff = interval
	default:
		b.backoff = min(2*max(b.backoff, interval), max(MaxBackoff, interval))
	}
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

// branch makes sure head's objects are in the store, then creates the branch
// cell that names them.
func (b *Batcher) branch(ctx context.Context, head string, turns int) (string, error) {
	d, _, _ := b.view()
	if _, err := b.Publisher.Upload(ctx, d.Cell, head); err != nil {
		return "", err
	}
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

// surface shows a person-facing refusal once and answers whether err was one.
// Skew is not retried on the backoff: the next tick simply tries again.
func (b *Batcher) surface(err error) bool {
	if !errors.Is(err, wireauth.ErrSkew) {
		return false
	}
	b.mu.Lock()
	first := !b.skewShown
	b.skewShown = true
	b.mu.Unlock()
	if first && b.OnError != nil {
		b.OnError(err)
	}
	return true
}

// recovered notes that the directory answered, so a later skew is news again.
func (b *Batcher) recovered() { b.setFlag(&b.skewShown, false) }

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
