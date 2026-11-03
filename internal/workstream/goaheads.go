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
func (s *Store) GrantGoAhead(ctx context.Context, p GoAheadGrant) (GoAhead, Change, error) {
	// Store millisecond precision in the validation too: an end within the same
	// stored millisecond as the grant is not a future authorization.
	now := s.now()
	ends := p.EndsAt
	if math.IsNaN(p.CapUSD) || math.IsInf(p.CapUSD, 0) || p.CapUSD <= 0 || p.CapUSD > workspace.WorkstreamGoAheadCapMax || ends.IsZero() || !ends.After(now) || ends.After(now.Add(workspace.WorkstreamGoAheadDurationMax)) || ends.UnixMilli() <= now.UnixMilli() {
		return GoAhead{}, Change{}, invalid("go-ahead needs a cap greater than 0 and at most 10000, and a future end within 90 days")
	}
	switch p.Scope {
	case "job":
		if p.Job <= 0 || p.Wave != 0 || p.MilestoneID != "" {
			return GoAhead{}, Change{}, invalid("job scope needs only factory and job")
		}
		if err := (workspace.Ref{Kind: workspace.FactoryJobKind, ID: strconv.Itoa(p.Job), Factory: p.Factory}).Validate(); err != nil {
			return GoAhead{}, Change{}, err
		}
	case "wave":
		if p.Wave <= 0 || p.Factory != "" || p.Job != 0 || p.MilestoneID != "" {
			return GoAhead{}, Change{}, invalid("wave scope needs only wave")
		}
	case "milestone":
		if p.MilestoneID == "" || p.Factory != "" || p.Job != 0 || p.Wave != 0 {
			return GoAhead{}, Change{}, invalid("milestone scope needs only milestone_id")
		}
	default:
		return GoAhead{}, Change{}, invalid("go-ahead needs a scope")
	}
	var out GoAhead
	var c Change
	err := s.workspace.Write(ctx, func(tx *sql.Tx) error {
		w, err := getIn(ctx, tx, p.WorkstreamID)
		if err != nil {
			return err
		}
		if !w.DoneAt.IsZero() {
			return ErrConflict
		}
		// Recheck after waiting for the writer, so an expired grant cannot sneak in.
		at := s.now()
		if p.EndsAt.UnixMilli() <= at.UnixMilli() || p.EndsAt.After(at.Add(workspace.WorkstreamGoAheadDurationMax)) {
			return invalid("go-ahead end must be future and within 90 days")
		}
		valid := false
		switch p.Scope {
		case "job":
			for _, j := range w.Jobs() {
				if j.Factory == p.Factory && j.Number == p.Job {
					valid = true
					break
				}
			}
		case "wave", "milestone":
			plan, err := planIn(ctx, tx, p.WorkstreamID)
			if err != nil {
				return err
			}
			if p.Scope == "wave" {
				for _, v := range plan.Waves {
					if v.N == p.Wave {
						valid = true
						break
					}
				}
			} else {
				for _, v := range plan.Milestones {
					if v.ID == p.MilestoneID {
						valid = true
						break
					}
				}
			}
		}
		if !valid {
			return invalid("scope target is not a member or does not exist in the plan")
		}
		id, err := s.newID()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO workstream_goaheads(id,workstream_id,scope,factory,job,wave,milestone_id,cap_usd,ends_at,state,granted_at) VALUES (?,?,?,?,?,?,?,?,?,'active',?)`, id, p.WorkstreamID, p.Scope, nullString(p.Factory), nullInt(p.Job), nullInt(p.Wave), nullString(p.MilestoneID), p.CapUSD, p.EndsAt.UnixMilli(), at.UnixMilli()); err != nil {
			return err
		}
		if err := touch(ctx, tx, p.WorkstreamID, at.UnixMilli()); err != nil {
			return err
		}
		out, err = goaheadIn(ctx, tx, id)
		c = changed(p.WorkstreamID, PartGoAheads)
		return err
	})
	if err != nil {
		return GoAhead{}, Change{}, err
	}
	return out, c, nil
}
func (s *Store) RevokeGoAhead(ctx context.Context, id string) (GoAhead, Change, error) {
	var out GoAhead
	var c Change
	err := s.workspace.Write(ctx, func(tx *sql.Tx) error {
		g, err := goaheadIn(ctx, tx, id)
		if err != nil {
			return err
		}
		now := s.now()
		g = expire(g, now)
		if g.State == "ended" {
			out = g
			return nil
		}
		at := now.UnixMilli()
		if _, err := tx.ExecContext(ctx, "UPDATE workstream_goaheads SET state='ended',end_reason='revoked',ended_at=? WHERE id=?", at, id); err != nil {
			return err
		}
		if err := touch(ctx, tx, g.WorkstreamID, at); err != nil {
			return err
		}
		out, err = goaheadIn(ctx, tx, id)
		c = changed(g.WorkstreamID, PartGoAheads)
		return err
	})
	if err != nil {
		return GoAhead{}, Change{}, err
	}
	return out, c, nil
}
