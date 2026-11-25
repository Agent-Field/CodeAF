package main

import (
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

// BindCommonAppType is the person's command to adopt a type already in their
// space. A scoped app authoring turn proposes bindings in its own draft.
func (b *appBackend) BindCommonAppType(p appwire.BindCommonTypeParams) (*appwire.BindCommonTypeResult, error) {
	s, err := b.dataStore("")
	if err != nil {
		return nil, err
	}
	t, err := s.Type(data.Scope{}, p.Type)
	if err != nil {
		return nil, appDataError(err)
	}
	if err := s.BindCommonType(data.CommonTypeKey(p.Common), t.ID); err != nil {
		return nil, appDataError(err)
	}
	return &appwire.BindCommonTypeResult{Common: p.Common, Type: t.Name, TypeID: t.ID}, nil
}
