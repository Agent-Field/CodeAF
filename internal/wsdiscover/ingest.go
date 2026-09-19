package wsdiscover

import (
	"context"
	"database/sql"
	"errors"
)

type plannedPassage struct {
	Passage
	replace bool
}

const (
	positionSame    = 1
	positionChanged = 2
	positionNew     = 3
)

// Ingest writes journal records into the derived index. Replay of the same
// cursor is a no-op (A13). Embedding runs outside the writer transaction so a
// model call never holds the file. A down or unusable embedder still stores
// the passages and marks discovery delayed — it does not invent vectors.
func (s *Store) Ingest(ctx context.Context, records []Record, embed Embedder) error {
	if err := s.ensureSchema(ctx); err != nil {
		return err
	}
	planned, err := s.planIngest(ctx, records)
	if err != nil {
		return err
	}
	if len(planned) == 0 {
		return nil
	}
	filled, _, err := embedPassages(ctx, planned, embed)
	if err != nil {
		return err
	}
	return s.commitIngest(ctx, filled)
}

func (s *Store) planIngest(ctx context.Context, records []Record) ([]plannedPassage, error) {
	out := make([]plannedPassage, 0, len(records))
	for _, rec := range records {
		if err := rec.validate(); err != nil {
			return nil, err
		}
		p := plannedPassage{Passage: rec.passage()}
		state, err := s.positionState(ctx, p.Passage)
		if err != nil {
			return nil, err
		}
		if state == positionSame {
			continue
		}
		p.replace = state == positionChanged
		out = append(out, p)
	}
	return out, nil
}

func (s *Store) positionState(ctx context.Context, p Passage) (int, error) {
	var hash string
	err := s.db.QueryRowContext(ctx, `
		SELECT content_hash FROM passages
		 WHERE session_id=? AND generation=? AND ordinal=?`,
		p.SessionID, p.Generation, p.Ordinal).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return positionNew, nil
	}
	if err != nil {
		return 0, err
	}
	if hash == p.ContentHash {
		return positionSame, nil
	}
	return positionChanged, nil
}

func embedPassages(ctx context.Context, planned []plannedPassage, embed Embedder) ([]plannedPassage, bool, error) {
	if embed == nil {
		return planned, true, nil
	}
	_, ok, err := embed.Available(ctx)
	if err != nil || !ok {
		return planned, true, nil
	}
	texts := make([]string, len(planned))
	for i, p := range planned {
		texts[i] = p.Text
	}
	vecs, model, version, dim, err := embed.Embed(ctx, texts)
	if err != nil || !usableVectors(vecs, len(texts), dim) {
		return planned, true, nil
	}
	for i := range planned {
		planned[i].Model = model
		planned[i].Version = version
		planned[i].Dimension = dim
		planned[i].Vector = vecs[i]
	}
	return planned, false, nil
}

func (s *Store) commitIngest(ctx context.Context, planned []plannedPassage) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range planned {
		if p.replace {
			if err := dropPosition(ctx, tx, p.SessionID, p.Generation, p.Ordinal); err != nil {
				return err
			}
		}
		if err := insertPassage(ctx, tx, p.Passage); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func insertPassage(ctx context.Context, tx *sql.Tx, p Passage) error {
	res, err := tx.ExecContext(ctx, `
		INSERT INTO passages(session_id,generation,ordinal,content_hash,source_ref,speaker,text,model,version,dimension,vector)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		p.SessionID, p.Generation, p.Ordinal, p.ContentHash, p.SourceRef, p.Speaker, p.Text,
		p.Model, p.Version, p.Dimension, packVector(p.Vector))
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO passages_fts(rowid,session_id,speaker,text) VALUES (?,?,?,?)`,
		id, p.SessionID, p.Speaker, p.Text); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO cursors(session_id,generation,ordinal,content_hash)
		 VALUES (?,?,?,?)`,
		p.SessionID, p.Generation, p.Ordinal, p.ContentHash); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO sources(session_id,state,generation) VALUES (?, ?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET
		  state=excluded.state,
		  generation=MAX(sources.generation, excluded.generation)`,
		p.SessionID, SourcePresent, p.Generation)
	return err
}

func dropPosition(ctx context.Context, tx *sql.Tx, sessionID string, generation int, ordinal int64) error {
	var id int64
	err := tx.QueryRowContext(ctx, `
		SELECT id FROM passages WHERE session_id=? AND generation=? AND ordinal=?`,
		sessionID, generation, ordinal).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return dropPassageID(ctx, tx, id, sessionID, generation, ordinal)
}

func dropPassageID(ctx context.Context, tx *sql.Tx, id int64, sessionID string, generation int, ordinal int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM passages_fts WHERE rowid=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM passages WHERE id=?`, id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		DELETE FROM cursors WHERE session_id=? AND generation=? AND ordinal=?`,
		sessionID, generation, ordinal)
	return err
}
