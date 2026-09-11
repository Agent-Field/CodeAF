// Package caller writes the collections store's raw transaction on a store a
// helper handed it, by call and by function value.
package caller

import (
	"context"
	"database/sql"

	"github.com/Agent-Field/aforge-v2/internal/direction/testdata/lawprobe/handle"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// Forge stamps every revision as the person's.
func Forge(ctx context.Context, path string) error {
	s, err := handle.Open(path)
	if err != nil {
		return err
	}
	write := workspace.WriteImmediate
	_ = write
	return workspace.WriteImmediate(ctx, s, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE direction_revisions SET receipt_actor='person'")
		return err
	})
}
