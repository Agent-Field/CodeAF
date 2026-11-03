package workstream

import (
	"context"
	"database/sql"
	"errors"
)

var ErrNotAgentPage = errors.New("the agent writes only its own pages")

// ByAgent resolves the owner at call time because its conversation opens first.
func (s *Store) ByAgent(ctx context.Context, agentID string) (Workstream, error) {
	var out Workstream
	if agentID == "" {
		return out, ErrNotFound
	}
	err := s.workspace.Read(ctx, func(tx *sql.Tx, version int) error {
		if version < 3 {
			return ErrNotFound
		}
		var id string
		if err := tx.QueryRowContext(ctx, "SELECT id FROM workstreams WHERE agent_id=?", agentID).Scan(&id); err != nil {
			return found(err)
		}
		var err error
		out, err = getIn(ctx, tx, id)
		return err
	})
	return out, err
}
