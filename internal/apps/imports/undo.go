package imports

import (
	"context"
	"errors"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func (s *Service) Undo(ctx context.Context, scope svc.Scope, params bridge.ImportUndoParams) (bridge.ImportUndoResult, error) {
	if err := s.ready(bridge.MethodImportsUndo); err != nil {
		return bridge.ImportUndoResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result := bridge.ImportUndoResult{KeptChanged: []string{}}
	if err := real(scope); err != nil {
		return result, err
	}
	r, err := s.load(scope, params.ImportID)
	if err != nil {
		return result, err
	}
	if r.Member != scope.Caller.Member && scope.Caller.Role != svc.RoleEditor {
		return result, bridge.Errorf(bridge.CodeDenied, "only the importer or an Editor can undo")
	}
	if r.Status == "preview" {
		return result, bridge.Errorf(bridge.CodeConflict, "import has not run")
	}
	// Children are created after their references and removed first. A changed
	// child keeps its referenced company too, because the records store refuses
	// to leave a relation pointing at a deleted record.
	for i := len(r.Created) - 1; i >= 0; i-- {
		created := &r.Created[i]
		if created.Removed {
			continue
		}
		row, err := s.all.Records.Get(ctx, scope, bridge.GetRecordParams{Type: created.Type, ID: created.ID})
		if hasCode(err, bridge.CodeNotFound) {
			created.Removed = true
			continue
		}
		if err != nil {
			return result, err
		}
		if row.Version != created.Version {
			result.KeptChanged = append(result.KeptChanged, row.ID)
			continue
		}
		_, err = s.all.Records.Delete(ctx, scope, bridge.DeleteRecordParams{Type: created.Type, ID: row.ID})
		if hasCode(err, bridge.CodeConflict) {
			result.KeptChanged = append(result.KeptChanged, row.ID)
			continue
		}
		if err != nil {
			return result, err
		}
		created.Removed = true
		result.Removed++
		if err := s.save(r); err != nil {
			return result, err
		}
	}
	r.Status = "undone"
	if err := s.save(r); err != nil {
		return result, err
	}
	if result.Removed > 0 {
		err = s.spaces.Append(ctx, ActivityEntry{Scope: scope, Action: "import.undo", Object: r.ID, Text: fmt.Sprintf("@%s undid an import", scope.Caller.Member), Detail: fmt.Sprintf("%d removed · %d kept", result.Removed, len(result.KeptChanged))})
	}
	return result, err
}
func hasCode(err error, code bridge.ErrorCode) bool {
	var wire *bridge.Error
	return errors.As(err, &wire) && wire.Code == code
}
