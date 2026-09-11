// Package exposes hands out the collections store and a transaction through
// shapes that are not a struct field or a plain signature: a defined slice, a
// defined function type, and an empty interface. Names and Count hand out
// nothing.
package exposes

import (
	"database/sql"

	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// Handles is a defined slice of stores.
type Handles []*workspace.Store

// Opener is a defined function type that returns a transaction.
type Opener func() (*sql.Tx, error)

var stores []*workspace.Store

// Door returns a store as an empty interface, and takes nothing that names one.
func Door(i int) any { return stores[i] }

// Names is a defined slice of nothing forbidden.
type Names []string

// Count returns nothing forbidden.
func Count() int { return 0 }
