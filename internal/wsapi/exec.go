package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

// LaunchRequest / SteerRevision / WorkView / ResultView copy wsexec fields
// so this package does not import wsexec. SetExecutor injects the adapter; nil
// means LaunchOrJoin is absent, not a dummy completed view.
type LaunchRequest struct {
	RequestKey, EquivalenceKey, GrantID, CoordinatorID, OwnerChatID, Brief string
	GrantRev, AssignmentRev                                                string
}

type SteerRevision struct {
	WorkID, GrantID, Text, PersonRequestID string
	GrantRev                               int
}

type WorkView struct {
	WorkID, RequestKey, RunInstanceID, Road, State, OwnerChatID, GrantID string
	Joined                                                               bool
}

type ResultView struct {
	WorkID, RunInstanceID, State, Detail string
}

// Executor is the injected launch-or-join adapter, the same pattern as Inventory.
type Executor interface {
	LaunchOrJoin(ctx context.Context, req LaunchRequest) (WorkView, error)
	Inspect(ctx context.Context, workID string) (WorkView, error)
	Steer(ctx context.Context, rev SteerRevision) error
	PauseWork(ctx context.Context, workID string) error
	StopWork(ctx context.Context, workID string) error
	Observe(ctx context.Context, workID string) (ResultView, error)
	Recover(ctx context.Context, requestKey string) (WorkView, error)
}

type GrantRequest struct {
	CoordinatorID, Goal, ScopeKind, FolderID string
	ChatIDs                                  []string
	ActionClasses                            []string
	BudgetUSD                                float64
	Issuer                                   string
}

type GrantView struct {
	ID, Goal, CoordinatorID, ScopeKind, FolderID, Status, Issuer string
	ChatIDs                                                      []string
	ActionClasses                                                []string
	BudgetUSD                                                    float64
	Revision, RevocationRevision                                 int
}

type LaunchWorkRequest struct {
	GrantID, CoordinatorID, OwnerChatID, Brief, EquivalenceKey, IdempotencyKey string
}

// grantStore is the v5 grant door. *workspace.Store implements it; a v3 fake
// in tests does too. A store that does not is absence, not a second grant table.
type grantStore interface {
	PutGrant(ctx context.Context, g workspace.Grant) (workspace.Grant, error)
	GetGrant(ctx context.Context, id string) (workspace.Grant, error)
	ListGrants(ctx context.Context, coordinatorID string) ([]workspace.Grant, error)
	RevokeGrant(ctx context.Context, id string, expectedRevision int) (workspace.Grant, error)
}

var _ grantStore = (*workspace.Store)(nil)

func (s *Service) requireGrantStore() (grantStore, error) {
	if s == nil || s.store == nil {
		return nil, wrapStoreError(workspace.ErrInvalid)
	}
	g, ok := s.store.(grantStore)
	if !ok {
		return nil, fmt.Errorf("%w: grant store is absent", workspace.ErrInvalid)
	}
	return g, nil
}

func (s *Service) requireExecutor() error {
	if s == nil || s.exec == nil {
		return fmt.Errorf("%w: executor is absent", workspace.ErrInvalid)
	}
	return nil
}

// IssueGrant records a person-origin delegation. THE ORIGIN IS THE PERSON:
// this door stamps it; a coordinator cannot add classes or enlarge scope
// beyond the issuer's own grant.
func (s *Service) IssueGrant(ctx context.Context, req GrantRequest) (GrantView, error) {
	if err := s.ready(ctx); err != nil {
		return GrantView{}, err
	}
	g, err := s.requireGrantStore()
	if err != nil {
		return GrantView{}, err
	}
	row, err := s.newGrantRow(req)
	if err != nil {
		return GrantView{}, err
	}
	if err := s.refuseRootGrant(ctx, req); err != nil {
		return GrantView{}, err
	}
	if err := s.refuseSelfExpand(ctx, g, row); err != nil {
		return GrantView{}, err
	}
	stored, err := g.PutGrant(ctx, row)
	if err != nil {
		return GrantView{}, wrapStoreError(err)
	}
	return grantView(stored), nil
}

func (s *Service) newGrantRow(req GrantRequest) (workspace.Grant, error) {
	if req.CoordinatorID == "" {
		return workspace.Grant{}, fmt.Errorf("%w: grant needs a coordinator", workspace.ErrInvalid)
	}
	actions, err := marshalIDList(req.ActionClasses)
	if err != nil {
		return workspace.Grant{}, err
	}
	snapshot, err := marshalIDList(req.ChatIDs)
	if err != nil {
		return workspace.Grant{}, err
	}
	return workspace.Grant{
		Goal: req.Goal, CoordinatorID: req.CoordinatorID, ScopeKind: req.ScopeKind,
		FolderID: req.FolderID, SnapshotJSON: snapshot, ActionJSON: actions,
		Issuer: req.Issuer, Origin: workspace.OriginPerson, Status: workspace.GrantActive,
		BudgetUSD: req.BudgetUSD,
	}, nil
}

func (s *Service) refuseSelfExpand(ctx context.Context, g grantStore, next workspace.Grant) error {
	if next.Issuer == "" {
		return nil
	}
	issuer, err := g.GetGrant(ctx, next.Issuer)
	if errors.Is(err, workspace.ErrNotFound) {
		return nil
	}
	if err != nil {
		return wrapStoreError(err)
	}
	if grantWouldExpand(issuer, next) {
		return fmt.Errorf("%w: grant cannot expand", workspace.ErrInvalid)
	}
	return nil
}

// RevokeGrant is checked before the next launch/steer/stop commitment.
func (s *Service) RevokeGrant(ctx context.Context, id string, expectedRevision int) (GrantView, error) {
	if err := s.ready(ctx); err != nil {
		return GrantView{}, err
	}
	g, err := s.requireGrantStore()
	if err != nil {
		return GrantView{}, err
	}
	stored, err := g.RevokeGrant(ctx, id, expectedRevision)
	if err != nil {
		return GrantView{}, wrapStoreError(err)
	}
	return grantView(stored), nil
}

func (s *Service) InspectGrant(ctx context.Context, id string) (GrantView, error) {
	if err := s.ready(ctx); err != nil {
		return GrantView{}, err
	}
	row, err := s.loadGrant(ctx, id)
	if err != nil {
		return GrantView{}, err
	}
	return grantView(row), nil
}

// LaunchOrJoin authenticates the grant, then hands the request key to the
// executor. REQUEST KEY BEFORE ADMISSION: an empty IdempotencyKey is refused
// here so runtime Admit is never first. PauseCoordination still blocks new
// launch; it does not stop existing work.
func (s *Service) LaunchOrJoin(ctx context.Context, req LaunchWorkRequest) (WorkView, error) {
	if err := s.ready(ctx); err != nil {
		return WorkView{}, err
	}
	if err := s.requireExecutor(); err != nil {
		return WorkView{}, err
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" || strings.TrimSpace(req.GrantID) == "" {
		return WorkView{}, fmt.Errorf("%w: launch needs a request key and a grant", workspace.ErrInvalid)
	}
	grant, err := s.requireActiveGrant(ctx, req.GrantID, workspace.ClassExecute)
	if err != nil {
		return WorkView{}, err
	}
	coord := req.CoordinatorID
	if coord == "" {
		coord = grant.CoordinatorID
	}
	if err := s.refuseIfPaused(ctx, coord); err != nil {
		return WorkView{}, err
	}
	return s.exec.LaunchOrJoin(ctx, launchFrom(req, grant, coord))
}

func (s *Service) InspectWork(ctx context.Context, workID string) (WorkView, error) {
	if err := s.ready(ctx); err != nil {
		return WorkView{}, err
	}
	if err := s.requireExecutor(); err != nil {
		return WorkView{}, err
	}
	return s.exec.Inspect(ctx, workID)
}

func (s *Service) SteerWork(ctx context.Context, rev SteerRevision) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	if err := s.requireExecutor(); err != nil {
		return err
	}
	if err := refuseModelPerson(rev.PersonRequestID); err != nil {
		return err
	}
	if _, err := s.requireActiveGrant(ctx, rev.GrantID, workspace.ClassSteer); err != nil {
		return err
	}
	return s.exec.Steer(ctx, rev)
}

// PauseWork acts on an existing binding. It is not PauseCoordination.
func (s *Service) PauseWork(ctx context.Context, workID string) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	if err := s.requireExecutor(); err != nil {
		return err
	}
	return s.exec.PauseWork(ctx, workID)
}

// StopWork is the separate explicit action on existing work. A revoked grant
// cannot stop; PauseCoordination is a different verb.
func (s *Service) StopWork(ctx context.Context, workID string) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	if err := s.requireExecutor(); err != nil {
		return err
	}
	view, err := s.exec.Inspect(ctx, workID)
	if err != nil {
		return err
	}
	if err := s.refuseRevokedStop(ctx, view.GrantID); err != nil {
		return err
	}
	return s.exec.StopWork(ctx, workID)
}

func (s *Service) ObserveWork(ctx context.Context, workID string) (ResultView, error) {
	if err := s.ready(ctx); err != nil {
		return ResultView{}, err
	}
	if err := s.requireExecutor(); err != nil {
		return ResultView{}, err
	}
	return s.exec.Observe(ctx, workID)
}

func (s *Service) loadGrant(ctx context.Context, id string) (workspace.Grant, error) {
	g, err := s.requireGrantStore()
	if err != nil {
		return workspace.Grant{}, err
	}
	row, err := g.GetGrant(ctx, id)
	if err != nil {
		return workspace.Grant{}, wrapStoreError(err)
	}
	return row, nil
}

func (s *Service) requireActiveGrant(ctx context.Context, id, class string) (workspace.Grant, error) {
	row, err := s.loadGrant(ctx, id)
	if err != nil {
		return workspace.Grant{}, err
	}
	if row.Status != workspace.GrantActive {
		return workspace.Grant{}, fmt.Errorf("%w: grant is %s", workspace.ErrInvalid, row.Status)
	}
	if class != "" && !grantHasClass(row, class) {
		return workspace.Grant{}, fmt.Errorf("%w: grant lacks %s", workspace.ErrInvalid, class)
	}
	return row, nil
}

func (s *Service) refuseRevokedStop(ctx context.Context, grantID string) error {
	if grantID == "" {
		return nil
	}
	row, err := s.loadGrant(ctx, grantID)
	if err != nil {
		return err
	}
	if row.Status != workspace.GrantActive {
		return fmt.Errorf("%w: grant is %s", workspace.ErrInvalid, row.Status)
	}
	if grantHasClass(row, workspace.ClassStop) || grantHasClass(row, workspace.ClassExecute) {
		return nil
	}
	return fmt.Errorf("%w: grant lacks stop", workspace.ErrInvalid)
}

func launchFrom(req LaunchWorkRequest, grant workspace.Grant, coord string) LaunchRequest {
	return LaunchRequest{
		RequestKey: req.IdempotencyKey, EquivalenceKey: req.EquivalenceKey, GrantID: grant.ID,
		CoordinatorID: coord, OwnerChatID: req.OwnerChatID, Brief: req.Brief,
		GrantRev: strconv.Itoa(grant.Revision),
	}
}

func grantView(g workspace.Grant) GrantView {
	return GrantView{
		ID: g.ID, Goal: g.Goal, CoordinatorID: g.CoordinatorID, ScopeKind: g.ScopeKind,
		FolderID: g.FolderID, Status: g.Status, Issuer: g.Issuer, ChatIDs: parseSnapshot(g.SnapshotJSON),
		ActionClasses: parseSnapshot(g.ActionJSON), BudgetUSD: g.BudgetUSD,
		Revision: g.Revision, RevocationRevision: g.RevocationRevision,
	}
}

func marshalIDList(ids []string) (string, error) {
	raw, err := json.Marshal(copyIDs(ids))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func grantWouldExpand(base, next workspace.Grant) bool {
	if classesExpand(parseSnapshot(base.ActionJSON), parseSnapshot(next.ActionJSON)) {
		return true
	}
	if base.ScopeKind == ScopeSelected && next.ScopeKind == ScopeFolderDynamic {
		return true
	}
	if base.FolderID != "" && next.FolderID != base.FolderID {
		return true
	}
	return classesExpand(parseSnapshot(base.SnapshotJSON), parseSnapshot(next.SnapshotJSON))
}

func classesExpand(have, want []string) bool {
	set := map[string]struct{}{}
	for _, item := range have {
		set[item] = struct{}{}
	}
	for _, item := range want {
		if _, ok := set[item]; !ok {
			return true
		}
	}
	return false
}

func grantHasClass(g workspace.Grant, class string) bool {
	for _, got := range parseSnapshot(g.ActionJSON) {
		if got == class {
			return true
		}
	}
	return false
}

func refuseModelPerson(id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return fmt.Errorf("%w: steer needs the original person request", workspace.ErrInvalid)
	}
	switch strings.ToLower(strings.ReplaceAll(trimmed, "_", "")) {
	case workspace.OriginPerson, "fromperson", "from-person":
		return fmt.Errorf("%w: model-supplied person origin", workspace.ErrInvalid)
	}
	return nil
}
