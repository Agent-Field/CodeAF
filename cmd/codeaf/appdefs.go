package main

import (
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
)

// AppDefinitions uses the same home and data writer as the engine, so a
// Keep, revert or deletion applies to already-open data calls.
func (b *appBackend) AppDefinitions() *store.Store {
	return store.NewAt(b.dataRoot(), nil).WithData(func() (*data.Store, error) {
		place, err := b.appData()
		if err != nil {
			return nil, err
		}
		return place.Main()
	})
}

func (b *appBackend) dataRoot() string {
	if b.data.home != "" {
		return b.data.home
	}
	return home.Dir()
}

// ResolvedAppRecords reads the shared types under the app's local aliases and
// grants so the Desk can use the same schema as its scoped record queries.
func (b *appBackend) ResolvedAppRecords(scope appwire.DataScope, records map[string]schema.RecordType) (map[string]schema.RecordType, error) {
	s, err := b.dataStore(scope.Draft)
	if err != nil {
		return nil, err
	}
	return s.SharedRecordShapes(dataScope(scope), records), nil
}
