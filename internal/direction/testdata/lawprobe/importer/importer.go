// Package importer calls the migration entry point from a site no reviewer
// named.
package importer

import (
	"context"

	"github.com/Agent-Field/aforge-v2/internal/direction"
)

// Migrate imports one item.
func Migrate(ctx context.Context, s *direction.Store) (direction.ImportResult, error) {
	return direction.Import(ctx, s, direction.ImportRun{ID: "r", Mode: "apply", Binary: "x"}, direction.ImportItem{})
}
