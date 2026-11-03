package workstream

import (
	"context"
	"database/sql"
	"math"
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

const goaheadSelect = `SELECT id,workstream_id,scope,coalesce(factory,''),coalesce(job,0),coalesce(wave,0),coalesce(milestone_id,''),cap_usd,ends_at,state,coalesce(end_reason,''),granted_at,ended_at FROM workstream_goaheads WHERE id=?`

func goaheadIn(ctx context.Context, tx *sql.Tx, id string) (GoAhead, error) {
	var g GoAhead
	var ends, granted int64
	var ended sql.NullInt64
	err := tx.QueryRowContext(ctx, goaheadSelect, id).Scan(&g.ID, &g.WorkstreamID, &g.Scope, &g.Factory, &g.Job, &g.Wave, &g.MilestoneID, &g.CapUSD, &ends, &g.State, &g.EndReason, &granted, &ended)
	if err != nil {
		return g, found(err)
	}
	g.EndsAt, g.GrantedAt, g.EndedAt = instant(ends), instant(granted), optionalInstant(ended)
	return g, nil
}

// Expiry is derived on read, so listing never writes or emits a change.
func expire(g GoAhead, now time.Time) GoAhead {
	if g.State == "active" && !g.EndsAt.After(now) {
		g.State = "ended"
		g.EndReason = "expired"
		g.EndedAt = g.EndsAt
	}
	return g
}
func (s *Store) GoAheads(ctx context.Context, id string, ended bool) ([]GoAhead, error) {
	out := []GoAhead{}
	err := s.workspace.Read(ctx, func(tx *sql.Tx, v int) error {
		if v < 3 {
			return ErrNotFound
		}
		if _, err := getIn(ctx, tx, id); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, "SELECT id FROM workstream_goaheads WHERE workstream_id=? ORDER BY granted_at DESC,rowid DESC", id)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		now := s.now()
		for _, id := range ids {
			g, err := goaheadIn(ctx, tx, id)
			if err != nil {
				return err
			}
			g = expire(g, now)
			if ended || g.State == "active" {
				out = append(out, g)
			}
		}
		return nil
	})
	return out, err
}
func workstreamGoAheadFields(p GoAheadGrant, now time.Time) error {
	// Store millisecond precision in the validation too: an end within the same
	// stored millisecond as the grant is not a future authorization.
	ends := p.EndsAt
	if math.IsNaN(p.CapUSD) || math.IsInf(p.CapUSD, 0) || p.CapUSD <= 0 || p.CapUSD > workspace.WorkstreamGoAheadCapMax || ends.IsZero() || !ends.After(now) || ends.After(now.Add(workspace.WorkstreamGoAheadDurationMax)) || ends.UnixMilli() <= now.UnixMilli() {
		return invalid("go-ahead needs a cap greater than 0 and at most 10000, and a future end within 90 days")
	}
	switch p.Scope {
	case "job":
		if p.Job <= 0 || p.Wave != 0 || p.MilestoneID != "" {
			return invalid("job scope needs only factory and job")
		}
		if err := (workspace.Ref{Kind: workspace.FactoryJobKind, ID: strconv.Itoa(p.Job), Factory: p.Factory}).Validate(); err != nil {
			return err
		}
	case "wave":
		if p.Wave <= 0 || p.Factory != "" || p.Job != 0 || p.MilestoneID != "" {
			return invalid("wave scope needs only wave")
		}
	case "milestone":
		if p.MilestoneID == "" || p.Factory != "" || p.Job != 0 || p.Wave != 0 {
			return invalid("milestone scope needs only milestone_id")
		}
	default:
		return invalid("go-ahead needs a scope")
	}
	return nil
}

func workstreamGoAheadAllowedIn(ctx context.Context, tx *sql.Tx, p GoAheadGrant, at time.Time) error {
	w, err := getIn(ctx, tx, p.WorkstreamID)
	if err != nil {
		return err
	}
	if !w.DoneAt.IsZero() {
		return ErrConflict
	}
	if err := workstreamGoAheadFields(p, at); err != nil {
		return err
	}
	if p.Scope == "job" {
		for _, j := range w.Jobs() {
			if j.Factory == p.Factory && j.Number == p.Job {
				return nil
			}
		}
	} else {
		plan, err := planIn(ctx, tx, p.WorkstreamID)
		if err != nil {
			return err
		}
		if workstreamGoAheadInPlan(p, plan) {
			return nil
		}
	}
	return invalid("scope target is not a member or does not exist in the plan")
}

func workstreamGoAheadInPlan(p GoAheadGrant, plan Plan) bool {
	for _, wave := range plan.Waves {
		if p.Scope == "wave" && wave.N == p.Wave {
			return true
		}
	}
	for _, milestone := range plan.Milestones {
		if p.Scope == "milestone" && milestone.ID == p.MilestoneID {
			return true
		}
	}
	return false
}

// Both direct grants and kept proposals validate and insert under the writer.
func (s *Store) grantGoAheadIn(ctx context.Context, tx *sql.Tx, p GoAheadGrant, at time.Time) (GoAhead, error) {
	if err := workstreamGoAheadAllowedIn(ctx, tx, p, at); err != nil {
		return GoAhead{}, err
	}
	id, err := s.newID()
	if err != nil {
		return GoAhead{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO workstream_goaheads(id,workstream_id,scope,factory,job,wave,milestone_id,cap_usd,ends_at,state,granted_at) VALUES (?,?,?,?,?,?,?,?,?,'active',?)`, id, p.WorkstreamID, p.Scope, nullString(p.Factory), nullInt(p.Job), nullInt(p.Wave), nullString(p.MilestoneID), p.CapUSD, p.EndsAt.UnixMilli(), at.UnixMilli()); err != nil {
		return GoAhead{}, err
	}
	return goaheadIn(ctx, tx, id)
}

func (s *Store) GrantGoAhead(ctx context.Context, p GoAheadGrant) (GoAhead, Change, error) {
	if err := workstreamGoAheadFields(p, s.now()); err != nil {
		return GoAhead{}, Change{}, err
	}
	var out GoAhead
	var c Change
	err := s.workspace.Write(ctx, func(tx *sql.Tx) error {
		at := s.now()
		var err error
		out, err = s.grantGoAheadIn(ctx, tx, p, at)
		if err != nil {
			return err
		}
		if err := touch(ctx, tx, p.WorkstreamID, at.UnixMilli()); err != nil {
			return err
		}
		c = changed(p.WorkstreamID, PartGoAheads)
		return nil
	})
	if err != nil {
		return GoAhead{}, Change{}, err
	}
	return out, c, nil
}

func revokeGoAheadIn(ctx context.Context, tx *sql.Tx, id string, now time.Time) (GoAhead, bool, error) {
	g, err := goaheadIn(ctx, tx, id)
	if err != nil {
		return GoAhead{}, false, err
	}
	g = expire(g, now)
	if g.State == "ended" {
		return g, false, nil
	}
	if _, err := tx.ExecContext(ctx, "UPDATE workstream_goaheads SET state='ended',end_reason='revoked',ended_at=? WHERE id=?", now.UnixMilli(), id); err != nil {
		return GoAhead{}, false, err
	}
	g, err = goaheadIn(ctx, tx, id)
	return g, true, err
}

func (s *Store) RevokeGoAhead(ctx context.Context, id string) (GoAhead, Change, error) {
	var out GoAhead
	var c Change
	err := s.workspace.Write(ctx, func(tx *sql.Tx) error {
		now := s.now()
		g, ended, err := revokeGoAheadIn(ctx, tx, id, now)
		if err != nil {
			return err
		}
		out = g
		if !ended {
			return nil
		}
		at := now.UnixMilli()
		if err := touch(ctx, tx, g.WorkstreamID, at); err != nil {
			return err
		}
		c = changed(g.WorkstreamID, PartGoAheads)
		return nil
	})
	if err != nil {
		return GoAhead{}, Change{}, err
	}
	return out, c, nil
}
