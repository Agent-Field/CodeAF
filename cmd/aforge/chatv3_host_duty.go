package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/remote"
)

// hostFar is the far machine as the background duties are given it: the
// connection, and the one place a sentence about a duty goes.
//
// IT IS THE ONE SEAM A DUTY IS ARMED THROUGH. The four readings hostOptions keeps
// warm behind the surface — the places, spending, memory, the standing items —
// each hold their wire door as a closure ([hostStanding] says why), and a closure
// made from a nil client is a perfectly good function right up until it is
// called, on a goroutine, where the nil dereference is a panic the guard swallows
// and nobody sees. So the fact "there is a connection behind this" is established
// here, once, and every duty asks it before it starts rather than finding out on
// the wire.
type hostFar struct {
	client *remote.Client
	// tell is where the one sentence about a duty that fell over goes — the
	// surface's one-off notice line, joined in by [hostNews]. Nil is told nothing.
	tell func(string)
}

// arm gives one duty its facts: whether there is a connection, where to speak,
// and what to call itself when it does.
func (f hostFar) arm(d *hostDuty, what string) {
	d.absent = f.client == nil
	d.tell = f.tell
	d.what = what
}

// hostDuty is one background reading of the far machine: the latch that keeps
// one round trip in flight at a time, the clock that says when the last one came
// back, and the door it leaves through. Three laws, and the type exists so that
// they are written once rather than four times:
//
// A DUTY WITHOUT A CONNECTION DOES NOT START. [hostFar.arm] says whether there
// is a client; a duty armed with none refuses every trip, so the surface reads
// what it holds — nothing — and no goroutine is ever sent down a nil.
//
// A DUTY THAT DIES LETS GO OF ITS LATCH. The latch is taken before the trip and
// used to be released inside it, after the answer came back — so a trip that
// panicked never released it, the next reading saw a fetch still out, and the
// page froze on whatever it held for the life of the process, with nothing said.
// The release is deferred here, so a beat later the question is asked again
// exactly as it would be after an ordinary error.
//
// AND THE SURFACE HEARS ABOUT IT ONCE. The fault is recorded where every fault
// is (guard.Note), and one sentence goes to the notice line the first time a
// duty falls over — once, because a duty that fails on every beat would
// otherwise be a notice on every beat, and the log already holds each of them.
//
// The zero value is a live duty with nobody to tell, which is what a test that
// hands a reader its own closure gets without arming anything.
type hostDuty struct {
	absent bool
	tell   func(string)
	what   string

	mu sync.Mutex
	// out is the key a trip is already out for, ended is when the last trip for
	// a key came back — however it came back — and said is whether this duty's
	// fault has been mentioned.
	out   map[string]bool
	ended map[string]time.Time
	said  bool
}

// due says whether key's last trip ended longer ago than every, or never ran.
// It is the staleness half of a reading; [hostDuty.run] is the other half and
// refuses on its own when a trip is already out, so a caller asks due and then
// runs without holding anything between the two.
func (d *hostDuty) due(key string, every time.Duration) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	at := d.ended[key]
	return at.IsZero() || time.Since(at) >= every
}

// age forgets when key last came back, so the next reading asks again at once —
// what a write that just landed wants, so the store is asked what it really
// thinks.
func (d *hostDuty) age(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.ended, key)
}

// run starts one trip for key on its own goroutine, unless one is already out
// or there is no connection to make it on, and reports whether it started.
func (d *hostDuty) run(scope, key string, trip func()) bool {
	if d.absent || !d.claim(key) {
		return false
	}
	guard.Go(scope, func() {
		defer d.land(scope, key)
		trip()
	})
	return true
}

// claim takes the latch for key, and says whether this caller got it. The lock
// is held from a defer, as every lock in the guarded tree is, so a fault inside
// the section can never leave the latch's own mutex taken.
func (d *hostDuty) claim(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.out[key] {
		return false
	}
	if d.out == nil {
		d.out = map[string]bool{}
	}
	d.out[key] = true
	return true
}

// land is deferred under every trip: the latch is released and the clock
// stamped however the trip ended, and a panic is recorded and mentioned here
// rather than left for the guard above to swallow.
func (d *hostDuty) land(scope, key string) {
	recovered := recover()
	tell, what, first := d.release(key, recovered != nil)
	if recovered == nil {
		return
	}
	_ = guard.Note(scope, recovered)
	if first && tell != nil {
		tell(fmt.Sprintf("%s over this connection fell over once and will be tried again", what))
	}
}

// release lets go of key's latch and stamps the clock, and — when the trip
// faulted — answers whether this is the first fault this duty has to mention,
// with what to say it through. It is the locked half of [hostDuty.land], split
// out so the mutex is held from a defer and let go before anything is said.
func (d *hostDuty) release(key string, faulted bool) (tell func(string), what string, first bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ended == nil {
		d.ended = map[string]time.Time{}
	}
	d.out[key] = false
	d.ended[key] = time.Now()
	first = faulted && !d.said
	if first {
		d.said = true
	}
	return d.tell, d.what, first
}

// hostNews joins the duties' sentences onto the connection's own one-off notice,
// so the surface goes on reading one line, once, from the seam it already has.
type hostNews struct {
	mu    sync.Mutex
	lines []string
}

// say queues one sentence for the next reading.
func (n *hostNews) say(sentence string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.lines = append(n.lines, sentence)
}

// join wraps the connection's notice: what it has to say comes first, then what
// the duties queued, joined the way the client joins two of its own. IT DRAINS,
// as the notice it wraps does, and answers the empty string when nobody has
// anything to say — which is the emptiness law's reading for a notice line.
func (n *hostNews) join(connection func() string) func() string {
	return func() string {
		var lines []string
		if connection != nil {
			if said := connection(); said != "" {
				lines = append(lines, said)
			}
		}
		return strings.Join(append(lines, n.take()...), " — ")
	}
}

// take drains what the duties queued, under the lock held from a defer.
func (n *hostNews) take() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	lines := n.lines
	n.lines = nil
	return lines
}
