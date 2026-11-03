package workstream

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

const (
	PartWakes            ChangedPart = "wakes"
	WakeWorkstreamDayMax             = 24
	WakeHomeDayMax                   = 200
	WakeAbandonedAfter               = 15 * time.Minute
	WakeStoppedError                 = "Stopped before it finished."
	WakeAnswerError                  = "The agent's answer could not be read."
)

var ErrWakeCap = errors.New("workstream wake daily cap reached")

type WakeEvent struct {
	Kind    string    `json:"kind"`
	Factory string    `json:"factory,omitempty"`
	Job     int       `json:"job,omitempty"`
	Label   string    `json:"label,omitempty"`
	Detail  string    `json:"detail,omitempty"`
	At      time.Time `json:"at"`
}

type WakeItem struct {
	ID        string
	Kind      string
	State     string
	Text      string
	Why       string
	Factory   string
	Job       int
	DraftID   string
	Wave      int
	PageID    string
	RecordID  string
	BudgetUSD *float64
	Note      string
	AppliedAt time.Time
	UndoneAt  time.Time
	Proposal  WakeProposal
	Undo      workstreamWakeUndo
}

type WakeRun struct {
	ID           string
	WorkstreamID string
	Event        WakeEvent
	State        string
	StartedAt    time.Time
	SettledAt    time.Time
	CostUSD      *float64
	Summary      string
	Error        string
	DismissedAt  time.Time
	Items        []WakeItem
}

type workstreamWakeUndo struct {
	Wave             int
	Position         int
	Version          int
	PreviousVersion  int
	Created          bool
	CreatedWave      bool
	CreatedWaveImage *Wave
	PlanItem         PlanItem
	Page             *PageNode
}

// BeginWake claims the event before any model call, under the stage and cost guards.
// A losing claimant returns false, including another process using the same home.
func (s *Store) BeginWake(ctx context.Context, id, key string, event WakeEvent) (WakeRun, bool, error) {
	var out WakeRun
	if key == "" || !lineWithin(event.Label, 200) || !textWithin(event.Detail, 4000) {
		return out, false, invalid("invalid wake event")
	}
	switch event.Kind {
	case "morning", "finished", "merged", "failed", "needs_you":
	default:
		return out, false, invalid("unknown wake kind")
	}
	woke := false
	err := s.workspace.Write(ctx, func(tx *sql.Tx) error {
		if event.Kind != "morning" {
			if event.Factory == "" || event.Job <= 0 {
				return invalid("job event needs factory and positive job")
			}
			var owner string
			err := tx.QueryRowContext(ctx, "SELECT workstream_id FROM workstream_jobs WHERE factory=? AND job=?", event.Factory, strconv.Itoa(event.Job)).Scan(&owner)
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			if id != "" && id != owner {
				return nil
			}
			id = owner
		}
		w, err := getIn(ctx, tx, id)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if w.Stage == "done" || w.Autonomy == "off" {
			return nil
		}
		if event.Kind == "morning" && (!w.Wake.Morning || event.At.Before(w.CreatedAt)) {
			return nil
		}
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM workstream_wakes WHERE workstream_id=? AND event_key=?)", id, key).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return nil
		}
		now := s.now()
		local := now.In(time.Local)
		day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
		var home, stream int
		if err := tx.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(workstream_id=?),0) FROM workstream_wakes WHERE started_at>=? AND started_at<?", id, day.UnixMilli(), day.AddDate(0, 0, 1).UnixMilli()).Scan(&home, &stream); err != nil {
			return err
		}
		if home >= WakeHomeDayMax {
			return fmt.Errorf("%w: at most %d per home per local day", ErrWakeCap, WakeHomeDayMax)
		}
		if stream >= WakeWorkstreamDayMax {
			return fmt.Errorf("%w: at most %d per local day for %s", ErrWakeCap, WakeWorkstreamDayMax, id)
		}
		wakeID, err := s.newID()
		if err != nil {
			return err
		}
		raw, err := json.Marshal(event)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO workstream_wakes(id,workstream_id,event_key,event_json,state,started_at) VALUES (?,?,?,?,'running',?)", wakeID, id, key, string(raw), now.UnixMilli()); err != nil {
			return err
		}
		out = WakeRun{ID: wakeID, WorkstreamID: id, Event: event, State: "running", StartedAt: instant(now.UnixMilli()), Items: []WakeItem{}}
		woke = true
		return nil
	})
	return out, woke, err
}

func workstreamWakeIn(ctx context.Context, tx *sql.Tx, id string) (WakeRun, error) {
	out := WakeRun{Items: []WakeItem{}}
	var raw string
	var started int64
	var settled, dismissed sql.NullInt64
	var cost sql.NullFloat64
	err := tx.QueryRowContext(ctx, "SELECT id,workstream_id,event_json,state,summary,error,cost_usd,started_at,settled_at,dismissed_at FROM workstream_wakes WHERE id=?", id).Scan(&out.ID, &out.WorkstreamID, &raw, &out.State, &out.Summary, &out.Error, &cost, &started, &settled, &dismissed)
	if err != nil {
		return out, found(err)
	}
	if err := json.Unmarshal([]byte(raw), &out.Event); err != nil {
		return out, err
	}
	out.StartedAt, out.SettledAt, out.DismissedAt = instant(started), optionalInstant(settled), optionalInstant(dismissed)
	if cost.Valid {
		out.CostUSD = &cost.Float64
	}
	rows, err := tx.QueryContext(ctx, "SELECT item_json FROM workstream_wake_items WHERE wake_id=? ORDER BY position", id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return out, err
		}
		var item WakeItem
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	return out, rows.Err()
}

func workstreamSaveWakeItemIn(ctx context.Context, tx *sql.Tx, wake string, pos int, item WakeItem) error {
	raw, err := json.Marshal(item)
	if err != nil {
		return err
	}
	undo, err := json.Marshal(item.Undo)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO workstream_wake_items(id,wake_id,position,kind,state,why,item_json,undo_json,note,applied_at,undone_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(id) DO UPDATE SET position=excluded.position,state=excluded.state,item_json=excluded.item_json,undo_json=excluded.undo_json,note=excluded.note,applied_at=excluded.applied_at,undone_at=excluded.undone_at`, item.ID, wake, pos, item.Kind, item.State, item.Why, string(raw), string(undo), item.Note, workstreamWakeTime(item.AppliedAt), workstreamWakeTime(item.UndoneAt))
	return err
}
func workstreamWakeTime(at time.Time) any {
	if at.IsZero() {
		return nil
	}
	return at.UnixMilli()
}

// WakeRun lets the worker recheck a durable claim after waiting in its queue.
func (s *Store) WakeRun(ctx context.Context, id string) (WakeRun, error) {
	var out WakeRun
	err := s.workspace.Read(ctx, func(tx *sql.Tx, v int) error {
		if v < workspace.WorkstreamWakeSchemaVersion {
			return ErrNotFound
		}
		var err error
		out, err = workstreamWakeIn(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Store) Wakes(ctx context.Context, id string, limit int) ([]WakeRun, error) {
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 100 {
		return nil, invalid("wake limit must be 1–100")
	}
	out := []WakeRun{}
	err := s.workspace.Read(ctx, func(tx *sql.Tx, v int) error {
		if v < 3 {
			return ErrNotFound
		}
		if _, err := getIn(ctx, tx, id); err != nil {
			return err
		}
		if v < workspace.WorkstreamWakeSchemaVersion {
			return nil
		}
		rows, err := tx.QueryContext(ctx, "SELECT id FROM workstream_wakes WHERE workstream_id=? ORDER BY started_at DESC,rowid DESC LIMIT ?", id, limit)
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
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, id := range ids {
			w, err := workstreamWakeIn(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, w)
		}
		return nil
	})
	return out, err
}

// WakeSpendByAgent preserves unknown conversation spend at the adapter boundary.
func (s *Store) WakeSpendByAgent(ctx context.Context, agent string) (float64, error) {
	var cost float64
	err := s.workspace.Read(ctx, func(tx *sql.Tx, v int) error {
		if v < workspace.WorkstreamWakeSchemaVersion {
			return nil
		}
		return tx.QueryRowContext(ctx, "SELECT coalesce(sum(r.cost_usd),0) FROM workstream_wakes r JOIN workstreams w ON w.id=r.workstream_id WHERE w.agent_id=?", agent).Scan(&cost)
	})
	return cost, err
}

// ExpireWakes does not take a writer or migrate a schema-3 file unless a stale row exists.
func (s *Store) ExpireWakes(ctx context.Context) ([]Change, error) {
	ids := []string{}
	cutoff := s.now().Add(-WakeAbandonedAfter).UnixMilli()
	err := s.workspace.Read(ctx, func(tx *sql.Tx, v int) error {
		if v < workspace.WorkstreamWakeSchemaVersion {
			return nil
		}
		rows, err := tx.QueryContext(ctx, "SELECT DISTINCT workstream_id FROM workstream_wakes WHERE state='running' AND started_at<?", cutoff)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	changes := []Change{}
	err = s.workspace.Write(ctx, func(tx *sql.Tx) error {
		for _, id := range ids {
			result, err := tx.ExecContext(ctx, "UPDATE workstream_wakes SET state='failed',error=?,cost_usd=NULL,settled_at=? WHERE workstream_id=? AND state='running' AND started_at<?", WakeStoppedError, s.now().UnixMilli(), id, cutoff)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n > 0 {
				if err := touch(ctx, tx, id, s.now().UnixMilli()); err != nil {
					return err
				}
				changes = append(changes, changed(id, PartWakes))
			}
		}
		return nil
	})
	return changes, err
}

func workstreamWakeChange(id string, parts map[ChangedPart]bool) Change {
	c := Change{ID: id, Parts: []ChangedPart{PartWakes}}
	for _, p := range []ChangedPart{PartWorkstream, PartPlan, PartPages, PartRecords} {
		if parts[p] {
			c.Parts = append(c.Parts, p)
		}
	}
	return c
}

func workstreamWakeCost(cost *float64) *float64 {
	if cost == nil || math.IsNaN(*cost) || math.IsInf(*cost, 0) || *cost < 0 {
		return nil
	}
	return cost
}

func (s *Store) SettleWake(ctx context.Context, id string, answer WakeAnswer, cost *float64, failure string) (WakeRun, Change, error) {
	var out WakeRun
	var change Change
	err := s.workspace.Write(ctx, func(tx *sql.Tx) error {
		run, err := workstreamWakeIn(ctx, tx, id)
		if err != nil {
			return err
		}
		out = run
		if run.State != "running" {
			return nil
		}
		w, err := getIn(ctx, tx, run.WorkstreamID)
		if err != nil {
			return err
		}
		parts := map[ChangedPart]bool{PartWorkstream: true}
		if failure == "" {
			pageTargets := map[string]bool{}
			for _, p := range answer.Items {
				p = workstreamWakeProposal(p)
				if len(run.Items) == WakeItemsMax {
					break
				}
				if !workstreamWakeValid(p) {
					continue
				}
				if p.Kind == "page" && p.PageID != "" {
					page, _, err := livePageIn(ctx, tx, p.PageID)
					if errors.Is(err, ErrNotFound) {
						continue
					}
					if err != nil {
						return err
					}
					if page.WorkstreamID != w.ID || page.Owner != "agent" {
						continue
					}
					p.Title = page.Title
				}
				if p.Kind == "page" {
					target := "title:" + p.Title
					if p.PageID != "" {
						target = "id:" + p.PageID
					}
					if pageTargets[target] {
						continue
					}
					pageTargets[target] = true
				}
				itemID, err := s.newID()
				if err != nil {
					return err
				}
				item := WakeItem{ID: itemID, Kind: p.Kind, State: "proposed", Why: p.Why, Factory: p.Factory, Job: p.Job, DraftID: p.DraftID, Wave: p.Wave, PageID: p.PageID, BudgetUSD: p.BudgetUSD, Proposal: p}
				if p.Kind == "move" {
					item.Wave = p.ToWave
				}
				if w.Autonomy == "small" && w.Stage != "done" {
					ok, err := s.workstreamWakeSmallIn(ctx, tx, w, item)
					if err != nil {
						return err
					}
					if ok {
						part, err := s.workstreamWakeApplyIn(ctx, tx, w, run.Event, &item)
						if err != nil {
							return err
						}
						parts[part] = true
					}
				}
				item.Text = workstreamWakeItemText(item)
				run.Items = append(run.Items, item)
			}
			sort.SliceStable(run.Items, func(i, j int) bool { return run.Items[i].State == "applied" && run.Items[j].State != "applied" })
			for i, item := range run.Items {
				if err := workstreamSaveWakeItemIn(ctx, tx, id, i, item); err != nil {
					return err
				}
			}
		}
		run.State = "settled"
		if failure != "" {
			run.State = "failed"
		}
		run.Summary = workstreamWakeClip(answer.Summary, 2000)
		run.Error = failure
		run.CostUSD = workstreamWakeCost(cost)
		run.SettledAt = instant(s.now().UnixMilli())
		if _, err := tx.ExecContext(ctx, "UPDATE workstream_wakes SET state=?,summary=?,error=?,cost_usd=?,settled_at=? WHERE id=?", run.State, run.Summary, run.Error, run.CostUSD, run.SettledAt.UnixMilli(), id); err != nil {
			return err
		}
		if err := touch(ctx, tx, w.ID, s.now().UnixMilli()); err != nil {
			return err
		}
		out = run
		change = workstreamWakeChange(w.ID, parts)
		return nil
	})
	if err != nil {
		return WakeRun{}, Change{}, err
	}
	return out, change, nil
}

func workstreamWakeItemText(item WakeItem) string {
	applied := item.State == "applied"
	switch item.Kind {
	case "move":
		if !applied {
			return "Move the plan item to the proposed wave."
		}
		target := item.DraftID
		if item.Factory != "" {
			target = fmt.Sprintf("%s#%d", item.Factory, item.Job)
		}
		verb := "Move"
		if applied {
			verb = "Moved"
		}
		return fmt.Sprintf("%s %s to wave %d.", verb, target, item.Wave)
	case "draft":
		verb := "Add draft: "
		if applied {
			verb = "Added draft: "
		}
		return verb + item.Proposal.Title + "."
	case "page":
		verb := "Update agent page: "
		if applied {
			verb = "Updated agent page: "
		}
		return verb + item.Proposal.Title + "."
	default:
		verb := "Keep "
		if applied {
			verb = "Kept "
		}
		return verb + item.Kind + ": " + item.Proposal.Text
	}
}
