package syncsetup

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstats"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/executor"
)

// simClock is the drive side's time in a test: nothing wakes until advance says
// so, so a scripted session of minutes runs in milliseconds and counts the same
// on every machine.
type simClock struct {
	mu      sync.Mutex
	changed *sync.Cond
	now     time.Duration
	waiters []*simWait
	// quiet says the flush loop has nothing noted, so it waits for a turn and
	// not on the clock; set once the drive exists.
	quiet func() bool
}

type simWait struct {
	wake time.Duration
	done chan struct{}
}

func newSimClock() *simClock {
	c := &simClock{}
	c.changed = sync.NewCond(&c.mu)
	return c
}

func (c *simClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	w := &simWait{wake: c.now + d, done: make(chan struct{})}
	c.waiters = append(c.waiters, w)
	c.changed.Broadcast()
	c.mu.Unlock()
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		c.waiters = slices.DeleteFunc(c.waiters, func(x *simWait) bool { return x == w })
		c.changed.Broadcast()
		c.mu.Unlock()
		return ctx.Err()
	}
}

// advance moves time on and wakes every wait that is now due.
func (c *simClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now += d
	kept := c.waiters[:0]
	for _, w := range c.waiters {
		if w.wake <= c.now {
			close(w.done)
			continue
		}
		kept = append(kept, w)
	}
	c.waiters = kept
}

// settle waits until the drive side's loops are asleep again, that is, until
// the work the last advance woke has finished. The heartbeat loop always sleeps
// on the clock; the flush loop sleeps on it inside a window and otherwise waits
// for a noted turn, which is when nothing is noted.
func (c *simClock) settle() {
	for !c.settled() {
		time.Sleep(50 * time.Microsecond)
	}
}

func (c *simClock) settled() bool {
	c.mu.Lock()
	n := len(c.waiters)
	c.mu.Unlock()
	return n == 2 || (n == 1 && c.quiet != nil && c.quiet())
}

// scriptedSession is a scripted chat on a simulated clock: turns of tool calls a few
// seconds apart, with the person away between turns.
type scriptedSession struct {
	r     *driveRig
	clock *simClock
	chat  *chat
	end   func() // what the door does when the agent finishes a turn
}

// callGap is how long a model takes between two tool calls; turnGap is how long
// the person takes to answer.
const (
	callGap = 3 * time.Second
	turnGap = 20 * time.Second
)

func (s *scriptedSession) tick(d time.Duration) {
	for ; d > 0; d -= time.Second {
		s.clock.advance(time.Second)
		s.clock.settle()
	}
}

func (s *scriptedSession) turn(calls int) {
	for range calls {
		s.tick(callGap)
		s.chat.mustSay()
	}
	s.end()
	s.tick(turnGap)
}

// cost is what a session asked of the relay, from the stats the chat wrote.
type cost struct {
	Puts, Publishes, Frames, Bytes int64
}

// costOf reads the cost of the chat's cell from its stats file.
func (r *driveRig) costOf() cost {
	lines, err := cellstats.Read(r.homeA, r.cell.ID)
	if err != nil {
		r.t.Fatal(err)
	}
	var c cost
	for _, l := range lines {
		if l.Turn != "" {
			c.Publishes++
		}
		c.Frames += int64(l.Frames)
	}
	total := cellstats.Total(lines)
	c.Puts, c.Bytes = total.Puts, total.BytesUp
	return c
}

// runScript plays four turns of ten tool calls over the real engine reached
// through its daemon, as a chat reaches it, and closes the chat.
func runScript(t *testing.T, idle func(*Drive)) cost {
	r := newDriveRig(t)
	r.engine.Transport = cellstore.Daemon{Socket: filepath.Join(t.TempDir(), "e.sock"), Binary: r.bin}
	r.a.Interval = DefaultInterval // the rig shortens it for tests that run on real time
	sim := newSimClock()
	drive, err := r.a.Drive(context.Background(), r.engine, r.cell, DriveOptions{DeviceName: "spark", Sleep: sim.Sleep})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = drive.Close(context.Background()) })
	sim.quiet = func() bool { return drive.batcher.Pending() == 0 }
	seat, err := cellstore.SeatOver(executor.HostBound, r.cell, r.work, nil, nil,
		func(cellstore.Engine) cellstore.Store { return drive.Store(r.engine) })
	if err != nil {
		t.Fatal(err)
	}
	s := &scriptedSession{r: r, clock: sim, chat: &chat{r: r, drive: drive, seat: executor.Gated(seat, drive.Gate)}}
	s.end = func() { idle(drive) }
	sim.settle()
	for range 4 {
		s.turn(10)
	}
	if err := drive.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	return r.costOf()
}

// TestScriptedSessionCosts pins what a busy chat costs the relay: forty tool
// calls over about three minutes make one publish per five-second window that
// had news, plus one when the agent stops, and each publish is one frame in one
// put however many objects its calls sealed. Over the engine's daemon, as a
// chat runs, the frame packing once fell to one frame per object (329 puts for
// 28 publishes on this script); the last line of this test is that regression.
func TestScriptedSessionCosts(t *testing.T) {
	got := runScript(t, (*Drive).Idle)
	t.Logf("cost: %+v", got)
	if got.Frames != got.Publishes || got.Puts != got.Frames {
		t.Fatalf("%d publishes made %d frames in %d puts; want one frame and one put each", got.Publishes, got.Frames, got.Puts)
	}
	const windows = int64(4*(10*callGap+turnGap)/DefaultInterval) + 4 // a window each, plus one idle upload per turn
	if got.Publishes == 0 || got.Publishes > windows {
		t.Fatalf("%d publishes, want between 1 and %d", got.Publishes, windows)
	}
}
