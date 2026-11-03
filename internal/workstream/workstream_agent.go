package workstream

import (
	"context"
	"database/sql"
	"errors"
)

var ErrNotAgentPage = errors.New("the agent writes only its own pages")

var ErrDone = errors.New("the workstream is done")

// workstreamWriteAllowed checks done in the transaction that owns the write.
func workstreamWriteAllowed(ctx context.Context, tx *sql.Tx, id, by string) error {
	w, err := getIn(ctx, tx, id)
	if err != nil {
		return err
	}
	if by == "agent" && !w.DoneAt.IsZero() {
		return ErrDone
	}
	return nil
}

func workstreamPageWriteAllowed(ctx context.Context, tx *sql.Tx, p Page, by string) error {
	if by != "agent" {
		return nil
	}
	if err := workstreamWriteAllowed(ctx, tx, p.WorkstreamID, by); err != nil {
		return err
	}
	if p.Owner != "agent" {
		return ErrNotAgentPage
	}
	return nil
}

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
