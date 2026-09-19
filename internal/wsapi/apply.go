package wsapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

type batchStore interface {
	ApplyBatch(ctx context.Context, ops []workspace.BatchOp, proposal workspace.Proposal) (workspace.Proposal, []workspace.MembershipEvent, error)
}

// ApplyActionPlan validates, then applies. Revalidate happens immediately
// before the writes because organize ran outside the collections transaction.
// Production (*workspace.Store) applies the action-set plus the proposal row
// in one writer transaction. A fake store still walks per-op methods.
func (s *Service) ApplyActionPlan(ctx context.Context, plan ActionPlan) (ApplyResult, error) {
	if err := s.ValidateActionPlan(ctx, plan); err != nil {
		return ApplyResult{}, err
	}
	if plan.Kind == PlanNoAction {
		return s.recordProposal(ctx, plan, "no-action", nil)
	}
	if err := s.ValidateActionPlan(ctx, plan); err != nil {
		return ApplyResult{}, err
	}
	if batcher, ok := s.store.(batchStore); ok {
		return s.applyThroughBatch(ctx, plan, batcher)
	}
	applied, err := s.applyActions(ctx, plan)
	if err != nil {
		return ApplyResult{}, err
	}
	return s.recordProposal(ctx, plan, "applied", applied)
}

func (s *Service) applyThroughBatch(ctx context.Context, plan ActionPlan, batcher batchStore) (ApplyResult, error) {
	raw, err := json.Marshal(plan)
	if err != nil {
		return ApplyResult{}, err
	}
	stored, applied, err := batcher.ApplyBatch(ctx, batchOpsOf(plan), workspace.Proposal{
		ChatID:         plan.ChatID,
		SourceRev:      plan.SourceRev,
		PlanJSON:       string(raw),
		Result:         "applied",
		IdempotencyKey: proposalKey(plan),
	})
	if err != nil {
		return ApplyResult{}, wrapStoreError(err)
	}
	if applied == nil {
		applied = []workspace.MembershipEvent{}
	}
	return ApplyResult{PlanID: stored.ID, Applied: applied}, nil
}

func batchOpsOf(plan ActionPlan) []workspace.BatchOp {
	actor := organizerActor(plan)
	ops := make([]workspace.BatchOp, 0, len(plan.Actions))
	for _, action := range plan.Actions {
		ops = append(ops, workspace.BatchOp{
			Kind:         action.Kind,
			CollectionID: action.CollectionID,
			FromID:       action.FromID,
			ToID:         action.ToID,
			Ref:          normalizeRef(action.Ref),
			FolderName:   action.FolderName,
			ParentIDs:    append([]string(nil), action.ParentIDs...),
			Provenance:   organizerProvenance(plan, action, actor),
		})
	}
	return ops
}

func (s *Service) applyActions(ctx context.Context, plan ActionPlan) ([]workspace.MembershipEvent, error) {
	aliases := map[string]string{}
	actor := organizerActor(plan)
	applied := make([]workspace.MembershipEvent, 0)
	for _, action := range plan.Actions {
		events, err := s.applyAction(ctx, plan, action, actor, aliases)
		if err != nil {
			return nil, err
		}
		applied = append(applied, events...)
	}
	return applied, nil
}

func (s *Service) applyAction(ctx context.Context, plan ActionPlan, action Action, actor string, aliases map[string]string) ([]workspace.MembershipEvent, error) {
	action = rewriteAliases(action, aliases)
	action.Ref = normalizeRef(action.Ref)
	p := organizerProvenance(plan, action, actor)
	switch action.Kind {
	case PlanAdd:
		return s.applyAdd(ctx, action, p)
	case PlanRemove:
		return s.applyRemove(ctx, action, p)
	case PlanMove:
		return s.applyMove(ctx, action, p)
	case PlanCreateFolder:
		return s.applyCreateFolder(ctx, action, p, aliases)
	default:
		return nil, fmt.Errorf("%w: unknown action kind %q", workspace.ErrInvalid, action.Kind)
	}
}

func (s *Service) applyAdd(ctx context.Context, action Action, p workspace.Provenance) ([]workspace.MembershipEvent, error) {
	if err := s.store.AddWith(ctx, action.CollectionID, action.Ref, p); err != nil {
		return nil, wrapStoreError(err)
	}
	return s.eventAfter(ctx, action.CollectionID, action.Ref)
}

func (s *Service) applyRemove(ctx context.Context, action Action, p workspace.Provenance) ([]workspace.MembershipEvent, error) {
	if err := s.store.RemoveWith(ctx, action.CollectionID, action.Ref, p); err != nil {
		return nil, wrapStoreError(err)
	}
	return s.eventAfter(ctx, action.CollectionID, action.Ref)
}

func (s *Service) applyMove(ctx context.Context, action Action, p workspace.Provenance) ([]workspace.MembershipEvent, error) {
	if err := s.store.Move(ctx, action.FromID, action.ToID, action.Ref, p); err != nil {
		return nil, wrapStoreError(err)
	}
	return s.eventAfter(ctx, action.ToID, action.Ref)
}

func (s *Service) applyCreateFolder(ctx context.Context, action Action, p workspace.Provenance, aliases map[string]string) ([]workspace.MembershipEvent, error) {
	created, err := s.store.Create(ctx, action.FolderName)
	if err != nil {
		return nil, wrapStoreError(err)
	}
	rememberAlias(aliases, action, created.ID)
	events := make([]workspace.MembershipEvent, 0, len(action.ParentIDs))
	child := collectionRef(created.ID)
	for _, parent := range action.ParentIDs {
		parent = resolveAlias(parent, aliases)
		if err := s.store.AddWith(ctx, parent, child, p); err != nil {
			return nil, wrapStoreError(err)
		}
		got, err := s.eventAfter(ctx, parent, child)
		if err != nil {
			return nil, err
		}
		events = append(events, got...)
	}
	return events, nil
}

func (s *Service) eventAfter(ctx context.Context, collectionID string, ref workspace.Ref) ([]workspace.MembershipEvent, error) {
	why, err := s.store.WhyHere(ctx, collectionID, ref)
	if err != nil {
		return nil, wrapStoreError(err)
	}
	return []workspace.MembershipEvent{why}, nil
}

func (s *Service) recordProposal(ctx context.Context, plan ActionPlan, result string, applied []workspace.MembershipEvent) (ApplyResult, error) {
	raw, err := json.Marshal(plan)
	if err != nil {
		return ApplyResult{}, err
	}
	stored, err := s.store.PutProposal(ctx, workspace.Proposal{
		ChatID:         plan.ChatID,
		SourceRev:      plan.SourceRev,
		PlanJSON:       string(raw),
		Result:         result,
		IdempotencyKey: proposalKey(plan),
	})
	if err != nil {
		return ApplyResult{}, wrapStoreError(err)
	}
	if applied == nil {
		applied = []workspace.MembershipEvent{}
	}
	return ApplyResult{PlanID: stored.ID, Applied: applied}, nil
}

// SuppressPlacement records that this evidence must not re-file the same object.
func (s *Service) SuppressPlacement(ctx context.Context, collectionID string, ref workspace.Ref, evidenceHash string, p workspace.Provenance) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	return wrapStoreError(s.store.Suppress(ctx, collectionID, ref, evidenceHash, normalize(p)))
}

func organizerActor(plan ActionPlan) string {
	if plan.ChatID != "" {
		return plan.ChatID
	}
	return workspace.OriginOrganizer
}

func organizerProvenance(plan ActionPlan, action Action, actor string) workspace.Provenance {
	return workspace.Provenance{
		Origin:           workspace.OriginOrganizer,
		Reason:           action.Reason,
		Actor:            actor,
		Evidence:         evidenceHash(actionEvidence(plan, action)),
		IdempotencyKey:   action.IdempotencyKey,
		ExpectedRevision: action.ExpectedRevision,
		ExpectedFrom:     action.ExpectedFrom,
		ExpectedTo:       action.ExpectedTo,
	}
}

func proposalKey(plan ActionPlan) string {
	if plan.ChatID == "" && plan.SourceRev == "" {
		return ""
	}
	return plan.ChatID + "/" + plan.SourceRev
}

func rewriteAliases(action Action, aliases map[string]string) Action {
	action.CollectionID = resolveAlias(action.CollectionID, aliases)
	action.FromID = resolveAlias(action.FromID, aliases)
	action.ToID = resolveAlias(action.ToID, aliases)
	if action.Ref.Kind == workspace.CollectionKind {
		action.Ref.ID = resolveAlias(action.Ref.ID, aliases)
	}
	for i, parent := range action.ParentIDs {
		action.ParentIDs[i] = resolveAlias(parent, aliases)
	}
	return action
}

func rememberAlias(aliases map[string]string, action Action, id string) {
	aliases[action.FolderName] = id
	if action.CollectionID != "" {
		aliases[action.CollectionID] = id
	}
}

func resolveAlias(id string, aliases map[string]string) string {
	if resolved, ok := aliases[id]; ok {
		return resolved
	}
	return id
}
