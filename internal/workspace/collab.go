package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// PutParticipant records who is in a discussion. ACTOR IDS ARE MINTED HERE:
// a supplied actor_id is ignored so a model cannot stamp itself as a person
// or as another chat. Re-inviting the same source chat is a no-op.
func (s *Store) PutParticipant(ctx context.Context, p Participant) (Participant, error) {
	p, err := prepareParticipant(p)
	if err != nil {
		return Participant{}, storeError(err)
	}
	if err := s.ensureSchema(ctx); err != nil {
		return Participant{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Participant{}, storeError(err)
	}
	defer tx.Rollback()
	stored, err := storeParticipant(ctx, tx, p, s.stamp())
	if err != nil {
		return Participant{}, storeError(err)
	}
	return stored, storeError(tx.Commit())
}

func prepareParticipant(p Participant) (Participant, error) {
	if !validText(p.DiscussionID, 4096) {
		return Participant{}, fmt.Errorf("%w: participant needs a discussion id", ErrInvalid)
	}
	if p.Kind == "" {
		p.Kind = ActorKindChat
	}
	if !validActorKind(p.Kind) {
		return Participant{}, fmt.Errorf("%w: unknown actor kind %q", ErrInvalid, p.Kind)
	}
	if p.Status == "" {
		p.Status = ParticipantActive
	}
	if !validParticipantStatus(p.Status) || !validScopeKind(p.ScopeKind) {
		return Participant{}, fmt.Errorf("%w: unknown participant status or scope", ErrInvalid)
	}
	if !validOptionalText(p.ID, 4096) || !validOptionalText(p.Role, 256) || !validOptionalText(p.SourceChatID, 4096) {
		return Participant{}, fmt.Errorf("%w: participant field is too long", ErrInvalid)
	}
	if !validOptionalText(p.FolderID, 4096) || !validOptionalText(p.SnapshotJSON, 64*1024) {
		return Participant{}, fmt.Errorf("%w: participant scope is too long", ErrInvalid)
	}
	prepared, err := prepareCollabProvenance(Provenance{Origin: p.Origin, Actor: p.Actor})
	if err != nil {
		return Participant{}, err
	}
	p.Origin, p.Actor = prepared.Origin, prepared.Actor
	p.ActorID = ""
	return p, nil
}

func storeParticipant(ctx context.Context, tx *sql.Tx, p Participant, now string) (Participant, error) {
	if p.ID != "" {
		if existing, err := loadParticipant(ctx, tx, p.ID); err == nil {
			return updateParticipant(ctx, tx, existing, p, now)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return Participant{}, err
		}
	}
	if p.SourceChatID != "" {
		if existing, err := lookupSource(ctx, tx, p.DiscussionID, p.SourceChatID); err == nil {
			return existing, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return Participant{}, err
		}
	}
	if err := mintParticipantIDs(&p); err != nil {
		return Participant{}, err
	}
	p.CreatedAt, p.UpdatedAt = now, now
	if err := insertParticipant(ctx, tx, p); err != nil {
		if p.SourceChatID != "" && isUniqueConstraint(err) {
			return lookupSource(ctx, tx, p.DiscussionID, p.SourceChatID)
		}
		return Participant{}, err
	}
	return p, nil
}

func mintParticipantIDs(p *Participant) error {
	if p.ID == "" {
		id, err := mintID()
		if err != nil {
			return err
		}
		p.ID = id
	}
	id, err := mintID()
	if err != nil {
		return err
	}
	p.ActorID = id
	return nil
}

func updateParticipant(ctx context.Context, tx *sql.Tx, existing, patch Participant, now string) (Participant, error) {
	existing.Role, existing.Status = patch.Role, patch.Status
	existing.ScopeKind, existing.FolderID, existing.SnapshotJSON = patch.ScopeKind, patch.FolderID, patch.SnapshotJSON
	existing.UpdatedAt = now
	_, err := tx.ExecContext(ctx, `UPDATE participants SET role=?, status=?, scope_kind=?, folder_id=?, snapshot_json=?, updated_at=? WHERE id=?`,
		existing.Role, existing.Status, existing.ScopeKind, existing.FolderID, existing.SnapshotJSON, existing.UpdatedAt, existing.ID)
	return existing, err
}

// SetParticipantStatus pauses or archives a row without minting a second actor.
func (s *Store) SetParticipantStatus(ctx context.Context, id, status string) error {
	if strings.TrimSpace(id) == "" || !validParticipantStatus(status) {
		return storeError(fmt.Errorf("%w: participant status is unknown", ErrInvalid))
	}
	if err := s.writeReady(ctx); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE participants SET status=?, updated_at=? WHERE id=?`, status, s.stamp(), id)
	if err != nil {
		return storeError(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return storeError(err)
	}
	if n == 0 {
		return storeError(ErrNotFound)
	}
	return nil
}

// ListParticipants is a read. Listing never migrates, so a v1–v3 file returns
// nothing rather than creating the v4 tables.
func (s *Store) ListParticipants(ctx context.Context, discussionID string) ([]Participant, error) {
	ready, err := s.v4Ready(ctx)
	if err != nil || !ready {
		return make([]Participant, 0), err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+participantColumns+` FROM participants WHERE discussion_id=? ORDER BY seq`, discussionID)
	if err != nil {
		return nil, storeError(err)
	}
	defer rows.Close()
	result := make([]Participant, 0)
	for rows.Next() {
		p, err := scanParticipant(rows)
		if err != nil {
			return nil, storeError(err)
		}
		result = append(result, p)
	}
	return result, storeError(rows.Err())
}

// PutDelivery queues one envelope. Empty ids are minted here; a supplied id is
// the router's mailbox deliveryID. Identical id or idempotency key is a no-op
// success so resume cannot double-insert.
func (s *Store) PutDelivery(ctx context.Context, d Delivery) (Delivery, error) {
	d, err := prepareDelivery(d)
	if err != nil {
		return Delivery{}, storeError(err)
	}
	if err := s.ensureSchema(ctx); err != nil {
		return Delivery{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Delivery{}, storeError(err)
	}
	defer tx.Rollback()
	stored, err := storeDelivery(ctx, tx, d, s.stamp())
	if err != nil {
		return Delivery{}, storeError(err)
	}
	return stored, storeError(tx.Commit())
}

func prepareDelivery(d Delivery) (Delivery, error) {
	if !validText(d.ToChatID, 4096) {
		return Delivery{}, fmt.Errorf("%w: delivery needs a recipient chat", ErrInvalid)
	}
	if d.Pattern == "" {
		d.Pattern = PatternDirect
	}
	if !validDeliveryPattern(d.Pattern) {
		return Delivery{}, fmt.Errorf("%w: unknown delivery pattern %q", ErrInvalid, d.Pattern)
	}
	if d.State == "" {
		d.State = DeliveryPending
	}
	if nextDeliveryState(d.State) == "" && d.State != DeliveryProcessed {
		return Delivery{}, fmt.Errorf("%w: unknown delivery state %q", ErrInvalid, d.State)
	}
	if !validOptionalText(d.ID, 4096) || !validOptionalText(d.CauseID, 4096) || !validOptionalText(d.IdempotencyKey, 4096) {
		return Delivery{}, fmt.Errorf("%w: delivery id is too long", ErrInvalid)
	}
	prepared, err := prepareCollabProvenance(Provenance{Origin: d.Origin})
	if err != nil {
		return Delivery{}, err
	}
	d.Origin = prepared.Origin
	return d, nil
}

func storeDelivery(ctx context.Context, tx *sql.Tx, d Delivery, now string) (Delivery, error) {
	if existing, ok, err := existingDelivery(ctx, tx, d); err != nil || ok {
		return existing, err
	}
	if err := mintDeliveryIDs(&d); err != nil {
		return Delivery{}, err
	}
	d.CreatedAt, d.UpdatedAt = now, now
	if err := insertDelivery(ctx, tx, d); err != nil {
		if existing, ok, lookupErr := existingDelivery(ctx, tx, d); lookupErr != nil || ok {
			return existing, lookupErr
		}
		return Delivery{}, err
	}
	return d, nil
}

func existingDelivery(ctx context.Context, tx *sql.Tx, d Delivery) (Delivery, bool, error) {
	if d.ID != "" {
		existing, err := loadDelivery(ctx, tx, d.ID)
		if err == nil {
			return existing, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Delivery{}, false, err
		}
	}
	if d.IdempotencyKey == "" {
		return Delivery{}, false, nil
	}
	existing, err := lookupIdempotent(ctx, tx, d.IdempotencyKey)
	if err == nil {
		return existing, true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return Delivery{}, false, nil
	}
	return Delivery{}, false, err
}

func mintDeliveryIDs(d *Delivery) error {
	if d.ID == "" {
		id, err := mintID()
		if err != nil {
			return err
		}
		d.ID = id
	}
	if d.CauseID != "" {
		return nil
	}
	cause, err := mintID()
	if err != nil {
		return err
	}
	d.CauseID = cause
	return nil
}

// AckDelivery advances one step along pending → accepted → recorded → processed.
// A skip refuses with the word "state". Same-state ack is a no-op success.
func (s *Store) AckDelivery(ctx context.Context, id, state string) (Delivery, error) {
	if strings.TrimSpace(id) == "" || !knownAckState(state) {
		return Delivery{}, storeError(fmt.Errorf("%w: delivery ack needs an id and a known state", ErrInvalid))
	}
	if err := s.writeReady(ctx); err != nil {
		return Delivery{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Delivery{}, storeError(err)
	}
	defer tx.Rollback()
	stored, err := ackDelivery(ctx, tx, id, state, s.stamp())
	if err != nil {
		return Delivery{}, storeError(err)
	}
	return stored, storeError(tx.Commit())
}

func ackDelivery(ctx context.Context, tx *sql.Tx, id, state, now string) (Delivery, error) {
	d, err := loadDelivery(ctx, tx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Delivery{}, ErrNotFound
	}
	if err != nil {
		return Delivery{}, err
	}
	if state == d.State {
		return d, nil
	}
	if nextDeliveryState(d.State) != state {
		return Delivery{}, skipStateError(d.State, state)
	}
	stampAck(&d, state, now)
	_, err = tx.ExecContext(ctx, `UPDATE deliveries SET state=?, accepted_at=?, recorded_at=?, processed_at=?, updated_at=? WHERE id=?`,
		d.State, d.AcceptedAt, d.RecordedAt, d.ProcessedAt, d.UpdatedAt, d.ID)
	return d, err
}

func stampAck(d *Delivery, state, now string) {
	d.State, d.UpdatedAt = state, now
	switch state {
	case DeliveryAccepted:
		d.AcceptedAt = now
	case DeliveryRecorded:
		d.RecordedAt = now
	case DeliveryProcessed:
		d.ProcessedAt = now
	}
}

func (s *Store) GetDelivery(ctx context.Context, id string) (Delivery, error) {
	ready, err := s.v4Ready(ctx)
	if err != nil || !ready {
		return Delivery{}, storeError(ErrNotFound)
	}
	d, err := scanDelivery(s.db.QueryRowContext(ctx, `SELECT `+deliveryColumns+` FROM deliveries WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Delivery{}, storeError(ErrNotFound)
	}
	return d, storeError(err)
}

// ListDeliveries is a read keyed by shared cause. Listing never migrates.
func (s *Store) ListDeliveries(ctx context.Context, causeID string) ([]Delivery, error) {
	return s.listDeliveries(ctx, `SELECT `+deliveryColumns+` FROM deliveries WHERE cause_id=? ORDER BY seq`, causeID)
}

// ListPendingDeliveries is the offline resume scan: waiting rows for one chat.
func (s *Store) ListPendingDeliveries(ctx context.Context, toChatID string) ([]Delivery, error) {
	return s.listDeliveries(ctx, `SELECT `+deliveryColumns+` FROM deliveries WHERE to_chat_id=? AND state=? ORDER BY seq`, toChatID, DeliveryPending)
}

// ListChatTraffic is the management chat's visible deliveries: outbound and
// inbound, any state. Listing never migrates. The TUI maps these onto
// request / reply / sent; it must not call ListPendingDeliveries for paint.
func (s *Store) ListChatTraffic(ctx context.Context, chatID string) ([]Delivery, error) {
	if strings.TrimSpace(chatID) == "" {
		return []Delivery{}, nil
	}
	return s.listDeliveries(ctx, `SELECT `+deliveryColumns+` FROM deliveries WHERE from_chat_id=? OR to_chat_id=? ORDER BY seq LIMIT 40`, chatID, chatID)
}

func (s *Store) listDeliveries(ctx context.Context, query string, args ...any) ([]Delivery, error) {
	ready, err := s.v4Ready(ctx)
	if err != nil || !ready {
		return make([]Delivery, 0), err
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storeError(err)
	}
	defer rows.Close()
	result := make([]Delivery, 0)
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, storeError(err)
		}
		result = append(result, d)
	}
	return result, storeError(rows.Err())
}

func (s *Store) v4Ready(ctx context.Context) (bool, error) {
	ready, err := s.readyForRead(ctx)
	if err != nil || !ready {
		return false, err
	}
	s.schemaMu.Lock()
	defer s.schemaMu.Unlock()
	return s.version >= 4, nil
}

func insertParticipant(ctx context.Context, tx *sql.Tx, p Participant) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO participants(`+participantColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.DiscussionID, p.ActorID, p.Kind, p.Role, p.SourceChatID, p.Status, p.ScopeKind,
		p.FolderID, p.SnapshotJSON, p.Origin, p.Actor, p.CreatedAt, p.UpdatedAt)
	return err
}

func loadParticipant(ctx context.Context, tx *sql.Tx, id string) (Participant, error) {
	return scanParticipant(tx.QueryRowContext(ctx, `SELECT `+participantColumns+` FROM participants WHERE id=?`, id))
}

func lookupSource(ctx context.Context, tx *sql.Tx, discussionID, sourceChatID string) (Participant, error) {
	return scanParticipant(tx.QueryRowContext(ctx, `SELECT `+participantColumns+` FROM participants
 WHERE discussion_id=? AND source_chat_id=? AND status=?`, discussionID, sourceChatID, ParticipantActive))
}

func scanParticipant(row eventScanner) (Participant, error) {
	var p Participant
	err := row.Scan(&p.ID, &p.DiscussionID, &p.ActorID, &p.Kind, &p.Role, &p.SourceChatID, &p.Status,
		&p.ScopeKind, &p.FolderID, &p.SnapshotJSON, &p.Origin, &p.Actor, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func insertDelivery(ctx context.Context, tx *sql.Tx, d Delivery) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO deliveries(`+deliveryColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.CauseID, d.FromChatID, d.ToChatID, d.Pattern, d.State, d.Body, d.Origin, d.ActorID,
		d.DiscussionID, d.IdempotencyKey, d.Attempt, d.CreatedAt, d.AcceptedAt, d.RecordedAt, d.ProcessedAt, d.UpdatedAt)
	return err
}

func loadDelivery(ctx context.Context, tx *sql.Tx, id string) (Delivery, error) {
	return scanDelivery(tx.QueryRowContext(ctx, `SELECT `+deliveryColumns+` FROM deliveries WHERE id=?`, id))
}

func lookupIdempotent(ctx context.Context, tx *sql.Tx, key string) (Delivery, error) {
	return scanDelivery(tx.QueryRowContext(ctx, `SELECT `+deliveryColumns+` FROM deliveries WHERE idempotency_key=? LIMIT 1`, key))
}

func scanDelivery(row eventScanner) (Delivery, error) {
	var d Delivery
	err := row.Scan(&d.ID, &d.CauseID, &d.FromChatID, &d.ToChatID, &d.Pattern, &d.State, &d.Body, &d.Origin,
		&d.ActorID, &d.DiscussionID, &d.IdempotencyKey, &d.Attempt, &d.CreatedAt, &d.AcceptedAt, &d.RecordedAt,
		&d.ProcessedAt, &d.UpdatedAt)
	return d, err
}
