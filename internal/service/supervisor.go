package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// The supervisor's refusals.
var (
	ErrNotFound = errors.New("no such kept-running process")
	ErrClosed   = errors.New("codeaf is closing; nothing new is kept running")
	ErrRunning  = errors.New("it is still running; stop it first")
	ErrStopping = errors.New("it is still stopping")
	ErrFull     = fmt.Errorf("%d processes are already kept running for this agent; remove one first", maxPerOwner)
	// ErrNotSaved wraps a write of the services file that failed. A change it
	// is returned for was either rolled back or, for a stop, carried out with
	// the file still saying what it said before.
	ErrNotSaved = errors.New("could not write the services file")
	// ErrNotStopped is a stop whose process could not be proved gone: it is
	// still running, and still this codeaf's to stop.
	ErrNotStopped = errors.New("did not stop")
	// ErrClosing is a start asked for an agent that is being closed.
	ErrClosing = errors.New("its agent is closing; nothing new is started for it")
)

// defaultWorkers bounds how many checks and starts run at once, whatever the
// number of services: a slow check holds one slot, never the timer, the lock
// or anybody's request.
const defaultWorkers = 4

// stopPoll is how often a stop looks again at a process it did not start,
// whose exit nothing reports. It is bounded by StopGrace, so one stop polls at
// most fifty times and then sends SIGKILL.
const stopPoll = 100 * time.Millisecond

// Options open a [Supervisor].
type Options struct {
	// Dir is the store's folder, private to the person.
	Dir     string
	Runtime Runtime
	// Clock is the wall clock when nil.
	Clock Clock
	// Workers bounds concurrent checks and automatic starts; 4 when zero.
	Workers int
}

// Supervisor is the one owner of every kept-running service in this codeaf:
// one timer, a bounded set of workers, one file. It holds no goroutine per
// service beyond the wait on a process it started, which is the operating
// system's exit report and not a poll.
//
// ITS LOCK IS NEVER HELD ACROSS ANYTHING SLOW. A check, a start and a stop's
// grace all run in a worker with the lock released, and come back under it with
// the generation they started on; a result whose generation has moved — the
// person stopped it, restarted it, removed it — is dropped. That is what lets a
// stop take effect the instant it is asked for, while a five-second check of
// the same service is still running.
type Supervisor struct {
	dir   string
	rt    Runtime
	clock Clock
	lock  *os.File
	base  context.Context
	stop  context.CancelFunc
	// write is the file's one writer, a seam a test replaces to fail it.
	write func(dir string, raw []byte) error

	mu      sync.Mutex
	closed  bool
	seq     int
	entries map[string]*entry
	consent map[string]consent
	timer   Timer
	timerAt time.Time
	free    int
	workers int
	wg      sync.WaitGroup
	dirty   uint64

	listeners map[int]func(Event)
	// fences are the owners being closed: nothing new starts for one while
	// its close waits ([Supervisor.Fence]).
	fences       map[string]int
	nextListener int
	pending      map[string]Event
	pendingOrder []string
	kick         chan struct{}
	quit         chan struct{}

	saveMu sync.Mutex
	saved  uint64
	wrote  bool
	// released is set once Close let go of the store; nothing writes after.
	released bool
}

type entry struct {
	rec record
	// gen moves on every start, stop and restart. Work carries the gen it
	// began on and is dropped when it no longer matches.
	gen uint64
	// busy is a start in flight; probing is a check in flight.
	busy    bool
	probing bool
	// stopDone is a stop's grace in flight, closed when it is over. A second
	// stop during the grace is answered by the same one.
	stopDone chan struct{}
	// before is the state a person's restart found, put back if the process
	// it had to end would not stop.
	before State
	// abandoned is a gate let go after its identity could not be written:
	// still ours until its exit is seen, and not a failure of the service.
	abandoned bool
	// gone is closed when a process this codeaf started exits. It is nil for a
	// re-adopted process, whose exit nobody reports.
	gone      chan struct{}
	nextCheck time.Time
}

func (e *entry) stopping() bool { return e.stopDone != nil }

// exitedOwn says a process this codeaf started has already exited, its report
// on the way.
func (e *entry) exitedOwn() bool {
	if e.gone == nil {
		return false
	}
	select {
	case <-e.gone:
		return true
	default:
		return false
	}
}

// Open takes the store at opts.Dir, takes back what it can prove still runs,
// and starts the one timer. It answers [ErrBusy] when another codeaf holds the
// store, and a plain error when the file cannot be read — never an empty store
// in its place.
func Open(opts Options) (*Supervisor, error) {
	if opts.Runtime == nil {
		return nil, errors.New("kept-running processes are not supported on this platform")
	}
	if strings.TrimSpace(opts.Dir) == "" {
		return nil, errors.New("no folder for kept-running processes")
	}
	clock := opts.Clock
	if clock == nil {
		clock = RealClock()
	}
	workers := opts.Workers
	if workers <= 0 {
		workers = defaultWorkers
	}
	lock, err := lockStore(opts.Dir)
	if err != nil {
		return nil, err
	}
	image, err := readStore(opts.Dir)
	if err != nil {
		_ = filelock.Unlock(lock)
		_ = lock.Close()
		return nil, err
	}
	base, cancel := context.WithCancel(context.Background())
	s := &Supervisor{
		dir: opts.Dir, rt: opts.Runtime, clock: clock, lock: lock, base: base, stop: cancel, write: writeStore,
		seq: image.Seq, entries: map[string]*entry{}, consent: image.Consent, free: workers, workers: workers,
		listeners: map[int]func(Event){}, pending: map[string]Event{}, fences: map[string]int{},
		kick: make(chan struct{}, 1), quit: make(chan struct{}),
	}
	if s.consent == nil {
		s.consent = map[string]consent{}
	}
	go s.notifyLoop()
	// A log that grew while no codeaf was open to cut it is cut back first.
	for i := range image.Services {
		if truncated, _ := rotateLog(s.logPath(image.Services[i].ID)); truncated {
			image.Services[i].Truncated = true
		}
	}
	s.mu.Lock()
	// A REBOOT IS PROVEN, NEVER INFERRED. Both boots have to be known and
	// differ; a boot that cannot be read proves neither that the machine
	// restarted nor that it did not, and starting a saved service on that
	// guess could run a second copy beside the first.
	current := s.rt.Boot()
	rebooted := image.Boot != "" && current != "" && image.Boot != current
	for _, rec := range image.Services {
		e := &entry{rec: rec}
		s.entries[rec.ID] = e
		s.adoptLocked(e, rebooted)
	}
	s.pumpLocked()
	s.mu.Unlock()
	if err := s.save(); err != nil {
		// Nothing was started that is not on disk: an adoption changes only
		// words, and a reboot's starts are written down after they launch, by
		// the start itself, which rolls back what it cannot record.
		s.Close()
		return nil, err
	}
	return s, nil
}

// adoptLocked decides, once, what a record read from the file is now.
//
// ONLY A PROVEN PROCESS IS TAKEN BACK. The pid, its start identity and its
// boot must all match a live group (internal/processgroup); anything less is a
// process this codeaf cannot tell apart from a stranger that was handed the
// same number, and it is left alone and said to be not running. After a proven
// reboot nothing can still be running, and a service whose policy restarts it
// is started again in its folder; one whose policy is never is not.
func (s *Supervisor) adoptLocked(e *entry, rebooted bool) {
	now := s.clock.Now()
	identity := e.rec.Identity
	ended := e.rec.State == StateStopped || e.rec.State == StateFailed || e.rec.State == StateNotRunning ||
		(e.rec.State == StateGaveUp && identity == nil)
	if ended {
		if identity != nil && s.rt.Alive(*identity) {
			if e.rec.State == StateStopped {
				// A stop that was in its grace when codeaf went, or one that
				// could not end it: finish it.
				s.stopLocked(e, e.rec.StoppedBy)
				return
			}
			// A PROCESS THAT IS STILL OURS STAYS OURS, whatever state the file
			// left it in — a launch whose write failed, a restart that could
			// not stop it. It is taken back and shown running; nothing is
			// restarted on its account.
			e.rec.StartedHow, e.rec.ReAdoptedAt = ReAdopted, now
			e.nextCheck = now.Add(CheckEvery)
			s.changedLocked(e, "re_adopted")
			return
		}
		e.rec.Identity = nil
		return
	}
	if identity != nil && s.rt.Alive(*identity) {
		e.rec.StartedHow, e.rec.ReAdoptedAt = ReAdopted, now
		e.nextCheck = now.Add(CheckEvery)
		if e.rec.State == StateRestarting && e.rec.BackoffUntil.IsZero() {
			e.rec.BackoffUntil = now
		}
		s.changedLocked(e, "re_adopted")
		return
	}
	e.rec.Identity = nil
	e.rec.BackoffUntil = time.Time{}
	if rebooted && e.rec.Policy != PolicyNever {
		e.gen++
		e.busy = true
		e.rec.State, e.rec.Reason = StateStarting, ""
		s.changedLocked(e, "state")
		gen := e.gen
		s.goWork(func() { _ = s.startWork(e.rec.ID, gen, StartedAfterRestart, false) })
		return
	}
	s.openEpisodeLocked(e, "not running after restart")
	e.rec.State, e.rec.Reason = StateNotRunning, "not running after restart"
	e.rec.EndedAt = now
	s.changedLocked(e, "state")
}

// Close lets the supervisor go and leaves every service running: a quit is
// not a stop. It waits for work already under way — at most one check's
// timeout or one stop's grace — so a stop a person asked for still ends with
// SIGKILL if it needs one.
//
// IT NEVER WAITS FOR A LISTENER. Listeners are called on the supervisor's own
// goroutine, and one that blocks, or that calls Close from inside its own
// callback, must not hold the store's lock or a quit hostage: the goroutine is
// told to go and finishes on its own after the listener returns.
func (s *Supervisor) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.stop()
	s.mu.Unlock()
	s.wg.Wait()
	_ = s.save()
	s.saveMu.Lock()
	s.released = true
	s.saveMu.Unlock()
	close(s.quit)
	_ = filelock.Unlock(s.lock)
	_ = s.lock.Close()
}

// Subscribe hears every change, on one goroutine of the supervisor's own, in
// order per service. Changes to one service that arrive faster than a listener
// takes them are folded into the newest row, which still carries the episode
// they belonged to ([Row.Episode]). A listener must return promptly: it is not
// joined by Close, and while it blocks every other listener waits. The returned
// func stops it.
func (s *Supervisor) Subscribe(listen func(Event)) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextListener++
	id := s.nextListener
	s.listeners[id] = listen
	return func() {
		s.mu.Lock()
		delete(s.listeners, id)
		s.mu.Unlock()
	}
}

// Approved reports the policy a person already chose for this exact command
// in this exact folder with this exact check. A proposal that matches is not
// asked about again and keeps the person's policy, whatever it proposed; one
// that changes the check is asked about again.
func (s *Supervisor) Approved(spec Spec) (Policy, bool) {
	spec, err := spec.Check()
	if err != nil {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.consent[spec.ConsentKey()]
	if !ok || c.Health != spec.Health {
		return "", false
	}
	return c.Policy, true
}

// Keep is a person's yes, carried out: it banks the yes for this exact spec,
// starts the spec as a kept-running service for owner, and answers its row
// once the process is launched — or once it is proven it cannot be. When the
// same spec was already approved, the person's policy stands whatever policy
// says.
//
// NOTHING RUNS THAT THE FILE DOES NOT KNOW ABOUT. The yes and the record are
// written before the process starts, and the process's identity right after;
// a write that fails before the start undoes the yes and starts nothing, and
// one that fails after it ends the process it could not record. A kept process
// no later codeaf could find is exactly the orphan this package exists to
// prevent.
//
// The same spec already running for the same owner is answered as it is, so
// asking twice never starts two.
func (s *Supervisor) Keep(owner string, spec Spec, policy Policy, from Provenance) (Row, error) {
	spec, err := spec.Check()
	if err != nil {
		return Row{}, err
	}
	if owner == "" || len(owner) > maxOwner || strings.TrimSpace(owner) != owner {
		return Row{}, errors.New("a kept-running process needs an owner")
	}
	if !policy.Valid() {
		return Row{}, fmt.Errorf("restart policy is never, on_failure or always, not %q", policy)
	}
	from.Label = oneLine(from.Label, maxProvenance)
	from.CallID = oneLine(from.CallID, maxProvenance)
	now := s.clock.Now()
	from.At = now

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Row{}, ErrClosed
	}
	if s.fences[owner] > 0 {
		s.mu.Unlock()
		return Row{}, ErrClosing
	}
	key := spec.ConsentKey()
	prior, hadPrior := s.consent[key]
	if hadPrior && prior.Health == spec.Health {
		policy = prior.Policy
	}
	var e *entry
	for _, candidate := range s.entries {
		if candidate.rec.Owner == owner && candidate.rec.Spec.ConsentKey() == key {
			e = candidate
			break
		}
	}
	if e != nil && (e.rec.Identity != nil || e.busy || e.stopping()) && e.rec.Spec.Health == spec.Health {
		row := s.rowLocked(e)
		s.mu.Unlock()
		return row, nil
	}
	if e != nil && (e.rec.Identity != nil || e.busy || e.stopping()) {
		s.mu.Unlock()
		return Row{}, ErrRunning
	}
	created := e == nil
	var before record
	if created {
		owned := 0
		for _, other := range s.entries {
			if other.rec.Owner == owner {
				owned++
			}
		}
		if owned >= maxPerOwner || len(s.entries) >= maxServices {
			s.mu.Unlock()
			return Row{}, ErrFull
		}
		s.seq++
		id := "s" + strconv.Itoa(s.seq)
		e = &entry{rec: record{ID: id, Owner: owner, Created: now}}
		s.entries[id] = e
	} else {
		before = e.rec
	}
	s.consent[key] = consent{Policy: policy, Health: spec.Health, At: now}
	s.trimConsentLocked()
	e.rec.Spec, e.rec.Policy, e.rec.From = spec, policy, from
	e.rec.StoppedBy = ""
	e.gen++
	gen := e.gen
	e.busy = true
	e.before = ""
	e.rec.State, e.rec.Reason = StateStarting, ""
	s.changedLocked(e, "state")
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()

	if err := s.save(); err != nil {
		s.mu.Lock()
		if hadPrior {
			s.consent[key] = prior
		} else {
			delete(s.consent, key)
		}
		e.busy = false
		e.gen++
		if created {
			delete(s.entries, e.rec.ID)
			s.dirty++
			s.enqueueLocked(Event{Row: s.rowLocked(e), Change: "removed", Removed: true})
		} else {
			e.rec = before
			s.changedLocked(e, "state")
		}
		s.mu.Unlock()
		return Row{}, err
	}
	if err := s.startWork(e.rec.ID, gen, StartedByAgent, false); err != nil {
		row, _ := s.Get(owner, e.rec.ID)
		return row, err
	}
	return s.Get(owner, e.rec.ID)
}

// List is owner's services, oldest first. One agent never sees another's.
func (s *Supervisor) List(owner string) []Row {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := make([]Row, 0, len(s.entries))
	for _, e := range s.entries {
		if e.rec.Owner == owner {
			rows = append(rows, s.rowLocked(e))
		}
	}
	sort.Slice(rows, func(i, j int) bool { return idLess(rows[i].ID, rows[j].ID) })
	return rows
}

// All is every owner's services, oldest first: the host's view, for a quit
// that counts what stays up. A conversation never reads it ([List] is its).
func (s *Supervisor) All() []Row {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := make([]Row, 0, len(s.entries))
	for _, e := range s.entries {
		rows = append(rows, s.rowLocked(e))
	}
	sort.Slice(rows, func(i, j int) bool { return idLess(rows[i].ID, rows[j].ID) })
	return rows
}

// Running counts the services every owner has with a process up or starting,
// which is what a quit says will stay up.
func (s *Supervisor) Running() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.entries {
		if e.rec.Identity != nil || e.busy {
			n++
		}
	}
	return n
}

// Get is one of owner's services.
func (s *Supervisor) Get(owner, id string) (Row, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.findLocked(owner, id)
	if err != nil {
		return Row{}, err
	}
	return s.rowLocked(e), nil
}

// LogPath is where one of owner's services writes.
func (s *Supervisor) LogPath(owner, id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.findLocked(owner, id); err != nil {
		return "", err
	}
	return s.logPath(id), nil
}

// SetPolicy is the person changing what happens at the next failure. It
// changes nothing about the process now, and the banked yes follows it, so the
// agent asking again later gets the person's newest word. A change the file
// could not keep is undone and answered with [ErrNotSaved].
func (s *Supervisor) SetPolicy(owner, id string, policy Policy) (Row, error) {
	if !policy.Valid() {
		return Row{}, fmt.Errorf("restart policy is never, on_failure or always, not %q", policy)
	}
	s.mu.Lock()
	e, err := s.findLocked(owner, id)
	if err == nil && s.closed {
		err = ErrClosed
	}
	if err != nil {
		s.mu.Unlock()
		return Row{}, err
	}
	if e.rec.Policy == policy {
		row := s.rowLocked(e)
		s.mu.Unlock()
		return row, nil
	}
	was := e.rec.Policy
	key := e.rec.Spec.ConsentKey()
	prior, hadPrior := s.consent[key]
	e.rec.Policy = policy
	if hadPrior && prior.Health == e.rec.Spec.Health {
		s.consent[key] = consent{Policy: policy, Health: prior.Health, At: s.clock.Now()}
	}
	s.changedLocked(e, "policy")
	s.mu.Unlock()
	if err := s.save(); err != nil {
		s.mu.Lock()
		if s.entries[id] == e && e.rec.Policy == policy {
			e.rec.Policy = was
			if hadPrior {
				s.consent[key] = prior
			}
			s.changedLocked(e, "policy")
		}
		row := s.rowLocked(e)
		s.mu.Unlock()
		return row, err
	}
	return s.Get(owner, id)
}

// Restart is the person's restart: the process ends and starts again now, with
// no backoff. It is theirs, so it is outside the automatic budget — it neither
// spends a restart nor refills one, and the automatic history it found is
// kept as it was.
func (s *Supervisor) Restart(owner, id string) (Row, error) {
	return s.personStart(owner, id, true)
}

// Start is "start again" for a service with no process: its saved command, in
// its saved folder, with its saved check and policy, asking nothing. A service
// already running or already starting is answered as it is.
func (s *Supervisor) Start(owner, id string) (Row, error) {
	return s.personStart(owner, id, false)
}

func (s *Supervisor) personStart(owner, id string, restart bool) (Row, error) {
	s.mu.Lock()
	e, err := s.findLocked(owner, id)
	if err == nil && s.closed {
		err = ErrClosed
	}
	if err == nil && s.fences[owner] > 0 {
		err = ErrClosing
	}
	if err != nil {
		s.mu.Unlock()
		return Row{}, err
	}
	if e.stopping() {
		s.mu.Unlock()
		return Row{}, ErrStopping
	}
	if e.busy || (!restart && e.rec.Identity != nil) {
		row := s.rowLocked(e)
		s.mu.Unlock()
		return row, nil
	}
	e.gen++
	gen := e.gen
	e.busy = true
	e.before = e.rec.State
	e.rec.StoppedBy, e.rec.BackoffUntil, e.rec.Reason = "", time.Time{}, ""
	if restart && e.rec.Identity != nil {
		e.rec.State = StateRestarting
	} else {
		e.rec.State = StateStarting
	}
	s.changedLocked(e, "state")
	row := s.rowLocked(e)
	s.goWork(func() { _ = s.startWork(id, gen, StartedByPerson, false) })
	s.mu.Unlock()
	return row, nil
}

// Stop ends a service and keeps its output: SIGTERM to its group, SIGKILL
// after StopGrace. Nothing restarts it — the state is stopped the instant this
// returns, and the grace runs in the background. Stopping a stopped service,
// or one already in its grace, is answered as it is.
//
// A stop is always carried out. When the file cannot say so, the error says
// that instead, and a later codeaf reading the older file finds the process
// gone and says it is not running.
func (s *Supervisor) Stop(owner, id string) (Row, error) {
	s.mu.Lock()
	e, err := s.findLocked(owner, id)
	if err == nil && s.closed {
		err = ErrClosed
	}
	if err != nil {
		s.mu.Unlock()
		return Row{}, err
	}
	s.stopLocked(e, StartedByPerson)
	row := s.rowLocked(e)
	s.mu.Unlock()
	return row, s.save()
}

// StopOwner stops every service owner has, the way closing an agent does. The
// graces run in the background; it reports how many it stopped.
func (s *Supervisor) StopOwner(owner, by string) (int, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, ErrClosed
	}
	n := 0
	for _, e := range s.entries {
		if e.rec.Owner != owner {
			continue
		}
		if e.rec.Identity != nil || e.busy {
			n++
		}
		s.stopLocked(e, by)
	}
	s.mu.Unlock()
	return n, s.save()
}

// Fence keeps anything new from starting for owner — no Keep, no Start, no
// Restart — until release is called, so a close that waits for owner's
// processes to go cannot be overtaken by one started behind it. Fences nest;
// release is safe to call more than once.
func (s *Supervisor) Fence(owner string) (release func()) {
	s.mu.Lock()
	s.fences[owner]++
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			if s.fences[owner]--; s.fences[owner] <= 0 {
				delete(s.fences, owner)
			}
			s.mu.Unlock()
		})
	}
}

// StopOwnerWait stops every service owner has and waits, until ctx ends, for
// each group to be PROVEN gone — the way an explicit close of the agent that
// owns them does, so that close never hides an agent whose process is still
// up. It touches no other owner's services. It reports how many it stopped,
// and [ErrNotStopped] naming any it could not prove gone; those stay listed as
// running, still this codeaf's to stop again.
func (s *Supervisor) StopOwnerWait(ctx context.Context, owner, by string) (int, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, ErrClosed
	}
	n := 0
	var waits []chan struct{}
	for _, e := range s.entries {
		if e.rec.Owner != owner {
			continue
		}
		if e.rec.Identity != nil || e.busy {
			n++
		}
		if done := s.stopLocked(e, by); done != nil {
			waits = append(waits, done)
		}
	}
	s.mu.Unlock()
	saveErr := s.save()
	if err := waitAll(ctx, waits); err != nil {
		return n, err
	}
	return n, errors.Join(saveErr, s.stillRunning(func(e *entry) bool { return e.rec.Owner == owner }))
}

// waitAll waits for every stop's grace to be over, or for ctx.
func waitAll(ctx context.Context, waits []chan struct{}) error {
	for _, done := range waits {
		select {
		case <-done:
		case <-ctx.Done():
			return fmt.Errorf("%w: still stopping when the wait ran out", ErrNotStopped)
		}
	}
	return nil
}

// stillRunning is [ErrNotStopped] naming every service matched that still owns
// a process, or nil.
func (s *Supervisor) stillRunning(match func(*entry) bool) error {
	s.mu.Lock()
	var left []string
	for id, e := range s.entries {
		if match(e) && e.rec.Identity != nil {
			left = append(left, id)
		}
	}
	s.mu.Unlock()
	if len(left) == 0 {
		return nil
	}
	sort.Slice(left, func(i, j int) bool { return idLess(left[i], left[j]) })
	return fmt.Errorf("%w: %s", ErrNotStopped, strings.Join(left, ", "))
}

// StopAll stops every service of every owner — a quit that was asked to stop
// them — and waits until each group is gone or ctx ends.
func (s *Supervisor) StopAll(ctx context.Context, by string) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	var waits []chan struct{}
	for _, e := range s.entries {
		if done := s.stopLocked(e, by); done != nil {
			waits = append(waits, done)
		}
	}
	s.mu.Unlock()
	saveErr := s.save()
	if err := waitAll(ctx, waits); err != nil {
		return err
	}
	// A STOP THAT COULD NOT PROVE ITS PROCESS GONE IS AN ERROR HERE, because
	// the caller is a quit deciding whether it may say everything stopped.
	return errors.Join(saveErr, s.stillRunning(func(*entry) bool { return true }))
}

// Remove drops a service with no process from the list, and keeps its output:
// the log stays where it is and an archive descriptor names whose it is, so a
// notice that links to the output still opens it, after a restart of codeaf
// too ([Supervisor.Output]). It signals nothing, ever — a service still running
// is refused — and removing one that is already gone is not an error. If
// either write fails, nothing is removed.
func (s *Supervisor) Remove(owner, id string) error {
	s.mu.Lock()
	e, err := s.findLocked(owner, id)
	if errors.Is(err, ErrNotFound) {
		s.mu.Unlock()
		return nil
	}
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	if e.rec.Identity != nil || e.busy {
		s.mu.Unlock()
		return ErrRunning
	}
	if e.stopping() {
		s.mu.Unlock()
		return ErrStopping
	}
	archived := archivedOf(e.rec, s.clock.Now())
	s.mu.Unlock()

	if err := s.writeArchive(archived); err != nil {
		return fmt.Errorf("%w: %v", ErrNotSaved, err)
	}
	s.mu.Lock()
	if s.entries[id] != e || e.rec.Identity != nil || e.busy || e.stopping() || s.closed {
		// It started again while the descriptor was written; the record
		// stays, and so does the harmless descriptor its live entry outranks.
		s.mu.Unlock()
		return ErrRunning
	}
	e.gen++
	row := s.rowLocked(e)
	delete(s.entries, id)
	s.dirty++
	s.mu.Unlock()
	if err := s.save(); err != nil {
		s.mu.Lock()
		if _, taken := s.entries[id]; !taken {
			s.entries[id] = e
			s.dirty++
		}
		s.mu.Unlock()
		return err
	}
	s.mu.Lock()
	s.enqueueLocked(Event{Row: row, Change: "removed", Removed: true})
	s.mu.Unlock()
	return nil
}

// ── the lifecycle ───────────────────────────────────────────────────────────

// stopLocked marks e stopped and, when it has a process, starts the grace. It
// returns the channel the grace closes, or nil when there was nothing to end.
//
// A STOP DURING A STOP IS THE SAME STOP. The second is answered with the
// first's grace, and the grace writes down the end of the very process it
// signalled, matched by identity — so nothing a second stop does can leave the
// row claiming a dead process, or forget a live one.
func (s *Supervisor) stopLocked(e *entry, by string) chan struct{} {
	if e.stopping() {
		return e.stopDone
	}
	hadProcess := e.rec.Identity != nil
	if e.rec.State == StateStopped && !hadProcess && !e.busy {
		return nil
	}
	e.gen++
	e.busy = false
	e.rec.State, e.rec.StoppedBy, e.rec.Reason = StateStopped, oneLine(by, maxName), ""
	e.rec.BackoffUntil = time.Time{}
	s.closeEpisodeLocked(e)
	if !hadProcess {
		// A start in flight finds its generation moved and lets its gate go
		// without committing ([Supervisor.startWork]).
		e.rec.EndedAt = s.clock.Now()
		s.changedLocked(e, "stopped")
		return nil
	}
	identity, gone := *e.rec.Identity, e.gone
	done := make(chan struct{})
	e.stopDone = done
	s.changedLocked(e, "stopped")
	s.goWork(func() {
		err := s.terminate(identity, gone)
		s.mu.Lock()
		e.stopDone = nil
		if e.rec.Identity != nil && *e.rec.Identity == identity {
			if err != nil {
				// STILL OURS, AND SAID SO. The row keeps the process it could
				// not end, so the person sees it running and can stop it again.
				e.rec.Reason = oneLine("did not stop: "+err.Error(), maxReason)
			} else {
				e.rec.Identity, e.gone = nil, nil
				e.rec.EndedAt = s.clock.Now()
			}
		}
		// THE GRACE'S END IS ALWAYS SAID, even when the process's own exit
		// already cleared it: a listener's last row must not say stopping.
		if s.entries[e.rec.ID] == e {
			s.changedLocked(e, "stopped")
		}
		s.pumpLocked()
		s.mu.Unlock()
		_ = s.save()
		close(done)
	})
	return done
}

// killConfirm is how long SIGKILL has to show it worked. A kill that was sent
// is the kernel's to carry out, so this is a bound on noticing, not a grace.
const killConfirm = 2 * time.Second

// terminate ends one group — SIGTERM, then SIGKILL once StopGrace has passed —
// and answers nil only once the process is PROVEN gone. A process this codeaf
// started reports its own exit; one it re-adopted is looked at every stopPoll.
// Anything else is an error, and the caller still owns the process.
func (s *Supervisor) terminate(identity processgroup.Identity, gone chan struct{}) error {
	if !s.aliveNow(identity, gone) {
		return nil
	}
	termErr := s.rt.Signal(identity, false)
	if s.waitGone(identity, gone, StopGrace) {
		return nil
	}
	killErr := s.rt.Signal(identity, true)
	if killErr == nil && s.waitGone(identity, gone, killConfirm) {
		return nil
	}
	if !s.aliveNow(identity, gone) {
		return nil
	}
	switch {
	case killErr != nil:
		return fmt.Errorf("SIGKILL could not be sent: %v", killErr)
	case termErr != nil:
		return fmt.Errorf("SIGTERM could not be sent (%v) and SIGKILL did not end it", termErr)
	}
	return errors.New("it outlived SIGTERM and SIGKILL")
}

// aliveNow is one look: a process this codeaf started is alive until its exit
// is reported; any other is asked about through its identity.
func (s *Supervisor) aliveNow(identity processgroup.Identity, gone chan struct{}) bool {
	if gone != nil {
		select {
		case <-gone:
			return false
		default:
		}
	}
	return s.rt.Alive(identity)
}

// waitGone waits up to within for the process to go, and reports whether it
// did.
func (s *Supervisor) waitGone(identity processgroup.Identity, gone chan struct{}, within time.Duration) bool {
	if gone != nil {
		grace := make(chan struct{})
		timer := s.clock.AfterFunc(within, func() { close(grace) })
		select {
		case <-gone:
			timer.Stop()
			return true
		case <-grace:
			return !s.aliveNow(identity, gone)
		}
	}
	deadline := s.clock.Now().Add(within)
	for s.rt.Alive(identity) {
		if !s.clock.Now().Before(deadline) {
			return false
		}
		s.sleep(stopPoll)
	}
	return true
}

func (s *Supervisor) sleep(d time.Duration) {
	woke := make(chan struct{})
	s.clock.AfterFunc(d, func() { close(woke) })
	<-woke
}

// startWork launches e's process for generation gen, after ending the process
// it replaces, and writes the result down. It runs with the lock released, and
// its error is one the caller can act on.
//
// THE ORDER IS THE SAFETY. The process being replaced must be PROVEN gone
// before anything else: one that would not stop is still ours, stays on the
// row, and nothing is launched beside it — a second copy is not a restart, and
// its port answering is not "another program". Then the new group is made and
// held at its gate, its identity is written down, and only then is the
// approved command let run. A write that fails lets the gate go, so the command
// never ran, and the row keeps the gate's identity until its exit is seen.
func (s *Supervisor) startWork(id string, gen uint64, how string, auto bool) error {
	s.mu.Lock()
	e := s.entries[id]
	if e == nil || e.gen != gen {
		s.mu.Unlock()
		return nil
	}
	spec, old, oldGone, before := e.rec.Spec, e.rec.Identity, e.gone, e.before
	logPath := s.logPath(id)
	s.mu.Unlock()

	if old != nil {
		if err := s.terminate(*old, oldGone); err != nil {
			s.mu.Lock()
			if e = s.entries[id]; e != nil && e.gen == gen {
				e.busy = false
				reason := oneLine("could not stop it to restart: "+err.Error(), maxReason)
				s.openEpisodeLocked(e, reason)
				if auto || before == "" {
					// AN AUTOMATIC RESTART THAT CANNOT STOP ITS PROCESS RESTS. Trying
					// again on every backoff would be signals in a loop forever.
					before = StateGaveUp
				}
				e.rec.State, e.rec.Reason, e.rec.BackoffUntil = before, reason, time.Time{}
				s.changedLocked(e, "state")
				s.pumpLocked()
			}
			s.mu.Unlock()
			return errors.Join(err, s.save())
		}
		s.mu.Lock()
		if e.rec.Identity != nil && *e.rec.Identity == *old {
			e.rec.Identity, e.gone = nil, nil
		}
		s.mu.Unlock()
	}
	// A PORT ALREADY ANSWERING BEFORE THE SERVICE STARTS IS PROVEN TO BE
	// SOMEBODY ELSE'S: the service's own process is proven gone above, and
	// the new one has not run. It is said rather than started over, because
	// the check would pass against the other program and call this service
	// healthy while it never ran.
	conflict := spec.Health.Kind == HealthPort && s.rt.PortInUse(spec.Health.Port)
	var launch Launch
	var launchErr error
	truncated := false
	gone := make(chan struct{})
	if !conflict {
		truncated, _ = rotateLog(logPath)
		launch, launchErr = s.rt.Launch(spec, logPath, func(identity processgroup.Identity, exit Exit) {
			close(gone)
			s.exited(id, identity, exit)
		})
	}

	s.mu.Lock()
	e = s.entries[id]
	if e == nil || e.gen != gen {
		// Stopped, restarted or removed while this was starting: the gate is
		// let go and the command never runs.
		s.mu.Unlock()
		if launch != nil {
			launch.Abort()
		}
		return nil
	}
	now := s.clock.Now()
	e.rec.Truncated = e.rec.Truncated || truncated
	e.rec.ExitCode = nil
	e.rec.PortConflict = false
	if conflict || launchErr != nil {
		reason := "port " + strconv.Itoa(spec.Health.Port) + " is used by another program"
		if !conflict {
			reason = oneLine(launchErr.Error(), maxReason)
		}
		e.busy = false
		s.openEpisodeLocked(e, reason)
		e.rec.State, e.rec.PortConflict, e.rec.Reason = StateFailed, conflict, reason
		e.rec.EndedAt = now
		s.changedLocked(e, "state")
		s.pumpLocked()
		s.mu.Unlock()
		return s.save()
	}
	identity := launch.Identity()
	e.rec.Identity, e.gone = &identity, gone
	e.rec.StartedAt, e.rec.StartedHow, e.rec.EndedAt = now, how, time.Time{}
	e.rec.State, e.rec.Reason = StateStarting, ""
	s.changedLocked(e, "state")
	s.mu.Unlock()

	if err := s.save(); err != nil {
		launch.Abort()
		s.mu.Lock()
		if s.entries[id] == e && e.gen == gen {
			e.busy = false
			// The gate's identity stays on the row until its exit is seen, so
			// the process is never out of reach while it might still exist.
			e.abandoned = e.rec.Identity != nil && *e.rec.Identity == identity
			e.rec.State, e.rec.Reason = StateFailed, oneLine(err.Error(), maxReason)
			s.openEpisodeLocked(e, e.rec.Reason)
			s.changedLocked(e, "state")
		}
		s.mu.Unlock()
		return err
	}

	s.mu.Lock()
	defer func() {
		s.pumpLocked()
		s.mu.Unlock()
		_ = s.save()
	}()
	if s.entries[id] != e || e.gen != gen {
		// A stop took the gate while it was being written down; it ends it.
		launch.Abort()
		return nil
	}
	e.busy = false
	if e.rec.Identity == nil || *e.rec.Identity != identity {
		// The gate ended on its own before it was let run.
		launch.Abort()
		e.rec.State, e.rec.Reason = StateFailed, "the process ended before it could run"
		s.openEpisodeLocked(e, e.rec.Reason)
		s.changedLocked(e, "state")
		return errors.New(e.rec.Reason)
	}
	// The commit is a few bytes into an empty pipe and never blocks, so it is
	// made under the lock: nothing can stop the service between the last look
	// and the command being let run.
	if err := launch.Commit(); err != nil {
		e.abandoned = true
		e.rec.State, e.rec.Reason = StateFailed, "the process ended before it could run"
		s.openEpisodeLocked(e, e.rec.Reason)
		if !s.aliveNow(identity, gone) {
			e.rec.Identity, e.gone = nil, nil
			e.abandoned = false
		}
		s.changedLocked(e, "state")
		return err
	}
	e.rec.BackoffUntil, e.rec.HealthySince = time.Time{}, time.Time{}
	if e.rec.Check != nil {
		e.rec.Check.Fails = 0
	}
	if auto {
		e.rec.Restarts = append(restartsWithin(e.rec.Restarts, now), now)
	}
	e.nextCheck = now.Add(CheckEvery)
	s.changedLocked(e, "started")
	return nil
}

// exited is a started process's exit report, from its reaper, matched to the
// record by the identity it ended with — never by a generation, which a
// failed replacement may have moved past while this process was still ours.
func (s *Supervisor) exited(id string, identity processgroup.Identity, exit Exit) {
	s.mu.Lock()
	e := s.entries[id]
	if s.closed || e == nil || e.rec.Identity == nil || *e.rec.Identity != identity {
		s.mu.Unlock()
		return
	}
	if e.busy || e.stopping() {
		// The process being replaced, or the one a stop is ending: whoever is
		// doing that writes down what comes next.
		e.rec.Identity, e.gone = nil, nil
		e.rec.EndedAt = s.clock.Now()
		s.mu.Unlock()
		return
	}
	s.exitLocked(e, exit)
	s.pumpLocked()
	s.mu.Unlock()
	_ = s.save()
}

// exitLocked settles a process that ended on its own.
func (s *Supervisor) exitLocked(e *entry, exit Exit) {
	now := s.clock.Now()
	e.rec.Identity, e.gone = nil, nil
	e.rec.EndedAt = now
	e.rec.ExitCode = nil
	if e.abandoned || e.rec.State == StateStopped {
		// A gate let go after a failed write, or a stopped service's process
		// whose report beat its stop: it is gone, and that is all.
		e.abandoned = false
		s.changedLocked(e, "state")
		return
	}
	words := "exited"
	if exit.Known {
		code := exit.Code
		e.rec.ExitCode = &code
		words = "exited " + strconv.Itoa(code)
	}
	if exit.Known && exit.Code == 0 && e.rec.Policy != PolicyAlways {
		e.rec.State, e.rec.Reason = StateNotRunning, words
		e.rec.HealthySince = time.Time{}
		s.changedLocked(e, "state")
		return
	}
	s.failLocked(e, words, true)
}

// failLocked is one failure — an exit, or UnhealthyAfter failed checks in a
// row — meeting the policy the person chose, as it is NOW.
func (s *Supervisor) failLocked(e *entry, reason string, exited bool) {
	now := s.clock.Now()
	s.openEpisodeLocked(e, reason)
	e.rec.HealthySince = time.Time{}
	if e.rec.Policy == PolicyNever {
		if exited {
			e.rec.State, e.rec.Reason = StateFailed, reason+" · not restarted (policy: never)"
		} else {
			e.rec.State, e.rec.Reason = StateUnhealthy, reason
		}
		s.changedLocked(e, "state")
		return
	}
	recent := restartsWithin(e.rec.Restarts, now)
	e.rec.Restarts = recent
	if len(recent) >= RestartLimit {
		e.rec.State = StateGaveUp
		e.rec.Reason = fmt.Sprintf("stopped restarting after %d failures in 10 minutes", RestartLimit)
		s.changedLocked(e, "state")
		return
	}
	e.rec.State, e.rec.Reason = StateRestarting, reason
	e.rec.BackoffUntil = now.Add(FirstBackoff << len(recent))
	s.changedLocked(e, "state")
}

// openEpisodeLocked starts a new run of trouble unless one is already open, so
// unhealthy, restarting and gave up are one episode — one Inbox line that
// changes its words — and not three. What first went wrong is kept with it.
func (s *Supervisor) openEpisodeLocked(e *entry, reason string) {
	if !e.rec.EpisodeOpen {
		e.rec.Episode++
		e.rec.EpisodeOpen = true
		e.rec.EpisodeAt = s.clock.Now()
		e.rec.EpisodeEnded = time.Time{}
		e.rec.EpisodeReason = oneLine(reason, maxReason)
	}
}

// closeEpisodeLocked ends the open episode, once: the moment it ended is
// written when it ends and never again, so a surface's "recovered at" does not
// move with every passing check after it.
func (s *Supervisor) closeEpisodeLocked(e *entry) {
	if e.rec.EpisodeOpen {
		e.rec.EpisodeOpen = false
		e.rec.EpisodeEnded = s.clock.Now()
	}
}

// probeWork runs one check of e for generation gen.
func (s *Supervisor) probeWork(id string, gen uint64) {
	s.mu.Lock()
	e := s.entries[id]
	if e == nil || e.gen != gen || e.rec.Identity == nil {
		if e != nil {
			e.probing = false
		}
		s.mu.Unlock()
		return
	}
	spec, identity, own := e.rec.Spec, *e.rec.Identity, e.gone != nil
	exitedOwn := e.exitedOwn()
	began := s.clock.Now()
	s.mu.Unlock()

	// A re-adopted process reports no exit, so its check is also where its end
	// is noticed. A process this codeaf started reports its own, and one whose
	// report is on its way is not checked as though it ran.
	alive := !exitedOwn && (own || s.rt.Alive(identity))
	var err error
	if alive {
		err = s.rt.Probe(s.base, spec)
	}
	truncated, _ := rotateLog(s.logPath(id))

	s.mu.Lock()
	defer func() {
		s.mu.Unlock()
		_ = s.save()
	}()
	e.probing = false
	if truncated {
		e.rec.Truncated = true
	}
	if s.closed || s.entries[id] != e || e.gen != gen || e.rec.Identity == nil || e.stopping() || e.busy || exitedOwn {
		return
	}
	if !alive {
		s.exitLocked(e, Exit{})
		return
	}
	now := s.clock.Now()
	e.nextCheck = began.Add(CheckEvery)
	if e.nextCheck.Before(now) {
		e.nextCheck = now
	}
	if err == nil {
		e.rec.Check = &Check{OK: true, At: now}
		if e.rec.State != StateHealthy {
			e.rec.State, e.rec.Reason = StateHealthy, ""
			e.rec.HealthySince = now
			s.closeEpisodeLocked(e)
			s.changedLocked(e, "state")
			return
		}
		if !e.rec.HealthySince.IsZero() && now.Sub(e.rec.HealthySince) >= RestartWindow && len(e.rec.Restarts) > 0 {
			e.rec.Restarts = nil
		}
		s.changedLocked(e, "check")
		return
	}
	fails := 1
	if e.rec.Check != nil && !e.rec.Check.OK {
		fails = e.rec.Check.Fails + 1
	}
	detail := oneLine(err.Error(), maxProbeDetail)
	e.rec.Check = &Check{OK: false, At: now, Fails: fails, Detail: detail}
	failing := e.rec.State == StateStarting || e.rec.State == StateHealthy ||
		(e.rec.State == StateUnhealthy && e.rec.Policy != PolicyNever)
	if fails >= UnhealthyAfter && failing {
		s.failLocked(e, detail+" · "+strconv.Itoa(fails)+" checks", false)
		return
	}
	s.changedLocked(e, "check")
}

// ── the timer ───────────────────────────────────────────────────────────────

// pumpLocked hands every piece of due work a free worker and arms the one
// timer for the earliest piece that is not yet due. Work that found no free
// worker stays due and is handed out when a worker comes back.
func (s *Supervisor) pumpLocked() {
	if s.closed {
		return
	}
	now := s.clock.Now()
	var next time.Time
	soonest := func(at time.Time) {
		if next.IsZero() || at.Before(next) {
			next = at
		}
	}
	ids := make([]string, 0, len(s.entries))
	for id := range s.entries {
		ids = append(ids, id)
	}
	// Oldest overdue work goes first. Stable id ordering alone lets eight slow
	// checks consume every worker repeatedly while newer services never run.
	due := func(e *entry) time.Time {
		if e.rec.State == StateRestarting {
			return e.rec.BackoffUntil
		}
		return e.nextCheck
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := due(s.entries[ids[i]]), due(s.entries[ids[j]])
		if !a.Equal(b) {
			return a.Before(b)
		}
		return idLess(ids[i], ids[j])
	})
	for _, id := range ids {
		e := s.entries[id]
		if e.busy || e.stopping() {
			continue
		}
		switch {
		case e.rec.State == StateRestarting:
			if now.Before(e.rec.BackoffUntil) {
				soonest(e.rec.BackoffUntil)
				continue
			}
			if s.free == 0 {
				continue
			}
			s.free--
			e.gen++
			e.busy = true
			e.before = ""
			gen := e.gen
			s.wg.Add(1)
			go func() {
				defer s.workerDone()
				_ = s.startWork(id, gen, StartedByRestart, true)
			}()
		case e.rec.Identity != nil && !e.probing && e.rec.State != StateStopped && !e.abandoned:
			if now.Before(e.nextCheck) {
				soonest(e.nextCheck)
				continue
			}
			if s.free == 0 {
				continue
			}
			s.free--
			e.probing = true
			gen := e.gen
			s.wg.Add(1)
			go func() {
				defer s.workerDone()
				s.probeWork(id, gen)
			}()
		}
	}
	if next.IsZero() {
		if s.timer != nil {
			s.timer.Stop()
			s.timer = nil
		}
		return
	}
	if s.timer != nil && s.timerAt.Equal(next) {
		return
	}
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timerAt = next
	s.timer = s.clock.AfterFunc(next.Sub(now), s.tick)
}

func (s *Supervisor) workerDone() {
	s.mu.Lock()
	s.free++
	s.pumpLocked()
	s.mu.Unlock()
	s.wg.Done()
}

func (s *Supervisor) tick() {
	s.mu.Lock()
	s.timer = nil
	s.pumpLocked()
	s.mu.Unlock()
}

// goWork runs one piece of a person's or an adoption's work outside the worker
// bound: there is at most one per service at a time, so they are bounded by
// the store's own bound, and a person's stop never queues behind checks.
//
// IT IS CALLED UNDER THE LOCK, and refuses once Close has begun: Close waits
// for the work it can see, and work added behind that wait would run against
// a store another codeaf may already hold.
func (s *Supervisor) goWork(work func()) {
	if s.closed {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		work()
	}()
}

// ── rows, events, the file ──────────────────────────────────────────────────

func (s *Supervisor) findLocked(owner, id string) (*entry, error) {
	e := s.entries[id]
	if e == nil || e.rec.Owner != owner || owner == "" {
		return nil, ErrNotFound
	}
	return e, nil
}

func (s *Supervisor) rowLocked(e *entry) Row {
	return e.rec.row(s.clock.Now(), e.rec.Identity != nil, e.stopping(), s.logPath(e.rec.ID))
}

func (s *Supervisor) changedLocked(e *entry, change string) {
	e.rec.Updated = s.clock.Now()
	s.dirty++
	s.enqueueLocked(Event{Row: s.rowLocked(e), Change: change})
}

func (s *Supervisor) enqueueLocked(event Event) {
	if len(s.listeners) == 0 {
		return
	}
	id := event.Row.ID
	if _, waiting := s.pending[id]; !waiting {
		s.pendingOrder = append(s.pendingOrder, id)
	}
	s.pending[id] = event
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

func (s *Supervisor) notifyLoop() {
	for {
		select {
		case <-s.kick:
		case <-s.quit:
			s.deliver()
			return
		}
		s.deliver()
	}
}

func (s *Supervisor) deliver() {
	s.mu.Lock()
	order, pending := s.pendingOrder, s.pending
	s.pendingOrder, s.pending = nil, map[string]Event{}
	listeners := make([]func(Event), 0, len(s.listeners))
	ids := make([]int, 0, len(s.listeners))
	for id := range s.listeners {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		listeners = append(listeners, s.listeners[id])
	}
	s.mu.Unlock()
	for _, id := range order {
		for _, listen := range listeners {
			func() {
				defer func() { _ = recover() }()
				listen(pending[id])
			}()
		}
	}
}

func (s *Supervisor) trimConsentLocked() {
	for len(s.consent) > maxConsent {
		var oldest string
		var at time.Time
		for key, c := range s.consent {
			if oldest == "" || c.At.Before(at) {
				oldest, at = key, c.At
			}
		}
		delete(s.consent, oldest)
	}
}

// save writes the newest state with the lock released, and says when it
// could not. Two saves racing write the newer of the two and skip the older;
// a save that finds the newest state already on disk writes nothing.
func (s *Supervisor) save() error {
	s.mu.Lock()
	gen := s.dirty
	image := storeImage{Version: storeVersion, Boot: s.rt.Boot(), Seq: s.seq, Consent: s.consent}
	for _, e := range s.entries {
		image.Services = append(image.Services, e.rec)
	}
	sort.Slice(image.Services, func(i, j int) bool { return idLess(image.Services[i].ID, image.Services[j].ID) })
	raw, err := json.Marshal(image)
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotSaved, err)
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	if s.released {
		return ErrClosed
	}
	if s.wrote && gen <= s.saved {
		return nil
	}
	if err := s.write(s.dir, raw); err != nil {
		return fmt.Errorf("%w: %v", ErrNotSaved, err)
	}
	s.saved, s.wrote = gen, true
	return nil
}

// idLess orders "s2" before "s10".
func idLess(a, b string) bool {
	na, _ := strconv.Atoi(strings.TrimPrefix(a, "s"))
	nb, _ := strconv.Atoi(strings.TrimPrefix(b, "s"))
	if na != nb {
		return na < nb
	}
	return a < b
}
