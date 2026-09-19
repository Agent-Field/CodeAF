package workspace

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const eventColumns = "collection_id,kind,ref_id,session_id,action,origin,reason,actor,evidence,at,idempotency_key"

func (s *Store) Add(ctx context.Context, id string, ref Ref) error {
	return s.AddWith(ctx, id, ref, Provenance{Origin: OriginPerson})
}

// Remove detaches a reference; it never removes or stops the referenced work.
func (s *Store) Remove(ctx context.Context, id string, ref Ref) error {
	return s.RemoveWith(ctx, id, ref, Provenance{Origin: OriginPerson})
}

func (s *Store) AddWith(ctx context.Context, id string, ref Ref, p Provenance) error {
	if err := s.prepareWrite(ctx, ref, &p); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	replay, err := eventKeyUsed(ctx, tx, p.IdempotencyKey)
	if err != nil {
		return storeError(err)
	}
	if replay {
		return storeError(tx.Commit())
	}
	added, err := insertMembership(ctx, tx, id, ref)
	if err != nil {
		return storeError(err)
	}
	if added {
		if err := recordEvent(ctx, tx, id, ref, ActionAdd, p, s.stamp()); err != nil {
			return storeError(err)
		}
	}
	return storeError(tx.Commit())
}

func (s *Store) RemoveWith(ctx context.Context, id string, ref Ref, p Provenance) error {
	if err := s.prepareWrite(ctx, ref, &p); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	replay, err := eventKeyUsed(ctx, tx, p.IdempotencyKey)
	if err != nil {
		return storeError(err)
	}
	if replay {
		return storeError(tx.Commit())
	}
	if err := requireCollection(ctx, tx, id); err != nil {
		return storeError(err)
	}
	removed, err := deleteMembership(ctx, tx, id, ref)
	if err != nil {
		return storeError(err)
	}
	if removed {
		if err := recordEvent(ctx, tx, id, ref, ActionRemove, p, s.stamp()); err != nil {
			return storeError(err)
		}
	}
	return storeError(tx.Commit())
}

// Move files a reference into the destination and detaches the source edge in
// one writer transaction so a refused cycle cannot leave a half-applied move.
func (s *Store) Move(ctx context.Context, fromID, toID string, ref Ref, p Provenance) error {
	if err := s.prepareWrite(ctx, ref, &p); err != nil {
		return err
	}
	if fromID == toID {
		return s.requireExisting(ctx, fromID)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	replay, err := eventKeyUsed(ctx, tx, p.IdempotencyKey)
	if err != nil {
		return storeError(err)
	}
	if replay {
		return storeError(tx.Commit())
	}
	if err := requireCollection(ctx, tx, fromID); err != nil {
		return storeError(err)
	}
	if _, err := insertMembership(ctx, tx, toID, ref); err != nil {
		return storeError(err)
	}
	if _, err := deleteMembership(ctx, tx, fromID, ref); err != nil {
		return storeError(err)
	}
	at := s.stamp()
	if err := recordEvent(ctx, tx, toID, ref, ActionAdd, p, at); err != nil {
		return storeError(err)
	}
	remove := p
	remove.IdempotencyKey = ""
	if err := recordEvent(ctx, tx, fromID, ref, ActionRemove, remove, at); err != nil {
		return storeError(err)
	}
	return storeError(tx.Commit())
}

func (s *Store) WhyHere(ctx context.Context, id string, ref Ref) (MembershipEvent, error) {
	if err := ref.Validate(); err != nil {
		return MembershipEvent{}, storeError(err)
	}
	ready, err := s.readyForRead(ctx)
	if err != nil {
		return MembershipEvent{}, err
	}
	if !ready || s.version < 2 {
		return MembershipEvent{}, storeError(ErrNotFound)
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+eventColumns+` FROM membership_events
 WHERE collection_id=? AND kind=? AND ref_id=? AND session_id=? ORDER BY seq DESC LIMIT 1`,
		id, ref.Kind, ref.ID, ref.SessionID)
	ev, err := scanEvent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return MembershipEvent{}, storeError(ErrNotFound)
	}
	return ev, storeError(err)
}

func (s *Store) Events(ctx context.Context, id string, ref Ref) ([]MembershipEvent, error) {
	if err := ref.Validate(); err != nil {
		return nil, storeError(err)
	}
	ready, err := s.readyForRead(ctx)
	if err != nil {
		return nil, err
	}
	if !ready {
		return nil, storeError(ErrNotFound)
	}
	if err := s.requireExisting(ctx, id); err != nil {
		return nil, err
	}
	if s.version < 2 {
		return make([]MembershipEvent, 0), nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+eventColumns+` FROM membership_events
 WHERE collection_id=? AND kind=? AND ref_id=? AND session_id=? ORDER BY seq`,
		id, ref.Kind, ref.ID, ref.SessionID)
	if err != nil {
		return nil, storeError(err)
	}
	defer rows.Close()
	result := make([]MembershipEvent, 0)
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, storeError(err)
		}
		result = append(result, ev)
	}
	return result, storeError(rows.Err())
}

func (s *Store) prepareWrite(ctx context.Context, ref Ref, p *Provenance) error {
	if err := ref.Validate(); err != nil {
		return storeError(err)
	}
	prepared, err := prepareProvenance(*p)
	if err != nil {
		return storeError(err)
	}
	*p = prepared
	return s.writeReady(ctx)
}

func (s *Store) requireExisting(ctx context.Context, id string) error {
	var found int
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM collections WHERE id=?", id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return storeError(ErrNotFound)
	}
	return storeError(err)
}

func (s *Store) stamp() string {
	return s.now().UTC().Format(time.RFC3339)
}

func insertMembership(ctx context.Context, tx *sql.Tx, id string, ref Ref) (bool, error) {
	if err := requireCollection(ctx, tx, id); err != nil {
		return false, err
	}
	var target any
	if ref.Kind == CollectionKind {
		if err := requireCollection(ctx, tx, ref.ID); err != nil {
			return false, err
		}
		if err := refuseCycle(ctx, tx, id, ref.ID); err != nil {
			return false, err
		}
		target = ref.ID
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO memberships(collection_id,kind,ref_id,session_id,target_collection)
 VALUES (?,?,?,?,?) ON CONFLICT(collection_id,kind,ref_id,session_id) DO NOTHING`, id, ref.Kind, ref.ID, ref.SessionID, target)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

func deleteMembership(ctx context.Context, tx *sql.Tx, id string, ref Ref) (bool, error) {
	result, err := tx.ExecContext(ctx, "DELETE FROM memberships WHERE collection_id=? AND kind=? AND ref_id=? AND session_id=?",
		id, ref.Kind, ref.ID, ref.SessionID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

func refuseCycle(ctx context.Context, tx *sql.Tx, parent, child string) error {
	// THE CYCLE CHECK AND INSERT SHARE THE WRITER TRANSACTION. Two
	// processes cannot both approve opposite edges against an old snapshot.
	var cycle bool
	err := tx.QueryRowContext(ctx, `WITH RECURSIVE descendants(id) AS (
 VALUES (?) UNION
 SELECT m.ref_id FROM memberships m JOIN descendants d ON m.collection_id=d.id WHERE m.kind='collection'
) SELECT EXISTS(SELECT 1 FROM descendants WHERE id=?)`, child, parent).Scan(&cycle)
	if err != nil {
		return err
	}
	if cycle {
		return ErrCycle
	}
	return nil
}

func eventKeyUsed(ctx context.Context, tx *sql.Tx, key string) (bool, error) {
	if key == "" {
		return false, nil
	}
	var found int
	err := tx.QueryRowContext(ctx, "SELECT 1 FROM membership_events WHERE idempotency_key=? LIMIT 1", key).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func recordEvent(ctx context.Context, tx *sql.Tx, id string, ref Ref, action string, p Provenance, at string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO membership_events(`+eventColumns+`)
 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		id, ref.Kind, ref.ID, ref.SessionID, action, p.Origin, p.Reason, p.Actor, p.Evidence, at, p.IdempotencyKey)
	return err
}

type eventScanner interface {
	Scan(dest ...any) error
}

func scanEvent(row eventScanner) (MembershipEvent, error) {
	var ev MembershipEvent
	err := row.Scan(&ev.CollectionID, &ev.Kind, &ev.RefID, &ev.SessionID, &ev.Action, &ev.Origin, &ev.Reason, &ev.Actor, &ev.Evidence, &ev.At, &ev.IdempotencyKey)
	return ev, err
}
