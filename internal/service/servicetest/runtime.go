// Package servicetest is a machine with no processes on it, for tests of
// what sits above the supervisor: a conversation's keep-running card, the
// app's rows. Nothing it does forks, sleeps or listens.
package servicetest

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
	"github.com/Agent-Field/codeaf/internal/service"
)

// Runtime pretends every start succeeds and every check passes, and says what
// it was asked to do.
type Runtime struct {
	mu      sync.Mutex
	nextPID int
	procs   map[int]*proc
	// Started is every spec whose command was let run, in order.
	Started []service.Spec
	// PortsUsed are ports another program answers on.
	PortsUsed map[int]bool
	// failing makes every check fail; refusing makes every signal fail and
	// leaves the process up.
	failing  bool
	refusing bool
}

// Failing makes every check fail, or pass again.
func (r *Runtime) Failing(on bool) {
	r.mu.Lock()
	r.failing = on
	r.mu.Unlock()
}

// Refusing makes every signal fail and leaves its process up, or not.
func (r *Runtime) Refusing(on bool) {
	r.mu.Lock()
	r.refusing = on
	r.mu.Unlock()
}

// Exit ends one running process by itself, as though it exited with code.
func (r *Runtime) Exit(pid, code int) bool {
	r.mu.Lock()
	p := r.procs[pid]
	if p == nil || !p.alive {
		r.mu.Unlock()
		return false
	}
	p.alive = false
	r.mu.Unlock()
	p.exited(p.id, service.Exit{Code: code, Known: true})
	return true
}

type proc struct {
	id     processgroup.Identity
	alive  bool
	exited func(processgroup.Identity, service.Exit)
}

// New is an empty machine.
func New() *Runtime {
	return &Runtime{nextPID: 1000, procs: map[int]*proc{}, PortsUsed: map[int]bool{}}
}

const boot = "servicetest-boot"

func (r *Runtime) Launch(spec service.Spec, _ string, exited func(processgroup.Identity, service.Exit)) (service.Launch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextPID++
	id := processgroup.Identity{PID: r.nextPID, Start: uint64(r.nextPID), Boot: boot}
	r.procs[id.PID] = &proc{id: id, alive: true, exited: exited}
	return &launch{r: r, id: id, spec: spec}, nil
}

type launch struct {
	r    *Runtime
	id   processgroup.Identity
	spec service.Spec
	done bool
}

func (l *launch) Identity() processgroup.Identity { return l.id }

func (l *launch) Commit() error {
	l.r.mu.Lock()
	defer l.r.mu.Unlock()
	l.done = true
	l.r.Started = append(l.r.Started, l.spec)
	return nil
}

func (l *launch) Abort() {
	l.r.mu.Lock()
	p := l.r.procs[l.id.PID]
	if l.done || p == nil || !p.alive {
		l.r.mu.Unlock()
		return
	}
	l.done = true
	p.alive = false
	l.r.mu.Unlock()
	go p.exited(l.id, service.Exit{Code: 125, Known: true})
}

func (r *Runtime) Alive(id processgroup.Identity) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.procs[id.PID]
	return p != nil && p.alive && p.id == id
}

func (r *Runtime) Signal(id processgroup.Identity, _ bool) error {
	r.mu.Lock()
	p := r.procs[id.PID]
	if p == nil || p.id != id || !p.alive {
		r.mu.Unlock()
		return nil
	}
	if r.refusing {
		r.mu.Unlock()
		return errors.New("operation not permitted")
	}
	p.alive = false
	r.mu.Unlock()
	go p.exited(p.id, service.Exit{Code: 143, Known: true})
	return nil
}

func (r *Runtime) Probe(context.Context, service.Spec) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failing {
		return errors.New("not answering")
	}
	return nil
}

func (r *Runtime) PortInUse(port int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.PortsUsed[port]
}

func (r *Runtime) Boot() string { return boot }

// Running counts the processes still up.
func (r *Runtime) Running() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, p := range r.procs {
		if p.alive {
			n++
		}
	}
	return n
}

// Starts counts the specs started.
func (r *Runtime) Starts() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.Started)
}

// Clock is a clock that moves only when it is told to, for a supervisor whose
// checks, backoffs and graces a test steps through.
type Clock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*timer
}

type timer struct {
	at   time.Time
	f    func()
	done bool
	c    *Clock
}

// NewClock is a clock standing at now.
func NewClock(now time.Time) *Clock { return &Clock{now: now} }

func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *Clock) AfterFunc(d time.Duration, f func()) service.Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &timer{at: c.now.Add(d), f: f, c: c}
	c.timers = append(c.timers, t)
	return t
}

func (t *timer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	was := !t.done
	t.done = true
	return was
}

// Advance moves the clock on by d and runs every timer then due, in order.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*timer
	kept := c.timers[:0]
	for _, t := range c.timers {
		switch {
		case t.done:
		case !t.at.After(c.now):
			t.done = true
			due = append(due, t)
		default:
			kept = append(kept, t)
		}
	}
	c.timers = kept
	c.mu.Unlock()
	for _, t := range due {
		t.f()
	}
}

// Pending counts the timers not yet run or stopped.
func (c *Clock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range c.timers {
		if !t.done {
			n++
		}
	}
	return n
}
