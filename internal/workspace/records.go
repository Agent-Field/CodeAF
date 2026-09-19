package workspace

import (
	"context"
	"database/sql"
	"errors"
)

func (s *Store) PutObservation(ctx context.Context, o Observation) (Observation, error) {
	if err := s.writeReady(ctx); err != nil {
		return Observation{}, err
	}
	if o.ID == "" {
		id, err := mintID()
		if err != nil {
			return Observation{}, storeError(err)
		}
		o.ID = id
	}
	o.CreatedAt = s.stamp()
	_, err := s.db.ExecContext(ctx, `INSERT INTO observations(id,chat_id,source_rev,purpose,body,evidence,model,prompt_version,created_at)
 VALUES (?,?,?,?,?,?,?,?,?)`,
		o.ID, o.ChatID, o.SourceRev, o.Purpose, o.Body, o.Evidence, o.Model, o.PromptVersion, o.CreatedAt)
	if err != nil {
		return Observation{}, storeError(err)
	}
	return o, nil
}

func (s *Store) PutProposal(ctx context.Context, p Proposal) (Proposal, error) {
	if err := s.writeReady(ctx); err != nil {
		return Proposal{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Proposal{}, storeError(err)
	}
	defer tx.Rollback()
	if p.IdempotencyKey != "" {
		existing, err := lookupProposal(ctx, tx, p.IdempotencyKey)
		if err == nil {
			return existing, storeError(tx.Commit())
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Proposal{}, storeError(err)
		}
	}
	if p.ID == "" {
		id, err := mintID()
		if err != nil {
			return Proposal{}, storeError(err)
		}
		p.ID = id
	}
	p.CreatedAt = s.stamp()
	if err := insertProposal(ctx, tx, p); err != nil {
		if p.IdempotencyKey != "" && isUniqueConstraint(err) {
			existing, lookupErr := lookupProposal(ctx, tx, p.IdempotencyKey)
			if lookupErr == nil {
				return existing, storeError(tx.Commit())
			}
		}
		return Proposal{}, storeError(err)
	}
	return p, storeError(tx.Commit())
}

func lookupProposal(ctx context.Context, tx *sql.Tx, key string) (Proposal, error) {
	var p Proposal
	err := tx.QueryRowContext(ctx, `SELECT id,chat_id,source_rev,plan_json,result,idempotency_key,created_at
 FROM proposed_actions WHERE idempotency_key=? LIMIT 1`, key).Scan(
		&p.ID, &p.ChatID, &p.SourceRev, &p.PlanJSON, &p.Result, &p.IdempotencyKey, &p.CreatedAt)
	return p, err
}

func insertProposal(ctx context.Context, tx *sql.Tx, p Proposal) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO proposed_actions(id,chat_id,source_rev,plan_json,result,idempotency_key,created_at)
 VALUES (?,?,?,?,?,?,?)`, p.ID, p.ChatID, p.SourceRev, p.PlanJSON, p.Result, p.IdempotencyKey, p.CreatedAt)
	return err
}
