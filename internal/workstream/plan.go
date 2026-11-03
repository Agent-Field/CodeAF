package workstream

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

func planIn(ctx context.Context, tx *sql.Tx, id string) (Plan, error) {
	p := Plan{WorkstreamID: id, Milestones: []Milestone{}, Waves: []Wave{}, UpdatedBy: "you"}
	var at int64
	var milestones, waves string
	err := tx.QueryRowContext(ctx, "SELECT version,updated_at,updated_by,milestones,waves FROM workstream_plans WHERE workstream_id=?", id).Scan(&p.Version, &at, &p.UpdatedBy, &milestones, &waves)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	p.UpdatedAt = instant(at)
	if err := json.Unmarshal([]byte(milestones), &p.Milestones); err != nil {
		return p, err
	}
	err = json.Unmarshal([]byte(waves), &p.Waves)
	return p, err
}
func (s *Store) Plan(ctx context.Context, id string) (Plan, error) {
	var p Plan
	err := s.workspace.Read(ctx, func(tx *sql.Tx, v int) error {
		if v < 3 {
			return ErrNotFound
		}
		if _, err := getIn(ctx, tx, id); err != nil {
			return err
		}
		var err error
		p, err = planIn(ctx, tx, id)
		return err
	})
	return p, err
}
func (s *Store) SetPlan(ctx context.Context, id string, base int, milestones []Milestone, waves []Wave, who Attribution) (Plan, Change, error) {
	by, err := actor(who.By)
	if err != nil {
		return Plan{}, Change{}, err
	}
	if base < 0 {
		return Plan{}, Change{}, invalid("base_version must be non-negative")
	}
	// Copy slices because assigned ids and normalized lists belong to the saved plan.
	milestones = append([]Milestone{}, milestones...)
	waves = append([]Wave{}, waves...)
	for i := range waves {
		waves[i].Items = append([]PlanItem{}, waves[i].Items...)
	}
	var out Plan
	var c Change
	err = s.workspace.Write(ctx, func(tx *sql.Tx) error {
		w, err := getIn(ctx, tx, id)
		if err != nil {
			return err
		}
		if !w.DoneAt.IsZero() {
			return ErrConflict
		}
		if len(milestones) > PlanMilestonesMax {
			return fmt.Errorf("%w: a plan holds at most %d milestones", ErrLimit, PlanMilestonesMax)
		}
		if len(waves) > PlanWavesMax {
			return fmt.Errorf("%w: a plan holds at most %d waves", ErrLimit, PlanWavesMax)
		}
		items := 0
		for _, wave := range waves {
			items += len(wave.Items)
			if items > PlanItemsMax {
				return fmt.Errorf("%w: a plan holds at most %d items", ErrLimit, PlanItemsMax)
			}
		}
		p, err := planIn(ctx, tx, id)
		if err != nil {
			return err
		}
		if base != p.Version {
			at := p.UpdatedAt
			if at.IsZero() {
				at = w.UpdatedAt
			}
			return &VersionConflict{ID: id, Version: p.Version, UpdatedAt: at, UpdatedBy: p.UpdatedBy}
		}
		ids := map[string]bool{}
		for i := range milestones {
			m := &milestones[i]
			if err := validTitle(m.Title); err != nil {
				return err
			}
			if m.ID == "" {
				m.ID, err = s.newID()
				if err != nil {
					return err
				}
			}
			if ids[m.ID] {
				return invalid("milestone ids must be unique")
			}
			ids[m.ID] = true
		}
		jobs := map[Job]bool{}
		for _, j := range w.Jobs() {
			jobs[j] = true
		}
		last := 0
		for _, wave := range waves {
			if wave.N <= last {
				return invalid("waves must be positive, unique and ascending")
			}
			last = wave.N
			if !lineWithin(wave.Title, 200) {
				return invalid("wave title must be at most 200 bytes without control characters")
			}
			if wave.MilestoneID != "" && !ids[wave.MilestoneID] {
				return invalid("wave milestone does not exist")
			}
			for _, item := range wave.Items {
				if err := workstreamDraftFields(item); err != nil {
					return err
				}
				switch item.Kind {
				case "job":
					if item.Job <= 0 || !jobs[Job{Factory: item.Factory, Number: item.Job}] {
						return invalid("plan job must be a member")
					}
				case "draft":
					if item.DraftID == "" || item.Title == "" {
						return invalid("draft needs draft_id and title")
					}
					if err := validTitle(item.Title); err != nil {
						return err
					}
					// Control characters stay invalid whatever the length.
					if !textWithin(item.Text, len(item.Text)) {
						return invalid("draft text allows only newline and tab control characters")
					}
					if len(item.Text) > PlanDraftTextMax {
						return fmt.Errorf("%w: draft text must be at most %d bytes", ErrLimit, PlanDraftTextMax)
					}
				default:
					return invalid("unknown plan item kind")
				}
			}
		}
		m, err := json.Marshal(milestones)
		if err != nil {
			return err
		}
		v, err := json.Marshal(waves)
		if err != nil {
			return err
		}
		if len(m)+len(v) > PlanEncodedMax {
			return fmt.Errorf("%w: a plan's milestones and waves must encode to at most %d bytes", ErrLimit, PlanEncodedMax)
		}
		at := s.now().UnixMilli()
		if _, err := tx.ExecContext(ctx, `INSERT INTO workstream_plans(workstream_id,version,updated_at,updated_by,milestones,waves) VALUES (?,?,?,?,?,?)
 ON CONFLICT(workstream_id) DO UPDATE SET version=excluded.version,updated_at=excluded.updated_at,updated_by=excluded.updated_by,milestones=excluded.milestones,waves=excluded.waves`, id, p.Version+1, at, by, string(m), string(v)); err != nil {
			return err
		}
		if err := touch(ctx, tx, id, at); err != nil {
			return err
		}
		out, err = planIn(ctx, tx, id)
		c = changed(id, PartPlan)
		return err
	})
	if err != nil {
		return Plan{}, Change{}, err
	}
	return out, c, nil
}

func workstreamDraftFields(item PlanItem) error {
	if item.Kind != "draft" && (item.Size != "" || item.BudgetUSD != 0 || item.Source != nil) {
		return invalid("size, budget_usd and source are allowed only on drafts")
	}
	if item.Size != "" && item.Size != "S" && item.Size != "M" && item.Size != "L" {
		return invalid("draft size must be S, M or L")
	}
	if math.IsNaN(item.BudgetUSD) || math.IsInf(item.BudgetUSD, 0) || item.BudgetUSD < 0 || item.BudgetUSD > workspace.WorkstreamGoAheadCapMax {
		return invalid(fmt.Sprintf("draft budget_usd must be greater than 0 and at most %d when set", workspace.WorkstreamGoAheadCapMax))
	}
	_, err := sourceJSON(item.Source)
	return err
}
