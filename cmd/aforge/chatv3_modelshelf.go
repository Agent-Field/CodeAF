package main

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/modelsource"
	"github.com/Agent-Field/aforge-v2/internal/tui3"
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
	direct  map[string][]tui3.Model
}

func newV3ModelShelf(models *catalog.Catalog, options catalog.Options) *v3ModelShelf {
	shelf := &v3ModelShelf{options: options, direct: make(map[string][]tui3.Model)}
	shelf.current.Store(models)
	return shelf
}

// setSources keeps the shelf's connected-service compartments aligned with the
// live profile. Existing compartments survive so a connection result already
// fetched into the shelf is not thrown away; a cold process fills them from the
// service-scoped picker cache without touching the network.
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
		if _, ok := s.direct[id]; !ok {
			if service.Source.Listing == modelsource.ListingModels {
				s.direct[id] = tui3.CachedModelsFor(service.Source.ID, service.Address)
			} else {
				s.direct[id] = nil
			}
		}
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
	if service.Source.Listing != modelsource.ListingModels {
		return nil
	}
	id := strings.ToLower(strings.TrimSpace(service.Source.ID))
	s.mu.RLock()
	rows := append([]tui3.Model(nil), s.direct[id]...)
	s.mu.RUnlock()
	if len(rows) > 0 {
		return rows
	}
	return tui3.CachedModelsFor(service.Source.ID, service.Address)
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
		s.mu.Lock()
		s.direct[id] = append([]tui3.Model(nil), seed...)
		s.mu.Unlock()
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
	s.mu.Lock()
	s.direct[id] = append([]tui3.Model(nil), rows...)
	s.mu.Unlock()
	_ = tui3.WriteModelCacheFor(service.Source.ID, service.Address, rows)
	return rows, nil
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
// router, put on the shelf, written to ~/.aforge/v3/models.json so the next
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
