package main

// ── codeaf app: THE JOB SEARCH BAR'S AI SEARCH ──────────────────────────────
//
// The engine's half of appwire.FactoryAISearcher. The items are the caller's
// own queue read (GET /v1/queue, already filtered to what the caller may
// see), so a search never reads a job its person could not open. The model is
// the factory's default model (the queue's), else the arrival read's; every
// call is booked on the spend ledger under `factory search`, failed calls
// included, as the judge's are under `factory filter`.
//
// A RUN BELONGS TO THIS ENGINE, NOT TO A WINDOW: it goes on when the window
// that started it closes, any window reads it by id, and the newest
// [appwire.AISearchRunsMost] stay readable. Every answer is kept per factory
// in <home>/app/factory-ai-search/, so a saved view reopened asks only about
// what is new.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/aisearch"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/roles"
)

var _ appwire.FactoryAISearcher = (*appBackend)(nil)

// factorySearchName is what the search bar's AI costs under on the spend
// ledger, beside the judge's `factory filter`. The manual quotes it
// (internal/manual/chat/factory-ai-search.md).
const factorySearchName = "factory search"

// appAISearchRunMost is the longest one run may take before it is stopped.
const appAISearchRunMost = 10 * time.Minute

type appAISearchRun struct {
	id       string
	factory  string
	question string
	started  time.Time
	cancel   context.CancelFunc
	mu       sync.Mutex
	status   aisearch.Status
}

type appAISearchRuns struct {
	mu     sync.Mutex
	runs   map[string]*appAISearchRun
	caches map[string]*aisearch.Cache
	// cacheMu keeps one factory's cache file to one writer at a time.
	cacheMu sync.Mutex
}

// appAISearch is the engine's one registry of runs.
var appAISearch = &appAISearchRuns{runs: map[string]*appAISearchRun{}, caches: map[string]*aisearch.Cache{}}

// appAISearchModel is the model a search is asked of: the factory's default
// model, else the arrival read's, so it runs where the factory's own cheap
// reads run.
var appAISearchLoad = config.Load

func appAISearchModel(q api.Queue) string {
	if m := strings.TrimSpace(q.Model); m != "" {
		return m
	}
	settings, err := appAISearchLoad()
	if err != nil {
		return ""
	}
	m, _ := factoryRoleModel(settings, roles.RoleInsightFactoryRead)
	return m
}

// appAISearchItems is the caller's queue as the search reads it: issue jobs
// that are not dropped or dismissed, in the order jobs names (else the
// queue's), each with the words the judge reads.
func appAISearchItems(q api.Queue, jobs []int) []factory.Item {
	byID := map[int]api.Job{}
	var order []int
	for _, j := range q.Jobs {
		if j.Dismissed || j.State == api.StateDropped || (j.Kind != "" && j.Kind != string(factory.KindIssue)) {
			continue
		}
		byID[j.ID] = j
		order = append(order, j.ID)
	}
	if jobs != nil {
		order = jobs
	}
	out := make([]factory.Item, 0, len(order))
	for _, id := range order {
		j, ok := byID[id]
		if !ok {
			continue
		}
		it := factory.Item{ID: j.ID, Repo: j.Repo, Num: j.Num, Kind: factory.KindIssue, Title: j.Title}
		if d := j.Desk; d != nil {
			it.Body, it.Labels, it.SourceRef = d.Body, d.Labels, d.SourceRef
			it.Tier, it.Origin = factory.Tier(d.Tier), factory.Origin(d.Origin)
			it.Triage.Area, it.Triage.Type = d.Triage.Area, d.Triage.Type
		}
		out = append(out, it)
	}
	return out
}

func (r *appAISearchRuns) cache(dir, connection string) *aisearch.Cache {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.caches[connection]; ok {
		return c
	}
	sum := sha256.Sum256([]byte(connection))
	c := aisearch.OpenCache(filepath.Join(dir, "factory-ai-search", hex.EncodeToString(sum[:8])+".json"))
	r.caches[connection] = c
	return c
}

func (b *appBackend) aiSearchPrice(model string) *aisearch.Price {
	models, err := b.ListModels()
	if err != nil {
		return nil
	}
	for _, m := range models {
		if m.ID == model && m.PriceInPerM != nil && m.PriceOutPerM != nil {
			return &aisearch.Price{In: *m.PriceInPerM, Out: *m.PriceOutPerM}
		}
	}
	return nil
}

func (b *appBackend) FactoryAISearchEstimate(ctx context.Context, p appwire.FactoryAISearchParams) (appwire.FactoryAISearchEstimate, error) {
	p, err := appwire.CheckAISearch(p)
	if err != nil {
		return appwire.FactoryAISearchEstimate{}, err
	}
	h := b.factoryHub()
	q, err := b.FactoryQueue(ctx, p.Factory)
	if err != nil {
		return appwire.FactoryAISearchEstimate{}, err
	}
	model := appAISearchModel(q)
	items := appAISearchItems(q, p.Jobs)
	e := aisearch.EstimateFor(p.Question, items, appAISearch.cache(h.dir(), p.Factory), b.aiSearchPrice(model))
	return appwire.FactoryAISearchEstimate{Factory: p.Factory, Question: p.Question, Model: model, Estimate: e}, nil
}

func (b *appBackend) FactoryAISearchStart(ctx context.Context, p appwire.FactoryAISearchParams) (appwire.FactoryAISearchStarted, error) {
	p, err := appwire.CheckAISearch(p)
	if err != nil {
		return appwire.FactoryAISearchStarted{}, err
	}
	h := b.factoryHub()
	q, err := b.FactoryQueue(ctx, p.Factory)
	if err != nil {
		return appwire.FactoryAISearchStarted{}, err
	}
	model := appAISearchModel(q)
	if model == "" {
		return appwire.FactoryAISearchStarted{}, appwire.Errorf(appwire.CodeConflict, "no model is set for the factory; pick one in Spending first")
	}
	items := appAISearchItems(q, p.Jobs)
	cache := appAISearch.cache(h.dir(), p.Factory)
	raw := make([]byte, 8)
	_, _ = rand.Read(raw)
	run := &appAISearchRun{id: "s_" + hex.EncodeToString(raw), factory: p.Factory, question: p.Question, started: time.Now(),
		status: aisearch.Status{Phase: aisearch.PhaseScreening, Total: len(items), Model: model, Matches: []aisearch.Match{}}}
	runCtx, cancel := context.WithTimeout(context.Background(), appAISearchRunMost)
	run.cancel = cancel
	appAISearch.add(run)
	call := aisearch.Spending(factoryAICall(appAISearchLoad, factorySearchName))
	guard.Go("app/factory ai search", func() {
		defer cancel()
		final := aisearch.Run(runCtx, aisearch.Options{Question: p.Question, Items: items, Model: model, Call: call, Cache: cache,
			Progress: func(s aisearch.Status) { run.set(s) }})
		if final.Phase == aisearch.PhaseStopped && runCtx.Err() == context.DeadlineExceeded {
			final.Phase, final.Error = aisearch.PhaseFailed, "the search took longer than ten minutes and was stopped"
		}
		run.set(final)
		appAISearch.save(cache)
	})
	return appwire.FactoryAISearchStarted{Run: run.id}, nil
}

// save writes one factory's answers, one writer at a time.
func (r *appAISearchRuns) save(c *aisearch.Cache) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	_ = c.Save()
}

func (r *appAISearchRun) set(s aisearch.Status) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// A stop already said is not undone by a late progress report.
	if r.status.Phase == aisearch.PhaseStopped {
		return
	}
	r.status = s
}

func (r *appAISearchRun) view() appwire.FactoryAISearchRun {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.status
	s.Matches = append([]aisearch.Match{}, s.Matches...)
	return appwire.FactoryAISearchRun{Run: r.id, Factory: r.factory, Question: r.question, Words: aisearch.Words(s), Status: s}
}

func (r *appAISearchRun) finished() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch r.status.Phase {
	case aisearch.PhaseDone, aisearch.PhaseFailed, aisearch.PhaseStopped:
		return true
	}
	return false
}

// add keeps run, forgetting the oldest finished runs past the bound.
func (r *appAISearchRuns) add(run *appAISearchRun) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs[run.id] = run
	if len(r.runs) <= appwire.AISearchRunsMost {
		return
	}
	all := make([]*appAISearchRun, 0, len(r.runs))
	for _, x := range r.runs {
		all = append(all, x)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].started.Before(all[j].started) })
	for _, x := range all {
		if len(r.runs) <= appwire.AISearchRunsMost {
			break
		}
		if x.finished() {
			delete(r.runs, x.id)
		}
	}
}

func (r *appAISearchRuns) get(id string) (*appAISearchRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[id]
	if !ok {
		return nil, appwire.Errorf(appwire.CodeNotFound, "that search is no longer kept; ask again")
	}
	return run, nil
}

func (b *appBackend) FactoryAISearchStatus(id string) (appwire.FactoryAISearchRun, error) {
	run, err := appAISearch.get(id)
	if err != nil {
		return appwire.FactoryAISearchRun{}, err
	}
	return run.view(), nil
}

func (b *appBackend) FactoryAISearchStop(id string) (appwire.FactoryAISearchRun, error) {
	run, err := appAISearch.get(id)
	if err != nil {
		return appwire.FactoryAISearchRun{}, err
	}
	if !run.finished() {
		run.cancel()
		run.stopped()
	}
	return run.view(), nil
}

func (r *appAISearchRun) stopped() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.Phase = aisearch.PhaseStopped
}
