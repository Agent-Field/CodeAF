package wsapi

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

type planWorld struct {
	ids     map[string]struct{}
	pending map[string]struct{}
	names   map[string]struct{}
	revs    map[string]int
	root    int
	guide   map[string]int
}

// ValidateActionPlan refuses a plan that is not typed, cites missing IDs,
// cycles, is stale, stamps the wrong authority, or tries to re-add suppressed
// evidence. THE MODEL NEVER WRITES SQL: this is the only write door.
func (s *Service) ValidateActionPlan(ctx context.Context, plan ActionPlan) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	return s.validatePlan(ctx, plan)
}

func (s *Service) validatePlan(ctx context.Context, plan ActionPlan) error {
	if !validPlanKind(plan.Kind) {
		return fmt.Errorf("%w: unknown plan kind %q", workspace.ErrInvalid, plan.Kind)
	}
	if plan.Kind == PlanNoAction {
		return validateNoAction(plan)
	}
	return s.validateMembershipPlan(ctx, plan)
}

func validPlanKind(kind string) bool {
	switch kind {
	case PlanNoAction, PlanAdd, PlanRemove, PlanMove, PlanCreateFolder:
		return true
	}
	return false
}

func validActionKind(kind string) bool {
	switch kind {
	case PlanAdd, PlanRemove, PlanMove, PlanCreateFolder:
		return true
	}
	return false
}

func validateNoAction(plan ActionPlan) error {
	if len(plan.Actions) != 0 {
		return fmt.Errorf("%w: no-action plans must have empty actions", workspace.ErrInvalid)
	}
	return nil
}

func (s *Service) validateMembershipPlan(ctx context.Context, plan ActionPlan) error {
	if plan.Model == "" {
		return fmt.Errorf("%w: keyword-only filer refused", workspace.ErrInvalid)
	}
	if plan.Degraded {
		return fmt.Errorf("%w: degraded plans must not apply membership", workspace.ErrInvalid)
	}
	if len(plan.Actions) == 0 {
		return fmt.Errorf("%w: membership plan has no actions", workspace.ErrInvalid)
	}
	world, err := s.newPlanWorld(ctx)
	if err != nil {
		return err
	}
	for _, action := range plan.Actions {
		if err := s.validateAction(ctx, plan, action, world); err != nil {
			return err
		}
		world.note(action)
	}
	return nil
}

func (s *Service) newPlanWorld(ctx context.Context) (*planWorld, error) {
	collections, err := s.store.Collections(ctx)
	if err != nil {
		return nil, wrapStoreError(err)
	}
	world := &planWorld{
		ids:     map[string]struct{}{},
		pending: map[string]struct{}{},
		names:   map[string]struct{}{},
		revs:    map[string]int{},
		guide:   map[string]int{},
	}
	for _, collection := range collections {
		world.ids[collection.ID] = struct{}{}
		world.names[strings.ToLower(collection.Name)] = struct{}{}
		world.revs[collection.ID] = collection.Revision
	}
	root, err := rootRevision(ctx, s.store)
	if err != nil {
		return nil, err
	}
	world.root = root
	if err := s.fillGuidanceRevs(ctx, world, collections); err != nil {
		return nil, err
	}
	return world, nil
}

func (s *Service) fillGuidanceRevs(ctx context.Context, world *planWorld, collections []workspace.Collection) error {
	if err := s.noteGuidanceRev(ctx, world, ""); err != nil {
		return err
	}
	for _, collection := range collections {
		if err := s.noteGuidanceRev(ctx, world, collection.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) noteGuidanceRev(ctx context.Context, world *planWorld, scopeID string) error {
	rows, err := s.store.ListGuidance(ctx, scopeID)
	if err != nil {
		return wrapStoreError(err)
	}
	if active := lastActive(rows); active.ID != "" {
		world.guide[scopeID] = active.Revision
	}
	return nil
}

func (s *Service) validateAction(ctx context.Context, plan ActionPlan, action Action, world *planWorld) error {
	if !validActionKind(action.Kind) {
		return fmt.Errorf("%w: unknown action kind %q", workspace.ErrInvalid, action.Kind)
	}
	action.Ref = normalizeRef(action.Ref)
	if err := s.checkActionIDs(action, world); err != nil {
		return err
	}
	if err := checkActionFreshness(action, world); err != nil {
		return err
	}
	if err := checkActionEvidence(plan, action); err != nil {
		return err
	}
	if err := s.checkActionAuthority(ctx, action); err != nil {
		return err
	}
	if err := s.checkActionSuppression(ctx, plan, action); err != nil {
		return err
	}
	return s.checkActionShape(ctx, action, world)
}

func (s *Service) checkActionIDs(action Action, world *planWorld) error {
	switch action.Kind {
	case PlanAdd, PlanRemove:
		if !world.has(action.CollectionID) {
			return fmt.Errorf("%w: collection %q does not exist", workspace.ErrInvalid, action.CollectionID)
		}
		return s.checkRefID(action.Ref, world)
	case PlanMove:
		if !world.has(action.FromID) || !world.has(action.ToID) {
			return fmt.Errorf("%w: move needs collections that exist", workspace.ErrInvalid)
		}
		return s.checkRefID(action.Ref, world)
	case PlanCreateFolder:
		return checkParentIDs(action.ParentIDs, world)
	}
	return nil
}

func (s *Service) checkRefID(ref workspace.Ref, world *planWorld) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	if ref.Kind == workspace.CollectionKind && !world.has(ref.ID) {
		return fmt.Errorf("%w: collection %q does not exist", workspace.ErrInvalid, ref.ID)
	}
	return nil
}

func checkParentIDs(ids []string, world *planWorld) error {
	for _, id := range ids {
		if !world.has(id) {
			return fmt.Errorf("%w: parent collection %q does not exist", workspace.ErrInvalid, id)
		}
	}
	return nil
}

func checkActionFreshness(action Action, world *planWorld) error {
	switch action.Kind {
	case PlanAdd, PlanRemove:
		return world.matchRev(action.CollectionID, action.ExpectedRevision)
	case PlanMove:
		if err := world.matchRev(action.FromID, action.ExpectedFrom); err != nil {
			return err
		}
		return world.matchRev(action.ToID, action.ExpectedTo)
	case PlanCreateFolder:
		if action.ExpectedRootRevision == 0 {
			return zeroRevision()
		}
		if action.ExpectedRootRevision != world.root {
			return workspace.ErrConflict
		}
	}
	if action.ExpectedGuidanceRevision == 0 {
		return nil
	}
	if action.ExpectedGuidanceRevision != world.guideRev(action.CollectionID) {
		return workspace.ErrConflict
	}
	return nil
}

func (w *planWorld) matchRev(id string, expected int) error {
	if expected == 0 {
		return zeroRevision()
	}
	if w.pendingID(id) {
		return nil
	}
	if w.revs[id] != expected {
		return workspace.ErrConflict
	}
	return nil
}

func zeroRevision() error {
	return fmt.Errorf("%w: organizer apply needs a non-zero expected revision", workspace.ErrInvalid)
}

func checkActionEvidence(plan ActionPlan, action Action) error {
	if action.Kind == PlanRemove {
		return nil
	}
	if hasPassageEvidence(actionEvidence(plan, action)) {
		return nil
	}
	return fmt.Errorf("%w: membership needs passage evidence", workspace.ErrInvalid)
}

func (s *Service) checkActionAuthority(ctx context.Context, action Action) error {
	if action.Kind != PlanRemove {
		return nil
	}
	members, err := s.store.Members(ctx, action.CollectionID)
	if err != nil {
		return wrapStoreError(err)
	}
	if !hasMember(members, action.Ref) {
		return nil
	}
	why, err := s.store.WhyHere(ctx, action.CollectionID, action.Ref)
	if err != nil {
		return wrapStoreError(err)
	}
	switch why.Origin {
	case workspace.OriginOrganizer, workspace.OriginSystemFallback:
		return nil
	default:
		return fmt.Errorf("%w: organizer cannot remove a person placement", workspace.ErrInvalid)
	}
}

func hasMember(members []workspace.Ref, ref workspace.Ref) bool {
	for _, member := range members {
		if member.Kind == ref.Kind && member.ID == ref.ID && member.SessionID == ref.SessionID {
			return true
		}
	}
	return false
}

func (s *Service) checkActionSuppression(ctx context.Context, plan ActionPlan, action Action) error {
	if action.Kind != PlanAdd {
		return nil
	}
	hash := evidenceHash(actionEvidence(plan, action))
	suppressed, err := s.store.IsSuppressed(ctx, action.CollectionID, action.Ref, hash)
	if err != nil {
		return wrapStoreError(err)
	}
	if suppressed {
		return fmt.Errorf("%w: this evidence is suppressed", workspace.ErrInvalid)
	}
	return nil
}

func (s *Service) checkActionShape(ctx context.Context, action Action, world *planWorld) error {
	switch action.Kind {
	case PlanCreateFolder:
		return s.checkCreateFolder(ctx, action, world)
	case PlanAdd:
		if action.Ref.Kind != workspace.CollectionKind {
			return nil
		}
		cycle, err := s.wouldCycle(ctx, action.CollectionID, action.Ref.ID)
		if err != nil {
			return err
		}
		if cycle {
			return workspace.ErrCycle
		}
	}
	return nil
}

func (s *Service) checkCreateFolder(ctx context.Context, action Action, world *planWorld) error {
	if err := workspace.ValidateName(action.FolderName); err != nil {
		return err
	}
	fold := strings.ToLower(action.FolderName)
	if _, ok := world.names[fold]; ok {
		return fmt.Errorf("%w: equivalent folder %q already exists", workspace.ErrInvalid, action.FolderName)
	}
	if err := s.equivalentPurpose(ctx, action.Purpose); err != nil {
		return err
	}
	for _, parent := range action.ParentIDs {
		cycle, err := s.wouldCycle(ctx, parent, action.CollectionID)
		if err != nil {
			return err
		}
		if action.CollectionID != "" && cycle {
			return workspace.ErrCycle
		}
		if parent == action.FolderName || parent == action.CollectionID {
			return workspace.ErrCycle
		}
	}
	return nil
}

func (s *Service) equivalentPurpose(ctx context.Context, purpose string) error {
	purpose = strings.TrimSpace(purpose)
	if purpose == "" {
		return nil
	}
	collections, err := s.store.Collections(ctx)
	if err != nil {
		return wrapStoreError(err)
	}
	for _, collection := range collections {
		if strings.EqualFold(strings.TrimSpace(collection.Purpose), purpose) {
			return fmt.Errorf("%w: equivalent folder purpose already exists", workspace.ErrInvalid)
		}
	}
	return nil
}

func (s *Service) wouldCycle(ctx context.Context, parentID, childID string) (bool, error) {
	if childID == "" {
		return false, nil
	}
	if parentID == childID {
		return true, nil
	}
	walked := map[string]struct{}{}
	stack := []string{childID}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := walked[id]; ok {
			continue
		}
		walked[id] = struct{}{}
		if id == parentID {
			return true, nil
		}
		members, err := s.store.Members(ctx, id)
		if err != nil {
			if errors.Is(err, workspace.ErrNotFound) {
				continue
			}
			return false, wrapStoreError(err)
		}
		for _, member := range members {
			if member.Kind == workspace.CollectionKind {
				stack = append(stack, member.ID)
			}
		}
	}
	return false, nil
}

func (w *planWorld) has(id string) bool {
	_, ok := w.ids[id]
	return id != "" && ok
}

func (w *planWorld) pendingID(id string) bool {
	_, ok := w.pending[id]
	return ok
}

func (w *planWorld) guideRev(id string) int {
	return w.guide[id]
}

func (w *planWorld) note(action Action) {
	switch action.Kind {
	case PlanAdd, PlanRemove:
		w.bump(action.CollectionID)
	case PlanMove:
		w.bump(action.FromID)
		w.bump(action.ToID)
	case PlanCreateFolder:
		alias := action.CollectionID
		if alias == "" {
			alias = action.FolderName
		}
		w.ids[alias] = struct{}{}
		w.pending[alias] = struct{}{}
		w.names[strings.ToLower(action.FolderName)] = struct{}{}
		w.revs[alias] = 1
		w.root++
		for _, parent := range action.ParentIDs {
			w.bump(parent)
		}
	}
}

func (w *planWorld) bump(id string) {
	w.revs[id]++
	w.root++
}
