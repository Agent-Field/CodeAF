package main

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// v3ModelShelf is the catalog the surface's model lists read, behind ONE
// pointer that a refresh can swap.
//
// It exists because a [catalog.Catalog] is immutable once resolved and shared by
// everything that asks it a question, so "today's list" cannot be written into
// the one the process warmed at launch — it has to be a second catalog, handed
// out in the first one's place. The shelf is that place: every reader that asks
// the never-waiting question ([v3Catalog.ModelsNow]) through it sees the new
// list the moment a refresh lands, and a reader that was handed the launch
// catalog directly keeps the launch catalog, which is still true, only older.
type v3ModelShelf struct {
	current atomic.Pointer[catalog.Catalog]
	// options is how the launch catalog was loaded, so a refresh asks the same
	// router, with the same key, into the same cache file.
	options catalog.Options
	mu      sync.RWMutex
	direct  map[string]serviceCompartment
}

type serviceCompartment struct {
	address string
	door    string
	models  []tui3.Model
}

func newV3ModelShelf(models *catalog.Catalog, options catalog.Options) *v3ModelShelf {
	shelf := &v3ModelShelf{options: options, direct: make(map[string]serviceCompartment)}
	shelf.current.Store(models)
	return shelf
}

// setSources keeps the shelf's connected-service compartments aligned with the
// live profile. A compartment survives only while its address and proved door
// do: reconnecting to another billing road must replace a wider catalog left by
// the old one. A fixed door catalog is seeded directly without a fetch; every
// other cold compartment reads the service-scoped cache.
func (s *v3ModelShelf) setSources(sources modelsource.Set) {
	if s == nil || sources.Empty() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	keep := make(map[string]bool)
	for _, service := range sources.All()[1:] {
		id := strings.ToLower(strings.TrimSpace(service.Source.ID))
		keep[id] = true
		address, door := serviceCompartmentIdentity(service)
		if held, ok := s.direct[id]; ok && held.address == address && held.door == door {
			continue
		}
		rows := fixedDoorModels(service)
		if len(rows) == 0 && service.Source.Listing == modelsource.ListingModels {
			rows = tui3.CachedModelsFor(service.Source.ID, service.Address)
		}
		s.direct[id] = serviceCompartment{address: address, door: door, models: rows}
	}
	for id := range s.direct {
		if !keep[id] {
			delete(s.direct, id)
		}
	}
}

// modelsForService is the never-waiting half of the connected-service shelf
// seam. The default keeps reading the atomic launch catalog; another service
// reads only its own compartment.
func (s *v3ModelShelf) modelsForService(service modelsource.Connected) []tui3.Model {
	if s == nil {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(service.Source.ID), modelsource.DefaultID) {
		return v3Models(s)
	}
	if service.Source.Listing != modelsource.ListingModels && len(service.Door.Models) == 0 {
		return nil
	}
	id := strings.ToLower(strings.TrimSpace(service.Source.ID))
	address, door := serviceCompartmentIdentity(service)
	held, rows, ok := s.compartment(id)
	if ok && held.address == address && held.door == door && len(rows) > 0 {
		return rows
	}
	// A PLAN DOOR'S CATALOG IS VENDORED AND COSTS NO FILE. [fixedDoorModels]
	// reads `service.Door.Models` — the four documented ids the plan covers,
	// held in memory since the profile was read — so this rung stays on the
	// right side of the law below and answers a bound plan door whose
	// compartment is empty or is still holding the other door's list.
	if fixed := fixedDoorModels(service); len(fixed) > 0 {
		return fixed
	}
	// AN EMPTY COMPARTMENT IS AN EMPTY ANSWER, and the surface falls to its own
	// rung below this one. This used to read the service's cache file here —
	// os.ReadFile plus a JSON parse of the whole list, with no memo in front of
	// it — and the seam is taken from a DRAW (tui3's setupModelRows →
	// setupModelChoices → modelList), so a profile with two model services paid
	// that read on every paint of the first-run screen. [setSources] already
	// fills each compartment from the same file when the profile is read, and
	// tui3 keeps its own memo of it (internal/tui3/learned.go), so the file is
	// still read — once, off the frame, on both sides of this seam.
	return rows
}

// compartment is one service's compartment as it stands, with the rows already
// copied so the caller walks its own list after the lock is let go.
func (s *v3ModelShelf) compartment(id string) (serviceCompartment, []tui3.Model, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	held, ok := s.direct[id]
	return held, append([]tui3.Model(nil), held.models...), ok
}

// stock writes one service's compartment. The cache-file write that pairs with
// it runs in the caller, after the lock is let go: a refresh is answered from a
// command off the event loop, and the disk half of it has no business holding
// the shelf from the readers behind it.
func (s *v3ModelShelf) stock(id, address, door string, models []tui3.Model) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.direct[id] = serviceCompartment{address: address, door: door, models: models}
}

// refreshService fetches a newly connected service into the same shelf /model
// reads and writes both service-scoped caches. It is the connected-service twin
// of refresh; the caller runs it as a command away from the event loop.
func (s *v3ModelShelf) refreshService(ctx context.Context, service modelsource.Connected, seed []tui3.Model) ([]tui3.Model, error) {
	if s == nil {
		return nil, errors.New("there is no model shelf")
	}
	options := s.options
	options.Source = service.Source.ID
	options.BaseURL = service.Address
	options.APIKey = service.Key
	if len(seed) > 0 {
		minimal := make([]catalog.Model, 0, len(seed))
		for _, model := range seed {
			minimal = append(minimal, catalog.Model{ID: model.ID})
		}
		if err := catalog.Remember(options, minimal); err != nil {
			return nil, err
		}
		id := strings.ToLower(strings.TrimSpace(service.Source.ID))
		address, door := serviceCompartmentIdentity(service)
		s.stock(id, address, door, append([]tui3.Model(nil), seed...))
		if err := tui3.WriteModelCacheFor(service.Source.ID, service.Address, seed); err != nil {
			return nil, err
		}
	}
	fresh, err := catalog.Refresh(ctx, options)
	if err != nil {
		if len(seed) > 0 {
			return append([]tui3.Model(nil), seed...), nil
		}
		return nil, v3FetchReason(err)
	}
	rows := v3Models(fresh)
	id := strings.ToLower(strings.TrimSpace(service.Source.ID))
	address, door := serviceCompartmentIdentity(service)
	s.stock(id, address, door, append([]tui3.Model(nil), rows...))
	_ = tui3.WriteModelCacheFor(service.Source.ID, service.Address, rows)
	return rows, nil
}

func serviceCompartmentIdentity(service modelsource.Connected) (address, door string) {
	return strings.TrimRight(strings.TrimSpace(service.Address), "/"), strings.ToLower(strings.TrimSpace(service.Door.ID))
}

func fixedDoorModels(service modelsource.Connected) []tui3.Model {
	rows := make([]tui3.Model, 0, len(service.Door.Models))
	for _, id := range service.Door.Models {
		if id = strings.TrimSpace(id); id != "" {
			rows = append(rows, tui3.Model{ID: id})
		}
	}
	return rows
}

// ModelsNow is the list on the shelf, answered without waiting — nil while a
// lazily loaded launch catalog is still warming, and nil for a nil shelf, so
// every reader falls through to the disk cache exactly as it did before.
func (s *v3ModelShelf) ModelsNow() []catalog.Model {
	if s == nil {
		return nil
	}
	return s.current.Load().ModelsNow()
}

// refresh is the surface's [tui3.Options.RefreshModels]: today's list from the
// router, put on the shelf, written to ~/.codeaf/v3/models.json so the next
// launch opens on it, and handed back with when it left the router.
//
// A FAILURE PUTS NOTHING ON THE SHELF. [catalog.Refresh] has already degraded to
// the cache it could not replace, and that cache is no newer than what the
// shelf holds, so the list the person was shown stays exactly as it was and
// only the reason travels back. The fetch's ceiling is the catalog's own
// fifteen seconds, applied by its client.
func (s *v3ModelShelf) refresh(ctx context.Context) ([]tui3.Model, time.Time, error) {
	fresh, err := catalog.Refresh(ctx, s.options)
	if err != nil {
		return nil, time.Time{}, v3FetchReason(err)
	}
	s.current.Store(fresh)
	rows := v3Models(fresh)
	_ = tui3.WriteModelCache(rows)
	return rows, fresh.FetchedAt(), nil
}

// v3FetchReason is the error as a person reads it on one line: the transport's
// own reason, without the `Get "https://…/models?output_modalities=all":` the
// HTTP client puts in front of it. The address is the same on every failure and
// says nothing about why this one happened.
func v3FetchReason(err error) error {
	var failed *url.Error
	if errors.As(err, &failed) && failed.Err != nil {
		return failed.Err
	}
	return err
}
