package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	replay, err := replaySameOp(ctx, tx, p.IdempotencyKey, MembershipEvent{
		CollectionID: id, Kind: ref.Kind, RefID: ref.ID, SessionID: ref.SessionID, Action: ActionAdd,
	}, "")
	if err != nil {
		return storeError(err)
	}
	if replay {
		return storeError(tx.Commit())
	}
	if err := matchRevision(ctx, tx, id, p.ExpectedRevision); err != nil {
		return storeError(err)
	}
	added, err := insertMembership(ctx, tx, id, ref)
	if err != nil {
		return storeError(err)
	}
	if added {
		at := s.stamp()
		if err := recordEvent(ctx, tx, id, ref, ActionAdd, p, at); err != nil {
			return storeError(err)
		}
		if err := bumpTouched(ctx, tx, at, id); err != nil {
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
	replay, err := replaySameOp(ctx, tx, p.IdempotencyKey, MembershipEvent{
		CollectionID: id, Kind: ref.Kind, RefID: ref.ID, SessionID: ref.SessionID, Action: ActionRemove,
	}, "")
	if err != nil {
		return storeError(err)
	}
	if replay {
		return storeError(tx.Commit())
	}
	if err := matchRevision(ctx, tx, id, p.ExpectedRevision); err != nil {
		return storeError(err)
	}
	removed, err := deleteMembership(ctx, tx, id, ref)
	if err != nil {
		return storeError(err)
	}
	if removed {
		at := s.stamp()
		if err := recordEvent(ctx, tx, id, ref, ActionRemove, p, at); err != nil {
			return storeError(err)
		}
		if err := bumpTouched(ctx, tx, at, id); err != nil {
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
	replay, err := replaySameOp(ctx, tx, p.IdempotencyKey, MembershipEvent{
		CollectionID: toID, Kind: ref.Kind, RefID: ref.ID, SessionID: ref.SessionID, Action: ActionAdd,
	}, fromID)
	if err != nil {
		return storeError(err)
	}
	if replay {
		return storeError(tx.Commit())
	}
	// THE EXPECTED REVISION IS READ INSIDE THE WRITER TRANSACTION. A check
	// against a snapshot taken before Begin would race a peer's commit, and a
	// mismatch after we had already inserted would still have to roll the edge
	// back. Asking here means a stale ExpectedFrom or ExpectedTo never writes,
	// so one mismatch leaves neither membership nor event behind.
	if err := matchRevision(ctx, tx, fromID, p.ExpectedFrom); err != nil {
		return storeError(err)
	}
	if err := matchRevision(ctx, tx, toID, p.ExpectedTo); err != nil {
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
	if err := bumpTouched(ctx, tx, at, fromID, toID); err != nil {
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

// replaySameOp is the idempotency gate. A second call with the same key is a
// no-op only when it is the same collection, ref, and action (and, for Move,
// the same source). Any other use of that key is refused rather than applied.
func replaySameOp(ctx context.Context, tx *sql.Tx, key string, want MembershipEvent, moveFrom string) (bool, error) {
	if key == "" {
		return false, nil
	}
	ev, err := lookupKeyedEvent(ctx, tx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if ev.CollectionID != want.CollectionID || ev.Kind != want.Kind || ev.RefID != want.RefID || ev.SessionID != want.SessionID || ev.Action != want.Action {
		return false, fmt.Errorf("%w: idempotency key already used for a different operation", ErrInvalid)
	}
	if want.Action != ActionAdd {
		return true, nil
	}
	moved, err := hasRemoveAt(ctx, tx, Ref{Kind: ev.Kind, ID: ev.RefID, SessionID: ev.SessionID}, ev.At)
	if err != nil {
		return false, err
	}
	if moveFrom == "" {
		if moved {
			return false, fmt.Errorf("%w: idempotency key already used for a different operation", ErrInvalid)
		}
		return true, nil
	}
	fromMatched, err := hasRemoveFromAt(ctx, tx, moveFrom, Ref{Kind: ev.Kind, ID: ev.RefID, SessionID: ev.SessionID}, ev.At)
	if err != nil {
		return false, err
	}
	if !fromMatched {
		return false, fmt.Errorf("%w: idempotency key already used for a different operation", ErrInvalid)
	}
	return true, nil
}

func lookupKeyedEvent(ctx context.Context, tx *sql.Tx, key string) (MembershipEvent, error) {
	row := tx.QueryRowContext(ctx, `SELECT `+eventColumns+` FROM membership_events WHERE idempotency_key=? LIMIT 1`, key)
	return scanEvent(row)
}

func hasRemoveAt(ctx context.Context, tx *sql.Tx, ref Ref, at string) (bool, error) {
	var found int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM membership_events WHERE kind=? AND ref_id=? AND session_id=? AND action=? AND at=? LIMIT 1`,
		ref.Kind, ref.ID, ref.SessionID, ActionRemove, at).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func hasRemoveFromAt(ctx context.Context, tx *sql.Tx, fromID string, ref Ref, at string) (bool, error) {
	var found int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM membership_events WHERE collection_id=? AND kind=? AND ref_id=? AND session_id=? AND action=? AND at=? LIMIT 1`,
		fromID, ref.Kind, ref.ID, ref.SessionID, ActionRemove, at).Scan(&found)
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

func matchRevision(ctx context.Context, tx *sql.Tx, id string, expected int) error {
	var got int
	err := tx.QueryRowContext(ctx, "SELECT revision FROM collections WHERE id=?", id).Scan(&got)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if expected != 0 && got != expected {
		return ErrConflict
	}
	return nil
}

func bumpTouched(ctx context.Context, tx *sql.Tx, now string, ids ...string) error {
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, "UPDATE collections SET revision=revision+1, updated_at=? WHERE id=?", now, id); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, "UPDATE root_state SET revision=revision+1, updated_at=? WHERE id=1", now)
	return err
}
