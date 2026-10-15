package appwire

import (
	"sync"
	"time"
)

// The two numbers delta coalescing is measured in. A streamed reply arrives as
// many small chunks, often a word each, and a line per chunk is a line the app
// has to parse and a render it has to schedule for every word. So chunks of one
// kind are folded into one turn.delta and sent when the first of them is
// DeltaWindow old or the folded text reaches DeltaBytes, whichever comes first.
// Sixteen milliseconds is one frame at sixty hertz: the app could not have
// drawn the chunks separately anyway.
const (
	DeltaWindow = 16 * time.Millisecond
	DeltaBytes  = 4 << 10
)

// Outbox is one agent's ordered queue of events on the way to the writer.
//
// IT IS WHAT LETS AN ENGINE'S READ LOOP NEVER WAIT ON STDOUT. Adding to it
// takes a lock and appends; the waiting on the writer happens on the outbox's
// own goroutine. And because every event of one agent goes through the same
// queue, a delta folded here can never be overtaken by the tool event or the
// turn end that followed it: anything queued behind a delta sends the delta
// first, at once, without waiting out the window.
type Outbox struct {
	emit Emitter

	mu     sync.Mutex
	queue  []outboxItem
	closed bool
	wake   chan struct{}
	done   chan struct{}
	now    func() time.Time
}

type outboxItem struct {
	event string
	data  any
	delta *TurnDelta
	born  time.Time
}

// NewOutbox starts an outbox writing to emit.
func NewOutbox(emit Emitter) *Outbox {
	o := &Outbox{emit: emit, wake: make(chan struct{}, 1), done: make(chan struct{}), now: time.Now}
	go o.run()
	return o
}

// Delta queues one chunk of streamed text, folding it into the chunk before it
// when that one is the same turn, message and kind and still waiting.
func (o *Outbox) Delta(agent string, turn uint64, msg int, kind, text string) {
	if text == "" {
		return
	}
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return
	}
	if n := len(o.queue); n > 0 {
		last := o.queue[n-1].delta
		if last != nil && last.Agent == agent && last.Turn == turn && last.Msg == msg && last.Kind == kind && len(last.Text) < DeltaBytes {
			last.Text += text
			o.mu.Unlock()
			o.poke()
			return
		}
	}
	o.queue = append(o.queue, outboxItem{delta: &TurnDelta{Agent: agent, Turn: turn, Msg: msg, Kind: kind, Text: text}, born: o.now()})
	o.mu.Unlock()
	o.poke()
}

// Event queues one event behind everything already queued.
func (o *Outbox) Event(event string, data any) {
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return
	}
	o.queue = append(o.queue, outboxItem{event: event, data: data})
	o.mu.Unlock()
	o.poke()
}

// Close sends everything still queued, without waiting out any window, and
// returns once it has been handed to the writer.
func (o *Outbox) Close() {
	o.mu.Lock()
	o.closed = true
	o.mu.Unlock()
	o.poke()
	<-o.done
}

func (o *Outbox) poke() {
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

func (o *Outbox) run() {
	defer close(o.done)
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	for {
		o.mu.Lock()
		if len(o.queue) == 0 {
			closed := o.closed
			o.mu.Unlock()
			if closed {
				return
			}
			<-o.wake
			continue
		}
		head := o.queue[0]
		// A lone delta that is young and small waits for more of itself.
		// Anything behind it, a full one, or a closing outbox sends it now.
		if head.delta != nil && len(o.queue) == 1 && !o.closed && len(head.delta.Text) < DeltaBytes {
			if wait := head.born.Add(DeltaWindow).Sub(o.now()); wait > 0 {
				o.mu.Unlock()
				timer.Reset(wait)
				select {
				case <-timer.C:
				case <-o.wake:
					if !timer.Stop() {
						<-timer.C
					}
				}
				continue
			}
		}
		o.queue = o.queue[1:]
		if len(o.queue) == 0 {
			o.queue = nil
		}
		o.mu.Unlock()
		if head.delta != nil {
			o.emit.Emit(EventTurnDelta, *head.delta)
		} else {
			o.emit.Emit(head.event, head.data)
		}
	}
}
