package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// BatchOp is one membership mutation inside [Store.ApplyBatch]. Kind is add,
// remove, move, or create-folder — the same strings the organizer plan uses.
type BatchOp struct {
	Kind                       string
	CollectionID, FromID, ToID string
	Ref                        Ref
	FolderName                 string
	ParentIDs                  []string
	Provenance                 Provenance
}

// ApplyBatch applies every membership op and records the proposal in ONE
// writer transaction. A later op's conflict rolls the earlier ops back, so a
// mid-plan refusal cannot leave a half-applied action-set.
func (s *Store) ApplyBatch(ctx context.Context, ops []BatchOp, proposal Proposal) (Proposal, []MembershipEvent, error) {
	if err := s.writeReady(ctx); err != nil {
		return Proposal{}, nil, err
	}
	prepared, err := prepareBatchOps(ops)
	if err != nil {
		return Proposal{}, nil, storeError(err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Proposal{}, nil, storeError(err)
	}
	defer tx.Rollback()
	events, err := applyBatchOps(ctx, tx, s.stamp(), prepared)
	if err != nil {
		return Proposal{}, nil, storeError(err)
	}
	stored, err := putProposalInTx(ctx, tx, proposal, s.stamp())
	if err != nil {
		return Proposal{}, nil, storeError(err)
	}
	if err := tx.Commit(); err != nil {
		return Proposal{}, nil, storeError(err)
	}
	return stored, events, nil
}

func prepareBatchOps(ops []BatchOp) ([]BatchOp, error) {
	out := make([]BatchOp, len(ops))
	for i, op := range ops {
		prepared, err := prepareProvenance(op.Provenance)
		if err != nil {
			return nil, err
		}
		op.Provenance = prepared
		if op.Kind != PlanCreateFolderKind && op.Kind != "create-folder" {
			if err := op.Ref.Validate(); err != nil {
				return nil, err
			}
		}
		out[i] = op
	}
	return out, nil
}

const (
	ActionMove           = "move"
	PlanCreateFolderKind = "create-folder"
)

func applyBatchOps(ctx context.Context, tx *sql.Tx, at string, ops []BatchOp) ([]MembershipEvent, error) {
	aliases := map[string]string{}
	events := make([]MembershipEvent, 0)
	for _, op := range ops {
		got, err := applyOneBatchOp(ctx, tx, at, rewriteBatchOp(op, aliases), aliases)
		if err != nil {
			return nil, err
		}
		events = append(events, got...)
	}
	return events, nil
}

func applyOneBatchOp(ctx context.Context, tx *sql.Tx, at string, op BatchOp, aliases map[string]string) ([]MembershipEvent, error) {
	switch op.Kind {
	case ActionAdd:
		return addInTx(ctx, tx, at, op.CollectionID, op.Ref, op.Provenance)
	case ActionRemove:
		return removeInTx(ctx, tx, at, op.CollectionID, op.Ref, op.Provenance)
	case ActionMove:
		return moveInTx(ctx, tx, at, op.FromID, op.ToID, op.Ref, op.Provenance)
	case PlanCreateFolderKind:
		return createFolderInTx(ctx, tx, at, op, aliases)
	default:
		return nil, fmt.Errorf("%w: unknown batch kind %q", ErrInvalid, op.Kind)
	}
}

func rewriteBatchOp(op BatchOp, aliases map[string]string) BatchOp {
	op.CollectionID = resolveAlias(op.CollectionID, aliases)
	op.FromID = resolveAlias(op.FromID, aliases)
	op.ToID = resolveAlias(op.ToID, aliases)
	if op.Ref.Kind == CollectionKind {
		op.Ref.ID = resolveAlias(op.Ref.ID, aliases)
	}
	for i, parent := range op.ParentIDs {
		op.ParentIDs[i] = resolveAlias(parent, aliases)
	}
	return op
}

func resolveAlias(id string, aliases map[string]string) string {
	if resolved, ok := aliases[id]; ok {
		return resolved
	}
	return id
}

func addInTx(ctx context.Context, tx *sql.Tx, at, id string, ref Ref, p Provenance) ([]MembershipEvent, error) {
	replay, err := replaySameOp(ctx, tx, p.IdempotencyKey, MembershipEvent{
		CollectionID: id, Kind: ref.Kind, RefID: ref.ID, SessionID: ref.SessionID, Action: ActionAdd,
	}, "")
	if err != nil {
		return nil, err
	}
	if replay {
		return whyHereTx(ctx, tx, id, ref)
	}
	if err := matchRevision(ctx, tx, id, p.ExpectedRevision); err != nil {
		return nil, err
	}
	added, err := insertMembership(ctx, tx, id, ref)
	if err != nil {
		return nil, err
	}
	if added {
		if err := recordEvent(ctx, tx, id, ref, ActionAdd, p, at); err != nil {
			return nil, err
		}
		if err := bumpTouched(ctx, tx, at, id); err != nil {
			return nil, err
		}
	}
	return whyHereTx(ctx, tx, id, ref)
}

func removeInTx(ctx context.Context, tx *sql.Tx, at, id string, ref Ref, p Provenance) ([]MembershipEvent, error) {
	replay, err := replaySameOp(ctx, tx, p.IdempotencyKey, MembershipEvent{
		CollectionID: id, Kind: ref.Kind, RefID: ref.ID, SessionID: ref.SessionID, Action: ActionRemove,
	}, "")
	if err != nil {
		return nil, err
	}
	if replay {
		return whyHereTx(ctx, tx, id, ref)
	}
	if err := matchRevision(ctx, tx, id, p.ExpectedRevision); err != nil {
		return nil, err
	}
	removed, err := deleteMembership(ctx, tx, id, ref)
	if err != nil {
		return nil, err
	}
	if removed {
		if err := recordEvent(ctx, tx, id, ref, ActionRemove, p, at); err != nil {
			return nil, err
		}
		if err := bumpTouched(ctx, tx, at, id); err != nil {
			return nil, err
		}
	}
	return whyHereTx(ctx, tx, id, ref)
}

func moveInTx(ctx context.Context, tx *sql.Tx, at, fromID, toID string, ref Ref, p Provenance) ([]MembershipEvent, error) {
	if fromID == toID {
		if err := requireCollection(ctx, tx, fromID); err != nil {
			return nil, err
		}
		return whyHereTx(ctx, tx, toID, ref)
	}
	replay, err := replaySameOp(ctx, tx, p.IdempotencyKey, MembershipEvent{
		CollectionID: toID, Kind: ref.Kind, RefID: ref.ID, SessionID: ref.SessionID, Action: ActionAdd,
	}, fromID)
	if err != nil {
		return nil, err
	}
	if replay {
		return whyHereTx(ctx, tx, toID, ref)
	}
	if err := matchRevision(ctx, tx, fromID, p.ExpectedFrom); err != nil {
		return nil, err
	}
	if err := matchRevision(ctx, tx, toID, p.ExpectedTo); err != nil {
		return nil, err
	}
	if _, err := insertMembership(ctx, tx, toID, ref); err != nil {
		return nil, err
	}
	if _, err := deleteMembership(ctx, tx, fromID, ref); err != nil {
		return nil, err
	}
	if err := recordEvent(ctx, tx, toID, ref, ActionAdd, p, at); err != nil {
		return nil, err
	}
	remove := p
	remove.IdempotencyKey = ""
	if err := recordEvent(ctx, tx, fromID, ref, ActionRemove, remove, at); err != nil {
		return nil, err
	}
	if err := bumpTouched(ctx, tx, at, fromID, toID); err != nil {
		return nil, err
	}
	return whyHereTx(ctx, tx, toID, ref)
}

func createFolderInTx(ctx context.Context, tx *sql.Tx, at string, op BatchOp, aliases map[string]string) ([]MembershipEvent, error) {
	created, err := insertCollection(ctx, tx, op.FolderName, at)
	if err != nil {
		return nil, err
	}
	aliases[op.FolderName] = created.ID
	if op.CollectionID != "" {
		aliases[op.CollectionID] = created.ID
	}
	events := make([]MembershipEvent, 0, len(op.ParentIDs))
	child := Ref{Kind: CollectionKind, ID: created.ID}
	for _, parent := range op.ParentIDs {
		parent = resolveAlias(parent, aliases)
		got, err := addInTx(ctx, tx, at, parent, child, op.Provenance)
		if err != nil {
			return nil, err
		}
		events = append(events, got...)
	}
	return events, nil
}

func insertCollection(ctx context.Context, tx *sql.Tx, name, at string) (Collection, error) {
	if err := ValidateName(name); err != nil {
		return Collection{}, err
	}
	id, err := mintID()
	if err != nil {
		return Collection{}, err
	}
	c := Collection{
		ID: id, Name: name, Lifecycle: LifecycleActive, Revision: 1,
		CreatedAt: at, UpdatedAt: at,
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO collections(id,name,purpose,lifecycle,revision,created_at,updated_at)
 VALUES (?,?,?,?,?,?,?)`, c.ID, c.Name, c.Purpose, c.Lifecycle, c.Revision, c.CreatedAt, c.UpdatedAt)
	if err != nil {
		return Collection{}, err
	}
	if err := bumpTouched(ctx, tx, at); err != nil {
		return Collection{}, err
	}
	return c, nil
}

func whyHereTx(ctx context.Context, tx *sql.Tx, id string, ref Ref) ([]MembershipEvent, error) {
	row := tx.QueryRowContext(ctx, `SELECT `+eventColumns+` FROM membership_events
 WHERE collection_id=? AND kind=? AND ref_id=? AND session_id=? ORDER BY seq DESC LIMIT 1`,
		id, ref.Kind, ref.ID, ref.SessionID)
	ev, err := scanEvent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return []MembershipEvent{}, nil
	}
	if err != nil {
		return nil, err
	}
	return []MembershipEvent{ev}, nil
}

func putProposalInTx(ctx context.Context, tx *sql.Tx, p Proposal, at string) (Proposal, error) {
	if p.IdempotencyKey != "" {
		existing, err := lookupProposal(ctx, tx, p.IdempotencyKey)
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Proposal{}, err
		}
	}
	if p.ID == "" {
		id, err := mintID()
		if err != nil {
			return Proposal{}, err
		}
		p.ID = id
	}
	p.CreatedAt = at
	if err := insertProposal(ctx, tx, p); err != nil {
		if p.IdempotencyKey != "" && isUniqueConstraint(err) {
			existing, lookupErr := lookupProposal(ctx, tx, p.IdempotencyKey)
			if lookupErr == nil {
				return existing, nil
			}
		}
		return Proposal{}, err
	}
	return p, nil
}

// RemoveAndSuppress detaches the edge and, when evidenceHash is set, records
// that hash in the same writer transaction. A suppress refusal cannot report
// a successful remove of an organizer placement.
func (s *Store) RemoveAndSuppress(ctx context.Context, id string, ref Ref, evidenceHash string, p Provenance) error {
	if err := s.prepareWrite(ctx, ref, &p); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	if _, err := removeInTx(ctx, tx, s.stamp(), id, ref, p); err != nil {
		return storeError(err)
	}
	if evidenceHash != "" {
		if err := suppressInTx(ctx, tx, id, ref, evidenceHash, p, s.stamp()); err != nil {
			return storeError(err)
		}
	}
	return storeError(tx.Commit())
}
