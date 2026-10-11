// Package lease holds the factory's claims desk. Workers pull queued items;
// the desk records one expiring hold per job and never executes the work.
package lease

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// DefaultTTL keeps a disconnected worker's job from remaining taken forever.
const DefaultTTL = time.Minute

var (
	ErrNoJob      = errors.New("factory lease: no queued job for this worker")
	ErrLeased     = errors.New("factory lease: job is already taken")
	ErrWorkerBusy = errors.New("factory lease: worker already has a job")
	ErrNoLease    = errors.New("factory lease: no live lease for this job")
	ErrPerson     = errors.New("factory lease: job belongs to another person")
	ErrWorker     = errors.New("factory lease: job belongs to another worker")
)

// Lease is the persisted and wire shape, so the worker and person survive a
// server restart without a second registry becoming the ownership authority.
type Lease struct {
	// ID distinguishes successive holds on the same job so delayed reports cannot write into a new run.
	ID        string    `json:"id,omitempty"`
	Job       int       `json:"job"`
	Worker    string    `json:"worker"`
	Handle    string    `json:"handle"`
	ExpiresAt time.Time `json:"expires_at"`
}

// EveryRepo in TakesUnassigned is consent to unassigned work in every
// repository: a machine working its own loopback factory, where every job is
// its person's.
const EveryRepo = "*"

// Worker carries authenticated identity and repository-specific consent.
// TakesUnassigned never grants access to a job that already has a person.
type Worker struct {
	ID              string
	Handle          string
	TakesUnassigned map[string]bool
}

// Options makes time deterministic in tests; a zero TTL uses DefaultTTL.
type Options struct {
	TTL time.Duration
	Now func() time.Time
}

// Desk uses the store's root so every server opening the same factory shares
// the same flock, rather than trusting a mutex in one server process.
type Desk struct {
	store *store.Store
	ttl   time.Duration
	now   func() time.Time
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
	return &Desk{store: st, ttl: options.TTL, now: options.Now}, nil
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

// Pull takes the oldest eligible queued item. One worker holds one job; an
// opt-in applies only to unassigned work in that exact repository.
func (d *Desk) Pull(worker Worker) (Lease, error) {
	worker.Handle = factory.NormalizeHandle(worker.Handle)
	if strings.TrimSpace(worker.ID) == "" || worker.Handle == "" {
		return Lease{}, errors.New("factory lease: a worker needs an id and handle")
	}
	var taken Lease
	err := d.change(func(doc *document, now time.Time) (bool, error) {
		for _, held := range doc.Leases {
			if held.Worker == worker.ID {
				return false, ErrWorkerBusy
			}
		}
		items, err := d.store.List()
		if err != nil {
			return false, err
		}
		sort.Slice(items, func(i, j int) bool {
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
			taken = Lease{ID: "lease_" + hex.EncodeToString(token[:]), Job: it.ID, Worker: worker.ID, Handle: worker.Handle, ExpiresAt: now.Add(d.ttl)}
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
		it.State = factory.StateRunning
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
		held.ExpiresAt = now.Add(d.ttl)
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
		release(doc, id)
		return true, nil
	})
}

func eligible(it factory.Item, worker Worker) bool {
	if it.State != factory.StateQueued || paused(it) {
		return false
	}
	if owner := it.Person(); owner != "" {
		return owner == worker.Handle
	}
	return worker.TakesUnassigned[it.Repo] || worker.TakesUnassigned[EveryRepo]
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
	// A no-op must not stamp Changed or append activity to finished work.
	unchanged := errors.New("factory lease: job is not running")
	err := d.store.Update(id, func(it *factory.Item) error {
		if it.State != factory.StateRunning || paused(*it) {
			return unchanged
		}
		it.State = factory.StateQueued
		return nil
	})
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, unchanged) {
		return nil
	}
	return err
}

func paused(it factory.Item) bool { return it.Stream != nil && it.Stream.Paused }

func (d *Desk) expire(doc *document, now time.Time) ([]Lease, error) {
	var expired, kept []Lease
	for _, held := range doc.Leases {
		if now.Before(held.ExpiresAt) {
			kept = append(kept, held)
			continue
		}
		if err := d.queue(held.Job); err != nil {
			return nil, fmt.Errorf("factory lease: requeue job %d: %w", held.Job, err)
		}
		expired = append(expired, held)
		delete(doc.Signals, held.Job)
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
