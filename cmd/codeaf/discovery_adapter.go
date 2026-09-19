package main

// discoveryAdapter is the production Discoverer: a real discovery.db plus
// the shipped RoleEmbed client. Tests may inject a stand-in Embedder. Open
// never binds the discover package's test fake — a down or missing embedder
// is the labelled degraded path, not a silent zero-vector success.

import (
	"context"
	"sync"

	"github.com/Agent-Field/codeaf/internal/embed"
	"github.com/Agent-Field/codeaf/internal/wsapi"
	"github.com/Agent-Field/codeaf/internal/wsdiscover"
)

var (
	_ wsapi.Discoverer           = (*discoveryAdapter)(nil)
	_ wsdiscover.Embedder        = (*embed.Client)(nil)
	_ interface{ Close() error } = (*discoveryAdapter)(nil)
)

type discoveryAdapter struct {
	store    *wsdiscover.Store
	mu       sync.RWMutex
	embedder embed.Embedder
}

func newDiscoveryAdapter(store *wsdiscover.Store, embedder embed.Embedder) *discoveryAdapter {
	if store == nil {
		return nil
	}
	return &discoveryAdapter{store: store, embedder: embedder}
}

func (d *discoveryAdapter) Close() error {
	if d == nil || d.store == nil {
		return nil
	}
	return d.store.Close()
}

func (d *discoveryAdapter) SetEmbedder(embedder embed.Embedder) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.embedder = embedder
}

// Ingest writes journal records into the same discovery.db SearchEvidence
// reads. Replay of a stored cursor is a no-op. A nil or down embedder still
// stores the passages and leaves vectors empty — never a dummy success.
func (d *discoveryAdapter) Ingest(ctx context.Context, records []wsdiscover.Record) error {
	if d == nil || d.store == nil {
		return nil
	}
	return d.store.Ingest(ctx, records, discoverEmbedder(d.currentEmbedder()))
}

func discoverEmbedder(current embed.Embedder) wsdiscover.Embedder {
	if current == nil {
		return nil
	}
	if bound, ok := current.(wsdiscover.Embedder); ok {
		return bound
	}
	return nil
}

func (d *discoveryAdapter) currentEmbedder() embed.Embedder {
	if d == nil {
		return nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.embedder
}

func (d *discoveryAdapter) SearchLexical(ctx context.Context, query string, limit int) ([]wsapi.SearchHit, error) {
	if d == nil || d.store == nil {
		return []wsapi.SearchHit{}, nil
	}
	rows, err := d.store.SearchLexical(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	return hitsOf(rows, wsapi.ScoreBM25, false), nil
}

func (d *discoveryAdapter) SearchEmbed(ctx context.Context, query string, limit int) ([]wsapi.SearchHit, error) {
	if d == nil || d.store == nil {
		return []wsapi.SearchHit{}, nil
	}
	hits, ok, err := d.searchVectors(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	if ok {
		return hits, nil
	}
	return d.searchDegraded(ctx, query, limit)
}

func (d *discoveryAdapter) searchVectors(ctx context.Context, query string, limit int) ([]wsapi.SearchHit, bool, error) {
	embedder := d.currentEmbedder()
	if embedder == nil {
		return nil, false, nil
	}
	_, available, err := embedder.Available(ctx)
	if err != nil || !available {
		return nil, false, nil
	}
	vecs, model, version, dim, err := embedder.Embed(ctx, []string{query})
	if err != nil || !usableQueryVector(vecs, dim) {
		return nil, false, nil
	}
	rows, err := d.store.SearchSimilar(ctx, vecs[0], model, version, dim, limit)
	if err != nil {
		return nil, false, err
	}
	return hitsOf(rows, wsapi.ScoreEmbed, false), true, nil
}

func usableQueryVector(vecs [][]float32, dim int) bool {
	return dim > 0 && len(vecs) > 0 && len(vecs[0]) == dim
}

func (d *discoveryAdapter) searchDegraded(ctx context.Context, query string, limit int) ([]wsapi.SearchHit, error) {
	terms := embed.DegradedLexical(query).Terms
	if len(terms) == 0 {
		terms = []string{query}
	}
	seen := map[string]struct{}{}
	out := make([]wsapi.SearchHit, 0)
	for _, term := range terms {
		rows, err := d.store.SearchLexical(ctx, term, limit)
		if err != nil {
			return nil, err
		}
		out = appendDegraded(out, rows, seen, limit)
		if limit > 0 && len(out) >= limit {
			return out, nil
		}
	}
	return out, nil
}

func appendDegraded(out []wsapi.SearchHit, rows []wsdiscover.Passage, seen map[string]struct{}, limit int) []wsapi.SearchHit {
	for _, hit := range hitsOf(rows, wsapi.ScoreExpansion, true) {
		key := hit.SessionID + "\x00" + hit.Ref
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, hit)
		if limit > 0 && len(out) >= limit {
			return out
		}
	}
	return out
}

func (d *discoveryAdapter) IndexProgress(ctx context.Context) (wsapi.IndexView, error) {
	if d == nil || d.store == nil {
		return delayedView(), nil
	}
	prog, err := d.store.Progress(ctx)
	if err != nil {
		return wsapi.IndexView{}, err
	}
	view := wsapi.IndexView{
		Passages: prog.Passages,
		Vectors:  prog.Vectors,
		Delayed:  prog.Delayed,
		Degraded: prog.Degraded,
		Detail:   prog.Detail,
	}
	if view.Passages == 0 {
		view.Delayed = true
		if view.Detail == "" {
			view.Detail = embed.LabelDelayed
		}
	}
	if d.embedderDown(ctx) {
		view.Delayed = true
		view.Degraded = true
		if view.Detail == "" {
			view.Detail = embed.LabelDelayed
		}
	}
	return view, nil
}

func delayedView() wsapi.IndexView {
	return wsapi.IndexView{Delayed: true, Detail: embed.LabelDelayed}
}

func (d *discoveryAdapter) embedderDown(ctx context.Context) bool {
	embedder := d.currentEmbedder()
	if embedder == nil {
		return true
	}
	_, ok, err := embedder.Available(ctx)
	return err != nil || !ok
}

func hitsOf(rows []wsdiscover.Passage, kind string, degraded bool) []wsapi.SearchHit {
	out := make([]wsapi.SearchHit, len(rows))
	for i, p := range rows {
		ref := p.SourceRef
		if ref == "" {
			ref = p.SessionID
		}
		out[i] = wsapi.SearchHit{
			Ref:       ref,
			SessionID: p.SessionID,
			Passage:   p.Text,
			ScoreKind: kind,
			Degraded:  degraded,
		}
	}
	return out
}
