package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// PutGuidance writes standing instructions for a folder, or for Root when
// ScopeID is empty. A guidance change and its root revision bump share this
// writer transaction; no model I/O happens here.
func (s *Store) PutGuidance(ctx context.Context, g Guidance) (Guidance, error) {
	g, err := prepareGuidance(g)
	if err != nil {
		return Guidance{}, storeError(err)
	}
	if err := s.writeReady(ctx); err != nil {
		return Guidance{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Guidance{}, storeError(err)
	}
	defer tx.Rollback()
	if err := placeGuidance(ctx, tx, &g, s.stamp()); err != nil {
		return Guidance{}, storeError(err)
	}
	return g, storeError(tx.Commit())
}

func prepareGuidance(g Guidance) (Guidance, error) {
	if g.Status == "" {
		g.Status = GuidanceActive
	}
	if !validGuidanceStatus(g.Status) {
		return Guidance{}, fmt.Errorf("%w: unknown guidance status %q", ErrInvalid, g.Status)
	}
	if g.Status == GuidanceActive && !validGuidanceText(g.Text) {
		return Guidance{}, fmt.Errorf("%w: guidance text is empty or too long", ErrInvalid)
	}
	prepared, err := prepareProvenance(Provenance{Origin: g.Origin, Actor: g.Actor})
	if err != nil {
		return Guidance{}, err
	}
	g.Origin, g.Actor = prepared.Origin, prepared.Actor
	if g.Revision == 0 {
		g.Revision = 1
	}
	return g, nil
}

func placeGuidance(ctx context.Context, tx *sql.Tx, g *Guidance, now string) error {
	if g.ScopeID != "" {
		if err := requireCollection(ctx, tx, g.ScopeID); err != nil {
			return err
		}
	}
	if g.Supersedes != "" {
		if err := supersedeGuidance(ctx, tx, g.Supersedes, now); err != nil {
			return err
		}
	}
	if g.ID == "" {
		id, err := mintID()
		if err != nil {
			return err
		}
		g.ID = id
	}
	g.CreatedAt, g.UpdatedAt = now, now
	if _, err := tx.ExecContext(ctx, `INSERT INTO guidance(`+guidanceColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		g.ID, g.ScopeID, g.Text, g.Status, g.Origin, g.Actor, g.SourceRef, g.Supersedes, g.Revision, g.CreatedAt, g.UpdatedAt); err != nil {
		return err
	}
	ids := []string{}
	if g.ScopeID != "" {
		ids = append(ids, g.ScopeID)
	}
	return bumpTouched(ctx, tx, now, ids...)
}

func supersedeGuidance(ctx context.Context, tx *sql.Tx, id, now string) error {
	result, err := tx.ExecContext(ctx, `UPDATE guidance SET status=?, updated_at=? WHERE id=?`, GuidanceSuperseded, now, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListGuidance is a read. Listing never migrates, so a v1 or v2 file returns
// nothing rather than creating the v3 tables.
func (s *Store) ListGuidance(ctx context.Context, scopeID string) ([]Guidance, error) {
	ready, err := s.v3Ready(ctx)
	if err != nil || !ready {
		return make([]Guidance, 0), err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+guidanceColumns+` FROM guidance WHERE scope_id=? ORDER BY seq`, scopeID)
	if err != nil {
		return nil, storeError(err)
	}
	defer rows.Close()
	result := make([]Guidance, 0)
	for rows.Next() {
		g, err := scanGuidance(rows)
		if err != nil {
			return nil, storeError(err)
		}
		result = append(result, g)
	}
	return result, storeError(rows.Err())
}

func scanGuidance(row eventScanner) (Guidance, error) {
	var g Guidance
	err := row.Scan(&g.ID, &g.ScopeID, &g.Text, &g.Status, &g.Origin, &g.Actor, &g.SourceRef,
		&g.Supersedes, &g.Revision, &g.CreatedAt, &g.UpdatedAt)
	return g, err
}

func (s *Store) v3Ready(ctx context.Context) (bool, error) {
	ready, err := s.readyForRead(ctx)
	if err != nil || !ready {
		return false, err
	}
	s.schemaMu.Lock()
	defer s.schemaMu.Unlock()
	return s.version >= 3, nil
}

// Suppress records that this evidence must not re-file the same object into
// the same collection. Identical evidence cannot come back; a new hash may.
func (s *Store) Suppress(ctx context.Context, collectionID string, ref Ref, evidenceHash string, p Provenance) error {
	if evidenceHash == "" || len(evidenceHash) > 4096 {
		return storeError(fmt.Errorf("%w: suppression needs an evidence hash", ErrInvalid))
	}
	if err := s.prepareWrite(ctx, ref, &p); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	if err := requireCollection(ctx, tx, collectionID); err != nil {
		return storeError(err)
	}
	at := s.stamp()
	if err := suppressInTx(ctx, tx, collectionID, ref, evidenceHash, p, at); err != nil {
		return storeError(err)
	}
	return storeError(tx.Commit())
}

func suppressInTx(ctx context.Context, tx *sql.Tx, collectionID string, ref Ref, evidenceHash string, p Provenance, at string) error {
	if evidenceHash == "" || len(evidenceHash) > 4096 {
		return fmt.Errorf("%w: suppression needs an evidence hash", ErrInvalid)
	}
	if err := requireCollection(ctx, tx, collectionID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO placement_suppressions(collection_id,kind,ref_id,session_id,evidence_hash,actor,at)
 VALUES (?,?,?,?,?,?,?) ON CONFLICT(collection_id,kind,ref_id,session_id,evidence_hash) DO NOTHING`,
		collectionID, ref.Kind, ref.ID, ref.SessionID, evidenceHash, p.Actor, at)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	return bumpTouched(ctx, tx, at, collectionID)
}

func (s *Store) IsSuppressed(ctx context.Context, collectionID string, ref Ref, evidenceHash string) (bool, error) {
	if err := ref.Validate(); err != nil {
		return false, storeError(err)
	}
	ready, err := s.v3Ready(ctx)
	if err != nil || !ready {
		return false, err
	}
	var found int
	err = s.db.QueryRowContext(ctx, `SELECT 1 FROM placement_suppressions
 WHERE collection_id=? AND kind=? AND ref_id=? AND session_id=? AND evidence_hash=? LIMIT 1`,
		collectionID, ref.Kind, ref.ID, ref.SessionID, evidenceHash).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, storeError(err)
}
