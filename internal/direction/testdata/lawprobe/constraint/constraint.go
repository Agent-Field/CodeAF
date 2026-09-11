// Package constraint hides the importer behind a type parameter, so the call
// site resolves to the parameter, not the importer. The importer is a
// function, so the parameter is instantiated with it, and that names it.
package constraint

import (
	"context"

	"github.com/Agent-Field/aforge-v2/internal/direction"
)

func run[F ~func(context.Context, *direction.Store, direction.ImportRun, direction.ImportItem) (direction.ImportResult, error)](
	ctx context.Context, s *direction.Store, imp F) (direction.ImportResult, error) {
	return imp(ctx, s, direction.ImportRun{ID: "r", Mode: "apply", Binary: "x"}, direction.ImportItem{})
}

// Migrate imports one item through the type parameter.
func Migrate(ctx context.Context, s *direction.Store) (direction.ImportResult, error) {
	return run(ctx, s, direction.Import)
}
