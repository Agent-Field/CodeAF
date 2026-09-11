// Package ifaceimport hides the importer behind an interface of its own — the
// mocking idiom — so the call site resolves to the interface's method. The
// importer is a function, so the interface needs an adaptor, and the adaptor
// names the importer.
package ifaceimport

import (
	"context"

	"github.com/Agent-Field/aforge-v2/internal/direction"
)

type importer interface {
	Import(context.Context, direction.ImportRun, direction.ImportItem) (direction.ImportResult, error)
}

type adaptor struct{ s *direction.Store }

func (a adaptor) Import(ctx context.Context, run direction.ImportRun, item direction.ImportItem) (direction.ImportResult, error) {
	return direction.Import(ctx, a.s, run, item)
}

// Migrate imports one item through the interface.
func Migrate(ctx context.Context, s *direction.Store) (direction.ImportResult, error) {
	var imp importer = adaptor{s}
	return imp.Import(ctx, direction.ImportRun{ID: "r", Mode: "apply", Binary: "x"}, direction.ImportItem{})
}
