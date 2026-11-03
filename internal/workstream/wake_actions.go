package workstream

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/workstream/pagedoc"
)

func workstreamWakeWave(p Plan, n int) int {
	if n == 0 && len(p.Waves) > 0 {
		return len(p.Waves) - 1
	}
	for i, w := range p.Waves {
		if w.N == n {
			return i
		}
	}
	return -1
}
func workstreamWakeTarget(p Plan, item WakeItem) (int, int) {
	for wi, w := range p.Waves {
		for pos, v := range w.Items {
			if item.Factory != "" && v.Kind == "job" && v.Factory == item.Factory && v.Job == item.Job {
				return wi, pos
			}
			if item.DraftID != "" && v.Kind == "draft" && v.DraftID == item.DraftID {
				return wi, pos
			}
		}
	}
	return -1, -1
}
func (s *Store) workstreamWakeSmallIn(ctx context.Context, tx *sql.Tx, w Workstream, item WakeItem) (bool, error) {
	switch item.Kind {
	case "page":
		return true, nil
	case "draft", "move":
		p, err := planIn(ctx, tx, w.ID)
		if err != nil {
			return false, err
		}
		target := workstreamWakeWave(p, item.Wave)
		if item.Kind == "draft" {
			budget := item.BudgetUSD
			if budget == nil || *budget <= 0 || *budget >= 5 {
				return false, nil
			}
			if len(p.Waves) == 0 {
				return item.Wave == 0 || item.Wave == 1, nil
			}
			return target >= 0 && !p.Waves[target].AutoStart, nil
		}
		from, _ := workstreamWakeTarget(p, item)
		if target < 0 || from < 0 || target == from || p.Waves[target].AutoStart {
			return false, nil
		}
		var granted bool
		err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM workstream_goaheads WHERE workstream_id=? AND scope='wave' AND wave=? AND state='active' AND ends_at>?)", w.ID, item.Wave, s.now().UnixMilli()).Scan(&granted)
		return !granted, err
	}
	return false, nil
}

func workstreamWakeSource(w Workstream, event WakeEvent) *Source {
	for _, job := range w.Jobs() {
		if event.Kind != "morning" && job.Factory == event.Factory && job.Number == event.Job {
			return &Source{Kind: "factory_job", Factory: job.Factory, ID: strconv.Itoa(job.Number), Label: event.Label}
		}
	}
	return &Source{Kind: "chat", ID: w.AgentID, Label: event.Label}
}

func (s *Store) workstreamWakeApplyIn(ctx context.Context, tx *sql.Tx, w Workstream, event WakeEvent, item *WakeItem) (ChangedPart, error) {
	var part ChangedPart
	var err error
	switch item.Kind {
	case "move", "draft":
		part = PartPlan
		err = s.workstreamWakePlanApplyIn(ctx, tx, w, event, item)
	case "page":
		part = PartPages
		err = s.workstreamWakePageApplyIn(ctx, tx, w, event, item)
	case "decision", "question":
		part = PartRecords
		err = s.workstreamWakeRecordApplyIn(ctx, tx, w, event, item)
	default:
		return "", ErrConflict
	}
	if err != nil {
		return "", err
	}
	item.State = "applied"
	item.Note = ""
	item.AppliedAt = instant(s.now().UnixMilli())
	item.Text = workstreamWakeItemText(*item)
	return part, nil
}

func (s *Store) workstreamWakeSavePlanIn(ctx context.Context, tx *sql.Tx, p Plan) error {
	if len(p.Waves) > PlanWavesMax {
		return ErrLimit
	}
	count := 0
	for _, w := range p.Waves {
		count += len(w.Items)
	}
	if count > PlanItemsMax {
		return ErrLimit
	}
	milestones, err := json.Marshal(p.Milestones)
	if err != nil {
		return err
	}
	waves, err := json.Marshal(p.Waves)
	if err != nil {
		return err
	}
	if len(milestones)+len(waves) > PlanEncodedMax {
		return ErrLimit
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO workstream_plans(workstream_id,version,updated_at,updated_by,milestones,waves) VALUES (?,?,?,'agent',?,?)
 ON CONFLICT(workstream_id) DO UPDATE SET version=excluded.version,updated_at=excluded.updated_at,updated_by='agent',milestones=excluded.milestones,waves=excluded.waves`, p.WorkstreamID, p.Version+1, s.now().UnixMilli(), string(milestones), string(waves))
	return err
}

func (s *Store) workstreamWakePlanApplyIn(ctx context.Context, tx *sql.Tx, w Workstream, event WakeEvent, item *WakeItem) error {
	p, err := planIn(ctx, tx, w.ID)
	if err != nil {
		return err
	}
	target := workstreamWakeWave(p, item.Wave)
	if item.Kind == "move" {
		from, pos := workstreamWakeTarget(p, *item)
		if target < 0 || from < 0 || from == target {
			return ErrConflict
		}
		moved := p.Waves[from].Items[pos]
		item.Undo = workstreamWakeUndo{Wave: p.Waves[from].N, Position: pos, PlanItem: moved}
		p.Waves[from].Items = append(p.Waves[from].Items[:pos], p.Waves[from].Items[pos+1:]...)
		p.Waves[target].Items = append(p.Waves[target].Items, moved)
	} else {
		if len(p.Waves) == 0 && (item.Wave == 0 || item.Wave == 1) {
			p.Waves = append(p.Waves, Wave{N: 1, Items: []PlanItem{}})
			target = 0
			item.Undo.CreatedWave = true
		}
		if target < 0 {
			return ErrConflict
		}
		id, err := s.newID()
		if err != nil {
			return err
		}
		item.DraftID = "draft_" + id
		item.Wave = p.Waves[target].N
		v := PlanItem{Kind: "draft", DraftID: item.DraftID, Title: item.Proposal.Title, Repo: item.Proposal.Repo, Text: item.Proposal.Text, Size: item.Proposal.Size, Source: workstreamWakeSource(w, event)}
		if item.BudgetUSD != nil {
			v.BudgetUSD = *item.BudgetUSD
		}
		item.Undo.PlanItem = v
		p.Waves[target].Items = append(p.Waves[target].Items, v)
	}
	return s.workstreamWakeSavePlanIn(ctx, tx, p)
}

func (s *Store) workstreamWakePageApplyIn(ctx context.Context, tx *sql.Tx, w Workstream, event WakeEvent, item *WakeItem) error {
	source := workstreamWakeSource(w, event)
	if source.Label == "" {
		source.Label = "Workstream wake"
	}
	who := Attribution{By: "agent", From: source}
	doc := pagedoc.FromMarkdown(item.Proposal.Markdown)
	if item.PageID == "" {
		ask, raw, err := workstreamPreparePageCreate(PageCreate{WorkstreamID: w.ID, Title: item.Proposal.Title, Owner: "agent", ReadByJobs: ReadByJobsDefault(item.Proposal.Title), Doc: doc, Attribution: who})
		if err != nil {
			return err
		}
		page, err := s.workstreamCreatePageIn(ctx, tx, ask, raw)
		if err != nil {
			return err
		}
		item.PageID = page.ID
		item.Undo = workstreamWakeUndo{Created: true, Version: page.Version}
		return nil
	}
	page, _, err := livePageIn(ctx, tx, item.PageID)
	if errors.Is(err, ErrNotFound) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if page.WorkstreamID != w.ID || page.Owner != "agent" {
		return ErrConflict
	}
	raw, err := sourceJSON(source)
	if err != nil {
		return err
	}
	if err := workstreamSavePageIn(ctx, tx, page, page.Title, doc, "agent", raw, s.now().UnixMilli()); err != nil {
		return err
	}
	item.Proposal.Title = page.Title
	item.Undo = workstreamWakeUndo{Version: page.Version + 1, PreviousVersion: page.Version}
	return nil
}

func (s *Store) workstreamWakeRecordApplyIn(ctx context.Context, tx *sql.Tx, w Workstream, event WakeEvent, item *WakeItem) error {
	if err := workstreamRecordLimitIn(ctx, tx, w.ID); err != nil {
		return err
	}
	source, err := sourceJSON(workstreamWakeSource(w, event))
	if err != nil {
		return err
	}
	id, err := s.newID()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO workstream_records(id,workstream_id,kind,text,state,actor,source,created_at,decided_at) VALUES (?,?,?,?,'kept','agent',?,?,?)", id, w.ID, item.Kind, item.Proposal.Text, source, s.now().UnixMilli(), s.now().UnixMilli())
	if err == nil {
		item.RecordID = id
	}
	return err
}

func (s *Store) workstreamWakeUndoIn(ctx context.Context, tx *sql.Tx, w Workstream, event WakeEvent, item *WakeItem) (ChangedPart, error) {
	var part ChangedPart
	var err error
	switch item.Kind {
	case "move", "draft":
		part = PartPlan
		err = s.workstreamWakePlanUndoIn(ctx, tx, w, item)
	case "page":
		part = PartPages
		err = s.workstreamWakePageUndoIn(ctx, tx, w, event, item)
	case "decision", "question":
		part = PartRecords
		r, readErr := recordIn(ctx, tx, item.RecordID)
		if errors.Is(readErr, ErrNotFound) {
			return "", ErrConflict
		}
		if readErr != nil {
			return "", readErr
		}
		if r.WorkstreamID != w.ID || r.State != "kept" || r.SupersededBy != "" {
			return "", ErrConflict
		}
		_, err = tx.ExecContext(ctx, "UPDATE workstream_records SET state='dropped',decided_at=? WHERE id=?", s.now().UnixMilli(), r.ID)
	default:
		return "", ErrConflict
	}
	if err != nil {
		return "", err
	}
	item.State = "undone"
	item.Note = ""
	item.UndoneAt = instant(s.now().UnixMilli())
	item.Text = workstreamWakeItemText(*item)
	return part, nil
}
func (s *Store) workstreamWakePlanUndoIn(ctx context.Context, tx *sql.Tx, w Workstream, item *WakeItem) error {
	p, err := planIn(ctx, tx, w.ID)
	if err != nil {
		return err
	}
	from, pos := workstreamWakeTarget(p, *item)
	if from < 0 {
		return ErrConflict
	}
	if item.Kind == "move" && p.Waves[from].N != item.Wave {
		return ErrConflict
	}
	target := -1
	if item.Kind == "move" {
		target = workstreamWakeWave(p, item.Undo.Wave)
		if target < 0 {
			return ErrConflict
		}
	}
	v := p.Waves[from].Items[pos]
	p.Waves[from].Items = append(p.Waves[from].Items[:pos], p.Waves[from].Items[pos+1:]...)
	if item.Kind == "move" {
		items := p.Waves[target].Items
		at := min(item.Undo.Position, len(items))
		items = append(items, PlanItem{})
		copy(items[at+1:], items[at:])
		items[at] = v
		p.Waves[target].Items = items
	} else if item.Undo.CreatedWave && len(p.Waves[from].Items) == 0 {
		p.Waves = append(p.Waves[:from], p.Waves[from+1:]...)
	}
	return s.workstreamWakeSavePlanIn(ctx, tx, p)
}
func (s *Store) workstreamWakePageUndoIn(ctx context.Context, tx *sql.Tx, w Workstream, event WakeEvent, item *WakeItem) error {
	p, _, err := livePageIn(ctx, tx, item.PageID)
	if errors.Is(err, ErrNotFound) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if p.WorkstreamID != w.ID || p.Owner != "agent" || p.Version != item.Undo.Version {
		return ErrConflict
	}
	if item.Undo.Created {
		// A newly attached child is a later change; never delete another person's page.
		var children bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM workstream_pages WHERE parent_id=? AND deleted_at IS NULL)", p.ID).Scan(&children); err != nil {
			return err
		}
		if children {
			return ErrConflict
		}
		if _, err := tx.ExecContext(ctx, "UPDATE workstream_pages SET deleted_at=?,deleted_root=? WHERE id=?", s.now().UnixMilli(), p.ID, p.ID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE workstream_pages SET position=position-1 WHERE workstream_id=? AND parent_id IS ? AND position>? AND deleted_at IS NULL", w.ID, nullString(p.ParentID), p.Position)
		return err
	}
	var title, doc string
	if err := tx.QueryRowContext(ctx, "SELECT title,doc FROM workstream_page_versions WHERE page_id=? AND version=?", p.ID, item.Undo.PreviousVersion).Scan(&title, &doc); err != nil {
		return found(err)
	}
	source := workstreamWakeSource(w, event)
	if source.Label == "" {
		source.Label = "Workstream wake"
	}
	raw, err := sourceJSON(source)
	if err != nil {
		return err
	}
	return workstreamSavePageIn(ctx, tx, p, title, json.RawMessage(doc), "agent", raw, s.now().UnixMilli())
}

// ActOnWake applies one item atomically or all eligible items best effort.
// Undo walks backwards so dependent changes on the same target unwind in order.
func (s *Store) ActOnWake(ctx context.Context, id, itemID, action string) (WakeRun, Change, error) {
	var out WakeRun
	var change Change
	if action != "apply" && action != "undo" && action != "not_now" {
		return out, change, invalid("unknown wake action")
	}
	err := s.workspace.Write(ctx, func(tx *sql.Tx) error {
		run, err := workstreamWakeIn(ctx, tx, id)
		if err != nil {
			return err
		}
		w, err := getIn(ctx, tx, run.WorkstreamID)
		if err != nil {
			return err
		}
		if itemID != "" {
			found := false
			for _, item := range run.Items {
				found = found || item.ID == itemID
			}
			if !found {
				return ErrNotFound
			}
		}
		if action != "not_now" && w.Stage == "done" {
			return ErrConflict
		}
		parts := map[ChangedPart]bool{}
		changed := false
		for step := range run.Items {
			pos := step
			if action == "undo" {
				pos = len(run.Items) - 1 - step
			}
			item := run.Items[pos]
			if itemID != "" && item.ID != itemID {
				continue
			}
			source := "proposed"
			if action == "undo" {
				source = "applied"
			}
			if item.State != source {
				continue
			}
			before := item
			if action == "not_now" {
				item.State = "dismissed"
				run.DismissedAt = instant(s.now().UnixMilli())
			} else {
				var part ChangedPart
				if action == "apply" {
					part, err = s.workstreamWakeApplyIn(ctx, tx, w, run.Event, &item)
				} else {
					part, err = s.workstreamWakeUndoIn(ctx, tx, w, run.Event, &item)
				}
				if errors.Is(err, ErrConflict) && itemID == "" {
					item = before
					item.Note = "Its target is gone; not applied."
					if action == "undo" {
						item.Note = "Changed since; not undone."
					}
					err = nil
				} else if err != nil {
					return err
				} else {
					parts[part] = true
				}
			}
			if item == before {
				continue
			}
			if err := workstreamSaveWakeItemIn(ctx, tx, id, pos, item); err != nil {
				return err
			}
			run.Items[pos] = item
			changed = true
		}
		if changed {
			if _, err := tx.ExecContext(ctx, "UPDATE workstream_wakes SET dismissed_at=? WHERE id=?", workstreamWakeTime(run.DismissedAt), id); err != nil {
				return err
			}
			if err := touch(ctx, tx, w.ID, s.now().UnixMilli()); err != nil {
				return err
			}
			change = workstreamWakeChange(w.ID, parts)
		}
		out = run
		return nil
	})
	if err != nil {
		return WakeRun{}, Change{}, err
	}
	return out, change, nil
}
