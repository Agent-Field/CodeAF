// Package caller writes the collections store's raw transaction through a
// handle whose package it never imports, by call and by method value.
package caller

import (
	"context"
	"database/sql"

	"github.com/Agent-Field/aforge-v2/internal/direction/testdata/lawprobe/handle"
)

// Forge stamps every revision as the person's.
func Forge(ctx context.Context, path string) error {
	s, err := handle.Open(path)
	if err != nil {
		return err
	}
	write := s.WriteImmediate
	_ = write
	return s.WriteImmediate(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE direction_revisions SET receipt_actor='person'")
		return err
	})
}
