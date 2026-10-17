// Package lease holds the factory's claims desk. Workers pull queued items;
// the desk records one expiring hold per job and never executes the work.
package lease

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"context"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// DefaultTTL keeps a disconnected worker's job from remaining taken forever.
const DefaultTTL = factory.DefaultRunnerTimeoutSeconds * time.Second

// HeartbeatEvery is how often a worker renews a live lease. The worker loop's
// default beat is this same duration, so a quiet machine and a missed beat
// are measured on one clock.
const HeartbeatEvery = 2 * time.Second

// OfflineMisses is how many heartbeat intervals may pass with nothing heard
// before a machine is offline. S2 SETS SIXTY SECONDS: presence and the default lease share this deadline.
const OfflineMisses = int(OfflineAfter / HeartbeatEvery)

// OfflineAfter is the S2 runner silence deadline, shared by every presence read.
const OfflineAfter = factory.DefaultRunnerTimeoutSeconds * time.Second

var (
	ErrNoJob      = errors.New("factory lease: no queued job for this worker")
	ErrLeased     = errors.New("factory lease: job is already taken")
	ErrWorkerBusy = errors.New("factory lease: worker already has a job")
	ErrNoLease    = errors.New("factory lease: no live lease for this job")
	ErrPerson     = errors.New("factory lease: job belongs to another person")
	ErrWorker     = errors.New("factory lease: job belongs to another worker")
	// ErrNotTaken is an un-take of a job nobody has taken. A forge assignee
	// is not a take, so release does not clear one.
	ErrNotTaken = errors.New("factory lease: job is not taken")
)

// Lease is the persisted and wire shape, so the worker and person survive a
// server restart without a second registry becoming the ownership authority.
type Lease struct {
	// ID distinguishes successive holds on the same job so delayed reports cannot write into a new run.
	ID      string `json:"id,omitempty"`
	Job     int    `json:"job"`
	Worker  string `json:"worker"`
	Machine string `json:"machine,omitempty"`
	// ThreadOnly lends the manager to a reply without restarting job steps.
	ThreadOnly bool `json:"thread_only,omitempty"`
	Asleep     bool `json:"asleep,omitempty"`
	// Publish keeps a collected approval recoverable if the publisher vanishes.
	Publish string `json:"publish,omitempty"`
	// Merge is the commit of a collected merge, kept the same way.
	Merge  string `json:"merge,omitempty"`
	Handle string `json:"handle"`
	// Since is when this hold began. HeartbeatAt is the last renewal. Both
	// stay on the lease so a restart can still say who is working and when
	// they were last heard, without a second presence file.
	Since       time.Time `json:"since,omitempty"`
	HeartbeatAt time.Time `json:"heartbeat_at,omitempty"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// EveryRepo in TakesUnassigned is consent to unassigned work in every
// repository: a machine working its own loopback factory, where every job is
// its person's.
const EveryRepo = "*"

// Worker carries authenticated identity and repository-specific consent.
// TakesUnassigned never grants access to a job that already has a person.
type Worker struct {
	ID              string
	Machine         string
	Handle          string
	TakesUnassigned map[string]bool
	// Team refuses unassigned work even when an older client offers consent.
	Team  bool
	Solo  bool
	Slots int
}

// Options makes time deterministic in tests; a zero TTL uses DefaultTTL.
// Record, when set, receives a step_failed event when a hold expires and
// the job goes back to the queue.
type Options struct {
	TTL    time.Duration
	Now    func() time.Time
	Record *record.Record
}

// Desk uses the store's root so every server opening the same factory shares
// the same flock, rather than trusting a mutex in one server process.
type Desk struct {
	store  *store.Store
	ttl    time.Duration
	now    func() time.Time
	record *record.Record
}

// Open holds no handles. Reading is deferred to each operation so a desk
// opened before another server writes never keeps a stale ownership cache.
func Open(st *store.Store, options Options) (*Desk, error) {
	if st == nil || st.Root() == "" {
		return nil, errors.New("factory lease: no store")
	}
	if options.TTL < 0 {
		return nil, errors.New("factory lease: TTL must be positive")
	}
	if options.TTL == 0 {
		options.TTL = DefaultTTL
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Desk{store: st, ttl: options.TTL, now: options.Now, record: options.Record}, nil
}

// TTL is the hold length this desk renews to.
func (d *Desk) TTL() time.Duration {
	if _, err := os.Stat(filepath.Join(d.store.Root(), "runner-settings.json")); err == nil {
		if value, err := d.store.RunnerSettings(); err == nil {
			return time.Duration(value.RunnerTimeoutSeconds) * time.Second
		}
	}
	return d.ttl
}

// Live is the holds that are still inside their TTL, after releasing any
// that have expired. Reading presence reaps the same way a pull does, so a
// job is not described as held once its lease is gone.
func (d *Desk) Live() ([]Lease, error) {
	var live []Lease
	err := d.change(func(doc *document, now time.Time) (bool, error) {
		live = append([]Lease(nil), doc.Leases...)
		return false, nil
	})
	return live, err
}

// Assign implements Take: it gives an unassigned item a person, not a worker.
// Pull remains the only door that reserves a worker and starts a queued job.
func (d *Desk) Assign(id int, handle string) error {
	handle = factory.NormalizeHandle(handle)
	if handle == "" {
		return errors.New("factory lease: a person needs a handle")
	}
	return d.change(func(doc *document, now time.Time) (bool, error) {
		if find(doc, id) != nil {
			return false, ErrLeased
		}
		return false, d.store.Update(id, func(it *factory.Item) error {
			if owner := it.Person(); owner != "" && owner != handle {
				return ErrPerson
			}
			it.Taker = handle
			return nil
		})
	})
}

// Release is un-take. The person who took the job lets it go. A worker's
// hold is left alone: release does not stop the run or delete the branch.
func (d *Desk) Release(id int, handle string) error {
	handle = factory.NormalizeHandle(handle)
	if handle == "" {
		return errors.New("factory lease: a person needs a handle")
	}
	return d.change(func(doc *document, now time.Time) (bool, error) {
		return false, d.store.Update(id, func(it *factory.Item) error {
			if it.Taker == "" {
				return ErrNotTaken
			}
			if factory.NormalizeHandle(it.Taker) != handle {
				return ErrPerson
			}
			it.Taker = ""
			return nil
		})
	})
}

// Pull takes the oldest eligible queued item. One worker holds one job; an
// opt-in applies only to unassigned work in that exact repository.
func (d *Desk) Pull(worker Worker) (Lease, error) {
	worker.Handle = factory.NormalizeHandle(worker.Handle)
	worker.Machine = strings.TrimSpace(worker.Machine)
	if strings.TrimSpace(worker.ID) == "" || worker.Handle == "" {
		return Lease{}, errors.New("factory lease: a worker needs an id and handle")
	}
	var taken Lease
	err := d.change(func(doc *document, now time.Time) (bool, error) {
		occupied := 0
		for _, held := range doc.Leases {
			if worker.Machine != "" && held.Machine == worker.Machine && held.Handle == worker.Handle {
				occupied++
			}
			if held.Worker == worker.ID {
				return false, ErrWorkerBusy
			}
		}
		if worker.Slots > 0 && occupied >= worker.Slots {
			return false, ErrWorkerBusy
		}
		items, err := d.store.List()
		if err != nil {
			return false, err
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].Marked != items[j].Marked {
				return items[i].Marked
			}
			// Priority is a view mark. Execution remains oldest first unless a
			// person explicitly moved queued work to the top (Marked above).
			if items[i].Created.Equal(items[j].Created) {
				return items[i].ID < items[j].ID
			}
			return items[i].Created.Before(items[j].Created)
		})
		for _, it := range items {
			if !eligible(it, worker) || find(doc, it.ID) != nil {
				continue
			}
			var token [16]byte
			if _, err := rand.Read(token[:]); err != nil {
				return false, err
			}
			taken = Lease{ID: "lease_" + hex.EncodeToString(token[:]), Job: it.ID, Worker: worker.ID, Machine: worker.Machine, Handle: worker.Handle, Since: now, HeartbeatAt: now, ExpiresAt: now.Add(d.TTL()), ThreadOnly: it.State != factory.StateQueued || paused(it)}
			return false, d.reserve(doc, worker, taken)
		}
		return false, ErrNoJob
	})
	if err != nil {
		return Lease{}, err
	}
	return taken, nil
}

// reserve rechecks the item under its own lock because intake or a person
// may have changed it after List. THE LEASE IS DURABLE BEFORE RUNNING: a crash
// between files leaves a reserved queued job that expiry can recover, never
// running work without a lease that another worker could immediately take.
func (d *Desk) reserve(doc *document, worker Worker, taken Lease) error {
	return d.store.Update(taken.Job, func(it *factory.Item) error {
		if !eligible(*it, worker) {
			return ErrNoJob
		}
		doc.Leases = append(doc.Leases, taken)
		if err := d.write(*doc); err != nil {
			return err
		}
		if it.WorktreeMachine == "" {
			it.WorktreeMachine = taken.Machine
		}
		if !taken.ThreadOnly {
			it.State = factory.StateRunning
		}
		return nil
	})
}

// Heartbeat renews only the live holder's lease. At the expiry instant the
// lease is gone; a late heartbeat cannot reclaim a requeued job.
func (d *Desk) Heartbeat(id int, worker string) (Lease, error) {
	var renewed Lease
	err := d.change(func(doc *document, now time.Time) (bool, error) {
		held, err := holder(doc, id, worker)
		if err != nil {
			return false, err
		}
		held.ExpiresAt = now.Add(d.TTL())
		held.HeartbeatAt = now
		renewed = *held
		return true, nil
	})
	if err != nil {
		return Lease{}, err
	}
	return renewed, nil
}

// Expire returns the leases it released, even though completed or paused jobs
// keep their state. Only running work returns to the queue.
func (d *Desk) Expire() ([]Lease, error) {
	var expired []Lease
	err := d.locked(func(doc *document, now time.Time) error {
		var err error
		expired, err = d.expire(doc, now)
		if err != nil || len(expired) == 0 {
			return err
		}
		return d.write(*doc)
	})
	if err != nil {
		return nil, err
	}
	return expired, nil
}

// Requeue releases a worker's hold early. Ownership on the item stays put,
// so another person's machines cannot inherit a disconnected person's bill.
func (d *Desk) Requeue(id int, worker string) error {
	return d.change(func(doc *document, now time.Time) (bool, error) {
		if _, err := holder(doc, id, worker); err != nil {
			return false, err
		}
		if err := d.queue(id); err != nil {
			return false, err
		}
		if err := d.restorePublish(doc, id); err != nil {
			return false, err
		}
		return true, d.keepPublish(doc, id)
	})
}

func eligible(it factory.Item, worker Worker) bool {
	if (it.State != factory.StateQueued || paused(it)) && !pendingReply(it) || (it.WorktreeMachine != "" && it.WorktreeMachine != worker.Machine) {
		return false
	}
	if worker.Solo {
		return true
	}
	if owner := it.Person(); owner != "" {
		return owner == worker.Handle
	}
	// THE FORGE NAMED SOMEONE, so this is not unassigned work: several
	// assignees leave no single owner, and consent to unassigned work is not
	// consent to theirs. Only an explicit Assign or start gives it a person.
	if len(it.SourceAssignees) > 0 {
		return false
	}
	return !worker.Team && (worker.TakesUnassigned[it.Repo] || worker.TakesUnassigned[EveryRepo])
}

func pendingReply(it factory.Item) bool {
	if it.Talk == "" {
		return false
	}
	for _, m := range it.Thread {
		if m.State == "pending" {
			return true
		}
	}
	return false
}

func find(doc *document, id int) *Lease {
	for i := range doc.Leases {
		if doc.Leases[i].Job == id {
			return &doc.Leases[i]
		}
	}
	return nil
}

func holder(doc *document, id int, worker string) (*Lease, error) {
	held := find(doc, id)
	if held == nil {
		return nil, ErrNoLease
	}
	if held.Worker != worker {
		return nil, ErrWorker
	}
	return held, nil
}

func (d *Desk) queue(id int) error {
	return d.queueJob(id, time.Time{}, "")
}

// queueJob returns a running job to the queue. lost is set when the hold
// expired: that is the machine lost after the lease's own wait, and the job
// records a failure. A worker letting go passes the zero time and records none.
// machine is the hold's machine, empty when the worker did not name one.
func (d *Desk) queueJob(id int, lost time.Time, machine string) error {
	// A no-op must not stamp Changed or append activity to finished work.
	unchanged := errors.New("factory lease: job is not running")
	var failed factory.Failure
	err := d.store.Update(id, func(it *factory.Item) error {
		if it.State != factory.StateRunning || paused(*it) {
			return unchanged
		}
		it.State = factory.StateQueued
		if lost.IsZero() {
			return nil
		}
		step := ""
		attempts := 1
		if it.Stream != nil && it.Stream.Cur >= 0 && it.Stream.Cur < len(it.Stream.Phases) {
			ph := it.Stream.Phases[it.Stream.Cur]
			step = ph.Name
			if ph.Round > attempts {
				attempts = ph.Round
			}
		}
		if machine == "" {
			machine = it.WorktreeMachine
		}
		if it.Publishing {
			step = "publishing"
		}
		it.State = factory.StateNeedsYou
		it.QKind = factory.NeedsRunnerOffline
		it.Question = "Runner went offline mid-step"
		failed = factory.NewFailure(step, factory.CodeMachineLost, "Runner went offline mid-step", machine, factory.FailureStopped, attempts, lost)
		factory.StampFailure(it, failed)
		return nil
	})
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, unchanged) {
		return nil
	}
	if err != nil {
		return err
	}
	if !lost.IsZero() && d.record != nil {
		_, _ = d.record.Append(context.Background(), record.Event{
			Kind: record.StepFailed, Job: id, Detail: failed.Code + " · " + failed.Step + " · " + failed.Next,
		})
	}
	return nil
}

func paused(it factory.Item) bool { return it.Stream != nil && it.Stream.Paused }

func (d *Desk) expire(doc *document, now time.Time) ([]Lease, error) {
	var expired, kept []Lease
	for _, held := range append([]Lease(nil), doc.Leases...) {
		if !held.HeartbeatAt.IsZero() {
			held.ExpiresAt = held.HeartbeatAt.Add(d.TTL())
		}
		if held.Asleep || now.Before(held.ExpiresAt) {
			kept = append(kept, held)
			continue
		}
		if err := d.queueJob(held.Job, now, held.Machine); err != nil {
			return nil, fmt.Errorf("factory lease: requeue job %d: %w", held.Job, err)
		}
		expired = append(expired, held)
		if err := d.restorePublish(doc, held.Job); err != nil {
			return nil, err
		}
		if err := d.keepPublish(doc, held.Job); err != nil {
			return nil, err
		}
	}
	doc.Leases = kept
	return expired, nil
}

// change persists expiry even when the requested operation is refused. A
// worker polling the wrong person's job must still free disconnected workers.
func (d *Desk) change(change func(*document, time.Time) (bool, error)) error {
	return d.locked(func(doc *document, now time.Time) error {
		expired, err := d.expire(doc, now)
		if err != nil {
			return err
		}
		// Persist recovery before the next operation, which may itself write
		// a lease before changing an item's state.
		if len(expired) > 0 {
			if err := d.write(*doc); err != nil {
				return err
			}
		}
		dirty, err := change(doc, now)
		if err != nil || !dirty {
			return err
		}
		return d.write(*doc)
	})
}
