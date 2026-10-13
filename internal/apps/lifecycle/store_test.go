package lifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type definitionStore struct {
	svc.Store
	state           bridge.AppState
	getError        error
	definition      *spec.Bundle
	definitionError error
	calls           int
}

func (s *definitionStore) Get(context.Context, string) (bridge.AppDetail, error) {
	return bridge.AppDetail{AppSummary: bridge.AppSummary{State: s.state}}, s.getError
}

func (s *definitionStore) Definition(context.Context, string) (*spec.Bundle, error) {
	s.calls++
	return s.definition, s.definitionError
}

func TestWrapDefinitionPassthrough(t *testing.T) {
	definition := &spec.Bundle{}
	definitionError := errors.New("definition unavailable")
	for _, tc := range []struct {
		name            string
		state           bridge.AppState
		getError        error
		definitionError error
		deleted         bool
	}{
		{name: "kept", state: bridge.StateKept},
		{name: "archived", state: bridge.StateArchived},
		{name: "Get unavailable", getError: errors.New("detail unavailable")},
		{name: "Definition error", definitionError: definitionError},
		{name: "deleted", state: bridge.StateDeleted, deleted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := &definitionStore{state: tc.state, getError: tc.getError, definition: definition, definitionError: tc.definitionError}
			got, err := Wrap(base).Definition(context.Background(), "crm")
			if tc.deleted {
				requireCode(t, err, bridge.CodeNotFound)
				if got != nil || base.calls != 0 {
					t.Fatal("deleted app exposed its definition")
				}
				return
			}
			if got != definition || err != tc.definitionError || base.calls != 1 {
				t.Fatalf("Definition = %p, %v; calls = %d", got, err, base.calls)
			}
		})
	}
}
