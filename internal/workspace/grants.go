package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// PutGrant records a delegation. IDs ARE MINTED HERE: a supplied grant_id
// that does not already exist is ignored so a model cannot pick the identity.
// A second write that adds classes or enlarges scope refuses with "expand".
func (s *Store) PutGrant(ctx context.Context, g Grant) (Grant, error) {
	g, err := prepareGrant(g)
	if err != nil {
		return Grant{}, storeError(err)
	}
	if err := s.ensureSchema(ctx); err != nil {
		return Grant{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Grant{}, storeError(err)
	}
	defer tx.Rollback()
	stored, err := storeGrant(ctx, tx, g, s.stamp())
	if err != nil {
		return Grant{}, storeError(err)
	}
	return stored, storeError(tx.Commit())
}

func prepareGrant(g Grant) (Grant, error) {
	if !validText(g.CoordinatorID, 4096) {
		return Grant{}, fmt.Errorf("%w: grant needs a coordinator", ErrInvalid)
	}
	if !validOptionalText(g.Goal, 64*1024) || !validOptionalText(g.FolderID, 4096) || !validOptionalText(g.Issuer, 4096) {
		return Grant{}, fmt.Errorf("%w: grant field is too long", ErrInvalid)
	}
	if g.Status == "" {
		g.Status = GrantActive
	}
	if !validGrantStatus(g.Status) || !validScopeKind(g.ScopeKind) {
		return Grant{}, fmt.Errorf("%w: unknown grant status or scope", ErrInvalid)
	}
	if g.BudgetUSD < 0 {
		return Grant{}, fmt.Errorf("%w: grant budget is negative", ErrInvalid)
	}
	prepared, err := prepareCollabProvenance(Provenance{Origin: g.Origin, Actor: g.Actor})
	if err != nil {
		return Grant{}, err
	}
	g.Origin, g.Actor = prepared.Origin, prepared.Actor
	actions, err := canonicalActions(g.ActionJSON)
	if err != nil {
		return Grant{}, err
	}
	snap, err := canonicalSnapshot(g.SnapshotJSON)
	if err != nil {
		return Grant{}, err
	}
	g.ActionJSON, g.SnapshotJSON = actions, snap
	if g.Revision == 0 {
		g.Revision = 1
	}
	return g, nil
}

func storeGrant(ctx context.Context, tx *sql.Tx, g Grant, now string) (Grant, error) {
	if existing, ok, err := existingGrant(ctx, tx, g.ID); err != nil || ok {
		if err != nil || !ok {
			return existing, err
		}
		return updateGrant(ctx, tx, existing, g, now)
	}
	if err := refuseIssuerExpand(ctx, tx, g); err != nil {
		return Grant{}, err
	}
	if err := mintGrantID(&g); err != nil {
		return Grant{}, err
	}
	g.CreatedAt, g.UpdatedAt = now, now
	if err := insertGrant(ctx, tx, g); err != nil {
		return Grant{}, err
	}
	return g, bumpGrantRoot(ctx, tx, now, g.FolderID)
}

func existingGrant(ctx context.Context, tx *sql.Tx, id string) (Grant, bool, error) {
	if id == "" {
		return Grant{}, false, nil
	}
	existing, err := loadGrant(ctx, tx, id)
	if err == nil {
		return existing, true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return Grant{}, false, nil
	}
	return Grant{}, false, err
}

func refuseIssuerExpand(ctx context.Context, tx *sql.Tx, g Grant) error {
	if g.Issuer == "" {
		return nil
	}
	issuer, err := loadGrant(ctx, tx, g.Issuer)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if grantExpands(issuer, g) {
		return grantExpandError()
	}
	return nil
}

func mintGrantID(g *Grant) error {
	id, err := mintID()
	if err != nil {
		return err
	}
	g.ID = id
	return nil
}

func updateGrant(ctx context.Context, tx *sql.Tx, existing, patch Grant, now string) (Grant, error) {
	if existing.Status != GrantActive {
		return Grant{}, fmt.Errorf("%w: revoked grant cannot expand", ErrInvalid)
	}
	if grantExpands(existing, patch) {
		return Grant{}, grantExpandError()
	}
	existing.Goal, existing.ScopeKind, existing.FolderID = patch.Goal, patch.ScopeKind, patch.FolderID
	existing.SnapshotJSON, existing.ActionJSON, existing.BudgetUSD = patch.SnapshotJSON, patch.ActionJSON, patch.BudgetUSD
	existing.Revision++
	existing.UpdatedAt = now
	_, err := tx.ExecContext(ctx, `UPDATE grants SET goal=?, scope_kind=?, folder_id=?, snapshot_json=?, action_json=?, budget_usd=?, revision=?, updated_at=? WHERE id=?`,
		existing.Goal, existing.ScopeKind, existing.FolderID, existing.SnapshotJSON, existing.ActionJSON, existing.BudgetUSD, existing.Revision, existing.UpdatedAt, existing.ID)
	if err != nil {
		return Grant{}, err
	}
	return existing, bumpGrantRoot(ctx, tx, now, existing.FolderID)
}

func bumpGrantRoot(ctx context.Context, tx *sql.Tx, now, folderID string) error {
	ids := []string{}
	if folderID != "" {
		ids = append(ids, folderID)
	}
	return bumpTouched(ctx, tx, now, ids...)
}

// GetGrant is a read. Listing never migrates, so a v1–v4 file is not found.
func (s *Store) GetGrant(ctx context.Context, id string) (Grant, error) {
	ready, err := s.v5Ready(ctx)
	if err != nil || !ready {
		return Grant{}, storeError(ErrNotFound)
	}
	g, err := scanGrant(s.db.QueryRowContext(ctx, `SELECT `+grantColumns+` FROM grants WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Grant{}, storeError(ErrNotFound)
	}
	return g, storeError(err)
}

// ListGrants is a read keyed by coordinator. Listing never migrates.
func (s *Store) ListGrants(ctx context.Context, coordinatorID string) ([]Grant, error) {
	ready, err := s.v5Ready(ctx)
	if err != nil || !ready {
		return make([]Grant, 0), err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+grantColumns+` FROM grants WHERE coordinator_id=? ORDER BY seq`, coordinatorID)
	if err != nil {
		return nil, storeError(err)
	}
	defer rows.Close()
	result := make([]Grant, 0)
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			return nil, storeError(err)
		}
		result = append(result, g)
	}
	return result, storeError(rows.Err())
}

// RevokeGrant writes revoked, stores RevocationRevision, and increments
// Revision in the same writer transaction. expectedRevision 0 skips the CAS.
func (s *Store) RevokeGrant(ctx context.Context, id string, expectedRevision int) (Grant, error) {
	if strings.TrimSpace(id) == "" {
		return Grant{}, storeError(fmt.Errorf("%w: grant revoke needs an id", ErrInvalid))
	}
	if err := s.writeReady(ctx); err != nil {
		return Grant{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Grant{}, storeError(err)
	}
	defer tx.Rollback()
	stored, err := revokeGrant(ctx, tx, id, expectedRevision, s.stamp())
	if err != nil {
		return Grant{}, storeError(err)
	}
	return stored, storeError(tx.Commit())
}

func revokeGrant(ctx context.Context, tx *sql.Tx, id string, expectedRevision int, now string) (Grant, error) {
	g, err := loadGrant(ctx, tx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Grant{}, ErrNotFound
	}
	if err != nil {
		return Grant{}, err
	}
	if expectedRevision != 0 && g.Revision != expectedRevision {
		return Grant{}, ErrConflict
	}
	if g.Status == GrantRevoked {
		return g, nil
	}
	g.Status = GrantRevoked
	g.RevocationRevision = g.Revision
	g.Revision++
	g.UpdatedAt = now
	_, err = tx.ExecContext(ctx, `UPDATE grants SET status=?, revocation_revision=?, revision=?, updated_at=? WHERE id=?`,
		g.Status, g.RevocationRevision, g.Revision, g.UpdatedAt, g.ID)
	if err != nil {
		return Grant{}, err
	}
	return g, bumpGrantRoot(ctx, tx, now, g.FolderID)
}

func insertGrant(ctx context.Context, tx *sql.Tx, g Grant) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO grants(`+grantColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		g.ID, g.Goal, g.CoordinatorID, g.ScopeKind, g.FolderID, g.SnapshotJSON, g.ActionJSON, g.Issuer,
		g.Origin, g.Actor, g.Status, g.BudgetUSD, g.Revision, g.RevocationRevision, g.CreatedAt, g.UpdatedAt)
	return err
}

func loadGrant(ctx context.Context, tx *sql.Tx, id string) (Grant, error) {
	return scanGrant(tx.QueryRowContext(ctx, `SELECT `+grantColumns+` FROM grants WHERE id=?`, id))
}

func scanGrant(row eventScanner) (Grant, error) {
	var g Grant
	err := row.Scan(&g.ID, &g.Goal, &g.CoordinatorID, &g.ScopeKind, &g.FolderID, &g.SnapshotJSON, &g.ActionJSON,
		&g.Issuer, &g.Origin, &g.Actor, &g.Status, &g.BudgetUSD, &g.Revision, &g.RevocationRevision, &g.CreatedAt, &g.UpdatedAt)
	return g, err
}

// PutBinding writes launch intent. A non-empty RequestKey is unique: a second
// insert with the same key is a no-op success that returns the existing row.
func (s *Store) PutBinding(ctx context.Context, b ExecutionBinding) (ExecutionBinding, error) {
	b, err := prepareBinding(b)
	if err != nil {
		return ExecutionBinding{}, storeError(err)
	}
	if err := s.ensureSchema(ctx); err != nil {
		return ExecutionBinding{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ExecutionBinding{}, storeError(err)
	}
	defer tx.Rollback()
	stored, err := storeBinding(ctx, tx, b, s.stamp())
	if err != nil {
		return ExecutionBinding{}, storeError(err)
	}
	return stored, storeError(tx.Commit())
}

func prepareBinding(b ExecutionBinding) (ExecutionBinding, error) {
	if !validText(b.RequestKey, 4096) {
		return ExecutionBinding{}, fmt.Errorf("%w: binding needs a request key", ErrInvalid)
	}
	if b.State == "" {
		b.State = BindReserved
	}
	if !validBindState(b.State) || !validRoad(b.Road) {
		return ExecutionBinding{}, fmt.Errorf("%w: unknown binding state or road", ErrInvalid)
	}
	if !validOptionalText(b.EquivalenceKey, 4096) || !validOptionalText(b.WorkID, 4096) || !validOptionalText(b.RunInstanceID, 4096) {
		return ExecutionBinding{}, fmt.Errorf("%w: binding field is too long", ErrInvalid)
	}
	if !validOptionalText(b.OwnerChatID, 4096) || !validOptionalText(b.GrantID, 4096) || !validOptionalText(b.CoordinatorID, 4096) {
		return ExecutionBinding{}, fmt.Errorf("%w: binding field is too long", ErrInvalid)
	}
	return b, nil
}

func storeBinding(ctx context.Context, tx *sql.Tx, b ExecutionBinding, now string) (ExecutionBinding, error) {
	if existing, ok, err := existingBinding(ctx, tx, b); err != nil || ok {
		return existing, err
	}
	if err := mintBindingIDs(&b); err != nil {
		return ExecutionBinding{}, err
	}
	b.CreatedAt, b.UpdatedAt = now, now
	if err := insertBinding(ctx, tx, b); err != nil {
		if existing, ok, lookupErr := existingBinding(ctx, tx, b); lookupErr != nil || ok {
			return existing, lookupErr
		}
		return ExecutionBinding{}, err
	}
	return b, nil
}

func existingBinding(ctx context.Context, tx *sql.Tx, b ExecutionBinding) (ExecutionBinding, bool, error) {
	if b.RequestKey != "" {
		existing, err := lookupRequestKey(ctx, tx, b.RequestKey)
		if err == nil {
			return existing, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return ExecutionBinding{}, false, err
		}
	}
	if b.ID == "" {
		return ExecutionBinding{}, false, nil
	}
	existing, err := loadBinding(ctx, tx, b.ID)
	if err == nil {
		return existing, true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ExecutionBinding{}, false, nil
	}
	return ExecutionBinding{}, false, err
}

func mintBindingIDs(b *ExecutionBinding) error {
	id, err := mintID()
	if err != nil {
		return err
	}
	b.ID = id
	if b.WorkID == "" {
		b.WorkID = id
	}
	return nil
}

func (s *Store) GetBinding(ctx context.Context, id string) (ExecutionBinding, error) {
	return s.loadBindingBy(ctx, `SELECT `+bindingColumns+` FROM execution_bindings WHERE id=?`, id)
}

func (s *Store) BindingByRequestKey(ctx context.Context, requestKey string) (ExecutionBinding, error) {
	return s.loadBindingBy(ctx, `SELECT `+bindingColumns+` FROM execution_bindings WHERE request_key=?`, requestKey)
}

func (s *Store) BindingByEquivalence(ctx context.Context, equivalenceKey string) (ExecutionBinding, error) {
	return s.loadBindingBy(ctx, `SELECT `+bindingColumns+` FROM execution_bindings
 WHERE equivalence_key=? AND state IN (?,?,?,?) ORDER BY seq LIMIT 1`,
		equivalenceKey, BindReserved, BindAdmitted, BindBound, BindPaused)
}

// ListBindingsForChat is the TUI launch-state scan: owner or coordinator.
// Listing never migrates. Empty chat is emptiness, never a fabricated row.
func (s *Store) ListBindingsForChat(ctx context.Context, chatID string) ([]ExecutionBinding, error) {
	if strings.TrimSpace(chatID) == "" {
		return []ExecutionBinding{}, nil
	}
	return s.listBindings(ctx, `SELECT `+bindingColumns+` FROM execution_bindings
 WHERE owner_chat_id=? OR coordinator_id=? ORDER BY seq`, chatID, chatID)
}

// ListUnboundBindings is the recover scan: reserved or admitted rows with no
// run-instance id yet. Listing never migrates. Tick Recover binds these; it
// must not Admit a second time (A14).
func (s *Store) ListUnboundBindings(ctx context.Context) ([]ExecutionBinding, error) {
	return s.listBindings(ctx, `SELECT `+bindingColumns+` FROM execution_bindings
 WHERE run_instance_id='' AND state IN (?,?) ORDER BY seq`, BindReserved, BindAdmitted)
}

func (s *Store) listBindings(ctx context.Context, query string, args ...any) ([]ExecutionBinding, error) {
	ready, err := s.v5Ready(ctx)
	if err != nil || !ready {
		return make([]ExecutionBinding, 0), err
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storeError(err)
	}
	defer rows.Close()
	result := make([]ExecutionBinding, 0)
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, storeError(err)
		}
		result = append(result, b)
	}
	return result, storeError(rows.Err())
}

func (s *Store) loadBindingBy(ctx context.Context, query string, args ...any) (ExecutionBinding, error) {
	ready, err := s.v5Ready(ctx)
	if err != nil || !ready {
		return ExecutionBinding{}, storeError(ErrNotFound)
	}
	b, err := scanBinding(s.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return ExecutionBinding{}, storeError(ErrNotFound)
	}
	return b, storeError(err)
}

// BindRuntime moves reserved or admitted to bound. A second distinct
// RunInstanceID for the same request key refuses with "binding".
func (s *Store) BindRuntime(ctx context.Context, requestKey, runInstanceID, runtimeRef string) (ExecutionBinding, error) {
	if !validText(requestKey, 4096) || !validText(runInstanceID, 4096) {
		return ExecutionBinding{}, storeError(fmt.Errorf("%w: bind needs a request key and a run-instance id", ErrInvalid))
	}
	if !validOptionalText(runtimeRef, 4096) {
		return ExecutionBinding{}, storeError(fmt.Errorf("%w: runtime ref is too long", ErrInvalid))
	}
	if err := s.writeReady(ctx); err != nil {
		return ExecutionBinding{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ExecutionBinding{}, storeError(err)
	}
	defer tx.Rollback()
	stored, err := bindRuntime(ctx, tx, requestKey, runInstanceID, runtimeRef, s.stamp())
	if err != nil {
		return ExecutionBinding{}, storeError(err)
	}
	return stored, storeError(tx.Commit())
}

func bindRuntime(ctx context.Context, tx *sql.Tx, requestKey, runInstanceID, runtimeRef, now string) (ExecutionBinding, error) {
	b, err := lookupRequestKey(ctx, tx, requestKey)
	if errors.Is(err, sql.ErrNoRows) {
		return ExecutionBinding{}, ErrNotFound
	}
	if err != nil {
		return ExecutionBinding{}, err
	}
	if b.State == BindBound && b.RunInstanceID == runInstanceID {
		return b, nil
	}
	if b.RunInstanceID != "" && b.RunInstanceID != runInstanceID {
		return ExecutionBinding{}, bindingConflict()
	}
	if !liveBindState(b.State) && b.State != BindAdmitted {
		return ExecutionBinding{}, fmt.Errorf("%w: binding cannot bind from %s", ErrInvalid, b.State)
	}
	return stampBound(ctx, tx, b, runInstanceID, runtimeRef, now)
}

func stampBound(ctx context.Context, tx *sql.Tx, b ExecutionBinding, runInstanceID, runtimeRef, now string) (ExecutionBinding, error) {
	if b.AdmittedAt == "" {
		b.AdmittedAt = now
	}
	b.State, b.RunInstanceID, b.RuntimeRef = BindBound, runInstanceID, runtimeRef
	b.BoundAt, b.UpdatedAt = now, now
	_, err := tx.ExecContext(ctx, `UPDATE execution_bindings SET state=?, run_instance_id=?, runtime_ref=?, admitted_at=?, bound_at=?, updated_at=? WHERE id=?`,
		b.State, b.RunInstanceID, b.RuntimeRef, b.AdmittedAt, b.BoundAt, b.UpdatedAt, b.ID)
	return b, err
}

func (s *Store) v5Ready(ctx context.Context) (bool, error) {
	ready, err := s.readyForRead(ctx)
	if err != nil || !ready {
		return false, err
	}
	s.schemaMu.Lock()
	defer s.schemaMu.Unlock()
	return s.version >= 5, nil
}

func insertBinding(ctx context.Context, tx *sql.Tx, b ExecutionBinding) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO execution_bindings(`+bindingColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		b.ID, b.RequestKey, b.EquivalenceKey, b.WorkID, b.RunInstanceID, b.Road, b.OwnerChatID, b.GrantID,
		b.CoordinatorID, b.RuntimeRef, b.AssignmentRev, b.GrantRev, b.State, b.Fence, b.Owner, b.LeaseUntil,
		b.CreatedAt, b.UpdatedAt, b.BoundAt, b.AdmittedAt)
	return err
}

func loadBinding(ctx context.Context, tx *sql.Tx, id string) (ExecutionBinding, error) {
	return scanBinding(tx.QueryRowContext(ctx, `SELECT `+bindingColumns+` FROM execution_bindings WHERE id=?`, id))
}

func lookupRequestKey(ctx context.Context, tx *sql.Tx, key string) (ExecutionBinding, error) {
	return scanBinding(tx.QueryRowContext(ctx, `SELECT `+bindingColumns+` FROM execution_bindings WHERE request_key=?`, key))
}

func scanBinding(row eventScanner) (ExecutionBinding, error) {
	var b ExecutionBinding
	err := row.Scan(&b.ID, &b.RequestKey, &b.EquivalenceKey, &b.WorkID, &b.RunInstanceID, &b.Road, &b.OwnerChatID,
		&b.GrantID, &b.CoordinatorID, &b.RuntimeRef, &b.AssignmentRev, &b.GrantRev, &b.State, &b.Fence, &b.Owner,
		&b.LeaseUntil, &b.CreatedAt, &b.UpdatedAt, &b.BoundAt, &b.AdmittedAt)
	return b, err
}
