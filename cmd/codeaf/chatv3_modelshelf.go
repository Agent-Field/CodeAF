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
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/roles"
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
	// sources is the service set the compartments were last aligned with, kept
	// so a model id can be taken to ITS service's rows ([v3ModelShelf.contextWindow]).
	sources modelsource.Set
	// fetchErrors holds, per provider id, why its last listing attempt failed —
	// the sentence the provider's group shows until a fetch lands. Written only
	// from commands off the event loop, read on the draw path.
	fetchErrors map[string]string
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
	s.sources = sources
	keep := make(map[string]bool)
	for _, service := range sources.All()[1:] {
		id := strings.ToLower(strings.TrimSpace(service.Source.ID))
		keep[id] = true
		address, door := serviceCompartmentIdentity(service)
		codex := strings.EqualFold(service.Source.ID, "codex")
		// A CODEX COMPARTMENT IS RE-READ EVERY TIME. Its rows come from the
		// catalog the sign-in remembered and nothing else writes them, so a
		// reconnect on the same address — which is every reconnect — rewrote
		// that catalog underneath a compartment the rule below would keep, and
		// a running engine went on answering the old rows. It is one small file.
		if held, ok := s.direct[id]; ok && held.address == address && held.door == door && !codex {
			continue
		}
		rows := fixedDoorModels(service)
		if len(rows) == 0 && codex {
			rows = v3Models(v3Rows(config.CodexRememberedModels(service, s.options.Dir)))
		}
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

// contextWindow is how many tokens a model accepts according to ITS OWN
// service's rows, zero when those rows cannot say. It never waits: the default
// service answers from the rows the shelf already holds, and every other service
// from its compartment.
//
// IT EXISTS BECAUSE A SESSION'S WINDOW WAS ASKED OF THE WRONG CATALOG. A
// conversation is handed the catalog of the service it STARTED on, and a model
// it later moves to on another service is a model that catalog has never heard
// of: a conversation that started on an OpenRouter model and moved to
// `codex/gpt-5.5` asked OpenRouter's rows for a Codex id, got zero, and went on
// compacting at the old model's figure (#1383). Over an engine host that was the
// whole story, because the engine ignores the window a surface sends and
// resolves its own on the switch.
//
// It is read under the session's own lock ([session.Config.ContextWindowFor] is
// called from inside SetModel), so it takes the shelf's lock and no other: the
// process takes its own lock and then each session's when a profile moves
// ([v3Process.setModelSources]), and a reader here that reached for the
// process's would be the other half of a deadlock.
func (s *v3ModelShelf) contextWindow(model string) int {
	if s == nil {
		return 0
	}
	// A THINKING LEVEL IS NOT PART OF THE ID the rows are filed under
	// (config.ClientConfigFor splits it off the same way), so `codex/gpt-5.5:high`
	// is asked about as `codex/gpt-5.5`.
	model, _ = roles.SplitEffort(model)
	sources := s.sourcesNow()
	if sources.Empty() {
		return v3ContextWindow(v3Models(s), model)
	}
	service, bare := sources.For(model)
	if strings.EqualFold(strings.TrimSpace(service.Source.ID), modelsource.DefaultID) {
		return v3ContextWindow(v3Models(s), bare)
	}
	return v3ContextWindow(s.modelsForService(service), bare)
}

// sourcesNow is the service set the shelf was last aligned with.
func (s *v3ModelShelf) sourcesNow() modelsource.Set {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sources
}

// v3WindowFor is the session's answer to "how big is this model": its own
// service's rows first ([v3ModelShelf.contextWindow]), and the catalog the
// conversation started on when those cannot say — which is the answer this door
// gave before, kept so a model the shelf has no row for yet answers exactly as
// it always did.
func v3WindowFor(shelf *v3ModelShelf, launch *catalog.Catalog) func(string) int {
	return func(model string) int {
		if window := shelf.contextWindow(model); window > 0 {
			return window
		}
		if launch == nil {
			return 0
		}
		return launch.ContextLength(model)
	}
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
	options := config.CatalogOptionsFor(service, s.options.Dir)
	if len(seed) > 0 {
		// The seed keeps each row's window: a refresh that fails leaves these rows
		// standing, and a row remembered without its window is a model whose
		// conversation keeps compacting at the previous model's figure (#1383).
		minimal := make([]catalog.Model, 0, len(seed))
		for _, model := range seed {
			minimal = append(minimal, catalog.Model{ID: model.ID, ContextLength: model.ContextLength})
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
		reason := v3FetchReason(err)
		s.fetchError(strings.ToLower(strings.TrimSpace(service.Source.ID)), reason)
		return nil, reason
	}
	rows := v3Models(fresh)
	id := strings.ToLower(strings.TrimSpace(service.Source.ID))
	address, door := serviceCompartmentIdentity(service)
	s.stock(id, address, door, append([]tui3.Model(nil), rows...))
	s.fetchError(id, nil)
	_ = tui3.WriteModelCacheFor(service.Source.ID, service.Address, rows)
	return rows, nil
}

// v3ServiceFetch is one connected provider's answer to a warm or a refresh:
// the rows when the door answered, and the reason when it did not. A provider
// that lists no models reports neither rows nor error — its group says so
// through the surface's own empty-group sentence.
type v3ServiceFetch struct {
	service modelsource.Connected
	rows    []tui3.Model
	err     error
}

// warmAll fetches every connected provider whose compartment is cold, through
// the same door the connect path uses ([v3ModelShelf.refreshService]). It is
// issue #1508's launch half: a profile whose model_sources rows survive but
// whose per-provider caches do not (a new machine, a cleaned profile, a
// hand-written row) used to open /model with nothing to offer and nothing
// coming — the only fetch was the one at connect time.
//
// IT IS THE CALLER'S GOROUTINE: this walks the network and must never run on
// the event loop (the same law the connect command keeps). The draw path keeps
// its lock discipline — the shelf takes its own lock and no other — and the
// reader sees each provider's group fill the moment its fetch stocks the
// compartment, without a reopen.
//
// A PROVIDER THAT CANNOT LIST IS NOT SKIPPED SILENTLY: the reason is recorded
// per provider ([v3ModelShelf.fetchErrors]) so the group can say why, and the
// next provider is still tried. ctrl+r shares this walk.
func (s *v3ModelShelf) warmAll(ctx context.Context, onlyCold bool) []v3ServiceFetch {
	if s == nil {
		return nil
	}
	var fetches []v3ServiceFetch
	for _, service := range s.sourcesNow().All()[1:] {
		if strings.EqualFold(service.Source.ID, "codex") {
			// A CODEX COMPARTMENT IS RE-READ FROM ITS REMEMBERED CATALOG, never
			// from the wire; its rows arrive at setSources. Skipped here.
			continue
		}
		id := strings.ToLower(strings.TrimSpace(service.Source.ID))
		if onlyCold {
			held, rows, ok := s.compartment(id)
			if ok && (len(rows) > 0 || held.address == "" && held.door == "") {
				// Warm, or a compartment that says it cannot be listed at all.
				continue
			}
			if len(rows) == 0 && !ok {
				// Not kept by setSources: not a listing provider this run.
				if service.Source.Listing != modelsource.ListingModels || len(service.Door.Models) > 0 {
					continue
				}
			}
		}
		if service.Source.Listing != modelsource.ListingModels || len(service.Door.Models) > 0 {
			// A provider that declares no listing (or vendors its catalog in
			// the door) has nothing to fetch; its group is drawn from what the
			// compartment or the vendored rows hold.
			continue
		}
		rows, err := s.refreshService(ctx, service, nil)
		if err == nil && len(rows) == 0 {
			continue
		}
		fetches = append(fetches, v3ServiceFetch{service: service, rows: rows, err: err})
	}
	return fetches
}

// fetchError records one provider's listing refusal, and fetchErrorFor reads it
// back for the group's status line. The maps are only ever touched under the
// shelf lock, from commands off the loop.
func (s *v3ModelShelf) fetchError(id string, err error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fetchErrors == nil {
		s.fetchErrors = make(map[string]string)
	}
	if err == nil {
		delete(s.fetchErrors, id)
		return
	}
	s.fetchErrors[id] = err.Error()
}

// fetchErrorFor is the recorded reason one provider last failed to list.
func (s *v3ModelShelf) fetchErrorFor(id string) string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.fetchErrors[id]
}

// v3Rows is a list of catalog rows already in hand, asked the one question
// [v3Models] asks of a catalog.
type v3Rows []catalog.Model

// ModelsNow is the rows themselves.
func (rows v3Rows) ModelsNow() []catalog.Model { return rows }

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
