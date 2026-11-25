package main

import (
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/home"
)

// AppDefinitions uses the same home as the desktop engine so opening an app
// always reads the definitions belonging to that engine.
func (b *appBackend) AppDefinitions() *store.Store {
	return store.NewAt(home.Dir(), b.appBuildTypes)
}
