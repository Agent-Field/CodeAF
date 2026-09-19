package wsdiscover

import (
	"context"
	"database/sql"
)

// Rewind invalidates derived rows for one source generation. The journal is
// the authority; this only drops passages, FTS rows, and cursors that belonged
// to the rewritten generation (A22). Later generations are left alone.
func (s *Store) Rewind(ctx context.Context, sessionID string, generation int) error {
	if sessionID == "" || generation < 1 {
		return ErrInvalid
	}
	if err := s.ensureSchema(ctx); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := dropDerived(ctx, tx, sessionID, generation); err != nil {
		return err
	}
	if err := refreshSourceAfterDrop(ctx, tx, sessionID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteSource drops every derived row for a session and marks the source
// deleted. Abandoned proposals live in collections.db and are not touched.
// A later ingest of the same id writes a new present generation.
func (s *Store) DeleteSource(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return ErrInvalid
	}
	if err := s.ensureSchema(ctx); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := dropDerived(ctx, tx, sessionID, 0); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sources(session_id,state,generation) VALUES (?, ?, 0)
		 ON CONFLICT(session_id) DO UPDATE SET state=?, generation=0`,
		sessionID, SourceDeleted, SourceDeleted); err != nil {
		return err
	}
	return tx.Commit()
}

// MarkUnavailable records that a source still exists but cannot be read. It
// does not delete derived rows. Unavailable is not deleted and not absent.
func (s *Store) MarkUnavailable(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return ErrInvalid
	}
	if err := s.ensureSchema(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sources(session_id,state,generation) VALUES (?, ?, 0)
		 ON CONFLICT(session_id) DO UPDATE SET state=?`,
		sessionID, SourceUnavailable, SourceUnavailable)
	return err
}

// Reset wipes derived rows so the index can be rebuilt from journals. The
// schema stays; callers re-ingest.
func (s *Store) Reset(ctx context.Context) error {
	if err := s.ensureSchema(ctx); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM passages_fts`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM passages`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM cursors`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sources`); err != nil {
		return err
	}
	return tx.Commit()
}

// Rebuild recreates the FTS rows from stored passages and re-embeds them.
// Journals stay outside; this is how a file that already holds passages is
// made consistent after a vector-model change.
func (s *Store) Rebuild(ctx context.Context, embed Embedder) error {
	if err := s.ensureSchema(ctx); err != nil {
		return err
	}
	passages, err := s.queryPassages(ctx, `SELECT `+passageColumns+` FROM passages ORDER BY id`)
	if err != nil {
		return err
	}
	planned := make([]plannedPassage, len(passages))
	for i, p := range passages {
		p.Model, p.Version, p.Dimension, p.Vector = "", "", 0, nil
		planned[i] = plannedPassage{Passage: p}
	}
	filled, _, err := embedPassages(ctx, planned, embed)
	if err != nil {
		return err
	}
	return s.replaceDerived(ctx, filled)
}

func (s *Store) replaceDerived(ctx context.Context, planned []plannedPassage) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM passages_fts`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM passages`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM cursors`); err != nil {
		return err
	}
	for _, p := range planned {
		if err := insertPassage(ctx, tx, p.Passage); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// generation 0 means every generation of the session.
func dropDerived(ctx context.Context, tx *sql.Tx, sessionID string, generation int) error {
	q := `SELECT id, generation, ordinal FROM passages WHERE session_id=?`
	args := []any{sessionID}
	if generation > 0 {
		q += ` AND generation=?`
		args = append(args, generation)
	}
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	type row struct {
		id      int64
		gen     int
		ordinal int64
	}
	seen := make([]row, 0)
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.gen, &r.ordinal); err != nil {
			return err
		}
		seen = append(seen, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range seen {
		if err := dropPassageID(ctx, tx, r.id, sessionID, r.gen, r.ordinal); err != nil {
			return err
		}
	}
	return nil
}

func refreshSourceAfterDrop(ctx context.Context, tx *sql.Tx, sessionID string) error {
	var gen int
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(generation),0) FROM passages WHERE session_id=?`, sessionID).Scan(&gen)
	if err != nil {
		return err
	}
	if gen == 0 {
		_, err = tx.ExecContext(ctx, `DELETE FROM sources WHERE session_id=?`, sessionID)
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE sources SET generation=? WHERE session_id=?`, gen, sessionID)
	return err
}
