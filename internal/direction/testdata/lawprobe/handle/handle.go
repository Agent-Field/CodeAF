// Package handle hands out an organization store, so a caller holds one
// without importing the package that defines it.
package handle

import "github.com/Agent-Field/aforge-v2/internal/workspace"

// Open opens a store.
func Open(path string) (*workspace.Store, error) { return workspace.Open(path) }
