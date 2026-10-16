package service

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// fakeClock is time that moves only when a test says so.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	clock *fakeClock
	at    time.Time
	f     func()
	done  bool
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{clock: c, at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return t
}

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	was := !t.done
	t.done = true
	return was
}

// pending counts timers not yet fired or stopped, which is how a test proves
// the supervisor keeps one timer and not one per row.
func (c *fakeClock) pending() int {
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

// Advance moves time forward, firing each due timer in order, one step at a
// time so work a timer starts can arm the next before time moves past it.
func (c *fakeClock) Advance(t *testing.T, s *Supervisor, d time.Duration) {
	t.Helper()
	end := c.Now().Add(d)
	for {
		settle(t, s)
		c.mu.Lock()
		var due *fakeTimer
		live := c.timers[:0]
		for _, timer := range c.timers {
			if timer.done {
				continue
			}
			live = append(live, timer)
			if !timer.at.After(end) && (due == nil || timer.at.Before(due.at)) {
				due = timer
			}
		}
		c.timers = live
		if due == nil {
			c.now = end
			c.mu.Unlock()
			settle(t, s)
			return
		}
		if due.at.After(c.now) {
			c.now = due.at
		}
		due.done = true
		c.mu.Unlock()
		due.f()
	}
}

// fireDue moves time forward by d and fires whatever falls due, without
// waiting for the supervisor: for work that is itself waiting on the clock.
func (c *fakeClock) fireDue(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*fakeTimer
	for _, timer := range c.timers {
		if !timer.done && !timer.at.After(c.now) {
			timer.done = true
			due = append(due, timer)
		}
	}
	c.mu.Unlock()
	for _, timer := range due {
		timer.f()
	}
}

// settle waits for the supervisor's workers to finish what they are doing
// now. The outcomes the tests assert never depend on how long that takes.
func settle(t *testing.T, s *Supervisor) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		busy := s.free != s.workers
		for _, e := range s.entries {
			if e.busy || e.probing || e.stopping() {
				busy = true
			}
		}
		s.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the supervisor never settled")
}

type fakeProc struct {
	id         processgroup.Identity
	alive      bool
	ignoreTerm bool
	committed  bool
	exited     func(processgroup.Identity, Exit)
	spec       Spec
	signals    []bool
}

// fakeRuntime is a machine with no processes on it but the ones it pretends.
type fakeRuntime struct {
	mu      sync.Mutex
	boot    string
	nextPID int
	procs   map[int]*fakeProc
	// healthy decides each check by the service's name; absent is passing.
	failing   map[string]bool
	portsUsed map[int]bool
	// launches counts gates made; starts counts commands let run.
	launches  int
	starts    int
	startErr  error
	commitErr error
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{boot: "boot-1", nextPID: 100, procs: map[int]*fakeProc{}, failing: map[string]bool{}, portsUsed: map[int]bool{}}
}

func (r *fakeRuntime) Launch(spec Spec, _ string, exited func(processgroup.Identity, Exit)) (Launch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.startErr != nil {
		return nil, r.startErr
	}
	r.launches++
	r.nextPID++
	id := processgroup.Identity{PID: r.nextPID, Start: uint64(r.nextPID * 7), Boot: r.boot}
	r.procs[id.PID] = &fakeProc{id: id, alive: true, exited: exited, spec: spec}
	return &fakeLaunch{r: r, id: id}, nil
}

// fakeLaunch is a gate: the command runs only when it is committed.
type fakeLaunch struct {
	r    *fakeRuntime
	id   processgroup.Identity
	done bool
}

func (l *fakeLaunch) Identity() processgroup.Identity { return l.id }

func (l *fakeLaunch) Commit() error {
	l.r.mu.Lock()
	defer l.r.mu.Unlock()
	p := l.r.procs[l.id.PID]
	if l.done || p == nil || !p.alive || l.r.commitErr != nil {
		l.done = true
		return errors.New("the gate is gone")
	}
	l.done = true
	p.committed = true
	l.r.starts++
	return nil
}

func (l *fakeLaunch) Abort() {
	l.r.mu.Lock()
	if l.done {
		l.r.mu.Unlock()
		return
	}
	l.done = true
	p := l.r.procs[l.id.PID]
	if p == nil || !p.alive {
		l.r.mu.Unlock()
		return
	}
	p.alive = false
	exited := p.exited
	l.r.mu.Unlock()
	go exited(l.id, Exit{Code: 125, Known: true})
}

func (r *fakeRuntime) Alive(id processgroup.Identity) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.procs[id.PID]
	return p != nil && p.alive && p.id == id && id.Boot == r.boot
}

func (r *fakeRuntime) Signal(id processgroup.Identity, kill bool) error {
	r.mu.Lock()
	p := r.procs[id.PID]
	if p == nil || p.id != id || !p.alive {
		r.mu.Unlock()
		return nil
	}
	p.signals = append(p.signals, kill)
	if !kill && p.ignoreTerm {
		r.mu.Unlock()
		return nil
	}
	p.alive = false
	exited := p.exited
	r.mu.Unlock()
	if exited != nil {
		code := 143
		if kill {
			code = 137
		}
		go exited(p.id, Exit{Code: code, Known: true})
	}
	return nil
}

func (r *fakeRuntime) Probe(_ context.Context, spec Spec) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failing[spec.Name] {
		return errors.New("port 5173 not open")
	}
	return nil
}

func (r *fakeRuntime) PortInUse(port int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.portsUsed[port]
}

func (r *fakeRuntime) Boot() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.boot
}

func (r *fakeRuntime) fail(name string, failing bool) {
	r.mu.Lock()
	r.failing[name] = failing
	r.mu.Unlock()
}

// exit ends a process on its own, as a crash does.
func (r *fakeRuntime) exit(t *testing.T, s *Supervisor, pid, code int) {
	t.Helper()
	r.mu.Lock()
	p := r.procs[pid]
	if p == nil || !p.alive {
		r.mu.Unlock()
		t.Fatalf("process %d is not running", pid)
	}
	p.alive = false
	exited := p.exited
	r.mu.Unlock()
	if exited != nil {
		exited(p.id, Exit{Code: code, Known: true})
	}
	settle(t, s)
}

func (r *fakeRuntime) proc(pid int) *fakeProc {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.procs[pid]
}

func (r *fakeRuntime) running() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	var pids []int
	for pid, p := range r.procs {
		if p.alive {
			pids = append(pids, pid)
		}
	}
	sort.Ints(pids)
	return pids
}

type harness struct {
	t     *testing.T
	dir   string
	clock *fakeClock
	rt    *fakeRuntime
	s     *Supervisor
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, dir: t.TempDir(), clock: newFakeClock(), rt: newFakeRuntime()}
	h.open()
	return h
}

func (h *harness) open() {
	h.t.Helper()
	s, err := Open(Options{Dir: h.dir, Runtime: h.rt, Clock: h.clock})
	if err != nil {
		h.t.Fatal(err)
	}
	h.s = s
	h.t.Cleanup(s.Close)
	settle(h.t, s)
}

func (h *harness) reopen() {
	h.t.Helper()
	h.s.Close()
	h.open()
}

func (h *harness) advance(d time.Duration) { h.clock.Advance(h.t, h.s, d) }

func (h *harness) keep(name string, policy Policy) Row {
	h.t.Helper()
	row, err := h.s.Keep("agent-a", webSpec(name, h.dir), policy, Provenance{Label: "Start the " + name})
	if err != nil {
		h.t.Fatal(err)
	}
	return row
}

func (h *harness) get(id string) Row {
	h.t.Helper()
	row, err := h.s.Get("agent-a", id)
	if err != nil {
		h.t.Fatal(err)
	}
	return row
}

func webSpec(name, dir string) Spec {
	return Spec{Name: name, Command: "npm run dev -- --name " + name, Dir: dir, Health: Health{Port: 5173}}
}

// setWrite swaps the store's writer the way the supervisor reads it, under
// the save lock, so a save already running finishes with the writer it had.
func (h *harness) setWrite(write func(dir string, raw []byte) error) {
	h.s.saveMu.Lock()
	h.s.write = write
	h.s.saveMu.Unlock()
}
