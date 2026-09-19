package wsdiscover

import (
	"context"
	"database/sql"
	"errors"
	"sort"
)

// Head is the latest ingested cursor for a session. A session that was never
// indexed is not found; a deleted session is gone; an unavailable session still
// reports the last cursor so absence and unavailability stay distinct (A22).
func (s *Store) Head(ctx context.Context, sessionID string) (Cursor, error) {
	ready, err := s.readyForRead(ctx)
	if err != nil {
		return Cursor{}, err
	}
	if !ready {
		return Cursor{}, ErrNotFound
	}
	state, err := s.sourceState(ctx, sessionID)
	if err != nil {
		return Cursor{}, err
	}
	if state == SourceDeleted {
		return Cursor{}, ErrNotFound
	}
	var c Cursor
	err = s.db.QueryRowContext(ctx, `
		SELECT session_id, generation, ordinal, content_hash FROM cursors
		 WHERE session_id=?
		 ORDER BY generation DESC, ordinal DESC LIMIT 1`, sessionID).Scan(
		&c.SessionID, &c.Generation, &c.Ordinal, &c.ContentHash)
	if errors.Is(err, sql.ErrNoRows) {
		if state == SourceUnavailable {
			return Cursor{SessionID: sessionID}, ErrUnavailable
		}
		return Cursor{}, ErrNotFound
	}
	return c, err
}

func (s *Store) sourceState(ctx context.Context, sessionID string) (string, error) {
	var state string
	err := s.db.QueryRowContext(ctx, `SELECT state FROM sources WHERE session_id=?`, sessionID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return state, err
}

// Source reports present, deleted, or unavailable. Absent is ErrNotFound.
func (s *Store) Source(ctx context.Context, sessionID string) (state string, generation int, err error) {
	ready, err := s.readyForRead(ctx)
	if err != nil || !ready {
		if err != nil {
			return "", 0, err
		}
		return "", 0, ErrNotFound
	}
	err = s.db.QueryRowContext(ctx, `SELECT state, generation FROM sources WHERE session_id=?`, sessionID).
		Scan(&state, &generation)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, ErrNotFound
	}
	return state, generation, err
}

// Passages lists current derived rows for a present or unavailable session.
// Deleted and absent sources return nothing current.
func (s *Store) Passages(ctx context.Context, sessionID string) ([]Passage, error) {
	ready, err := s.readyForRead(ctx)
	if err != nil {
		return nil, err
	}
	if !ready {
		return []Passage{}, nil
	}
	state, err := s.sourceState(ctx, sessionID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return []Passage{}, nil
		}
		return nil, err
	}
	if state == SourceDeleted {
		return []Passage{}, nil
	}
	return s.queryPassages(ctx, `SELECT `+passageColumns+` FROM passages WHERE session_id=? ORDER BY generation, ordinal`, sessionID)
}

const passageColumns = `id,session_id,generation,ordinal,content_hash,source_ref,speaker,text,model,version,dimension,vector`

func scanPassage(rows *sql.Rows) (Passage, error) {
	var (
		p      Passage
		ignore int64
		raw    []byte
	)
	err := rows.Scan(&ignore, &p.SessionID, &p.Generation, &p.Ordinal, &p.ContentHash,
		&p.SourceRef, &p.Speaker, &p.Text, &p.Model, &p.Version, &p.Dimension, &raw)
	if err != nil {
		return Passage{}, err
	}
	p.Vector = unpackVector(raw)
	return p, nil
}

func (s *Store) queryPassages(ctx context.Context, q string, args ...any) ([]Passage, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Passage, 0)
	for rows.Next() {
		p, err := scanPassage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SearchLexical is FTS over present passages. Hostile syntax is a miss, the
// same way the conversation index treats it — not an error and not a dump.
func (s *Store) SearchLexical(ctx context.Context, query string, limit int) ([]Passage, error) {
	ready, err := s.readyForRead(ctx)
	if err != nil {
		return nil, err
	}
	if !ready || query == "" {
		return []Passage{}, nil
	}
	if limit < 1 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+passageColumns+` FROM passages
		 WHERE id IN (SELECT rowid FROM passages_fts WHERE passages_fts MATCH ?)
		   AND session_id IN (SELECT session_id FROM sources WHERE state != ?)
		 ORDER BY generation, ordinal
		 LIMIT ?`, query, SourceDeleted, limit)
	if err != nil {
		return []Passage{}, nil
	}
	defer rows.Close()
	out := make([]Passage, 0)
	for rows.Next() {
		p, err := scanPassage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SearchSimilar is brute-force cosine over vectors that share model, version,
// and dimension. A mismatch is skipped, never compared.
func (s *Store) SearchSimilar(ctx context.Context, query []float32, model, version string, dim, limit int) ([]Passage, error) {
	ready, err := s.readyForRead(ctx)
	if err != nil {
		return nil, err
	}
	if !ready || len(query) == 0 || dim < 1 || len(query) != dim {
		return []Passage{}, nil
	}
	if limit < 1 {
		limit = 20
	}
	candidates, err := s.queryPassages(ctx, `
		SELECT `+passageColumns+` FROM passages
		 WHERE model=? AND version=? AND dimension=?
		   AND vector IS NOT NULL
		   AND session_id IN (SELECT session_id FROM sources WHERE state != ?)`,
		model, version, dim, SourceDeleted)
	if err != nil {
		return nil, err
	}
	type scored struct {
		p Passage
		s float64
	}
	hits := make([]scored, 0, len(candidates))
	for _, p := range candidates {
		if !sameEmbedding(p, model, version, dim) {
			continue
		}
		hits = append(hits, scored{p: p, s: cosine(query, p.Vector)})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].s > hits[j].s })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Passage, len(hits))
	for i, h := range hits {
		out[i] = h.p
	}
	return out, nil
}

// Progress counts passages and vectors. Zero stays a number here so the
// surface can apply the emptiness law; Detail is empty when caught up.
func (s *Store) Progress(ctx context.Context) (IndexProgress, error) {
	ready, err := s.readyForRead(ctx)
	if err != nil || !ready {
		return IndexProgress{}, err
	}
	var p IndexProgress
	if err := s.db.QueryRowContext(ctx, `
		SELECT
		 COUNT(*),
		 COALESCE(SUM(CASE WHEN vector IS NOT NULL AND length(vector)>0 THEN 1 ELSE 0 END), 0)
		 FROM passages
		 WHERE session_id IN (SELECT session_id FROM sources WHERE state != ?)`,
		SourceDeleted).Scan(&p.Passages, &p.Vectors); err != nil {
		return IndexProgress{}, err
	}
	_ = s.db.QueryRowContext(ctx, `
		SELECT session_id, generation, ordinal, content_hash FROM cursors
		 ORDER BY generation DESC, ordinal DESC LIMIT 1`).Scan(
		&p.Cursor.SessionID, &p.Cursor.Generation, &p.Cursor.Ordinal, &p.Cursor.ContentHash)
	if p.Passages > p.Vectors {
		p.Delayed = true
		p.Detail = delayedDetail
	}
	return p, nil
}
